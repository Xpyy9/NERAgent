package knowledge

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// classNameRe matches fully qualified Java class names.
var classNameRe = regexp.MustCompile(`[a-zA-Z][\w]*(?:\.[\w]+){2,}`)

// securityKeywords for identifying security-relevant findings in text output.
var securityKeywords = []string{
	"encrypt", "decrypt", "cipher", "password", "secret", "key",
	"sign", "signature", "hmac", "hash", "md5", "sha", "aes", "rsa", "des",
	"token", "auth", "credential", "certificate", "ssl", "tls",
	"inject", "xss", "sql", "vulnerability", "exploit", "bypass",
	"webview", "javascript", "native", "jni", "loadlibrary",
}

// findingPatterns matches severity markers in LLM text output.
var findingPatterns = []struct {
	severity string
	re       *regexp.Regexp
}{
	{"critical", regexp.MustCompile(`(?i)\[?(严重|critical)\]?[：:]?\s*(.{10,120})`)},
	{"high", regexp.MustCompile(`(?i)\[?(高危|high)\]?[：:]?\s*(.{10,120})`)},
	{"medium", regexp.MustCompile(`(?i)\[?(中危|medium)\]?[：:]?\s*(.{10,120})`)},
	{"low", regexp.MustCompile(`(?i)\[?(低危|low)\]?[：:]?\s*(.{10,120})`)},
}

var exportedRe = regexp.MustCompile(`(?i)exported[=:\s]+true[^"]*?"?([a-zA-Z][\w]*(?:\.[\w]+){2,})`)
var dataFlowRe = regexp.MustCompile(`\[数据流\]\s*(.+?)\s*→\s*(.+?)(?:\s*→\s*(.+?))?(?:\s*\[(.+?)\])?`)

// sinkPattern represents a known security-relevant API call.
type sinkPattern struct {
	category string
	pattern  string         // human-readable label
	re       *regexp.Regexp // matches the API call in code
}

// SinkPatterns enumerates the security sinks the agent should consider "core path confirmed".
// Categories: crypto, webview, exec, sql, file, intent, network.
var sinkPatterns = []sinkPattern{
	// Crypto sinks
	{"crypto", "Cipher.doFinal", regexp.MustCompile(`\.doFinal\s*\(`)},
	{"crypto", "Cipher.getInstance", regexp.MustCompile(`Cipher\.getInstance\s*\(`)},
	{"crypto", "Cipher.init", regexp.MustCompile(`\.init\s*\(\s*\d+\s*,`)},
	{"crypto", "MessageDigest.getInstance", regexp.MustCompile(`MessageDigest\.getInstance\s*\(`)},
	{"crypto", "Mac.getInstance", regexp.MustCompile(`Mac\.getInstance\s*\(`)},
	{"crypto", "Mac.doFinal", regexp.MustCompile(`Mac.*\.doFinal\s*\(`)},
	{"crypto", "KeyGenerator", regexp.MustCompile(`KeyGenerator\.getInstance\s*\(`)},
	{"crypto", "SecretKeySpec", regexp.MustCompile(`new\s+SecretKeySpec\s*\(`)},
	{"crypto", "Signature", regexp.MustCompile(`Signature\.getInstance\s*\(`)},

	// WebView sinks
	{"webview", "loadUrl", regexp.MustCompile(`\.loadUrl\s*\(`)},
	{"webview", "evaluateJavascript", regexp.MustCompile(`\.evaluateJavascript\s*\(`)},
	{"webview", "addJavascriptInterface", regexp.MustCompile(`\.addJavascriptInterface\s*\(`)},
	{"webview", "loadData", regexp.MustCompile(`\.loadData(WithBaseURL)?\s*\(`)},

	// Code execution sinks
	{"exec", "Runtime.exec", regexp.MustCompile(`Runtime\.getRuntime\(\)\.exec\s*\(`)},
	{"exec", "ProcessBuilder", regexp.MustCompile(`new\s+ProcessBuilder\s*\(`)},
	{"exec", "DexClassLoader", regexp.MustCompile(`new\s+DexClassLoader\s*\(`)},
	{"exec", "PathClassLoader", regexp.MustCompile(`new\s+PathClassLoader\s*\(`)},
	{"exec", "System.loadLibrary", regexp.MustCompile(`System\.loadLibrary\s*\(`)},
	{"exec", "System.load", regexp.MustCompile(`System\.load\s*\(`)},

	// SQL sinks
	{"sql", "rawQuery", regexp.MustCompile(`\.rawQuery\s*\(`)},
	{"sql", "execSQL", regexp.MustCompile(`\.execSQL\s*\(`)},

	// File / IO sinks
	{"file", "FileOutputStream", regexp.MustCompile(`new\s+FileOutputStream\s*\(`)},
	{"file", "openFileOutput", regexp.MustCompile(`\.openFileOutput\s*\(`)},
	{"file", "getExternalStorageDirectory", regexp.MustCompile(`getExternalStorage(Directory|PublicDirectory)\s*\(`)},

	// Intent / component dispatch sinks (taint dispatch)
	{"intent", "startActivity", regexp.MustCompile(`\.startActivity\s*\(`)},
	{"intent", "startService", regexp.MustCompile(`\.startService\s*\(`)},
	{"intent", "sendBroadcast", regexp.MustCompile(`\.sendBroadcast\s*\(`)},
	{"intent", "PendingIntent", regexp.MustCompile(`PendingIntent\.get(Activity|Service|Broadcast)\s*\(`)},

	// Network sinks
	{"network", "HttpURLConnection", regexp.MustCompile(`(?i)\.openConnection\s*\(`)},
	{"network", "OkHttpClient.newCall", regexp.MustCompile(`\.newCall\s*\(`)},
	{"network", "URL.openStream", regexp.MustCompile(`URL\([^)]*\)\.openStream\s*\(`)},
}

