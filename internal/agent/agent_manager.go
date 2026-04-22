package agent

import (
	"NERAgent/internal/tools"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/planexecute"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/prompt"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/joho/godotenv"
)

//go:embed planPrompt.md
var PlanPrompt string

//go:embed executePrompt.md
var ExecutePrompt string

//go:embed replanPrompt.md
var ReplanPrompt string

var plannerPrompt = prompt.FromMessages(schema.FString,
	schema.SystemMessage(PlanPrompt),
	schema.UserMessage(`## 分析目标
{input}

{session_context}

{apk_profile}

{strategy_tables}

请根据以上目标，生成一个策略性的逆向分析计划。每个步骤必须是一条自包含的自然语言指令，包含完整的工具名、action 和参数信息，使执行器无需额外上下文即可独立完成。`))

var executorPrompt = prompt.FromMessages(schema.FString,
	schema.SystemMessage(ExecutePrompt),
	schema.UserMessage(`## 分析目标
{input}

## 当前执行计划
{plan}

## 已完成步骤及结果
以下是前序步骤的执行输出，当前步骤所需的参数（如全限定类名、方法签名等）必须从这些结果中精确提取：
{executed_steps}

## 当前需要执行的步骤
{step}

请严格执行以上步骤，调用相应工具获取数据并进行深入分析。`))

var replannerPrompt = prompt.FromMessages(schema.FString,
	schema.SystemMessage(ReplanPrompt),
	schema.UserMessage(`## 分析目标
{input}

{session_context}

## 迭代进度
当前第 {iteration_current} 次迭代（上限 {iteration_max} 次）。已完成 {steps_done} 个步骤。

## 原始计划
{plan}

## 已完成步骤及结果
{executed_steps}

请根据以上进展评估任务状态，然后选择以下操作之一：
- 若目标已完成，调用 '{respond_tool}' 输出最终分析结论
- 若仍需继续，调用 '{plan_tool}' 输出仅包含剩余步骤的新计划`))

// ModelConfig 三阶段模型配置
type ModelConfig struct {
	Planner   string `json:"planner"`
	Executor  string `json:"executor"`
	Replanner string `json:"replanner"`
}

var (
	mu              sync.RWMutex
	globalRunner    *adk.Runner
	modelConfig     ModelConfig
	availableModels []string
	sessionInitDone bool
)

// 当前活跃会话，供 GenInputFn 闭包读取
var (
	activeSession   *AnalysisSession
	activeSessionMu sync.RWMutex
)

// SetActiveSession 设置当前活跃会话（handler 在 query 前调用）
func SetActiveSession(s *AnalysisSession) {
	activeSessionMu.Lock()
	activeSession = s
	activeSessionMu.Unlock()
}

func getActiveSession() *AnalysisSession {
	activeSessionMu.RLock()
	defer activeSessionMu.RUnlock()
	return activeSession
}

// 导出的模型名称，供 handler 层 SSE stage 事件读取
var (
	PlannerModelName   string
	ExecutorModelName  string
	ReplannerModelName string
)

func MarkSessionInitDone() { sessionInitDone = true }
func ResetSessionInit()    { sessionInitDone = false }

// GetModelConfig 返回当前三阶段模型配置
func GetModelConfig() ModelConfig {
	mu.RLock()
	defer mu.RUnlock()
	return modelConfig
}

// GetAvailableModels 返回可选模型列表
func GetAvailableModels() []string {
	mu.RLock()
	defer mu.RUnlock()
	return availableModels
}

// Init 初始化 PER Agent 并构建全局 Runner
func Init() error {
	if err := godotenv.Load(); err != nil {
		return fmt.Errorf("未找到 .env 文件: %v", err)
	}
	log.Printf("[+]===PER-UniAgent Init===")

	// 解析可用模型列表
	availableModels = parseAvailableModels()

	// 从 env 读取初始模型
	modelConfig = ModelConfig{
		Planner:   envOrDefault("GPT_MODEL", "GPT-4.1"),
		Executor:  envOrDefault("GLM_MODEL", "GLM-5"),
		Replanner: envOrDefault("GPT_MODEL", "GPT-4.1"),
	}

	return buildRunner()
}

// SetModelAndRebuild 切换指定阶段模型并重建 agent pipeline
func SetModelAndRebuild(role, modelName string) error {
	mu.Lock()
	defer mu.Unlock()

	switch role {
	case "planner":
		modelConfig.Planner = modelName
	case "executor":
		modelConfig.Executor = modelName
	case "replanner":
		modelConfig.Replanner = modelName
	default:
		return fmt.Errorf("unknown role: %s", role)
	}

	log.Printf("[+] Switching %s model to %s, rebuilding agent...", role, modelName)
	return buildRunnerLocked()
}

