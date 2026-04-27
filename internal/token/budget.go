package token

import (
	"unicode"
	"unicode/utf8"
)

// BudgetManager handles token counting and budget allocation per PER stage.
type BudgetManager struct {
	contextWindow int // model's context window in tokens
}

// Budget describes the token allocation for each prompt section.
type Budget struct {
	Total          int
	SystemPrompt   int // ~25%
	Strategy       int // ~10%
	SessionContext int // ~10%
	UserInput      int // ~5%
	ExecutedSteps  int // ~45%
	CurrentStep    int // ~5%
}

// Known context windows (practical limits for quality output).
var modelContextWindows = map[string]int{
	"GPT-4.1":              128000,
	"GPT-4o":               128000,
	"GLM-5":                128000,
	"Claude-Opus-4.6":      200000,
	"Claude-Sonnet-4.6":    200000,
	"Gemini-2.5-pro":       256000,
	"DeepSeek-V3":          128000,
	"DeepSeek-R1":          128000,
	"Qwen3-Coder":          128000,
}

const defaultContextWindow = 128000

// NewBudgetManager creates a BudgetManager for the given model.
func NewBudgetManager(modelName string) *BudgetManager {
	window, ok := modelContextWindows[modelName]
	if !ok {
		window = defaultContextWindow
	}
	return &BudgetManager{contextWindow: window}
}

// AllocateForStage returns the token budget split for a PER stage.
// We reserve ~40% of context for model output, allocating ~60% for input.
func (bm *BudgetManager) AllocateForStage(stage string) Budget {
	inputBudget := bm.contextWindow * 60 / 100

	switch stage {
	case "planner":
		return Budget{
			Total:          inputBudget,
			SystemPrompt:   inputBudget * 30 / 100,
			Strategy:       inputBudget * 15 / 100,
			SessionContext: inputBudget * 15 / 100,
			UserInput:      inputBudget * 10 / 100,
			ExecutedSteps:  0, // planner doesn't receive executed steps
			CurrentStep:    0,
		}
	case "executor":
		return Budget{
			Total:          inputBudget,
			SystemPrompt:   inputBudget * 20 / 100,
			Strategy:       0, // executor doesn't need strategy tables
			SessionContext: inputBudget * 5 / 100,
			UserInput:      inputBudget * 5 / 100,
			ExecutedSteps:  inputBudget * 50 / 100,
			CurrentStep:    inputBudget * 10 / 100,
		}
	case "replanner":
		return Budget{
			Total:          inputBudget,
			SystemPrompt:   inputBudget * 25 / 100,
			Strategy:       0,
			SessionContext: inputBudget * 15 / 100,
			UserInput:      inputBudget * 5 / 100,
			ExecutedSteps:  inputBudget * 45 / 100,
			CurrentStep:    0,
		}
	default:
		return Budget{
			Total:          inputBudget,
			SystemPrompt:   inputBudget * 25 / 100,
			Strategy:       inputBudget * 10 / 100,
			SessionContext: inputBudget * 10 / 100,
			UserInput:      inputBudget * 5 / 100,
			ExecutedSteps:  inputBudget * 45 / 100,
			CurrentStep:    inputBudget * 5 / 100,
		}
	}
}

// EstimateTokens estimates token count for a string.
// Uses heuristic: ~4 chars per token for Latin/ASCII, ~1.5 chars per CJK character.
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	asciiChars := 0
	cjkChars := 0
	for _, r := range text {
		if r <= 127 {
			asciiChars++
		} else if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hiragana, r) {
			cjkChars++
		} else {
			asciiChars++ // treat other unicode as ~1 char
		}
	}
	// ASCII: ~4 chars/token, CJK: ~1.5 chars/token
	tokens := asciiChars/4 + cjkChars*2/3
	if tokens == 0 && utf8.RuneCountInString(text) > 0 {
		tokens = 1
	}
	return tokens
}

// TruncateToTokens truncates text to approximately maxTokens.
func TruncateToTokens(text string, maxTokens int) string {
	if maxTokens <= 0 {
		return ""
	}
	currentTokens := EstimateTokens(text)
	if currentTokens <= maxTokens {
		return text
	}

	// Approximate character limit: use ratio to scale
	ratio := float64(maxTokens) / float64(currentTokens)
	charLimit := int(float64(len(text)) * ratio)
	if charLimit >= len(text) {
		return text
	}
	if charLimit <= 0 {
		return ""
	}

	// Ensure we don't cut in the middle of a UTF-8 sequence
	truncated := text[:charLimit]
	for !utf8.ValidString(truncated) && charLimit > 0 {
		charLimit--
		truncated = text[:charLimit]
	}

	return truncated + "..."
}