// SinkCategories returns the canonical list of all sink categories.
func SinkCategories() []string {
	return []string{"crypto", "webview", "exec", "sql", "file", "intent", "network"}
}

// sourcePattern represents a known external input source API.
type sourcePattern struct {
	category string
	pattern  string         // human-readable label
	re       *regexp.Regexp // matches the API call in code
}

// sourcePatterns enumerates external input sources the agent should track as taint origins.
// Categories: intent, deeplink, network, file, clipboard.
var sourcePatterns = []sourcePattern{
	// Intent data sources
	{"intent", "getStringExtra", regexp.MustCompile(`\.getStringExtra\s*\(`)},
	{"intent", "getIntExtra", regexp.MustCompile(`\.getIntExtra\s*\(`)},
	{"intent", "getBooleanExtra", regexp.MustCompile(`\.getBooleanExtra\s*\(`)},
	{"intent", "getParcelableExtra", regexp.MustCompile(`\.getParcelableExtra\s*\(`)},
	{"intent", "getSerializableExtra", regexp.MustCompile(`\.getSerializableExtra\s*\(`)},
	{"intent", "getBundleExtra", regexp.MustCompile(`\.getBundleExtra\s*\(`)},
	{"intent", "getExtras", regexp.MustCompile(`\.getExtras\s*\(`)},
	{"intent", "getIntent", regexp.MustCompile(`\.getIntent\s*\(`)},

	// DeepLink / URI sources
	{"deeplink", "getData", regexp.MustCompile(`\.getData\s*\(`)},
	{"deeplink", "getDataString", regexp.MustCompile(`\.getDataString\s*\(`)},
	{"deeplink", "Uri.parse", regexp.MustCompile(`Uri\.parse\s*\(`)},
	{"deeplink", "getQueryParameter", regexp.MustCompile(`\.getQueryParameter\s*\(`)},
	{"deeplink", "getPathSegments", regexp.MustCompile(`\.getPathSegments\s*\(`)},
	{"deeplink", "getHost", regexp.MustCompile(`\.getHost\s*\(`)},

	// Network response sources
	{"network", "Response.body", regexp.MustCompile(`\.body\s*\(\s*\)`)},
	{"network", "Response.string", regexp.MustCompile(`\.string\s*\(\s*\)`)},
	{"network", "InputStream.read", regexp.MustCompile(`InputStream[^;]*\.read\s*\(`)},
	{"network", "BufferedReader.readLine", regexp.MustCompile(`BufferedReader[^;]*\.readLine\s*\(`)},
	{"network", "HttpURLConnection.getInputStream", regexp.MustCompile(`\.getInputStream\s*\(`)},

	// File read sources
	{"file", "FileInputStream", regexp.MustCompile(`new\s+FileInputStream\s*\(`)},
	{"file", "ContentResolver.openInputStream", regexp.MustCompile(`\.openInputStream\s*\(`)},
	{"file", "SharedPreferences.getString", regexp.MustCompile(`\.getString\s*\([^)]+,\s*[^)]+\)`)},

	// Clipboard sources
	{"clipboard", "getPrimaryClip", regexp.MustCompile(`\.getPrimaryClip\s*\(`)},
	{"clipboard", "getText", regexp.MustCompile(`ClipboardManager[^;]*\.getText\s*\(`)},
}

