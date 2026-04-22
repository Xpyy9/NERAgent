package web

import (
	"NERAgent/internal/agent"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/gin-gonic/gin"
)

// 全局 cancel 管理，供 /cancel 端点取消当前运行中的分析
var (
	activeCancel   context.CancelFunc
	activeCancelMu sync.Mutex
)

// CancelHandler 中止当前正在运行的分析
func CancelHandler(c *gin.Context) {
	activeCancelMu.Lock()
	if activeCancel != nil {
		activeCancel()
		activeCancel = nil
	}
	activeCancelMu.Unlock()
	c.JSON(200, gin.H{"status": "cancelled"})
}

// ChatHandler 处理聊天界面用户输入，以 SSE 流式返回分析结果（含通用重试机制）
func ChatHandler(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")

	ctx, cancel := context.WithCancel(c.Request.Context())
	activeCancelMu.Lock()
	activeCancel = cancel
	activeCancelMu.Unlock()
	defer func() {
		cancel()
		activeCancelMu.Lock()
		activeCancel = nil
		activeCancelMu.Unlock()
	}()

	runner, err := agent.GetRunner()
	if err != nil {
		c.SSEvent("error", fmt.Sprintf("Get Runner Failed: %v", err))
		c.Writer.Flush()
		return
	}

	var req chatRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Message == "" {
		c.SSEvent("error", "Invalid or empty input")
		c.Writer.Flush()
		return
	}

	// 获取或创建会话
	session := agent.GetOrCreateSession(req.SessionID)
	agent.SetActiveSession(session)

	const maxRetries = 3
	const maxRounds = 5 // 最多 5 轮迭代（每轮 40 次 PER 迭代）
	const retryDelay = 10 * time.Second
	const cooldownDelay = 2 * time.Minute

	message := req.Message
	round := 1

	for attempt := 0; ; attempt++ {
		outcome := streamQueryEvents(c, ctx, runner, message)

		// 每次流结束都更新会话记忆
		if outcome.AccumulatedContent != "" {
			agent.UpdateSessionFromOutput(session, outcome.AccumulatedContent)
		}

		if outcome.Result == streamOK {
			agent.MarkSessionInitDone()
			agent.AddChatTurn(session, req.Message, outcome.AccumulatedContent)
			break
		}

		// exceeds max iterations 走轮次续传，不走 retry（retry 是给临时网络/模型错误用的）
		if isExceedsMaxIter(outcome.ErrorMsg) {
			if outcome.AccumulatedContent != "" && round < maxRounds {
				summary := buildRoundSummary(outcome.AccumulatedContent)
				c.SSEvent("round_transition", gin.H{
					"round":   round,
					"message": fmt.Sprintf("第 %d 轮迭代次数使用完毕，携带已有分析成果开启第 %d 轮分析", round, round+1),
					"summary": summary,
				})
				c.Writer.Flush()

				// 构造携带上轮成果的新 prompt，包含轮次感知和紧迫度提示
				message = fmt.Sprintf("## 上轮分析成果（第 %d/%d 轮已完成）\n\n以下是上轮分析的关键成果摘要，请基于这些成果继续分析。\n\n**重要规则**：\n- 不要重复已完成的分析步骤\n- 直接从上轮未完成的部分继续\n- 这是第 %d/%d 轮，请高效推进，优先完成核心目标\n%s\n---上轮分析成果---\n%s\n\n## 原始分析目标\n%s",
					round, maxRounds, round+1, maxRounds,
					conditionalUrgency(round+1, maxRounds),
					summary, req.Message)
				round++
				attempt = 0 // 重置重试计数，这是新一轮不是重试
				continue
			}
			// 所有轮次用完：输出已有成果作为最终报告，不再 retry
			if outcome.AccumulatedContent != "" {
				fallback := buildFinalFallback(outcome.AccumulatedContent)
				c.SSEvent("message", gin.H{"role": "assistant", "content": fallback})
				c.Writer.Flush()
				agent.AddChatTurn(session, req.Message, outcome.AccumulatedContent)
			} else {
				c.SSEvent("error", "分析迭代轮次全部用完且未产出有效结论。请尝试缩小分析范围后重试。")
				c.Writer.Flush()
			}
			break
		}

		// Jadx 连接类错误时重置初始化状态，下次查询重新检查
		if strings.Contains(outcome.ErrorMsg, "connection refused") ||
			strings.Contains(outcome.ErrorMsg, "connection reset") {
			agent.ResetSessionInit()
		}

		// 判断是否可重试（仅针对非 exceeds max iterations 的临时错误）
		if attempt < maxRetries {
			var msg string
			if outcome.Result == streamRateLimit {
				msg = fmt.Sprintf("后端模型负载饱和，正在进行第 %d/%d 次重试...", attempt+1, maxRetries)
			} else {
				msg = fmt.Sprintf("分析过程遇到异常（%s），正在进行第 %d/%d 次重试...", truncateErr(outcome.ErrorMsg, 80), attempt+1, maxRetries)
			}
			c.SSEvent("retry", retryEvent{Attempt: attempt + 1, Message: msg, WaitSec: int(retryDelay.Seconds())})
			c.Writer.Flush()
			if !waitWithContext(ctx, retryDelay) {
				break
			}
			continue
		}

		if attempt == maxRetries {
			var msg string
			if outcome.Result == streamRateLimit {
				msg = "后端模型持续饱和，等待 2 分钟后进行最后一次尝试..."
			} else {
				msg = "分析持续异常，等待 2 分钟后进行最后一次尝试..."
			}
			c.SSEvent("retry", retryEvent{
				Attempt: maxRetries + 1,
				Message: msg,
				WaitSec: int(cooldownDelay.Seconds()),
			})
			c.Writer.Flush()
			if !waitWithContext(ctx, cooldownDelay) {
				break
			}
			continue
		}

		// attempt > maxRetries: 最终失败
		if outcome.Result == streamRateLimit {
			c.SSEvent("error", "后端模型持续饱和，本次分析失败。请稍后重试。")
		} else {
			c.SSEvent("error", fmt.Sprintf("分析多次重试均失败：%s", truncateErr(outcome.ErrorMsg, 120)))
		}
		c.Writer.Flush()
		break
	}

	c.SSEvent("done", "finish")
	c.Writer.Flush()
}

