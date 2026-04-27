package agent

import (
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk/prebuilt/planexecute"
)

// formatReplannerSteps formats executed steps for the replanner stage.
// All steps compressed to status-tagged 1-line summaries.
func formatReplannerSteps(steps []planexecute.ExecutedStep) string {
	if len(steps) == 0 {
		return "（尚无已完成步骤）"
	}

	var sb strings.Builder
	const maxTotal = 15000

	for i, s := range steps {
		result := fmt.Sprintf("%v", s.Result)
		status := classifyStepResult(result)
		oneLine := extractOneLiner(result)
		fmt.Fprintf(&sb, "%d. [%s] %v: %s\n", i+1, status, s.Step, oneLine)

		if sb.Len() > maxTotal {
			sb.WriteString(fmt.Sprintf("...[省略剩余 %d 步]\n", len(steps)-i-1))
			break
		}
	}
	return sb.String()
}

// classifyStepResult categorizes a step result for the replanner.
func classifyStepResult(result string) string {
	lower := strings.ToLower(result)

	if strings.Contains(lower, "\"error\"") || strings.Contains(lower, "error:") {
		return "ERROR"
	}
	if result == "" || result == "{}" || result == "null" {
		return "EMPTY"
	}

	for _, kw := range []string{"vulnerability", "漏洞", "critical", "high", "严重", "高危",
		"exported=true", "hardcoded", "硬编码", "明文"} {
		if strings.Contains(lower, kw) {
			return "FINDING"
		}
	}

	return "OK"
}