// SourceCategories returns the canonical list of all source categories.
func SourceCategories() []string {
	return []string{"intent", "deeplink", "network", "file", "clipboard"}
}

// IngestToolResult parses a structured JSON tool response and populates the graph.
// action should match JADX API action names (e.g. "getClassWithStructure", "scanCrypto").
func (g *Graph) IngestToolResult(action string, rawJSON string) {
	if rawJSON == "" || rawJSON == "{}" || rawJSON == "null" {
		return
	}

	switch action {
	case "getClassWithStructure", "getClassCode", "getClassStructure":
		g.ingestClassInfo(action, rawJSON)
	case "getMethodWithCallers":
		g.ingestMethodWithCallers(rawJSON)
	case "getMethodCode":
		g.ingestMethodCode(rawJSON)
	case "getManifestDetail":
		g.ingestManifest(rawJSON)
	case "scanCrypto":
		g.ingestCryptoScan(rawJSON)
	case "smartSearch", "searchString", "searchClass", "searchMethod":
		g.ingestSearchResults(rawJSON)
	case "getXrefs":
		g.ingestXrefs(rawJSON)
	case "batchGetClassCode":
		g.ingestBatchClassCode(rawJSON)
	case "getAllClasses", "getMainAppClasses":
		g.ingestClassList(rawJSON)
	case "getApkOverview":
		g.ingestApkOverview(rawJSON)
	case "analyzeComponent":
		g.ingestComponentAnalysis(rawJSON)
	}
}

// IngestLLMOutput extracts findings, class names, exported components, and data flows
// from unstructured LLM text output (fallback for non-JSON content).
func (g *Graph) IngestLLMOutput(output string) {
	if output == "" {
		return
	}

	// Extract fully qualified class names
	classNames := classNameRe.FindAllString(output, 100)
	for _, cn := range classNames {
		if len(cn) >= 10 {
			g.EnsureClass(cn)
		}
	}

	// Extract exported components
	for _, match := range exportedRe.FindAllStringSubmatch(output, 50) {
		if len(match) >= 2 {
			comp := match[1]
			g.mu.Lock()
			if _, exists := g.Components[comp]; !exists {
				g.Components[comp] = &ComponentEntity{
					Name:     comp,
					Exported: true,
				}
			}
			g.mu.Unlock()
		}
	}

	// Extract security findings by severity
	for _, fp := range findingPatterns {
		for _, match := range fp.re.FindAllStringSubmatch(output, 10) {
			if len(match) >= 3 {
				desc := strings.TrimSpace(match[2])
				g.AddFinding(&Finding{
					Type:        "vulnerability",
					Severity:    fp.severity,
					Description: desc,
				})
			}
		}
	}

	// Extract data flows
	for _, match := range dataFlowRe.FindAllStringSubmatch(output, 20) {
		if len(match) >= 3 {
			df := &DataFlowChain{
				Source: TaintNode{Method: match[1]},
				Sink:   TaintNode{Method: match[2]},
			}
			if len(match) >= 4 && match[3] != "" {
				df.Sink = TaintNode{Method: match[3]}
				df.Waypoints = []TaintNode{{Method: match[2]}}
			}
			if len(match) >= 5 && match[4] != "" {
				df.Risk = match[4]
			}
			g.AddDataFlow(df)
		}
	}
}

// --- internal ingestion methods ---

