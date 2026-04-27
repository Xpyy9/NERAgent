package agent

import (
	"context"

	"github.com/cloudwego/eino/adk"
)

// genPlannerInput generates input messages for the planner stage.
// The planner receives: user input, strategy tables, APK profile, session context, init hint.
// It does NOT receive executed_steps (the planner makes the initial plan).
func genPlannerInput(ctx context.Context, userInput []adk.Message) ([]adk.Message, error) {
	input := formatInput(userInput)
	sess := getActiveSession()
	return plannerPrompt.Format(ctx, map[string]any{
		"input":           input,
		"strategy_tables": BuildStrategySection(input),
		"init_hint":       buildInitHint(),
		"session_context": BuildSessionContext(sess),
		"apk_profile":    BuildApkProfile(),
	})
}