// GetRunner 获取已初始化的全局 Runner（读锁保护）
func GetRunner() (*adk.Runner, error) {
	mu.RLock()
	defer mu.RUnlock()
	if globalRunner == nil {
		return nil, fmt.Errorf("runner has not been initialized yet")
	}
	return globalRunner, nil
}

// ──────────────────────────────────────────
// 内部构建函数
// ──────────────────────────────────────────

// buildRunner 外部调用入口（自行加写锁）
func buildRunner() error {
	mu.Lock()
	defer mu.Unlock()
	return buildRunnerLocked()
}

// buildRunnerLocked 在已持有写锁的情况下重建整个 pipeline
func buildRunnerLocked() error {
	ctx := context.Background()

	PlannerModelName = modelConfig.Planner
	ExecutorModelName = modelConfig.Executor
	ReplannerModelName = modelConfig.Replanner

	plannerModel, err := newChatModel(ctx, modelConfig.Planner)
	if err != nil {
		return fmt.Errorf("init planner model: %v", err)
	}
	replannerModel, err := newChatModel(ctx, modelConfig.Replanner)
	if err != nil {
		return fmt.Errorf("init replanner model: %v", err)
	}
	executorModel, err := newChatModel(ctx, modelConfig.Executor)
	if err != nil {
		return fmt.Errorf("init executor model: %v", err)
	}

	planAgent, err := newPlanner(ctx, plannerModel)
	if err != nil {
		return fmt.Errorf("newPlanner: %v", err)
	}
	executeAgent, err := newExecutor(ctx, executorModel)
	if err != nil {
		return fmt.Errorf("newExecutor: %v", err)
	}
	replanAgent, err := newReplanner(ctx, replannerModel)
	if err != nil {
		return fmt.Errorf("newReplanner: %v", err)
	}

	uniAgent, err := planexecute.New(ctx, &planexecute.Config{
		Planner:       planAgent,
		Executor:      executeAgent,
		Replanner:     replanAgent,
		MaxIterations: 40,
	})
	if err != nil {
		return fmt.Errorf("planexecute.New: %v", err)
	}

	globalRunner = adk.NewRunner(ctx, adk.RunnerConfig{Agent: uniAgent})
	log.Printf("[+] Agent rebuilt: planner=%s executor=%s replanner=%s",
		modelConfig.Planner, modelConfig.Executor, modelConfig.Replanner)
	return nil
}

// newChatModel 使用共享的 base_url/api_key 创建指定模型名的 ChatModel
func newChatModel(ctx context.Context, modelName string) (model.ToolCallingChatModel, error) {
	baseURL := os.Getenv("One_BASE_URL")
	apiKey := os.Getenv("One_API_KEY")
	if baseURL == "" || apiKey == "" {
		return nil, fmt.Errorf("缺少 LLM 配置项: One_BASE_URL / One_API_KEY")
	}
	return openai.NewChatModel(ctx, &openai.ChatModelConfig{
		BaseURL:     baseURL,
		APIKey:      apiKey,
		Model:       modelName,
		Temperature: ptr(float32(0.7)),
	})
}

func newPlanner(ctx context.Context, mod model.ToolCallingChatModel) (adk.Agent, error) {
	return planexecute.NewPlanner(ctx, &planexecute.PlannerConfig{
		ToolCallingChatModel: mod,
		GenInputFn: func(ctx context.Context, userInput []adk.Message) ([]adk.Message, error) {
			input := formatInput(userInput)
			sess := getActiveSession()
			return plannerPrompt.Format(ctx, map[string]any{
				"input":           input,
				"strategy_tables": BuildStrategySection(input),
				"init_hint":       buildInitHint(),
				"session_context": BuildSessionContext(sess),
				"apk_profile":    BuildApkProfile(),
			})
		},
	})
}

func newExecutor(ctx context.Context, execModel model.ToolCallingChatModel) (adk.Agent, error) {
	jc, err := tools.NewJadxClient()
	if err != nil {
		return nil, fmt.Errorf("NewJadxClient: %v", err)
	}
	jadxTools, err := jc.BuildJadxTools()
	if err != nil {
		return nil, err
	}

	return planexecute.NewExecutor(ctx, &planexecute.ExecutorConfig{
		Model: execModel,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{Tools: jadxTools},
		},
		MaxIterations: 15,
		GenInputFn: func(ctx context.Context, in *planexecute.ExecutionContext) ([]adk.Message, error) {
			planJSON, err := in.Plan.MarshalJSON()
			if err != nil {
				return nil, err
			}
			return executorPrompt.Format(ctx, map[string]any{
				"input":          formatInput(in.UserInput),
				"plan":           string(planJSON),
				"executed_steps": formatExecutedSteps(in.ExecutedSteps),
				"step":           in.Plan.FirstStep(),
			})
		},
	})
}