func (g *Graph) ingestClassInfo(action, rawJSON string) {
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(rawJSON), &obj); err != nil {
		return
	}
	if _, hasErr := obj["error"]; hasErr {
		return
	}

	className, _ := obj["class_name"].(string)
	if className == "" {
		return
	}

	cls := g.EnsureClass(className)
	g.mu.Lock()
	defer g.mu.Unlock()

	if superClass, ok := obj["super_class"].(string); ok {
		cls.SuperClass = superClass
	}
	if ifaces, ok := obj["implements"].([]interface{}); ok {
		cls.Interfaces = toStringSlice(ifaces)
	}
	if methods, ok := obj["methods"].([]interface{}); ok {
		cls.Methods = extractMethodKeys(className, methods)
	}
	if fields, ok := obj["fields"].([]interface{}); ok {
		cls.Fields = toStringSlice(fields)
	}
	cls.SourceTool = action

	// Upgrade depth
	switch action {
	case "getClassWithStructure":
		if cls.Depth < StructureKnown {
			cls.Depth = StructureKnown
		}
	case "getClassCode":
		if cls.Depth < CodeAnalyzed {
			cls.Depth = CodeAnalyzed
		}
	}

	// Check for security-relevant code
	if code, ok := obj["code"].(string); ok {
		g.detectCodeFindings(className, code)
		g.detectSinks(className, "", code)
		g.detectSources(className, "", code)
	}
}

func (g *Graph) ingestMethodWithCallers(rawJSON string) {
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(rawJSON), &obj); err != nil {
		return
	}
	if _, hasErr := obj["error"]; hasErr {
		return
	}

	className, _ := obj["class_name"].(string)
	methodName, _ := obj["method_name"].(string)
	if className == "" || methodName == "" {
		return
	}

	// Update class depth to DeepAnalyzed
	cls := g.EnsureClass(className)
	g.mu.Lock()
	if cls.Depth < DeepAnalyzed {
		cls.Depth = DeepAnalyzed
	}
	g.mu.Unlock()

	method := g.EnsureMethod(className, methodName)
	g.mu.Lock()
	defer g.mu.Unlock()

	if sig, ok := obj["signature"].(string); ok {
		method.Signature = sig
	}
	if callers, ok := obj["callers"].([]interface{}); ok {
		method.Callers = toStringSlice(callers)
	}
}

func (g *Graph) ingestManifest(rawJSON string) {
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(rawJSON), &obj); err != nil {
		return
	}

	// Extract components
	if comps, ok := obj["components"].(map[string]interface{}); ok {
		for _, compType := range []string{"activities", "services", "receivers", "providers"} {
			arr, ok := comps[compType].([]interface{})
			if !ok {
				continue
			}
			typeName := strings.TrimSuffix(compType, "s") // activities -> activity
			for _, item := range arr {
				m, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				name, _ := m["name"].(string)
				if name == "" {
					continue
				}
				exported, _ := m["exported"].(bool)
				perm, _ := m["permission"].(string)

				var filters []string
				if ifArr, ok := m["intent_filters"].([]interface{}); ok {
					filters = toStringSlice(ifArr)
				}

				g.AddComponent(&ComponentEntity{
					Name:          name,
					Type:          typeName,
					Exported:      exported,
					IntentFilters: filters,
					Permission:    perm,
				})

				// Also register class
				cls := g.EnsureClass(name)
				g.mu.Lock()
				cls.IsExported = exported
				g.mu.Unlock()
			}
		}
	}

	// Extract package name
	if pkgName, ok := obj["package_name"].(string); ok && pkgName != "" {
		g.mu.Lock()
		g.PackageName = pkgName
		g.mu.Unlock()
	}

	// Ingest security findings from manifest analysis
	if findings, ok := obj["security_findings"].([]interface{}); ok {
		findingsJSON, _ := json.Marshal(findings)
		g.ingestSecurityFindings(string(findingsJSON))
	}

	// Ingest deep links
	if links, ok := obj["deep_links"].([]interface{}); ok {
		linksJSON, _ := json.Marshal(links)
		g.ingestDeepLinks(string(linksJSON))
	}
}

