package agent

import (
	"NERAgent/internal/knowledge"
	"NERAgent/internal/tools"
	"context"
	_ "embed"
	"fmt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/planexecute"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/prompt"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

//go:embed planPrompt.md
var PlanPrompt string

//go:embed executePrompt.md
var ExecutePrompt string

//go:embed replanPrompt.md
var ReplanPrompt string

var plannerPrompt = prompt.FromMessages(schema.FString,
	schema.SystemMessage(PlanPrompt),
	schema.UserMessage(`## 分析目标
{input}

{session_context}

{apk_profile}

{strategy_tables}

请根据以上目标，生成一个策略性的逆向分析计划。每个步骤必须是一条自包含的自然语言指令，包含完整的工具名、action 和参数信息，使执行器无需额外上下文即可独立完成。`))

var executorPrompt = prompt.FromMessages(schema.FString,
	schema.SystemMessage(ExecutePrompt),
	schema.UserMessage(`## 分析目标
{input}

## 当前执行计划
{plan}

## 已完成步骤及结果
以下是前序步骤的执行输出，当前步骤所需的参数（如全限定类名、方法签名等）必须从这些结果中精确提取：
{executed_steps}

## 当前需要执行的步骤
{step}

请严格执行以上步骤，调用相应工具获取数据并进行深入分析。`))

var replannerPrompt = prompt.FromMessages(schema.FString,
	schema.SystemMessage(ReplanPrompt),
	schema.UserMessage(`## 分析目标
{input}

{session_context}

## 迭代进度
当前第 {iteration_current} 次迭代（上限 {iteration_max} 次）。已完成 {steps_done} 个步骤。
{convergence_hint}
{stagnation_hint}

## 原始计划
{plan}

## 已完成步骤及结果
{executed_steps}

请根据以上进展评估任务状态，然后选择以下操作之一：
- 若目标已完成（或已有足够信息），调用 '{respond_tool}' 输出最终分析结论（优先选择此项）
- 若核心目标确实未达成且有明确可行路径，调用 '{plan_tool}' 输出仅包含剩余步骤的新计划（不超过 5 步）`))

// buildRunnerLocked rebuilds the entire PER pipeline (caller must hold mu write lock).
func buildRunnerLocked() error {
	ctx := context.Background()

	PlannerModelName = modelConfig.Planner
	ExecutorModelName = modelConfig.Executor
	ReplannerModelName = modelConfig.Replanner

	plannerModel, err := newChatModel(ctx, modelConfig.Planner)
	if err != nil {
		return fmt.Errorf("init planner model: %v", err)
	}
	replannerModel, err := newChatModel(ctx, modelConfig.Replanner)
	if err != nil {
		return fmt.Errorf("init replanner model: %v", err)
	}
	executorModel, err := newChatModel(ctx, modelConfig.Executor)
	if err != nil {
		return fmt.Errorf("init executor model: %v", err)
	}

	planAgent, err := newPlanner(ctx, plannerModel)
	if err != nil {
		return fmt.Errorf("newPlanner: %v", err)
	}
	executeAgent, err := newExecutor(ctx, executorModel)
	if err != nil {
		return fmt.Errorf("newExecutor: %v", err)
	}
	replanAgent, err := newReplanner(ctx, replannerModel)
	if err != nil {
		return fmt.Errorf("newReplanner: %v", err)
	}

	uniAgent, err := planexecute.New(ctx, &planexecute.Config{
		Planner:       planAgent,
		Executor:      executeAgent,
		Replanner:     replanAgent,
		MaxIterations: 25,
	})
	if err != nil {
		return fmt.Errorf("planexecute.New: %v", err)
	}

	globalRunner = adk.NewRunner(ctx, adk.RunnerConfig{Agent: uniAgent, EnableStreaming: true})
	return nil
}

func newPlanner(ctx context.Context, mod model.ToolCallingChatModel) (adk.Agent, error) {
	return planexecute.NewPlanner(ctx, &planexecute.PlannerConfig{
		ToolCallingChatModel: mod,
		GenInputFn:           genPlannerInput,
	})
}

func newExecutor(ctx context.Context, execModel model.ToolCallingChatModel) (adk.Agent, error) {
	jc, err := tools.NewJadxClient(&globalCfg.JADX)
	if err != nil {
		return nil, fmt.Errorf("NewJadxClient: %v", err)
	}
	jadxTools, err := jc.BuildJadxTools()
	if err != nil {
		return nil, err
	}

	// Wrap tools with knowledge graph interceptor — uses active session's graph dynamically
	graphProvider := func() *knowledge.Graph {
		sess := getActiveSession()
		if sess == nil {
			return nil
		}
		return sess.Graph
	}
	wrappedTools := tools.WrapToolsWithGraphProvider(jadxTools, graphProvider)

	return planexecute.NewExecutor(ctx, &planexecute.ExecutorConfig{
		Model: execModel,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{Tools: wrappedTools},
		},
		MaxIterations: 15,
		GenInputFn: func(ctx context.Context, in *planexecute.ExecutionContext) ([]adk.Message, error) {
			planJSON, err := in.Plan.MarshalJSON()
			if err != nil {
				return nil, err
			}
			return executorPrompt.Format(ctx, map[string]any{
				"input":          formatInput(in.UserInput),
				"plan":           string(planJSON),
				"executed_steps": formatExecutorSteps(in.ExecutedSteps, ExecutorModelName),
				"step":           in.Plan.FirstStep(),
			})
		},
	})
}

