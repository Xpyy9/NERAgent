package agent

import (
	"NERAgent/internal/token"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk/prebuilt/planexecute"
)

// formatExecutorSteps formats executed steps for the executor stage.
// Uses token budget to allocate detail: recent steps get full detail,
// older steps are compressed proportionally to their importance score.
func formatExecutorSteps(steps []planexecute.ExecutedStep, modelName string) string {
	total := len(steps)
	if total == 0 {
		return "（尚无已完成步骤）"
	}

	bm := token.NewBudgetManager(modelName)
	budget := bm.AllocateForStage("executor")
	maxTokens := budget.ExecutedSteps

	// Convert to StepEntry for the compressor
	entries := make([]token.StepEntry, total)
	for i, s := range steps {
		entries[i] = token.StepEntry{
			Desc:   fmt.Sprintf("%v", s.Step),
			Result: fmt.Sprintf("%v", s.Result),
		}
	}

	return token.CompressSteps(entries, maxTokens, extractOneLiner, extractKeyInfo)
}

// formatExecutorStepsDefault is the fallback without model-aware budget.
// Last 3 steps: full detail with extractKeyInfo.
// Older steps: compressed to 1-line summaries.
func formatExecutorStepsDefault(steps []planexecute.ExecutedStep) string {
	total := len(steps)
	if total == 0 {
		return "（尚无已完成步骤）"
	}

	var sb strings.Builder
	const detailThreshold = 3 // last N steps shown in full detail
	const maxTotal = 20000

	for i, s := range steps {
		result := fmt.Sprintf("%v", s.Result)
		if i >= total-detailThreshold {
			// Recent steps: full detail
			summary := extractKeyInfo(result)
			fmt.Fprintf(&sb, "## Step %d/%d: %v\nResult:\n%s\n\n", i+1, total, s.Step, summary)
		} else {
			// Older steps: compressed to 1-line
			oneLine := extractOneLiner(result)
			fmt.Fprintf(&sb, "- Step %d: %v -> %s\n", i+1, s.Step, oneLine)
		}

		if sb.Len() > maxTotal {
			sb.WriteString(fmt.Sprintf("\n...[已截断，省略剩余 %d 步]\n", total-i-1))
			break
		}
	}
	return sb.String()
}
