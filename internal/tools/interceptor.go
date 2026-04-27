package tools

import (
	"NERAgent/internal/knowledge"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// GraphProvider is a function that returns the current session's knowledge graph.
// This allows the interceptor to work with the active session dynamically.
type GraphProvider func() *knowledge.Graph

// InterceptedTool wraps an InvokableTool with post-hook that feeds results into a Knowledge Graph.
type InterceptedTool struct {
	inner    tool.InvokableTool
	getGraph GraphProvider
}

// Compile-time check: InterceptedTool implements InvokableTool (and thus BaseTool).
var _ tool.InvokableTool = (*InterceptedTool)(nil)

// WrapToolsWithGraphProvider wraps each tool with a post-hook that ingests results
// into the graph returned by the provider function at invocation time.
// Tools that don't implement InvokableTool are passed through unwrapped.
func WrapToolsWithGraphProvider(baseTools []tool.BaseTool, provider GraphProvider) []tool.BaseTool {
	if provider == nil {
		return baseTools
	}
	wrapped := make([]tool.BaseTool, len(baseTools))
	for i, t := range baseTools {
		if invokable, ok := t.(tool.InvokableTool); ok {
			wrapped[i] = &InterceptedTool{inner: invokable, getGraph: provider}
		} else {
			wrapped[i] = t
		}
	}
	return wrapped
}

func (it *InterceptedTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return it.inner.Info(ctx)
}

func (it *InterceptedTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	graph := it.getGraph()
	callKey := buildCallKey(argumentsInJSON)

	// Programmatic duplicate call detection: block 3rd+ identical call.
	if graph != nil && callKey != "" {
		if rec := graph.GetCallRecord(callKey); rec != nil && rec.Count >= 2 {
			graph.RecordCall(callKey, rec.LastResult)
			warning := fmt.Sprintf(
				"⚠️ 重复调用拦截：「%s」已被调用 %d 次（首次+%d次重试），本次直接返回缓存结果。请更换分析策略或目标，不要再用相同参数调用此接口。\n\n---\n（以下为上次调用的缓存结果）\n\n",
				callKey, rec.Count+1, rec.Count)
			return warning + rec.LastResult, nil
		}
	}

	result, err := it.inner.InvokableRun(ctx, argumentsInJSON, opts...)
	if err != nil {
		return result, err
	}

	// Record this call for dedup tracking.
	if graph != nil && callKey != "" {
		graph.RecordCall(callKey, result)
	}

	// Feed result into knowledge graph.
	if graph != nil {
		action := extractAction(argumentsInJSON)
		if action != "" && result != "" {
			graph.IngestToolResult(action, result)
		}
	}

	return result, nil
}

// extractAction parses the "action" field from a JSON tool input string.
func extractAction(argumentsJSON string) string {
	var args struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal([]byte(argumentsJSON), &args); err == nil && args.Action != "" {
		return args.Action
	}

	// Fallback: for tools without action field (e.g. getXrefs)
	if strings.Contains(argumentsJSON, "class") {
		return "getXrefs"
	}
	return ""
}

// dedupExcluded lists actions that should never be subject to call deduplication.
var dedupExcluded = map[string]bool{
	"systemStatus": true, // health check, always allowed
	"clearCache":   true, // cache reset, always allowed
	"taskStatus":   true, // async polling, always allowed
}

// buildCallKey produces a normalized key from tool arguments for dedup tracking.
// Returns "" for actions excluded from dedup or if arguments can't be parsed.
func buildCallKey(argumentsJSON string) string {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
		return ""
	}

	action, _ := args["action"].(string)
	if action == "" || dedupExcluded[action] {
		return ""
	}

	parts := []string{action}
	for _, key := range []string{"class_name", "code_name", "method_name", "component_name", "keyword", "file_path", "query"} {
		if v, ok := args[key].(string); ok && v != "" {
			parts = append(parts, v)
		}
	}

	return strings.Join(parts, ":")
}