func newReplanner(ctx context.Context, mod model.ToolCallingChatModel) (adk.Agent, error) {
	return planexecute.NewReplanner(ctx, &planexecute.ReplannerConfig{
		ChatModel: mod,
		GenInputFn: func(ctx context.Context, in *planexecute.ExecutionContext) ([]adk.Message, error) {
			planJSON, err := in.Plan.MarshalJSON()
			if err != nil {
				return nil, err
			}
			iterCurrent := len(in.ExecutedSteps) + 1
			stepsDone := len(in.ExecutedSteps)

			// Stagnation detection: snapshot now, compare with previous round, then update.
			sess := getActiveSession()
			var stagnationHint string
			var minFloorHint string
			if sess != nil && sess.Graph != nil {
				current := sess.Graph.TakeSnapshot()
				stagnationHint = buildStagnationHint(sess.PrevSnapshot, current, sess.StagnantRounds, stepsDone)
				if sess.PrevSnapshot != nil && !current.HasNewEvidence(sess.PrevSnapshot) {
					sess.StagnantRounds++
				} else {
					sess.StagnantRounds = 0
				}
				sess.PrevSnapshot = current

				// Minimum analysis floor: forbid respond in early steps when no objective evidence exists.
				if stepsDone < 5 && current.FindingsTotal == 0 && current.SinksHit == 0 && current.SourcesHit == 0 && current.CodeAnalyzedCount == 0 && current.DeepAnalyzed == 0 {
					minFloorHint = fmt.Sprintf("🚫 分析深度不足（已完成 %d 步，findings=0, sinks=0, sources=0, code_analyzed=0）。**禁止调用 respond**，必须继续执行分析计划以获取更深入的结果。", stepsDone)
				}
			}

			// Combine floor guard with stagnation hint
			combinedStagnation := minFloorHint
			if stagnationHint != "" {
				if combinedStagnation != "" {
					combinedStagnation += "\n"
				}
				combinedStagnation += stagnationHint
			}

			return replannerPrompt.Format(ctx, map[string]any{
				"input":             formatInput(in.UserInput),
				"plan":              string(planJSON),
				"executed_steps":    formatReplannerSteps(in.ExecutedSteps),
				"plan_tool":         planexecute.PlanToolInfo.Name,
				"respond_tool":      planexecute.RespondToolInfo.Name,
				"iteration_current": iterCurrent,
				"iteration_max":     25,
				"steps_done":        stepsDone,
				"mid_summary_hint":  buildMidSummaryHint(stepsDone),
				"convergence_hint":  buildConvergenceHint(stepsDone),
				"stagnation_hint":   combinedStagnation,
				"session_context":   BuildSessionContextForReplanner(sess),
			})
		},
	})
}
