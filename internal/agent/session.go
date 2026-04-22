package agent

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// AnalysisSession 跨 chat 持久化的分析上下文
type AnalysisSession struct {
	ID              string
	PackageName     string
	Findings        []Finding
	AnalyzedClasses map[string]bool
	DataFlows       []DataFlow
	ExportedComps   []string
	ChatHistory     []ChatTurn // 对话历史（用户问题 + LLM 结论摘要）
	CreatedAt       time.Time
	LastActiveAt    time.Time
}

// ChatTurn 一轮对话记录
type ChatTurn struct {
	UserQuery string
	Summary   string // LLM 输出的关键结论摘要（非全文）
	Timestamp time.Time
}

// Finding 已确认的安全发现
type Finding struct {
	Type        string // vulnerability | crypto | component | info
	Severity    string // critical | high | medium | low | info
	Description string
	Evidence    string
}

// DataFlow 数据流链路
type DataFlow struct {
	Source    string
	Sink     string
	Class    string
	Risk     string
}

var (
	sessions  = make(map[string]*AnalysisSession)
	sessionMu sync.RWMutex
)

const sessionExpiry = 2 * time.Hour

// GetOrCreateSession 获取或创建会话
func GetOrCreateSession(id string) *AnalysisSession {
	if id == "" {
		id = fmt.Sprintf("default-%d", time.Now().UnixNano())
	}
	sessionMu.Lock()
	defer sessionMu.Unlock()

	// 清理过期会话
	cleanExpiredLocked()

	if s, ok := sessions[id]; ok {
		s.LastActiveAt = time.Now()
		return s
	}
	s := &AnalysisSession{
		ID:              id,
		AnalyzedClasses: make(map[string]bool),
		CreatedAt:       time.Now(),
		LastActiveAt:    time.Now(),
	}
	sessions[id] = s
	return s
}

// ResetSession 重置会话（新 APK 或用户点击新会话）
func ResetSession(id string) {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	delete(sessions, id)
}

// AddChatTurn 添加一轮对话记录到会话
func AddChatTurn(s *AnalysisSession, userQuery, llmOutput string) {
	if s == nil || userQuery == "" {
		return
	}
	sessionMu.Lock()
	defer sessionMu.Unlock()

	summary := extractChatSummary(llmOutput)
	s.ChatHistory = append(s.ChatHistory, ChatTurn{
		UserQuery: userQuery,
		Summary:   summary,
		Timestamp: time.Now(),
	})
	// 只保留最近 10 轮对话
	if len(s.ChatHistory) > 10 {
		s.ChatHistory = s.ChatHistory[len(s.ChatHistory)-10:]
	}
}

// extractChatSummary 从 LLM 输出中提取结论摘要（最后的结论部分，或截断前 500 字符）
func extractChatSummary(output string) string {
	if output == "" {
		return "（无输出）"
	}

	// 尝试提取 "分析结论" / "安全发现" / "## 结论" 段落
	for _, marker := range []string{"## 分析结论", "## 结论", "## 安全发现", "## 分析报告", "**分析结论**"} {
		if idx := strings.LastIndex(output, marker); idx >= 0 {
			tail := output[idx:]
			if len(tail) > 800 {
				tail = tail[:800] + "..."
			}
			return tail
		}
	}

	// 无结论标记：取最后 500 字符（最新输出通常是结论）
	if len(output) > 500 {
		return "..." + output[len(output)-500:]
	}
	return output
}

func cleanExpiredLocked() {
	now := time.Now()
	for id, s := range sessions {
		if now.Sub(s.LastActiveAt) > sessionExpiry {
			delete(sessions, id)
		}
	}
}

// ──────────────────────────────────────────
// Session 更新：从 LLM 输出中提取信息
// ──────────────────────────────────────────

// 安全发现关键词匹配
var findingPatterns = []struct {
	severity string
	re       *regexp.Regexp
}{
	{"critical", regexp.MustCompile(`(?i)\[?(严重|critical)\]?[：:]?\s*(.{10,120})`)},
	{"high", regexp.MustCompile(`(?i)\[?(高危|high)\]?[：:]?\s*(.{10,120})`)},
	{"medium", regexp.MustCompile(`(?i)\[?(中危|medium)\]?[：:]?\s*(.{10,120})`)},
	{"low", regexp.MustCompile(`(?i)\[?(低危|low)\]?[：:]?\s*(.{10,120})`)},
}

// exported 组件提取
var exportedRe = regexp.MustCompile(`(?i)exported[=:\s]+true[^"]*?"?([a-zA-Z][\w]*(?:\.[\w]+){2,})`)

// 数据流模式
var dataFlowRe = regexp.MustCompile(`\[数据流\]\s*(.+?)\s*→\s*(.+?)(?:\s*→\s*(.+?))?(?:\s*\[(.+?)\])?`)

