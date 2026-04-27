package agent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// classNameRe matches fully qualified Java class names.
var classNameRe = regexp.MustCompile(`[a-zA-Z][\w]*(?:\.[\w]+){2,}`)

// securityKeywords for identifying security-relevant content.
var securityKeywords = []string{
	"encrypt", "decrypt", "cipher", "password", "secret", "key",
	"sign", "signature", "hmac", "hash", "md5", "sha", "aes", "rsa", "des",
	"token", "auth", "credential", "certificate", "ssl", "tls",
	"inject", "xss", "sql", "vulnerability", "exploit", "bypass",
	"webview", "javascript", "native", "jni", "loadlibrary",
}

// extractKeyInfo intelligently extracts key information from tool results.
func extractKeyInfo(result string) string {
	const maxSummary = 20000

	var jsonObj map[string]interface{}
	if err := json.Unmarshal([]byte(result), &jsonObj); err == nil {
		return extractFromJSON(jsonObj, maxSummary)
	}

	var jsonArr []interface{}
	if err := json.Unmarshal([]byte(result), &jsonArr); err == nil {
		return extractFromArray(jsonArr, maxSummary)
	}

	return extractFromText(result, maxSummary)
}

func extractFromJSON(obj map[string]interface{}, maxLen int) string {
	var sb strings.Builder

	if errVal, ok := obj["error"]; ok {
		sb.WriteString(fmt.Sprintf("Error: %v", errVal))
		if hint, ok := obj["hint"]; ok {
			sb.WriteString(fmt.Sprintf(" | Hint: %v", hint))
		}
		return truncateStr(sb.String(), maxLen)
	}

	for _, key := range []string{"class_name", "type", "method_name", "super_class", "strategy"} {
		if val, ok := obj[key]; ok {
			sb.WriteString(fmt.Sprintf("%s: %v | ", key, val))
		}
	}

	if impl, ok := obj["implements"]; ok {
		sb.WriteString(fmt.Sprintf("implements: %v | ", impl))
	}

	if app, ok := obj["application"].(map[string]interface{}); ok {
		sb.WriteString("application: ")
		for _, key := range []string{"name", "debuggable", "allowBackup", "networkSecurityConfig", "usesCleartextTraffic"} {
			if val, ok := app[key]; ok {
				sb.WriteString(fmt.Sprintf("%s=%v ", key, val))
			}
		}
		sb.WriteString("| ")
	}

	if components, ok := obj["components"].(map[string]interface{}); ok {
		for _, compType := range []string{"activities", "services", "receivers", "providers"} {
			if arr, ok := components[compType].([]interface{}); ok && len(arr) > 0 {
				var exported []interface{}
				for _, item := range arr {
					if m, ok := item.(map[string]interface{}); ok {
						if exp, _ := m["exported"].(bool); exp {
							exported = append(exported, item)
						}
					}
				}
				sb.WriteString(fmt.Sprintf("%s(%d, exported=%d): ", compType, len(arr), len(exported)))
				for i, item := range exported {
					if i > 0 {
						sb.WriteString("; ")
					}
					sb.WriteString(fmt.Sprintf("%v", item))
				}
				if len(exported) > 0 {
					sb.WriteString(" | ")
				}
			}
		}
	}

	if perms, ok := obj["permissions_used"].([]interface{}); ok && len(perms) > 0 {
		sb.WriteString(fmt.Sprintf("permissions_used(%d): %v | ", len(perms), perms))
	}
	if decl, ok := obj["permissions_declared"].([]interface{}); ok && len(decl) > 0 {
		sb.WriteString(fmt.Sprintf("permissions_declared: %v | ", decl))
	}

	if methods, ok := obj["methods"]; ok {
		if arr, ok := methods.([]interface{}); ok {
			sb.WriteString(fmt.Sprintf("methods(%d): ", len(arr)))
			for i, m := range arr {
				if i >= 15 {
					sb.WriteString(fmt.Sprintf("...+%d more", len(arr)-15))
					break
				}
				if i > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString(fmt.Sprintf("%v", m))
			}
			sb.WriteString(" | ")
		}
	}

	if fields, ok := obj["fields"]; ok {
		if arr, ok := fields.([]interface{}); ok {
			sb.WriteString(fmt.Sprintf("fields(%d): ", len(arr)))
			for i, f := range arr {
				if i >= 10 {
					sb.WriteString(fmt.Sprintf("...+%d more", len(arr)-10))
					break
				}
				if i > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString(fmt.Sprintf("%v", f))
			}
			sb.WriteString(" | ")
		}
	}

	if callers, ok := obj["callers"]; ok {
		if arr, ok := callers.([]interface{}); ok {
			sb.WriteString(fmt.Sprintf("callers(%d): ", len(arr)))
			for i, c := range arr {
				if i >= 10 {
					sb.WriteString(fmt.Sprintf("...+%d more", len(arr)-10))
					break
				}
				if i > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString(fmt.Sprintf("%v", c))
			}
			sb.WriteString(" | ")
		}
	}

	if code, ok := obj["code"].(string); ok {
		sb.WriteString("code: ")
		lines := strings.Split(code, "\n")
		kept := 0
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if isSignificantLine(trimmed) {
				sb.WriteString(trimmed)
				sb.WriteString("\n")
				kept++
				if kept >= 15 {
					sb.WriteString(fmt.Sprintf("...(%d lines total)", len(lines)))
					break
				}
			}
		}
	}

	if pag, ok := obj["pagination"].(map[string]interface{}); ok {
		sb.WriteString(fmt.Sprintf("total: %v, has_more: %v", pag["total"], pag["has_more"]))
	}

	for _, key := range []string{"results", "classes", "references"} {
		if arr, ok := obj[key].([]interface{}); ok {
			sb.WriteString(fmt.Sprintf("%s(%d): ", key, len(arr)))
			for i, item := range arr {
				if i >= 10 {
					sb.WriteString(fmt.Sprintf("...+%d more", len(arr)-10))
					break
				}
				if i > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString(fmt.Sprintf("%v", item))
			}
		}
	}

	return truncateStr(sb.String(), maxLen)
}

