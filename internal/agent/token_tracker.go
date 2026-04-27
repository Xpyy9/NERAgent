package agent

import (
	"context"
	"sync"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// TokenTracker 用于追踪各个阶段的token使用情况
type TokenTracker struct {
	mu           sync.RWMutex
	stageUsage   map[string]*schema.TokenUsage // 阶段名称 -> token使用量
	currentStage string                        // 当前阶段（用于callback中识别阶段）
}

var globalTokenTracker = &TokenTracker{
	stageUsage: make(map[string]*schema.TokenUsage),
}

// GetTokenTracker 获取全局token追踪器
func GetTokenTracker() *TokenTracker {
	return globalTokenTracker
}

// GetTokenCallback 获取token追踪callback（用于Query时传递）
func GetTokenCallback() callbacks.Handler {
	return NewTokenTrackingCallback()
}

// Reset 重置追踪器
func (t *TokenTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stageUsage = make(map[string]*schema.TokenUsage)
	t.currentStage = ""
}

// SetCurrentStage 设置当前阶段
func (t *TokenTracker) SetCurrentStage(stage string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.currentStage = stage
}

// GetCurrentStage 获取当前阶段
func (t *TokenTracker) GetCurrentStage() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.currentStage
}

// GetStageUsage 获取指定阶段的token使用量
func (t *TokenTracker) GetStageUsage(stage string) *schema.TokenUsage {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if usage, ok := t.stageUsage[stage]; ok {
		return usage
	}
	return nil
}

// GetAndClearStageUsage 获取并清除指定阶段的token使用量
func (t *TokenTracker) GetAndClearStageUsage(stage string) *schema.TokenUsage {
	t.mu.Lock()
	defer t.mu.Unlock()
	if usage, ok := t.stageUsage[stage]; ok {
		delete(t.stageUsage, stage)
		return usage
	}
	return nil
}

// SetStageUsage 设置指定阶段的token使用量
func (t *TokenTracker) SetStageUsage(stage string, usage *schema.TokenUsage) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stageUsage[stage] = usage
}

// AddStageUsage 累加指定阶段的token使用量
func (t *TokenTracker) AddStageUsage(stage string, usage *schema.TokenUsage) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if usage == nil {
		return
	}
	if existing, ok := t.stageUsage[stage]; ok {
		existing.PromptTokens += usage.PromptTokens
		existing.CompletionTokens += usage.CompletionTokens
		existing.TotalTokens += usage.TotalTokens
		existing.CompletionTokensDetails.ReasoningTokens += usage.CompletionTokensDetails.ReasoningTokens
	} else {
		// 复制一份，避免引用问题
		copied := &schema.TokenUsage{
			PromptTokens:     usage.PromptTokens,
			CompletionTokens: usage.CompletionTokens,
			TotalTokens:      usage.TotalTokens,
			CompletionTokensDetails: schema.CompletionTokensDetails{
				ReasoningTokens: usage.CompletionTokensDetails.ReasoningTokens,
			},
		}
		t.stageUsage[stage] = copied
	}
}

// AddModelTokenUsage 从model.TokenUsage转换并累加
func (t *TokenTracker) AddModelTokenUsage(stage string, usage *model.TokenUsage) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if usage == nil {
		return
	}
	if existing, ok := t.stageUsage[stage]; ok {
		existing.PromptTokens += usage.PromptTokens
		existing.CompletionTokens += usage.CompletionTokens
		existing.TotalTokens += usage.TotalTokens
		existing.CompletionTokensDetails.ReasoningTokens += usage.CompletionTokensDetails.ReasoningTokens
	} else {
		copied := &schema.TokenUsage{
			PromptTokens:     usage.PromptTokens,
			CompletionTokens: usage.CompletionTokens,
			TotalTokens:      usage.TotalTokens,
			CompletionTokensDetails: schema.CompletionTokensDetails{
				ReasoningTokens: usage.CompletionTokensDetails.ReasoningTokens,
			},
		}
		t.stageUsage[stage] = copied
	}
}

// NewTokenTrackingCallback 创建一个callback handler来追踪token使用量
// 这个handler会监听LLM调用的结束事件，从中提取token usage信息
func NewTokenTrackingCallback() callbacks.Handler {
	return callbacks.NewHandlerBuilder().
		OnEndFn(func(ctx context.Context, info *callbacks.RunInfo, output callbacks.CallbackOutput) context.Context {
			// 从output中提取token usage
			modelOutput, ok := output.(*model.CallbackOutput)
			if !ok || modelOutput == nil {
				return ctx
			}

			// 获取当前阶段
			stage := globalTokenTracker.GetCurrentStage()
			if stage == "" {
				return ctx
			}

			// 从TokenUsage字段提取
			if modelOutput.TokenUsage != nil {
				globalTokenTracker.AddModelTokenUsage(stage, modelOutput.TokenUsage)
			}

			// 同时从Message的ResponseMeta中提取
			if modelOutput.Message != nil && modelOutput.Message.ResponseMeta != nil && modelOutput.Message.ResponseMeta.Usage != nil {
				globalTokenTracker.AddStageUsage(stage, modelOutput.Message.ResponseMeta.Usage)
			}

			return ctx
		}).
		OnEndWithStreamOutputFn(func(ctx context.Context, info *callbacks.RunInfo, output *schema.StreamReader[callbacks.CallbackOutput]) context.Context {
			// 流式输出结束时，从流中提取token usage
			defer output.Close()

			stage := globalTokenTracker.GetCurrentStage()
			if stage == "" {
				return ctx
			}

			// 读取流中的所有消息，寻找最后一个包含usage的消息
			for {
				chunk, err := output.Recv()
				if err != nil {
					break
				}

				modelOutput, ok := chunk.(*model.CallbackOutput)
				if !ok || modelOutput == nil {
					continue
				}

				// 从TokenUsage字段提取
				if modelOutput.TokenUsage != nil {
					globalTokenTracker.AddModelTokenUsage(stage, modelOutput.TokenUsage)
				}

				// 同时从Message的ResponseMeta中提取
				if modelOutput.Message != nil && modelOutput.Message.ResponseMeta != nil && modelOutput.Message.ResponseMeta.Usage != nil {
					globalTokenTracker.AddStageUsage(stage, modelOutput.Message.ResponseMeta.Usage)
				}
			}

			return ctx
		}).
		Build()
}

// stageKey 用于在context中存储当前阶段
type stageKey struct{}

// SetStageInContext 在context中设置当前阶段
func SetStageInContext(ctx context.Context, stage string) context.Context {
	return context.WithValue(ctx, stageKey{}, stage)
}

// getStageFromContext 从context中获取当前阶段
func getStageFromContext(ctx context.Context) string {
	if stage, ok := ctx.Value(stageKey{}).(string); ok {
		return stage
	}
	return ""
}