// buildRoundSummary 智能压缩上轮分析成果，保留首部（背景）和尾部（最新发现）
func buildRoundSummary(accumulated string) string {
	const maxLen = 20000
	if len(accumulated) <= maxLen {
		return accumulated
	}
	// 首尾各保留一半，避免丢失最新分析结论
	half := maxLen / 2
	return accumulated[:half] + "\n\n...[中间部分已省略，保留首尾关键信息]...\n\n" + accumulated[len(accumulated)-half:]
}

// conditionalUrgency 根据当前轮次生成紧迫度提示
func conditionalUrgency(currentRound, maxRounds int) string {
	if currentRound >= maxRounds {
		return "- **这是最后一轮**，必须在本轮结束前输出完整的分析结论，部分结论优于无结论\n"
	}
	if currentRound >= maxRounds-1 {
		return "- 剩余轮次有限，请聚焦最高优先级目标\n"
	}
	return ""
}

// buildFinalFallback 所有轮次耗尽时，将已有成果包装为最终输出
func buildFinalFallback(accumulated string) string {
	const maxLen = 20000
	content := accumulated
	if len(content) > maxLen {
		half := maxLen / 2
		content = content[:half] + "\n\n...[中间部分已省略]...\n\n" + content[len(content)-half:]
	}
	return fmt.Sprintf("## 分析报告（因迭代轮次用尽，以下为已获取的分析成果）\n\n%s\n\n> 注：分析因迭代次数限制未能完全完成，以上为已确认的分析结论。", content)
}