func (g *Graph) ingestCryptoScan(rawJSON string) {
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(rawJSON), &obj); err != nil {
		// Try as array
		var arr []interface{}
		if err2 := json.Unmarshal([]byte(rawJSON), &arr); err2 == nil {
			for _, item := range arr {
				if m, ok := item.(map[string]interface{}); ok {
					g.ingestCryptoItem(m)
				}
			}
		}
		return
	}

	// May have results array
	if results, ok := obj["results"].([]interface{}); ok {
		for _, item := range results {
			if m, ok := item.(map[string]interface{}); ok {
				g.ingestCryptoItem(m)
			}
		}
	}
}

func (g *Graph) ingestCryptoItem(m map[string]interface{}) {
	className, _ := m["class_name"].(string)
	if className == "" {
		className, _ = m["class"].(string)
	}
	if className != "" {
		g.EnsureClass(className)
	}

	severity, _ := m["severity"].(string)
	if severity == "" {
		severity = "medium"
	}
	category, _ := m["category"].(string)
	pattern, _ := m["pattern_matched"].(string)

	desc := fmt.Sprintf("crypto usage in %s", className)
	if pattern != "" {
		desc = fmt.Sprintf("crypto: %s in %s", pattern, className)
	} else if method, ok := m["method"].(string); ok {
		desc = fmt.Sprintf("crypto usage: %s in %s", method, className)
	}

	findingType := "crypto"
	if category != "" {
		findingType = category
	}

	g.AddFinding(&Finding{
		Type:        findingType,
		Severity:    severity,
		Description: desc,
		ClassRef:    className,
	})
}

func (g *Graph) ingestSearchResults(rawJSON string) {
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(rawJSON), &obj); err != nil {
		return
	}

	// Extract class names from results, classes, or references arrays
	for _, key := range []string{"results", "classes", "references"} {
		arr, ok := obj[key].([]interface{})
		if !ok {
			continue
		}
		for _, item := range arr {
			switch v := item.(type) {
			case string:
				if classNameRe.MatchString(v) && len(v) >= 10 {
					g.EnsureClass(v)
				}
			case map[string]interface{}:
				if cn, ok := v["class_name"].(string); ok && cn != "" {
					g.EnsureClass(cn)
				}
				if cn, ok := v["class"].(string); ok && cn != "" {
					g.EnsureClass(cn)
				}
			}
		}
	}
}

func (g *Graph) ingestXrefs(rawJSON string) {
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(rawJSON), &obj); err != nil {
		return
	}

	className, _ := obj["class_name"].(string)
	methodName, _ := obj["method_name"].(string)

	if className != "" && methodName != "" {
		method := g.EnsureMethod(className, methodName)
		if callers, ok := obj["callers"].([]interface{}); ok {
			g.mu.Lock()
			method.Callers = toStringSlice(callers)
			g.mu.Unlock()
		}
	}

	// Register all referenced classes
	for _, key := range []string{"callers", "references"} {
		if arr, ok := obj[key].([]interface{}); ok {
			for _, item := range arr {
				if s, ok := item.(string); ok && classNameRe.MatchString(s) {
					g.EnsureClass(s)
				}
				if m, ok := item.(map[string]interface{}); ok {
					if cn, ok := m["class_name"].(string); ok && cn != "" {
						g.EnsureClass(cn)
					}
				}
			}
		}
	}
}

func (g *Graph) ingestBatchClassCode(rawJSON string) {
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(rawJSON), &obj); err != nil {
		return
	}
	// batch result may be a map of class_name -> code or an array
	if results, ok := obj["results"].([]interface{}); ok {
		for _, item := range results {
			if m, ok := item.(map[string]interface{}); ok {
				if cn, ok := m["class_name"].(string); ok && cn != "" {
					cls := g.EnsureClass(cn)
					g.mu.Lock()
					if cls.Depth < CodeAnalyzed {
						cls.Depth = CodeAnalyzed
					}
					g.mu.Unlock()
				}
			}
		}
	}
}

