package agent

import (
	"NERAgent/internal/knowledge"
	"errors"
	"fmt"

	"github.com/cloudwego/eino/adk"
)

var errMissingLLMConfig = errors.New("缺少 LLM 配置项: BaseURL / APIKey")

func formatInput(in []adk.Message) string {
	if len(in) == 0 {
		return "If the user input is empty, please prompt the user to enter something."
	}
	return in[0].Content
}

// buildConvergenceHint 根据已完成步骤数产生收敛提示，直接注入 replanner prompt
func buildConvergenceHint(stepsDone int) string {
	if stepsDone >= 16 {
		return "⚠️ 已完成超过 16 步，**必须**立即调用 respond 输出最终结论。不允许再规划新步骤。"
	}
	if stepsDone >= 12 {
		return "⚠️ 已完成较多步骤，**强烈建议**调用 respond 输出结论。仅当缺少回答核心问题的关键信息时才继续。"
	}
	if stepsDone >= 8 {
		return "已完成多个步骤，请评估是否已有足够信息输出结论。"
	}
	if stepsDone < 5 {
		return "分析尚处于早期阶段，继续深入执行计划，不要过早收敛。"
	}
	return ""
}

func buildMidSummaryHint(stepsDone int) string {
	if stepsDone >= 12 {
		return fmt.Sprintf(`已完成 %d 个步骤，已超过建议的分析步数。**强烈建议立即调用 respond 工具输出最终结论。**
除非有极其关键的未完成目标，否则现在就应该输出报告。部分结论远优于因迭代耗尽而被迫截断的报告。`, stepsDone)
	}
	if stepsDone >= 8 {
		return fmt.Sprintf(`已完成 %d 个步骤。请评估：
- 当前已确认的关键发现
- 原始目标的完成进度（百分比估计）
- 如果进度 ≥ 80%%，可考虑调用 respond 输出结论
- 如果确需继续，剩余步骤不超过 3 步`, stepsDone)
	}
	if stepsDone >= 5 {
		return fmt.Sprintf(`已完成 %d 个步骤。继续执行计划以获取更深入的分析结果。`, stepsDone)
	}
	return "分析初期，继续深入执行计划，确保获取足够的分析深度。"
}

func buildInitHint() string {
	if !sessionInitDone {
		return `## 启动流程（首次分析必须执行）

1. ` + "`system_manager(action=systemStatus)`" + ` → 确认 health.status=="UP" 且 decompiler_ready==true，memory.usage_percent > 85% 则先 clearCache
2. ` + "`system_manager(action=getApkOverview)`" + ` → 获取包名、组件列表、权限声明、SDK 版本，据此制定后续策略
3. 如需分析 Manifest 安全属性（exported/intent-filter/debuggable/allowBackup 等），使用 ` + "`resource_explorer(action=getManifestDetail)`" + ` 一次获取全部结构化数据。**禁止使用 getResourceFile 分页读取 AndroidManifest.xml**。`
	}
	return `## 启动流程（跳过）

Jadx 服务已确认正常运行，无需重复调用 systemStatus/getApkOverview，直接进入分析步骤。
如需分析 Manifest 安全属性，使用 ` + "`resource_explorer(action=getManifestDetail)`" + ` 一次获取，禁止使用 getResourceFile 分页读取 AndroidManifest.xml。`
}

func ptr[T any](v T) *T { return &v }

// buildStagnationHint emits a hard convergence signal when the knowledge graph
// stops growing across consecutive replanner rounds. Returns "" when progress is healthy.
//   - prev: snapshot taken at the previous replanner round (nil on first round)
//   - current: snapshot taken now
//   - stagnantRounds: how many consecutive prior rounds were stagnant
//   - stepsDone: total executed steps so far
func buildStagnationHint(prev, current *knowledge.Snapshot, stagnantRounds, stepsDone int) string {
	if current == nil {
		return ""
	}
	// First round — nothing to compare; only complain if we've burned >=5 steps with zero evidence.
	if prev == nil {
		if stepsDone >= 5 && current.FindingsTotal == 0 && current.SinksHit == 0 && current.SourcesHit == 0 && current.CodeAnalyzedCount == 0 && current.DeepAnalyzed == 0 {
			return fmt.Sprintf(`⚠️ 已执行 %d 步但知识图谱无任何 finding/sink/source/已分析类。请更换分析策略或从不同入口切入，不要放弃分析。`, stepsDone)
		}
		return ""
	}

	if current.HasNewEvidence(prev) {
		return ""
	}

	// No new evidence this round.
	switch {
	case stagnantRounds == 0:
		// First stagnant round — soft warning, suggest strategy change.
		return fmt.Sprintf(`⚠️ 本轮无新增证据（findings=%d, sinks=%d, sources=%d, code_analyzed=%d, classes=%d 与上轮相同）。建议更换分析策略或目标，探索尚未覆盖的代码路径。`,
			current.FindingsTotal, current.SinksHit, current.SourcesHit, current.CodeAnalyzedCount, current.ClassesTotal)
	case stagnantRounds == 1:
		// Second stagnant round — moderate warning, urge strategy change.
		return fmt.Sprintf(`⚠️ 已连续 2 轮无新增证据（findings=%d, sinks=%d, sources=%d, code_analyzed=%d）。**强烈建议更换分析路径**——尝试搜索不同关键词、追踪不同组件、或从其他入口点切入。如果确认所有可行路径均已穷尽，再考虑输出结论。`,
			current.FindingsTotal, current.SinksHit, current.SourcesHit, current.CodeAnalyzedCount)
	case stagnantRounds >= 2 && stepsDone < 8:
		// 3+ stagnant rounds but still early — urge path change, don't force terminate.
		return fmt.Sprintf(`⚠️ 已连续 %d 轮无新增证据，但分析步数仅 %d 步，深度不足。**必须更换分析策略**（不同的类/方法/搜索关键词），不要放弃分析。仅当确认所有路径均已穷尽时才输出结论。`,
			stagnantRounds+1, stepsDone)
	case stagnantRounds >= 2:
		// 3+ stagnant rounds with sufficient steps — hard requirement to respond.
		return fmt.Sprintf(`🛑 已连续 %d 轮无新增证据（findings=%d, sinks=%d, sources=%d），且已完成 %d 步。判定为分析停滞。**必须立即调用 respond 输出已确认的结论**。如确有未尽之处，在结论中如实说明并建议用户提供更具体的方向。`,
			stagnantRounds+1, current.FindingsTotal, current.SinksHit, current.SourcesHit, stepsDone)
	}
	return ""
}
