package web

// chatRequest 聊天请求体
type chatRequest struct {
	Message   string `json:"message"`
	SessionID string `json:"session_id"`
}

// usageStats 用于 SSE event:usage 的 JSON 负载
type usageStats struct {
	PromptTokens    int   `json:"prompt_tokens"`
	CompletionTokens int  `json:"completion_tokens"`
	TotalTokens     int   `json:"total_tokens"`
	ReasoningTokens int   `json:"reasoning_tokens"`
	ElapsedMs       int64 `json:"elapsed_ms"`
}

// stageEvent 用于 SSE event:stage 的 JSON 负载
type stageEvent struct {
	Name      string `json:"name"`       // "planner" | "executor" | "replanner"
	Model     string `json:"model"`      // 对应的模型名称
	Status    string `json:"status"`     // "running" | "done"
	LatencyMs int64  `json:"latency_ms"` // 该阶段累计耗时
	// 以下字段仅 status="done" 时有值
	PromptTokens     int `json:"prompt_tokens,omitempty"`
	CompletionTokens int `json:"completion_tokens,omitempty"`
	TotalTokens      int `json:"total_tokens,omitempty"`
	ReasoningTokens  int `json:"reasoning_tokens,omitempty"`
}

// retryEvent 用于 SSE event:retry 的 JSON 负载
type retryEvent struct {
	Attempt int    `json:"attempt"`  // 当前重试次数
	Message string `json:"message"`  // 提示信息
	WaitSec int    `json:"wait_sec"` // 本次等待秒数
}

// streamResult 表示一次 streamQueryEvents 的终止原因
type streamResult int

const (
	streamOK         streamResult = iota // 正常完成
	streamRateLimit                      // 429 限流
	streamFatalError                     // 致命错误（exceeds max iterations、模型崩溃等）
)

// streamOutcome 包含一次流式查询的终止信息
type streamOutcome struct {
	Result             streamResult
	ErrorMsg           string // 仅 streamFatalError 时非空
	AccumulatedContent string // 流式过程中累积的 LLM 输出，用于 exceeds max iterations 时携带到下一轮
}

// streamErrorEvent 用于 SSE event:stream_error 的 JSON 负载
type streamErrorEvent struct {
	Error     string `json:"error"`      // 错误详情
	AgentName string `json:"agent_name"` // 出错的阶段
}