func newReplanner(ctx context.Context, mod model.ToolCallingChatModel) (adk.Agent, error) {
	return planexecute.NewReplanner(ctx, &planexecute.ReplannerConfig{
		ChatModel: mod,
		GenInputFn: func(ctx context.Context, in *planexecute.ExecutionContext) ([]adk.Message, error) {
			planJSON, err := in.Plan.MarshalJSON()
			if err != nil {
				return nil, err
			}
			iterCurrent := len(in.ExecutedSteps) + 1
			stepsDone := len(in.ExecutedSteps)
			return replannerPrompt.Format(ctx, map[string]any{
				"input":             formatInput(in.UserInput),
				"plan":              string(planJSON),
				"executed_steps":    formatExecutedSteps(in.ExecutedSteps),
				"plan_tool":         planexecute.PlanToolInfo.Name,
				"respond_tool":      planexecute.RespondToolInfo.Name,
				"iteration_current": iterCurrent,
				"iteration_max":     40,
				"steps_done":        stepsDone,
				"mid_summary_hint":  buildMidSummaryHint(stepsDone),
				"session_context":   BuildSessionContext(getActiveSession()),
			})
		},
	})
}

// ──────────────────────────────────────────
// 辅助函数
// ──────────────────────────────────────────

func parseAvailableModels() []string {
	raw := os.Getenv("LLM_AVAILABLE_MODELS")
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	models := make([]string, 0, len(parts))
	for _, p := range parts {
		if m := strings.TrimSpace(p); m != "" {
			models = append(models, m)
		}
	}
	// 补充 env 中定义但不在列表中的模型
	envModels := []string{"GPT_MODEL", "GPT4o_MODEL", "CLAUDE_MODEL", "GLM_MODEL",
		"GEMINI_MODEL", "Qwen3_Coder_MODEL", "DeepSeek_MODEL", "DeepSeekR1_MODEL"}
	seen := make(map[string]bool, len(models))
	for _, m := range models {
		seen[m] = true
	}
	for _, key := range envModels {
		if v := os.Getenv(key); v != "" && !seen[v] {
			models = append(models, v)
			seen[v] = true
		}
	}
	return models
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func formatInput(in []adk.Message) string {
	if len(in) == 0 {
		return "If the user input is empty, please prompt the user to enter something."
	}
	return in[0].Content
}

func formatExecutedSteps(in []planexecute.ExecutedStep) string {
	const maxTotal = 20000

	var sb strings.Builder
	total := len(in)
	for i, s := range in {
		result := fmt.Sprintf("%v", s.Result)
		summary := extractKeyInfo(result)

		fmt.Fprintf(&sb, "## Step %d/%d: %v\nResult: %s\n\n", i+1, total, s.Step, summary)

		if sb.Len() > maxTotal {
			sb.WriteString(fmt.Sprintf("\n...[已截断，省略剩余 %d 步]\n", total-i-1))
			break
		}
	}
	return sb.String()
}

// 用于匹配全限定类名的正则
var classNameRe = regexp.MustCompile(`[a-zA-Z][\w]*(?:\.[\w]+){2,}`)

// 安全相关关键词
var securityKeywords = []string{
	"encrypt", "decrypt", "cipher", "password", "secret", "key",
	"sign", "signature", "hmac", "hash", "md5", "sha", "aes", "rsa", "des",
	"token", "auth", "credential", "certificate", "ssl", "tls",
	"inject", "xss", "sql", "vulnerability", "exploit", "bypass",
	"webview", "javascript", "native", "jni", "loadlibrary",
}

// extractKeyInfo 智能提取工具返回结果中的关键信息
func extractKeyInfo(result string) string {
	const maxSummary = 20000

	// 尝试 JSON 解析
	var jsonObj map[string]interface{}
	if err := json.Unmarshal([]byte(result), &jsonObj); err == nil {
		return extractFromJSON(jsonObj, maxSummary)
	}

	// 尝试解析 JSON 数组
	var jsonArr []interface{}
	if err := json.Unmarshal([]byte(result), &jsonArr); err == nil {
		return extractFromArray(jsonArr, maxSummary)
	}

	// 非 JSON：提取关键信息
	return extractFromText(result, maxSummary)
}

func extractFromJSON(obj map[string]interface{}, maxLen int) string {
	var sb strings.Builder

	// 错误信息优先完整保留
	if errVal, ok := obj["error"]; ok {
		sb.WriteString(fmt.Sprintf("Error: %v", errVal))
		if hint, ok := obj["hint"]; ok {
			sb.WriteString(fmt.Sprintf(" | Hint: %v", hint))
		}
		return truncateStr(sb.String(), maxLen)
	}

	// 保留关键标识字段
	for _, key := range []string{"class_name", "type", "method_name", "super_class", "strategy"} {
		if val, ok := obj[key]; ok {
			sb.WriteString(fmt.Sprintf("%s: %v | ", key, val))
		}
	}

	// 接口列表
	if impl, ok := obj["implements"]; ok {
		sb.WriteString(fmt.Sprintf("implements: %v | ", impl))
	}

	// manifest-detail: application 安全属性
	if app, ok := obj["application"].(map[string]interface{}); ok {
		sb.WriteString("application: ")
		for _, key := range []string{"name", "debuggable", "allowBackup", "networkSecurityConfig", "usesCleartextTraffic"} {
			if val, ok := app[key]; ok {
				sb.WriteString(fmt.Sprintf("%s=%v ", key, val))
			}
		}
		sb.WriteString("| ")
	}

	// manifest-detail: components（activities/services/receivers/providers）— 保留 exported=true 的完整列表
	if components, ok := obj["components"].(map[string]interface{}); ok {
		for _, compType := range []string{"activities", "services", "receivers", "providers"} {
			if arr, ok := components[compType].([]interface{}); ok && len(arr) > 0 {
				// 先提取 exported 组件（安全分析最关键）
				var exported []interface{}
				for _, item := range arr {
					if m, ok := item.(map[string]interface{}); ok {
						if exp, _ := m["exported"].(bool); exp {
							exported = append(exported, item)
						}
					}
				}
				sb.WriteString(fmt.Sprintf("%s(%d, exported=%d): ", compType, len(arr), len(exported)))
				// exported 组件全部保留
				for i, item := range exported {
					if i > 0 {
						sb.WriteString("; ")
					}
					sb.WriteString(fmt.Sprintf("%v", item))
				}
				if len(exported) > 0 {
					sb.WriteString(" | ")
				}
			}
		}
	}

	// manifest-detail: permissions
	if perms, ok := obj["permissions_used"].([]interface{}); ok && len(perms) > 0 {
		sb.WriteString(fmt.Sprintf("permissions_used(%d): %v | ", len(perms), perms))
	}
	if decl, ok := obj["permissions_declared"].([]interface{}); ok && len(decl) > 0 {
		sb.WriteString(fmt.Sprintf("permissions_declared: %v | ", decl))
	}

	// methods 字段：保留全部方法签名（对结构分析至关重要）
	if methods, ok := obj["methods"]; ok {
		if arr, ok := methods.([]interface{}); ok {
			sb.WriteString(fmt.Sprintf("methods(%d): ", len(arr)))
			for i, m := range arr {
				if i >= 15 {
					sb.WriteString(fmt.Sprintf("...+%d more", len(arr)-15))
					break
				}
				if i > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString(fmt.Sprintf("%v", m))
			}
			sb.WriteString(" | ")
		}
	}

	// fields 字段
	if fields, ok := obj["fields"]; ok {
		if arr, ok := fields.([]interface{}); ok {
			sb.WriteString(fmt.Sprintf("fields(%d): ", len(arr)))
			for i, f := range arr {
				if i >= 10 {
					sb.WriteString(fmt.Sprintf("...+%d more", len(arr)-10))
					break
				}
				if i > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString(fmt.Sprintf("%v", f))
			}
			sb.WriteString(" | ")
		}
	}

	// callers 字段
	if callers, ok := obj["callers"]; ok {
		if arr, ok := callers.([]interface{}); ok {
			sb.WriteString(fmt.Sprintf("callers(%d): ", len(arr)))
			for i, c := range arr {
				if i >= 10 {
					sb.WriteString(fmt.Sprintf("...+%d more", len(arr)-10))
					break
				}
				if i > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString(fmt.Sprintf("%v", c))
			}
			sb.WriteString(" | ")
		}
	}

	// code 字段：仅保留方法签名行和前几行
	if code, ok := obj["code"].(string); ok {
		sb.WriteString("code: ")
		lines := strings.Split(code, "\n")
		kept := 0
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			// 保留方法/类声明行、import、关键安全调用
			if isSignificantLine(trimmed) {
				sb.WriteString(trimmed)
				sb.WriteString("\n")
				kept++
				if kept >= 15 {
					sb.WriteString(fmt.Sprintf("...(%d lines total)", len(lines)))
					break
				}
			}
		}
	}

	// pagination 信息
	if pag, ok := obj["pagination"].(map[string]interface{}); ok {
		sb.WriteString(fmt.Sprintf("total: %v, has_more: %v", pag["total"], pag["has_more"]))
	}

	// results/classes/references 数组：保留前 10 条
	for _, key := range []string{"results", "classes", "references"} {
		if arr, ok := obj[key].([]interface{}); ok {
			sb.WriteString(fmt.Sprintf("%s(%d): ", key, len(arr)))
			for i, item := range arr {
				if i >= 10 {
					sb.WriteString(fmt.Sprintf("...+%d more", len(arr)-10))
					break
				}
				if i > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString(fmt.Sprintf("%v", item))
			}
		}
	}

	return truncateStr(sb.String(), maxLen)
}