// UpdateSessionFromOutput 从 LLM 累积输出中提取信息更新会话
func UpdateSessionFromOutput(s *AnalysisSession, output string) {
	if s == nil || output == "" {
		return
	}
	sessionMu.Lock()
	defer sessionMu.Unlock()

	// 提取全限定类名 → AnalyzedClasses
	classNames := classNameRe.FindAllString(output, 100)
	for _, cn := range classNames {
		if len(cn) >= 10 {
			s.AnalyzedClasses[cn] = true
		}
	}

	// 提取 exported 组件
	for _, match := range exportedRe.FindAllStringSubmatch(output, 50) {
		if len(match) >= 2 {
			comp := match[1]
			if !containsStr(s.ExportedComps, comp) {
				s.ExportedComps = append(s.ExportedComps, comp)
			}
		}
	}

	// 提取安全发现
	for _, fp := range findingPatterns {
		for _, match := range fp.re.FindAllStringSubmatch(output, 10) {
			if len(match) >= 3 {
				desc := strings.TrimSpace(match[2])
				if !hasFinding(s.Findings, desc) {
					s.Findings = append(s.Findings, Finding{
						Type:     "vulnerability",
						Severity: fp.severity,
						Description: desc,
					})
				}
			}
		}
	}

	// 提取数据流
	for _, match := range dataFlowRe.FindAllStringSubmatch(output, 20) {
		if len(match) >= 3 {
			df := DataFlow{Source: match[1], Sink: match[2]}
			if len(match) >= 4 && match[3] != "" {
				df.Sink = match[3]
			}
			if len(match) >= 5 {
				df.Class = match[4]
			}
			if !hasDataFlow(s.DataFlows, df) {
				s.DataFlows = append(s.DataFlows, df)
			}
		}
	}

	// 限制大小防止无限增长
	if len(s.Findings) > 50 {
		s.Findings = s.Findings[len(s.Findings)-50:]
	}
	if len(s.DataFlows) > 30 {
		s.DataFlows = s.DataFlows[len(s.DataFlows)-30:]
	}
	if len(s.ExportedComps) > 100 {
		s.ExportedComps = s.ExportedComps[:100]
	}
}

// ──────────────────────────────────────────
// 构建 Prompt 注入内容
// ──────────────────────────────────────────

// BuildSessionContext 构建会话记忆 prompt 段
func BuildSessionContext(s *AnalysisSession) string {
	if s == nil {
		return ""
	}
	sessionMu.RLock()
	defer sessionMu.RUnlock()

	// 无任何记忆时不注入
	if len(s.AnalyzedClasses) == 0 && len(s.Findings) == 0 && len(s.ExportedComps) == 0 && len(s.DataFlows) == 0 && len(s.ChatHistory) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## 会话记忆（前次分析已确认的信息，无需重复获取）\n\n")

	if len(s.AnalyzedClasses) > 0 {
		sb.WriteString("**已分析类**: ")
		count := 0
		for cn := range s.AnalyzedClasses {
			if count > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(cn)
			count++
			if count >= 30 {
				sb.WriteString(fmt.Sprintf(" ...等共 %d 个", len(s.AnalyzedClasses)))
				break
			}
		}
		sb.WriteString("\n\n")
	}

	if len(s.ExportedComps) > 0 {
		sb.WriteString("**已确认 exported 组件**: ")
		sb.WriteString(strings.Join(s.ExportedComps, ", "))
		sb.WriteString("\n\n")
	}

	if len(s.Findings) > 0 {
		sb.WriteString("**已发现安全风险**:\n")
		for _, f := range s.Findings {
			sb.WriteString(fmt.Sprintf("- [%s] %s\n", f.Severity, f.Description))
		}
		sb.WriteString("\n")
	}

	if len(s.DataFlows) > 0 {
		sb.WriteString("**数据流链路**:\n")
		for _, df := range s.DataFlows {
			line := fmt.Sprintf("- %s → %s", df.Source, df.Sink)
			if df.Class != "" {
				line += fmt.Sprintf(" [%s]", df.Class)
			}
			sb.WriteString(line + "\n")
		}
		sb.WriteString("\n")
	}

	if len(s.ChatHistory) > 0 {
		sb.WriteString("**对话历史**（本会话已完成的问答摘要）:\n")
		for i, turn := range s.ChatHistory {
			sb.WriteString(fmt.Sprintf("%d. 用户: %s\n   结论: %s\n", i+1, turn.UserQuery, turn.Summary))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// ──────────────────────────────────────────
// 辅助
// ──────────────────────────────────────────

func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

func hasFinding(findings []Finding, desc string) bool {
	for _, f := range findings {
		if f.Description == desc {
			return true
		}
	}
	return false
}

func hasDataFlow(flows []DataFlow, df DataFlow) bool {
	for _, f := range flows {
		if f.Source == df.Source && f.Sink == df.Sink {
			return true
		}
	}
	return false
}
