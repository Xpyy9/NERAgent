package agent

import (
	"NERAgent/internal/knowledge"
	"fmt"
	"strings"
	"sync"
	"time"
)

// AnalysisSession wraps a Knowledge Graph for an APK analysis session.
type AnalysisSession struct {
	ID             string
	Graph          *knowledge.Graph
	CreatedAt      time.Time
	LastActiveAt   time.Time
	PrevSnapshot   *knowledge.Snapshot // last replanner-round snapshot, for stagnation detection
	StagnantRounds int                 // consecutive replanner invocations with no new evidence
}

var (
	sessions  = make(map[string]*AnalysisSession)
	sessionMu sync.RWMutex
)

const sessionExpiry = 2 * time.Hour

// GetOrCreateSession returns an existing session or creates a new one.
func GetOrCreateSession(id string) *AnalysisSession {
	if id == "" {
		id = fmt.Sprintf("default-%d", time.Now().UnixNano())
	}
	sessionMu.Lock()
	defer sessionMu.Unlock()

	cleanExpiredLocked()

	if s, ok := sessions[id]; ok {
		s.LastActiveAt = time.Now()
		s.Graph.Touch()
		return s
	}
	s := &AnalysisSession{
		ID:           id,
		Graph:        knowledge.NewGraph(),
		CreatedAt:    time.Now(),
		LastActiveAt: time.Now(),
	}
	sessions[id] = s
	return s
}

// ResetSession removes a session (e.g. new APK or user clicks new session).
func ResetSession(id string) {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	delete(sessions, id)
}

// AddChatTurn records a conversation turn in the session's knowledge graph.
func AddChatTurn(s *AnalysisSession, userQuery, llmOutput string) {
	if s == nil || userQuery == "" {
		return
	}
	summary := extractChatSummary(llmOutput)
	s.Graph.AddChatTurn(userQuery, summary)
}

// UpdateSessionFromOutput extracts information from LLM output into the knowledge graph.
func UpdateSessionFromOutput(s *AnalysisSession, output string) {
	if s == nil || output == "" {
		return
	}
	s.Graph.IngestLLMOutput(output)
}

// BuildSessionContext renders the knowledge graph for the planner stage.
func BuildSessionContext(s *AnalysisSession) string {
	if s == nil {
		return ""
	}
	return s.Graph.RenderForPlanner()
}

// BuildSessionContextForExecutor renders focused context for the executor's current step.
func BuildSessionContextForExecutor(s *AnalysisSession, currentStep string) string {
	if s == nil {
		return ""
	}
	return s.Graph.RenderForExecutor(currentStep)
}

// BuildSessionContextForReplanner renders progress assessment for the replanner.
func BuildSessionContextForReplanner(s *AnalysisSession) string {
	if s == nil {
		return ""
	}
	return s.Graph.RenderForReplanner()
}

// extractChatSummary extracts conclusion summary from LLM output.
func extractChatSummary(output string) string {
	if output == "" {
		return "（无输出）"
	}

	for _, marker := range []string{"## 分析结论", "## 结论", "## 安全发现", "## 分析报告", "**分析结论**"} {
		if idx := strings.LastIndex(output, marker); idx >= 0 {
			tail := output[idx:]
			if len(tail) > 800 {
				tail = tail[:800] + "..."
			}
			return tail
		}
	}

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