func extractFromArray(arr []interface{}, maxLen int) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[%d items] ", len(arr)))
	for i, item := range arr {
		if i >= 10 {
			sb.WriteString(fmt.Sprintf("...+%d more", len(arr)-10))
			break
		}
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(fmt.Sprintf("%v", item))
	}
	return truncateStr(sb.String(), maxLen)
}

func extractFromText(text string, maxLen int) string {
	var sb strings.Builder

	// 提取全限定类名
	classNames := classNameRe.FindAllString(text, 20)
	if len(classNames) > 0 {
		seen := make(map[string]bool)
		sb.WriteString("Classes: ")
		count := 0
		for _, cn := range classNames {
			if seen[cn] || len(cn) < 10 {
				continue
			}
			seen[cn] = true
			if count > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(cn)
			count++
			if count >= 10 {
				break
			}
		}
		sb.WriteString(" | ")
	}

	// 提取安全相关行
	lines := strings.Split(text, "\n")
	secLines := 0
	for _, line := range lines {
		lower := strings.ToLower(line)
		for _, kw := range securityKeywords {
			if strings.Contains(lower, kw) {
				sb.WriteString(strings.TrimSpace(line))
				sb.WriteString("\n")
				secLines++
				break
			}
		}
		if secLines >= 5 {
			break
		}
	}

	if sb.Len() == 0 {
		// 无结构化信息，回退到截断
		return truncateStr(text, maxLen)
	}

	return truncateStr(sb.String(), maxLen)
}