// isExceedsMaxIter 判断是否为迭代次数用完的错误
func isExceedsMaxIter(errMsg string) bool {
	return strings.Contains(errMsg, "exceeds max iterations")
}

// isFatalStreamError 判断错误是否为致命错误（需要整体重试），而非流内可恢复错误
func isFatalStreamError(errMsg string) bool {
	fatalPatterns := []string{
		"exceeds max iterations",
		"context deadline exceeded",
		"connection refused",
		"connection reset",
		"EOF",
		"broken pipe",
		"no tool call",
		"unmarshal plan error",
		"status code: 400",
		"status code: 500",
		"status code: 502",
		"status code: 503",
		"模型服务调用失败",
		"model service",
		"Bad Request",
		"Internal Server Error",
		"Service Unavailable",
	}
	for _, p := range fatalPatterns {
		if strings.Contains(errMsg, p) {
			return true
		}
	}
	return false
}

// truncateErr 截断错误信息到指定长度
func truncateErr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// streamQueryEvents 执行一次查询并流式发送 SSE 事件，返回终止原因
func streamQueryEvents(c *gin.Context, ctx context.Context, runner *adk.Runner, message string) streamOutcome {
	startTime := time.Now()
	var cumUsage usageStats
	var currentStage string
	var stageStart time.Time
	var lastFatalErr string
	var lastStreamErr string // 记录任何流错误，作为兜底
	var stageTokenSnapshot usageStats // 阶段开始时的 token 快照
	var accumulated strings.Builder   // 累积 LLM 输出，用于 exceeds max iterations 续传

	iter := runner.Query(ctx, message)
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			errMsg := event.Err.Error()
			if strings.Contains(errMsg, "429") {
				log.Printf("[-] 429 rate limit detected, will retry")
				return streamOutcome{Result: streamRateLimit}
			}
			log.Printf("[-] Stream error: %v", event.Err)
			lastStreamErr = errMsg
			c.SSEvent("stream_error", streamErrorEvent{
				Error:     errMsg,
				AgentName: event.AgentName,
			})
			c.Writer.Flush()
			if isFatalStreamError(errMsg) {
				lastFatalErr = errMsg
			}
			continue
		}

		agentName := event.AgentName
		if isValidStage(agentName) && agentName != currentStage {
			now := time.Now()
			if currentStage != "" {
				c.SSEvent("stage", stageEvent{
					Name:             currentStage,
					Model:            modelNameForStage(currentStage),
					Status:           "done",
					LatencyMs:        now.Sub(stageStart).Milliseconds(),
					PromptTokens:     cumUsage.PromptTokens - stageTokenSnapshot.PromptTokens,
					CompletionTokens: cumUsage.CompletionTokens - stageTokenSnapshot.CompletionTokens,
					TotalTokens:      cumUsage.TotalTokens - stageTokenSnapshot.TotalTokens,
					ReasoningTokens:  cumUsage.ReasoningTokens - stageTokenSnapshot.ReasoningTokens,
				})
				c.Writer.Flush()
			}
			currentStage = agentName
			stageStart = now
			stageTokenSnapshot = cumUsage // 记录新阶段起始 token 快照
			c.SSEvent("stage", stageEvent{
				Name:   currentStage,
				Model:  modelNameForStage(currentStage),
				Status: "running",
			})
			c.Writer.Flush()
		}

		if event.Output != nil {
			if msg, _, getErr := adk.GetMessage(event); getErr == nil {
				// 累积 LLM 文本输出，用于 exceeds max iterations 时续传
				if msg != nil && msg.Content != "" {
					accumulated.WriteString(msg.Content)
					accumulated.WriteString("\n")
				}
				if msg != nil && msg.ResponseMeta != nil && msg.ResponseMeta.Usage != nil {
					u := msg.ResponseMeta.Usage
					cumUsage.PromptTokens += u.PromptTokens
					cumUsage.CompletionTokens += u.CompletionTokens
					cumUsage.TotalTokens += u.TotalTokens
					cumUsage.ReasoningTokens += u.CompletionTokensDetails.ReasoningTokens
				}
				c.SSEvent("message", msg)
				if msg != nil && msg.ResponseMeta != nil && msg.ResponseMeta.Usage != nil {
					cumUsage.ElapsedMs = time.Since(startTime).Milliseconds()
					c.SSEvent("usage", cumUsage)
				}
			} else {
				c.SSEvent("message", fmt.Sprintf("LLM 处理异常: %v", getErr))
			}
			c.Writer.Flush()
		}
	}

	if currentStage != "" {
		c.SSEvent("stage", stageEvent{
			Name:             currentStage,
			Model:            modelNameForStage(currentStage),
			Status:           "done",
			LatencyMs:        time.Since(stageStart).Milliseconds(),
			PromptTokens:     cumUsage.PromptTokens - stageTokenSnapshot.PromptTokens,
			CompletionTokens: cumUsage.CompletionTokens - stageTokenSnapshot.CompletionTokens,
			TotalTokens:      cumUsage.TotalTokens - stageTokenSnapshot.TotalTokens,
			ReasoningTokens:  cumUsage.ReasoningTokens - stageTokenSnapshot.ReasoningTokens,
		})
		c.Writer.Flush()
	}
	cumUsage.ElapsedMs = time.Since(startTime).Milliseconds()
	c.SSEvent("usage", cumUsage)
	c.Writer.Flush()

	accContent := accumulated.String()
	if lastFatalErr != "" {
		return streamOutcome{Result: streamFatalError, ErrorMsg: lastFatalErr, AccumulatedContent: accContent}
	}
	// 兜底：流中有错误但未匹配 fatal 模式，仍视为可重试错误
	if lastStreamErr != "" {
		return streamOutcome{Result: streamFatalError, ErrorMsg: lastStreamErr, AccumulatedContent: accContent}
	}
	return streamOutcome{Result: streamOK}
}