func (g *Graph) ingestClassList(rawJSON string) {
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(rawJSON), &obj); err != nil {
		// Try as array of strings
		var arr []interface{}
		if err2 := json.Unmarshal([]byte(rawJSON), &arr); err2 == nil {
			for _, item := range arr {
				if s, ok := item.(string); ok && len(s) >= 5 {
					g.EnsureClass(s)
				}
			}
		}
		return
	}
	for _, key := range []string{"classes", "results"} {
		if arr, ok := obj[key].([]interface{}); ok {
			for _, item := range arr {
				switch v := item.(type) {
				case string:
					if len(v) >= 5 {
						g.EnsureClass(v)
					}
				case map[string]interface{}:
					if cn, ok := v["class_name"].(string); ok && cn != "" {
						g.EnsureClass(cn)
					}
					if cn, ok := v["name"].(string); ok && cn != "" {
						g.EnsureClass(cn)
					}
				}
			}
		}
	}
}

func (g *Graph) ingestApkOverview(rawJSON string) {
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(rawJSON), &obj); err != nil {
		return
	}
	if pkgName, ok := obj["package_name"].(string); ok && pkgName != "" {
		g.mu.Lock()
		g.PackageName = pkgName
		g.mu.Unlock()
	}
}

func (g *Graph) ingestMethodCode(rawJSON string) {
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(rawJSON), &obj); err != nil {
		return
	}

	className, _ := obj["class_name"].(string)
	methodName, _ := obj["method_name"].(string)
	if className == "" || methodName == "" {
		return
	}

	cls := g.EnsureClass(className)
	g.mu.Lock()
	if cls.Depth < CodeAnalyzed {
		cls.Depth = CodeAnalyzed
	}
	g.mu.Unlock()

	method := g.EnsureMethod(className, methodName)
	g.mu.Lock()
	if sig, ok := obj["method_signature"].(string); ok {
		method.Signature = sig
	}
	g.mu.Unlock()

	if code, ok := obj["code"].(string); ok {
		g.mu.Lock()
		g.detectCodeFindings(className, code)
		g.detectSinks(className, methodName, code)
		g.detectSources(className, methodName, code)
		g.mu.Unlock()
	}
}

func (g *Graph) ingestComponentAnalysis(rawJSON string) {
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(rawJSON), &obj); err != nil {
		return
	}

	componentName, _ := obj["component_name"].(string)
	if componentName == "" {
		return
	}

	// Ingest structure if present
	if structure, ok := obj["structure"].(map[string]interface{}); ok {
		structJSON, _ := json.Marshal(structure)
		g.ingestClassInfo("getClassStructure", string(structJSON))
	}

	// Ingest code if present
	cls := g.EnsureClass(componentName)
	if code, ok := obj["code"].(string); ok {
		g.mu.Lock()
		if cls.Depth < CodeAnalyzed {
			cls.Depth = CodeAnalyzed
		}
		g.detectCodeFindings(componentName, code)
		g.detectSinks(componentName, "", code)
		g.detectSources(componentName, "", code)
		g.mu.Unlock()
	}

	// Ingest manifest metadata
	if manifest, ok := obj["manifest"].(map[string]interface{}); ok {
		exported, _ := manifest["exported"].(bool)
		g.mu.Lock()
		cls.IsExported = exported
		g.mu.Unlock()

		g.AddComponent(&ComponentEntity{
			Name:     componentName,
			Exported: exported,
		})
	}
}

func (g *Graph) ingestSecurityFindings(rawJSON string) {
	var arr []interface{}
	if err := json.Unmarshal([]byte(rawJSON), &arr); err != nil {
		return
	}
	for _, item := range arr {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		severity, _ := m["severity"].(string)
		findingType, _ := m["type"].(string)
		desc, _ := m["description"].(string)
		if desc == "" {
			continue
		}
		g.AddFinding(&Finding{
			Type:        findingType,
			Severity:    severity,
			Description: desc,
		})
	}
}

func (g *Graph) ingestDeepLinks(rawJSON string) {
	var arr []interface{}
	if err := json.Unmarshal([]byte(rawJSON), &arr); err != nil {
		return
	}
	for _, item := range arr {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		component, _ := m["component"].(string)
		scheme, _ := m["scheme"].(string)
		host, _ := m["host"].(string)
		exported, _ := m["exported"].(string)

		desc := fmt.Sprintf("deep link: %s://%s (component=%s, exported=%s)", scheme, host, component, exported)
		if exported == "true" {
			g.AddFinding(&Finding{
				Type:        "deep_link",
				Severity:    "info",
				Description: desc,
				ClassRef:    component,
			})
		}
	}
}

