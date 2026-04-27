package agent

import (
	"NERAgent/internal/knowledge"
	"fmt"
	"strings"
)

// Checkpoint captures the state at a round boundary for structured resume.
type Checkpoint struct {
	Round     int
	MaxRounds int
	Graph     *knowledge.Graph
	UserGoal  string
}

// BuildCheckpoint creates a checkpoint from the current analysis state.
func BuildCheckpoint(sess *AnalysisSession, round, maxRounds int, userGoal string) *Checkpoint {
	if sess == nil {
		return &Checkpoint{Round: round, MaxRounds: maxRounds, UserGoal: userGoal}
	}
	return &Checkpoint{
		Round:     round,
		MaxRounds: maxRounds,
		Graph:     sess.Graph,
		UserGoal:  userGoal,
	}
}

// BuildResumePrompt generates a structured resume prompt from the checkpoint.
// Replaces the crude head/tail text truncation approach.
func (cp *Checkpoint) BuildResumePrompt() string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## 分析续传（第 %d/%d 轮已完成）\n\n", cp.Round, cp.MaxRounds))

	sb.WriteString("**重要规则**：\n")
	sb.WriteString("- 不要重复已完成的分析步骤\n")
	sb.WriteString("- 直接从未完成的部分继续\n")
	sb.WriteString(fmt.Sprintf("- 这是第 %d/%d 轮，请高效推进，优先完成核心目标\n", cp.Round+1, cp.MaxRounds))

	if cp.Round+1 >= cp.MaxRounds {
		sb.WriteString("- **这是最后一轮**，必须在本轮结束前输出完整的分析结论，部分结论优于无结论\n")
	} else if cp.Round+1 >= cp.MaxRounds-1 {
		sb.WriteString("- 剩余轮次有限，请聚焦最高优先级目标\n")
	}
	sb.WriteString("\n")

	// Knowledge graph summary instead of raw text
	if cp.Graph != nil {
		// Coverage
		total, discovered, structKnown, codeAnalyzed, deepAnalyzed := cp.Graph.Stats()
		if total > 0 {
			sb.WriteString(fmt.Sprintf("### 分析覆盖\n%d 类 (discovered=%d, structure=%d, code=%d, deep=%d)\n\n",
				total, discovered, structKnown, codeAnalyzed, deepAnalyzed))
		}

		// Findings
		severityCounts := cp.Graph.FindingsBySeverity()
		if len(severityCounts) > 0 {
			sb.WriteString("### 已确认发现\n")
			for _, sev := range []string{"critical", "high", "medium", "low", "info"} {
				if c, ok := severityCounts[sev]; ok {
					sb.WriteString(fmt.Sprintf("- %s: %d\n", sev, c))
				}
			}
			sb.WriteString("\n")
		}

		// Exported components not yet analyzed
		uncovered := cp.Graph.UnanalyzedExported()
		if len(uncovered) > 0 {
			sb.WriteString("### 未覆盖攻击面\n")
			for _, name := range uncovered {
				sb.WriteString(fmt.Sprintf("- %s\n", name))
			}
			sb.WriteString("\n")
		}

		// Exported components (already analyzed)
		exported := cp.Graph.ExportedComponents()
		analyzed := difference(exported, uncovered)
		if len(analyzed) > 0 {
			sb.WriteString("### 已分析 exported 组件\n")
			sb.WriteString(strings.Join(analyzed, ", "))
			sb.WriteString("\n\n")
		}
	}

	sb.WriteString("## 原始分析目标\n")
	sb.WriteString(cp.UserGoal)
	sb.WriteString("\n")

	return sb.String()
}

// BuildFinalReport generates a structured final report when all rounds are exhausted.
func (cp *Checkpoint) BuildFinalReport() string {
	var sb strings.Builder

	sb.WriteString("## 分析报告\n\n")

	if cp.Graph != nil {
		// Render the replanner view which includes all findings and coverage
		replannerView := cp.Graph.RenderForReplanner()
		if replannerView != "" {
			sb.WriteString(replannerView)
		}
	}

	sb.WriteString("\n> 注：以上为基于已获取数据的分析结论。如需更深入分析特定方向，请指定具体目标重新发起分析。\n")
	return sb.String()
}

// difference returns elements in a that are not in b.
func difference(a, b []string) []string {
	bSet := make(map[string]bool, len(b))
	for _, v := range b {
		bSet[v] = true
	}
	var result []string
	for _, v := range a {
		if !bSet[v] {
			result = append(result, v)
		}
	}
	return result
}