// waitWithContext 等待指定时长，若 context 取消则提前返回 false
func waitWithContext(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

// isValidStage 过滤有效的 PER 阶段名，忽略容器 agent
func isValidStage(name string) bool {
	return name == "planner" || name == "executor" || name == "replanner"
}

// modelNameForStage 返回各阶段对应的模型名称
func modelNameForStage(stage string) string {
	switch stage {
	case "planner":
		return agent.PlannerModelName
	case "executor":
		return agent.ExecutorModelName
	case "replanner":
		return agent.ReplannerModelName
	default:
		return stage
	}
}

// JadxStatusHandler 代理 Jadx 系统状态接口，供前端轮询获取实时内存数据
func JadxStatusHandler(c *gin.Context) {
	jadxURL := os.Getenv("JADX_BASE_URL")
	if jadxURL == "" {
		jadxURL = "http://localhost:13997"
	}

	resp, err := http.Get(jadxURL + "/systemManager?action=systemStatus")
	if err != nil {
		c.JSON(502, gin.H{"error": fmt.Sprintf("Failed to reach Jadx: %v", err)})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(502, gin.H{"error": "Failed to read Jadx response"})
		return
	}

	c.Data(resp.StatusCode, "application/json", body)
}

// ModelsGetHandler 返回可用模型列表和当前三阶段配置
func ModelsGetHandler(c *gin.Context) {
	c.JSON(200, gin.H{
		"available": agent.GetAvailableModels(),
		"current":   agent.GetModelConfig(),
	})
}

// ModelsPutHandler 切换指定阶段的模型
func ModelsPutHandler(c *gin.Context) {
	var req struct {
		Role  string `json:"role"`
		Model string `json:"model"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Role == "" || req.Model == "" {
		c.JSON(400, gin.H{"error": "missing role or model"})
		return
	}
	if err := agent.SetModelAndRebuild(req.Role, req.Model); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "ok", "current": agent.GetModelConfig()})
}
