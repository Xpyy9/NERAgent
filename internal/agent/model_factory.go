package agent

import (
	"context"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
)

// ModelConfig holds the three-stage model names.
type ModelConfig struct {
	Planner   string `json:"planner"`
	Executor  string `json:"executor"`
	Replanner string `json:"replanner"`
}

// newChatModel creates a ChatModel using the global config.
func newChatModel(ctx context.Context, modelName string) (model.ToolCallingChatModel, error) {
	if globalCfg == nil || globalCfg.LLM.BaseURL == "" || globalCfg.LLM.APIKey == "" {
		return nil, errMissingLLMConfig
	}
	return openai.NewChatModel(ctx, &openai.ChatModelConfig{
		BaseURL:     globalCfg.LLM.BaseURL,
		APIKey:      globalCfg.LLM.APIKey,
		Model:       modelName,
		Temperature: ptr(float32(0.7)),
	})
}