func extractFromArray(arr []interface{}, maxLen int) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[%d items] ", len(arr)))
	for i, item := range arr {
		if i >= 10 {
			sb.WriteString(fmt.Sprintf("...+%d more", len(arr)-10))
			break
		}
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(fmt.Sprintf("%v", item))
	}
	return truncateStr(sb.String(), maxLen)
}

func extractFromText(text string, maxLen int) string {
	var sb strings.Builder

	classNames := classNameRe.FindAllString(text, 20)
	if len(classNames) > 0 {
		seen := make(map[string]bool)
		sb.WriteString("Classes: ")
		count := 0
		for _, cn := range classNames {
			if seen[cn] || len(cn) < 10 {
				continue
			}
			seen[cn] = true
			if count > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(cn)
			count++
			if count >= 10 {
				break
			}
		}
		sb.WriteString(" | ")
	}

	lines := strings.Split(text, "\n")
	secLines := 0
	for _, line := range lines {
		lower := strings.ToLower(line)
		for _, kw := range securityKeywords {
			if strings.Contains(lower, kw) {
				sb.WriteString(strings.TrimSpace(line))
				sb.WriteString("\n")
				secLines++
				break
			}
		}
		if secLines >= 5 {
			break
		}
	}

	if sb.Len() == 0 {
		return truncateStr(text, maxLen)
	}

	return truncateStr(sb.String(), maxLen)
}

// isSignificantLine identifies key code lines (declarations, imports, security calls).
func isSignificantLine(line string) bool {
	if line == "" || line == "{" || line == "}" {
		return false
	}
	for _, prefix := range []string{"public ", "private ", "protected ", "class ", "interface ", "abstract ", "static ", "@"} {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	if strings.HasPrefix(line, "import ") {
		return true
	}
	lower := strings.ToLower(line)
	for _, kw := range securityKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// extractOneLiner extracts the single most important line from a tool result.
// Priority: error > class_name + method count > security keyword line > first 100 chars.
func extractOneLiner(result string) string {
	if result == "" || result == "{}" || result == "null" {
		return "(empty)"
	}

	// Try JSON
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(result), &obj); err == nil {
		// Error
		if errVal, ok := obj["error"]; ok {
			return fmt.Sprintf("Error: %v", errVal)
		}
		// Class name + method count
		if cn, ok := obj["class_name"].(string); ok {
			if methods, ok := obj["methods"].([]interface{}); ok {
				return fmt.Sprintf("%s (%d methods)", cn, len(methods))
			}
			return cn
		}
		// Strategy field
		if strategy, ok := obj["strategy"].(string); ok {
			s := strategy
			if len(s) > 80 {
				s = s[:80] + "..."
			}
			return s
		}
		// Results count
		for _, key := range []string{"results", "classes", "references"} {
			if arr, ok := obj[key].([]interface{}); ok {
				return fmt.Sprintf("%d %s found", len(arr), key)
			}
		}
	}

	// Try JSON array
	var arr []interface{}
	if err := json.Unmarshal([]byte(result), &arr); err == nil {
		return fmt.Sprintf("[%d items]", len(arr))
	}

	// Plain text: look for security keywords
	lines := strings.Split(result, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		for _, kw := range securityKeywords {
			if strings.Contains(lower, kw) {
				if len(trimmed) > 120 {
					return trimmed[:120] + "..."
				}
				return trimmed
			}
		}
	}

	// Fallback: first non-empty line, truncated
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			if len(trimmed) > 100 {
				return trimmed[:100] + "..."
			}
			return trimmed
		}
	}

	return "(no content)"
}