// isSignificantLine 判断代码行是否为关键行（签名、声明、安全相关）
func isSignificantLine(line string) bool {
	if line == "" || line == "{" || line == "}" {
		return false
	}
	// 类/方法声明
	for _, prefix := range []string{"public ", "private ", "protected ", "class ", "interface ", "abstract ", "static ", "@"} {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	// import
	if strings.HasPrefix(line, "import ") {
		return true
	}
	// 安全相关调用
	lower := strings.ToLower(line)
	for _, kw := range securityKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func buildMidSummaryHint(stepsDone int) string {
	if stepsDone > 0 && stepsDone%5 == 0 {
		return fmt.Sprintf(`已完成 %d 个步骤，触发中间总结。在做出决策前，请先用 2-3 句话简要总结：
- 当前已确认的关键发现
- 原始目标的完成进度（百分比估计）
- 下一步最高优先级方向

总结后再做决策。`, stepsDone)
	}
	return "未触发中间总结，直接进入决策。"
}

func buildInitHint() string {
	if !sessionInitDone {
		return `## 启动流程（首次分析必须执行）

1. ` + "`system_manager(action=systemStatus)`" + ` → 确认 health.status=="UP" 且 decompiler_ready==true，memory.usage_percent > 85% 则先 clearCache
2. ` + "`system_manager(action=getApkOverview)`" + ` → 获取包名、组件列表、权限声明、SDK 版本，据此制定后续策略
3. 如需分析 Manifest 安全属性（exported/intent-filter/debuggable/allowBackup 等），使用 ` + "`resource_explorer(action=getManifestDetail)`" + ` 一次获取全部结构化数据。**禁止使用 getResourceFile 分页读取 AndroidManifest.xml**。`
	}
	return `## 启动流程（跳过）

Jadx 服务已确认正常运行，无需重复调用 systemStatus/getApkOverview，直接进入分析步骤。
如需分析 Manifest 安全属性，使用 ` + "`resource_explorer(action=getManifestDetail)`" + ` 一次获取，禁止使用 getResourceFile 分页读取 AndroidManifest.xml。`
}

func ptr[T any](v T) *T { return &v }
