package knowledge

import (
	"fmt"
	"strings"
)

// RenderForPlanner produces a high-level summary for the planner stage.
// Includes: analysis coverage, all findings, exported components, data flows, chat history.
func (g *Graph) RenderForPlanner() string {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.isEmpty() {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## 会话记忆（前次分析已确认的信息，无需重复获取）\n\n")

	g.renderCoverage(&sb)
	g.renderExportedComponents(&sb)
	g.renderFindings(&sb)
	g.renderSources(&sb)
	g.renderSinks(&sb)
	g.renderDataFlows(&sb)
	g.renderChatHistory(&sb)

	return sb.String()
}

// RenderForExecutor produces focused context for the executor's current step.
// Includes: classes relevant to step (with method lists), related findings.
func (g *Graph) RenderForExecutor(currentStep string) string {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.isEmpty() {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## 已知信息（基于知识图谱）\n\n")

	// Find classes mentioned in the current step
	stepLower := strings.ToLower(currentStep)
	var relevantClasses []*ClassEntity
	for _, cls := range g.Classes {
		if strings.Contains(stepLower, strings.ToLower(cls.Name)) ||
			(cls.Depth >= StructureKnown && len(cls.FindingRefs) > 0) {
			relevantClasses = append(relevantClasses, cls)
		}
	}

	if len(relevantClasses) > 0 {
		sb.WriteString("**相关类**:\n")
		for _, cls := range relevantClasses {
			sb.WriteString(fmt.Sprintf("- %s [%s]", cls.Name, cls.Depth))
			if cls.SuperClass != "" {
				sb.WriteString(fmt.Sprintf(" extends %s", cls.SuperClass))
			}
			if len(cls.Methods) > 0 {
				methodCount := len(cls.Methods)
				sb.WriteString(fmt.Sprintf(" (%d methods)", methodCount))
			}
			if cls.IsExported {
				sb.WriteString(" [EXPORTED]")
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	// Related findings
	relatedFindings := g.findingsForStep(currentStep)
	if len(relatedFindings) > 0 {
		sb.WriteString("**相关发现**:\n")
		for _, f := range relatedFindings {
			sb.WriteString(fmt.Sprintf("- [%s] %s\n", f.Severity, f.Description))
		}
		sb.WriteString("\n")
	}

	if sb.Len() <= len("## 已知信息（基于知识图谱）\n\n") {
		return ""
	}

	return sb.String()
}

// RenderForReplanner produces a progress assessment for the replanner stage.
// Includes: coverage metrics, all findings with severity counts, uncovered attack surface, data flow completeness.
func (g *Graph) RenderForReplanner() string {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.isEmpty() {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## 知识图谱进度\n\n")

	// Coverage metrics
	g.renderCoverage(&sb)

	// Finding severity summary
	if len(g.Findings) > 0 {
		counts := make(map[string]int)
		for _, f := range g.Findings {
			counts[f.Severity]++
		}
		sb.WriteString("**安全发现统计**: ")
		for _, sev := range []string{"critical", "high", "medium", "low", "info"} {
			if c, ok := counts[sev]; ok {
				sb.WriteString(fmt.Sprintf("%s=%d ", sev, c))
			}
		}
		sb.WriteString("\n")

		// List all findings
		sb.WriteString("**发现详情**:\n")
		for _, f := range g.Findings {
			sb.WriteString(fmt.Sprintf("- [%s] %s", f.Severity, f.Description))
			if f.ClassRef != "" {
				sb.WriteString(fmt.Sprintf(" @ %s", f.ClassRef))
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	// Uncovered attack surface
	var uncovered []string
	for _, comp := range g.Components {
		if !comp.Exported {
			continue
		}
		cls, ok := g.Classes[comp.Name]
		if !ok || cls.Depth < CodeAnalyzed {
			uncovered = append(uncovered, fmt.Sprintf("%s(%s)", comp.Name, comp.Type))
		}
	}
	if len(uncovered) > 0 {
		sb.WriteString("**未覆盖攻击面**（exported 但未深入分析）:\n")
		for _, name := range uncovered {
			sb.WriteString(fmt.Sprintf("- %s\n", name))
		}
		sb.WriteString("\n")
	}

	g.renderSources(&sb)
	g.renderSinks(&sb)
	g.renderTaintCoverage(&sb)
	g.renderDataFlows(&sb)

	return sb.String()
}

// --- internal render helpers (caller must hold g.mu.RLock) ---

func (g *Graph) isEmpty() bool {
	return len(g.Classes) == 0 && len(g.Findings) == 0 &&
		len(g.Components) == 0 && len(g.DataFlows) == 0 &&
		len(g.ChatHistory) == 0
}

func (g *Graph) renderCoverage(sb *strings.Builder) {
	if len(g.Classes) == 0 {
		return
	}
	var discovered, structKnown, codeAnalyzed, deepAnalyzed int
	for _, c := range g.Classes {
		switch c.Depth {
		case Discovered:
			discovered++
		case StructureKnown:
			structKnown++
		case CodeAnalyzed:
			codeAnalyzed++
		case DeepAnalyzed:
			deepAnalyzed++
		}
	}
	sb.WriteString(fmt.Sprintf("**分析覆盖**: %d 类 (discovered=%d, structure=%d, code=%d, deep=%d)\n\n",
		len(g.Classes), discovered, structKnown, codeAnalyzed, deepAnalyzed))
}

func (g *Graph) renderExportedComponents(sb *strings.Builder) {
	var exported []string
	for _, comp := range g.Components {
		if comp.Exported {
			label := fmt.Sprintf("%s(%s)", comp.Name, comp.Type)
			if comp.Permission != "" {
				label += fmt.Sprintf("[perm:%s]", comp.Permission)
			}
			exported = append(exported, label)
		}
	}
	if len(exported) > 0 {
		sb.WriteString("**已确认 exported 组件**: ")
		sb.WriteString(strings.Join(exported, ", "))
		sb.WriteString("\n\n")
	}
}

func (g *Graph) renderFindings(sb *strings.Builder) {
	if len(g.Findings) == 0 {
		return
	}
	sb.WriteString("**已发现安全风险**:\n")
	for _, f := range g.Findings {
		sb.WriteString(fmt.Sprintf("- [%s] %s", f.Severity, f.Description))
		if f.ClassRef != "" {
			sb.WriteString(fmt.Sprintf(" @ %s", f.ClassRef))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
}

func (g *Graph) renderDataFlows(sb *strings.Builder) {
	if len(g.DataFlows) == 0 {
		return
	}
	sb.WriteString("**数据流链路**:\n")
	for _, df := range g.DataFlows {
		src := df.Source.Method
		if df.Source.Class != "" {
			src = df.Source.Class + "." + df.Source.Method
		}
		sink := df.Sink.Method
		if df.Sink.Class != "" {
			sink = df.Sink.Class + "." + df.Sink.Method
		}
		line := fmt.Sprintf("- %s → %s", src, sink)
		if df.Risk != "" {
			line += fmt.Sprintf(" [%s]", df.Risk)
		}
		sb.WriteString(line + "\n")
	}
	sb.WriteString("\n")
}

func (g *Graph) renderChatHistory(sb *strings.Builder) {
	if len(g.ChatHistory) == 0 {
		return
	}
	sb.WriteString("**对话历史**（本会话已完成的问答摘要）:\n")
	for i, turn := range g.ChatHistory {
		sb.WriteString(fmt.Sprintf("%d. 用户: %s\n   结论: %s\n", i+1, turn.UserQuery, turn.Summary))
	}
	sb.WriteString("\n")
}

// findingsForStep returns findings whose ClassRef or Description relates to the step text.
func (g *Graph) findingsForStep(step string) []*Finding {
	stepLower := strings.ToLower(step)
	var related []*Finding
	for _, f := range g.Findings {
		if f.ClassRef != "" && strings.Contains(stepLower, strings.ToLower(f.ClassRef)) {
			related = append(related, f)
		}
	}
	return related
}

// renderSources lists confirmed source hits grouped by category.
// Symmetric to renderSinks. Caller must hold g.mu.RLock.
func (g *Graph) renderSources(sb *strings.Builder) {
	hitByCategory := make(map[string][]string)
	for _, m := range g.Methods {
		if !m.IsSource {
			continue
		}
		label := fmt.Sprintf("%s @ %s.%s", m.SourcePattern, m.ClassName, m.MethodName)
		hitByCategory[m.SourceCategory] = append(hitByCategory[m.SourceCategory], label)
	}

	if len(hitByCategory) > 0 {
		sb.WriteString("**已命中 Source**（外部输入入口，潜在污点源）:\n")
		for _, cat := range SourceCategories() {
			hits, ok := hitByCategory[cat]
			if !ok {
				continue
			}
			sb.WriteString(fmt.Sprintf("- [%s] %d 处: %s\n", cat, len(hits), strings.Join(hits, "; ")))
		}
		sb.WriteString("\n")
	}
}

// renderTaintCoverage outputs a summary of source/sink/dataflow linkage status.
// Helps the replanner identify which sources lack a traced path to sinks.
// Caller must hold g.mu.RLock.
func (g *Graph) renderTaintCoverage(sb *strings.Builder) {
	var sourceCount, sinkCount int
	sourceClasses := make(map[string]bool)
	sinkClasses := make(map[string]bool)
	for _, m := range g.Methods {
		if m.IsSource {
			sourceCount++
			sourceClasses[m.ClassName] = true
		}
		if m.IsSink {
			sinkCount++
			sinkClasses[m.ClassName] = true
		}
	}

	if sourceCount == 0 && sinkCount == 0 {
		return
	}

	linkedFlows := len(g.DataFlows)
	sb.WriteString(fmt.Sprintf("**数据流覆盖**: source=%d, sink=%d, 已建立链路=%d\n", sourceCount, sinkCount, linkedFlows))

	// Identify source classes with no known path to any sink
	if sourceCount > 0 && sinkCount > 0 {
		linkedSourceClasses := make(map[string]bool)
		for _, df := range g.DataFlows {
			if df.Source.Class != "" {
				linkedSourceClasses[df.Source.Class] = true
			}
		}
		var unlinked []string
		for cls := range sourceClasses {
			if !linkedSourceClasses[cls] {
				unlinked = append(unlinked, cls)
			}
		}
		if len(unlinked) > 0 {
			sb.WriteString(fmt.Sprintf("**未关联 Source**（有外部输入但尚无到 sink 的链路，优先追踪）: %s\n", strings.Join(unlinked, ", ")))
		}
	}
	sb.WriteString("\n")
}

// renderSinks lists confirmed sink hits grouped by category, plus categories not yet hit.
// This gives the replanner objective evidence of "core path confirmed".
func (g *Graph) renderSinks(sb *strings.Builder) {
	hitByCategory := make(map[string][]string)
	for _, m := range g.Methods {
		if !m.IsSink {
			continue
		}
		label := fmt.Sprintf("%s @ %s.%s", m.SinkPattern, m.ClassName, m.MethodName)
		hitByCategory[m.SinkCategory] = append(hitByCategory[m.SinkCategory], label)
	}

	if len(hitByCategory) > 0 {
		sb.WriteString("**已命中 Sink**（关键路径证据，可作为收敛依据）:\n")
		for _, cat := range SinkCategories() {
			hits, ok := hitByCategory[cat]
			if !ok {
				continue
			}
			sb.WriteString(fmt.Sprintf("- [%s] %d 处: %s\n", cat, len(hits), strings.Join(hits, "; ")))
		}
		sb.WriteString("\n")
	}

	// Note categories with zero hits — useful for "did we cover X?" planning
	var missing []string
	for _, cat := range SinkCategories() {
		if _, ok := hitByCategory[cat]; !ok {
			missing = append(missing, cat)
		}
	}
	if len(missing) > 0 && len(hitByCategory) > 0 {
		sb.WriteString(fmt.Sprintf("**未命中 Sink 类别**: %s\n\n", strings.Join(missing, ", ")))
	}
}
