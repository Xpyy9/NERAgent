package token

import (
	"fmt"
	"strings"
)

// StepImportance scores a step result for semantic priority compression.
type StepImportance struct {
	Index    int
	Score    int
	OneLiner string
	FullText string
}

// securityKeywords used for scoring step importance.
var securityKeywords = []string{
	"encrypt", "decrypt", "cipher", "password", "secret", "key",
	"sign", "signature", "hmac", "hash", "md5", "sha", "aes", "rsa",
	"token", "auth", "credential", "vulnerability", "exploit", "bypass",
	"webview", "javascript", "hardcoded", "硬编码", "明文", "漏洞",
}

// ScoreStep assigns an importance score to a step result.
// Higher score = more important = gets more token budget.
func ScoreStep(index, total int, stepDesc, result string) int {
	score := 0
	lower := strings.ToLower(result)

	// Error: always important
	if strings.Contains(lower, "\"error\"") || strings.Contains(lower, "error:") {
		score += 2
	}

	// Security finding
	for _, kw := range securityKeywords {
		if strings.Contains(lower, kw) {
			score += 3
			break
		}
	}

	// Finding markers
	for _, marker := range []string{"vulnerability", "漏洞", "critical", "high", "严重", "高危",
		"exported=true", "hardcoded", "硬编码"} {
		if strings.Contains(lower, marker) {
			score += 2
			break
		}
	}

	// Recent steps (last 3) get a boost
	if index >= total-3 {
		score += 2
	}

	// Data flow
	if strings.Contains(lower, "数据流") || strings.Contains(lower, "dataflow") ||
		strings.Contains(lower, "taint") {
		score += 1
	}

	return score
}

// CompressSteps compresses executed steps to fit within maxTokens.
// High-scoring steps get more detail; low-scoring steps become 1-liners.
func CompressSteps(steps []StepEntry, maxTokens int, oneLineFn func(string) string, detailFn func(string) string) string {
	if len(steps) == 0 {
		return "（尚无已完成步骤）"
	}

	// Score all steps
	scored := make([]StepImportance, len(steps))
	totalScore := 0
	for i, s := range steps {
		score := ScoreStep(i, len(steps), s.Desc, s.Result)
		scored[i] = StepImportance{
			Index:    i,
			Score:    score,
			OneLiner: oneLineFn(s.Result),
			FullText: detailFn(s.Result),
		}
		totalScore += score
	}

	// If all scores are 0, give each step a base score of 1
	if totalScore == 0 {
		totalScore = len(steps)
		for i := range scored {
			scored[i].Score = 1
		}
	}

	var sb strings.Builder
	usedTokens := 0

	for i, si := range scored {
		stepDesc := steps[i].Desc

		// Allocate tokens proportionally to score
		stepBudget := maxTokens * si.Score / totalScore
		if stepBudget < 50 {
			stepBudget = 50 // minimum: enough for a 1-liner
		}

		oneLineTokens := EstimateTokens(si.OneLiner)
		fullTokens := EstimateTokens(si.FullText)

		if fullTokens <= stepBudget && usedTokens+fullTokens <= maxTokens {
			// Full detail fits
			fmt.Fprintf(&sb, "## Step %d/%d: %v\nResult:\n%s\n\n", i+1, len(steps), stepDesc, si.FullText)
			usedTokens += fullTokens
		} else if usedTokens+oneLineTokens <= maxTokens {
			// Compress to 1-liner
			fmt.Fprintf(&sb, "- Step %d: %v -> %s\n", i+1, stepDesc, si.OneLiner)
			usedTokens += oneLineTokens
		} else {
			// Budget exhausted
			sb.WriteString(fmt.Sprintf("...[省略剩余 %d 步]\n", len(steps)-i))
			break
		}
	}

	return sb.String()
}

// StepEntry is a simplified step descriptor for the compressor.
type StepEntry struct {
	Desc   string
	Result string
}
