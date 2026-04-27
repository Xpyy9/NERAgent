package agent

import (
	"NERAgent/internal/config"
	"fmt"
	"sync"

	"github.com/cloudwego/eino/adk"
)

var (
	mu              sync.RWMutex
	globalRunner    *adk.Runner
	globalCfg       *config.Config
	modelConfig     ModelConfig
	availableModels []string
	sessionInitDone bool
)

// Active session for GenInputFn closures.
var (
	activeSession   *AnalysisSession
	activeSessionMu sync.RWMutex
)

// Exported model names for handler-layer SSE stage events.
var (
	PlannerModelName   string
	ExecutorModelName  string
	ReplannerModelName string
)

// SetActiveSession sets the current active session (called by handler before query).
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

func MarkSessionInitDone() { sessionInitDone = true }
func ResetSessionInit()    { sessionInitDone = false }

// GetModelConfig returns the current three-stage model config.
func GetModelConfig() ModelConfig {
	mu.RLock()
	defer mu.RUnlock()
	return modelConfig
}

// GetAvailableModels returns the list of available models.
func GetAvailableModels() []string {
	mu.RLock()
	defer mu.RUnlock()
	return availableModels
}

// Init initializes the PER Agent pipeline with the given config.
func Init(cfg *config.Config) error {
	globalCfg = cfg
	availableModels = cfg.LLM.AvailableModels

	modelConfig = ModelConfig{
		Planner:   cfg.LLM.DefaultPlanner,
		Executor:  cfg.LLM.DefaultExecutor,
		Replanner: cfg.LLM.DefaultReplanner,
	}

	return buildRunner()
}

// SetModelAndRebuild switches the model for a given role and rebuilds the pipeline.
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

	return buildRunnerLocked()
}

// GetRunner returns the initialized global Runner (read-lock protected).
func GetRunner() (*adk.Runner, error) {
	mu.RLock()
	defer mu.RUnlock()
	if globalRunner == nil {
		return nil, fmt.Errorf("runner has not been initialized yet")
	}
	return globalRunner, nil
}

// buildRunner acquires the write lock and rebuilds.
func buildRunner() error {
	mu.Lock()
	defer mu.Unlock()
	return buildRunnerLocked()
}