// detectCodeFindings checks code for hardcoded secrets and weak crypto patterns.
// Runs with g.mu already held by caller.
func (g *Graph) detectCodeFindings(className, code string) {
	lower := strings.ToLower(code)

	// Hardcoded secrets
	hardcodedPatterns := []string{
		"password", "secret", "api_key", "apikey", "private_key",
		"-----begin", "jdbc:", "bearer ",
	}
	for _, pattern := range hardcodedPatterns {
		if strings.Contains(lower, pattern) {
			// Don't hold mu for AddFinding (it acquires mu itself)
			// We are called with mu held, so operate directly
			desc := fmt.Sprintf("potential hardcoded secret (%s) in %s", pattern, className)
			found := false
			for _, f := range g.Findings {
				if f.Description == desc {
					found = true
					break
				}
			}
			if !found {
				g.Findings = append(g.Findings, &Finding{
					ID:          len(g.Findings),
					Type:        "vulnerability",
					Severity:    "high",
					Description: desc,
					ClassRef:    className,
				})
			}
			break // one finding per class for hardcoded secrets
		}
	}

	// Weak crypto: ECB mode
	if strings.Contains(lower, "ecb") || strings.Contains(lower, "\"aes\"") {
		desc := fmt.Sprintf("potential weak crypto (ECB/bare AES) in %s", className)
		found := false
		for _, f := range g.Findings {
			if f.Description == desc {
				found = true
				break
			}
		}
		if !found {
			g.Findings = append(g.Findings, &Finding{
				ID:          len(g.Findings),
				Type:        "crypto",
				Severity:    "medium",
				Description: desc,
				ClassRef:    className,
			})
		}
	}
}

// --- helpers ---

func toStringSlice(arr []interface{}) []string {
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		switch s := v.(type) {
		case string:
			out = append(out, s)
		case map[string]interface{}:
			// Try common key patterns
			if name, ok := s["name"].(string); ok {
				out = append(out, name)
			} else {
				out = append(out, fmt.Sprintf("%v", s))
			}
		default:
			out = append(out, fmt.Sprintf("%v", v))
		}
	}
	return out
}

func extractMethodKeys(className string, methods []interface{}) []string {
	keys := make([]string, 0, len(methods))
	for _, m := range methods {
		switch v := m.(type) {
		case string:
			keys = append(keys, className+"#"+v)
		case map[string]interface{}:
			name, _ := v["name"].(string)
			if name != "" {
				keys = append(keys, className+"#"+name)
			}
		}
	}
	return keys
}

// detectSinks scans code for sink API patterns and marks matching methods in the graph.
// If methodName is non-empty, only the specified method is tagged; otherwise the pattern label is used as method id.
// Caller MUST hold g.mu (write lock) — operates directly on g.Methods.
func (g *Graph) detectSinks(className, methodName, code string) {
	if code == "" || className == "" {
		return
	}
	for _, sp := range sinkPatterns {
		if !sp.re.MatchString(code) {
			continue
		}
		mName := methodName
		if mName == "" {
			mName = sp.pattern
		}
		key := className + "#" + mName
		method, ok := g.Methods[key]
		if !ok {
			method = &MethodEntity{ClassName: className, MethodName: mName}
			g.Methods[key] = method
		}
		if !method.IsSink {
			method.IsSink = true
			method.SinkCategory = sp.category
			method.SinkPattern = sp.pattern
		}
	}
}

// detectSources scans code for external input source API patterns and marks matching methods.
// Symmetric to detectSinks. Caller MUST hold g.mu (write lock).
func (g *Graph) detectSources(className, methodName, code string) {
	if code == "" || className == "" {
		return
	}
	for _, sp := range sourcePatterns {
		if !sp.re.MatchString(code) {
			continue
		}
		mName := methodName
		if mName == "" {
			mName = sp.pattern
		}
		key := className + "#" + mName
		method, ok := g.Methods[key]
		if !ok {
			method = &MethodEntity{ClassName: className, MethodName: mName}
			g.Methods[key] = method
		}
		if !method.IsSource {
			method.IsSource = true
			method.SourceCategory = sp.category
			method.SourcePattern = sp.pattern
		}
	}
}
