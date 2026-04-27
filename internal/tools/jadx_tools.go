package tools

import (
	"NERAgent/internal/config"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

// 全局 APK overview 缓存，供 agent 包 APK 分类使用
var globalOverview atomic.Value // stores string

// GetCachedOverview 返回已缓存的 APK overview JSON（可能为空）
func GetCachedOverview() string {
	if v := globalOverview.Load(); v != nil {
		return v.(string)
	}
	return ""
}

func NewJadxClient(cfg *config.JADXConfig) (*JadxClient, error) {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "http://localhost:13997"
	}
	return &JadxClient{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: cfg.HTTPTimeout,
		},
		resultCache: NewLRUCache(cfg.CacheSize, cfg.CacheTTL),
	}, nil
}

// 参数构建器：简化重复的 params 构建逻辑
type paramBuilder map[string]string

func newParams(action string) paramBuilder {
	return paramBuilder{"action": action}
}

func newParamsRaw() paramBuilder {
	return paramBuilder{}
}

func (p paramBuilder) str(key, val string) paramBuilder {
	if val != "" {
		p[key] = val
	}
	return p
}

func (p paramBuilder) num(key string, val int) paramBuilder {
	if val > 0 {
		p[key] = strconv.Itoa(val)
	}
	return p
}

// mutatingActions 非幂等操作，不缓存且触发缓存清理
var mutatingActions = map[string]bool{
	"renameClass":    true,
	"renameMethod":   true,
	"renameField":    true,
	"renameVariable": true,
	"clearCache":     true,
	"exportMapping":  true,
}

// actionMaxLen 按 action 差异化响应截断长度（仅超过 20000 字符才截断）
var actionMaxLen = map[string]int{
	"getClassCode":          20000,
	"getClassWithStructure": 20000,
	"batchGetClassCode":     20000,
	"getMethodWithCallers":  20000,
	"getMethodCode":         20000,
	"analyzeComponent":      20000,
	"getAllClasses":          20000,
	"getMainAppClasses":     20000,
	"getXrefs":              20000,
	"searchString":          20000,
	"searchClass":           20000,
	"searchMethod":          20000,
	"scanCrypto":            20000,
	"smartSearch":           20000,
	"getClassStructure":     20000,
	"getClassSmali":         20000,
	"getResourceFile":       20000,
	"getManifestDetail":     20000,
	"searchResourceContent": 20000,
}

// cacheKey 生成缓存 key：path + 排序后的参数
func cacheKey(path string, params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	sb.WriteString(path)
	for _, k := range keys {
		sb.WriteByte('|')
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(params[k])
	}
	return sb.String()
}

// invalidateResultCache clears all result cache entries.
func (jc *JadxClient) invalidateResultCache() {
	jc.resultCache.Invalidate()
}

// Jadx统一请求函数（含缓存）
func (jc *JadxClient) callJadxAPI(ctx context.Context, path string, params map[string]string) (string, error) {
	action := params["action"]
	// Xrefs 路径无 action 参数，用路径推导
	if action == "" && strings.Contains(path, "Xrefs") {
		action = "getXrefs"
	}

	// 非幂等操作：清空缓存
	if mutatingActions[action] {
		jc.invalidateResultCache()
	}

	// 幂等查询：检查缓存
	if !mutatingActions[action] {
		ck := cacheKey(path, params)
		if cached, ok := jc.resultCache.Get(ck); ok {
			return cached, nil
		}
	}

	reqURL, err := url.Parse(jc.BaseURL)
	if err != nil {
		return "", fmt.Errorf("[-]解析BaseURL失败: %v", err)
	}
	reqURL = reqURL.JoinPath(path)
	query := reqURL.Query()
	for k, v := range params {
		if v != "" {
			query.Add(k, v)
		}
	}
	reqURL.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return "", fmt.Errorf("[-]创建请求失败: %v", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := jc.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("[-]请求Jadx后端失败: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			// 忽略关闭错误
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("[-]读取响应数据失败: %v", err)
	}
	if resp.StatusCode >= 400 {
		return fmt.Sprintf(`{"error":"Jadx returned HTTP %d","detail":%q,"hint":"%s"}`,
			resp.StatusCode, string(body), errorHint(resp.StatusCode)), nil
	}

	// 拦截 HTTP 202：异步任务，自动轮询直到完成
	if resp.StatusCode == 202 {
		result, err := jc.waitForAsyncResult(ctx, body)
		if err != nil {
			return "", err
		}
		truncated := truncateResultByAction(result, action)
		// 缓存异步结果
		if !mutatingActions[action] {
			jc.resultCache.Set(cacheKey(path, params), truncated)
		}
		return truncated, nil
	}

	truncated := truncateResultByAction(string(body), action)

	// 缓存成功结果
	if !mutatingActions[action] {
		jc.resultCache.Set(cacheKey(path, params), truncated)
	}

	return truncated, nil
}

// truncateResultByAction 按 action 差异化截断响应
func truncateResultByAction(result string, action string) string {
	maxLen := 20000 // 默认截断阈值
	if limit, ok := actionMaxLen[action]; ok {
		maxLen = limit
	}
	if len(result) > maxLen {
		return result[:maxLen] + fmt.Sprintf("\n\n...[响应已截断，原始长度 %d 字符，仅保留前 %d 字符]", len(result), maxLen)
	}
	return result
}

// waitForAsyncResult 拦截 202 响应，Go 层自动轮询 taskStatus 直到 SUCCESS/FAILED
func (jc *JadxClient) waitForAsyncResult(ctx context.Context, rawResp []byte) (string, error) {
	var taskResp struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(rawResp, &taskResp); err != nil || taskResp.TaskID == "" {
		// 无法解析 task_id，回退返回原始内容
		return string(rawResp), nil
	}

	const pollInterval = 5 * time.Second
	const maxPolls = 18 // ~90s 超时，与 HTTP client timeout 对齐

	for i := 0; i < maxPolls; i++ {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("context cancelled while polling task %s", taskResp.TaskID)
		case <-time.After(pollInterval):
		}

		result, err := jc.pollTaskStatus(ctx, taskResp.TaskID)
		if err != nil {
			continue
		}

		var status struct {
			Status string `json:"status"`
			Error  string `json:"error"`
		}
		if err := json.Unmarshal([]byte(result), &status); err != nil {
			continue
		}

		switch status.Status {
		case "SUCCESS":
			return result, nil
		case "FAILED":
			return "", fmt.Errorf("async task %s failed: %s", taskResp.TaskID, status.Error)
		default:
			// RUNNING → continue polling
		}
	}
	return "", fmt.Errorf("async task %s timed out after %d polls (~%d min)", taskResp.TaskID, maxPolls, maxPolls*int(pollInterval.Seconds())/60)
}

// pollTaskStatus 直接发 HTTP 请求查询任务状态，不走 callJadxAPI 避免递归
func (jc *JadxClient) pollTaskStatus(ctx context.Context, taskID string) (string, error) {
	reqURL, err := url.Parse(jc.BaseURL)
	if err != nil {
		return "", err
	}
	reqURL = reqURL.JoinPath("/systemManager")
	query := reqURL.Query()
	query.Set("action", "taskStatus")
	query.Set("task_id", taskID)
	reqURL.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := jc.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("taskStatus returned %d: %s", resp.StatusCode, string(body))
	}
	return string(body), nil
}

func (jc *JadxClient) CodeInsightSkill(ctx context.Context, input *CodeInsightInput) (string, error) {
	params := newParams(input.Action).
		str("keyword", input.Keyword).
		str("code_name", input.CodeName).
		str("code_names", input.CodeNames).
		str("class_name", input.ClassName).
		str("method_name", input.MethodName).
		num("offset", input.Offset).
		num("limit", input.Limit)
	return jc.callJadxAPI(ctx, "/codeInsight", params)
}

func (jc *JadxClient) ResourceExplorerSkill(ctx context.Context, input *ResourceExplorerInput) (string, error) {
	params := newParams(input.Action).
		str("keyword", input.Keyword).
		str("file_name", input.FileName).
		str("component_name", input.ComponentName).
		num("context_lines", input.ContextLines).
		num("startLine", input.StartLine).
		num("endLine", input.EndLine).
		num("offset", input.Offset).
		num("limit", input.Limit)
	return jc.callJadxAPI(ctx, "/resourceExplorer", params)
}

func (jc *JadxClient) SearchEngineSkill(ctx context.Context, input *SearchEngineInput) (string, error) {
	params := newParams(input.Action).
		str("method_name", input.MethodName).
		str("class_name", input.ClassName).
		str("package", input.Package).
		str("search_in", input.SearchIn).
		str("query", input.Query).
		num("offset", input.Offset).
		num("limit", input.Limit)
	return jc.callJadxAPI(ctx, "/searchEngine", params)
}

func (jc *JadxClient) XrefsSkill(ctx context.Context, input *XrefsInput) (string, error) {
	// 注意：这里的 URL query key 必须与 Java 后端严格对应 (class, method, field)
	params := newParamsRaw().
		str("class", input.ClassName).
		str("method", input.MethodName).
		str("field", input.FieldName).
		num("offset", input.Offset).
		num("limit", input.Limit)
	return jc.callJadxAPI(ctx, "/getXrefs", params)
}

func (jc *JadxClient) RefactorSkill(ctx context.Context, input *RefactorInput) (string, error) {
	params := newParams(input.Action).
		str("class_name", input.ClassName).
		str("method_name", input.MethodName).
		str("field_name", input.FieldName).
		str("variable_name", input.VariableName).
		str("new_name", input.NewName).
		str("reg", input.Reg).
		str("ssa", input.Ssa)
	return jc.callJadxAPI(ctx, "/refactor", params)
}

func (jc *JadxClient) SystemManagerSkill(ctx context.Context, input *SystemManagerInput) (string, error) {
	if input.Action == "getApkOverview" {
		return jc.getApkOverviewCached(ctx)
	}
	if input.Action == "clearCache" {
		jc.invalidateOverviewCache()
		jc.invalidateResultCache()
	}
	params := newParams(input.Action)
	return jc.callJadxAPI(ctx, "/systemManager", params)
}

// getApkOverviewCached 进程级缓存 getApkOverview 结果
func (jc *JadxClient) getApkOverviewCached(ctx context.Context) (string, error) {
	jc.overviewMu.RLock()
	if jc.overviewDone {
		result := jc.overviewCache
		jc.overviewMu.RUnlock()
		return result, nil
	}
	jc.overviewMu.RUnlock()

	jc.overviewMu.Lock()
	defer jc.overviewMu.Unlock()
	if jc.overviewDone { // double-check
		return jc.overviewCache, nil
	}

	result, err := jc.callJadxAPI(ctx, "/systemManager",
		map[string]string{"action": "getApkOverview"})
	if err != nil {
		return "", err // 不缓存失败结果
	}
	jc.overviewCache = result
	jc.overviewDone = true
	globalOverview.Store(result) // 供 APK 分类使用
	return result, nil
}

// invalidateOverviewCache 清除 APK overview 缓存
func (jc *JadxClient) invalidateOverviewCache() {
	jc.overviewMu.Lock()
	jc.overviewDone = false
	jc.overviewCache = ""
	jc.overviewMu.Unlock()
}

// errorHint 根据 HTTP 状态码给 LLM 提供简短的修正建议
func errorHint(code int) string {
	switch {
	case code == 400:
		return "参数无效，检查 action/参数拼写后换参数重试"
	case code == 404:
		return "目标不存在，用 searchClass 或 getAllClasses 重新定位，或跳过此步继续下一步"
	case code == 500:
		return "服务器内部错误，可尝试 clearCache 后重试一次"
	default:
		return "服务异常，跳过此步继续分析"
	}
}

func (jc *JadxClient) BuildJadxTools() ([]tool.BaseTool, error) {
	t1, err := utils.InferTool(
		"code_insight",
		"代码洞察工具。逆向分析核心能力，支持以下操作：\n"+
			"- getAllClasses: 搜索类名列表\n"+
			"- getClassCode: 获取反编译源码（支持精确方法签名/类名.方法名/完整类名）\n"+
			"- getClassStructure: 类结构摘要（字段含类型和access修饰符、方法含签名和access修饰符、继承关系）\n"+
			"- getClassSmali: Smali 字节码\n"+
			"- getClassWithStructure: **推荐** 一次返回类结构+完整源码，减少调用次数\n"+
			"- batchGetClassCode: 批量获取最多5个类的源码（code_names逗号分隔）\n"+
			"- getMethodWithCallers: 获取方法源码+调用者列表（结构化对象含class_name/method_name/method_signature）\n"+
			"- getMethodCode: **新** 获取单个方法反编译代码，无需拉取整个类（需class_name+method_name）\n"+
			"建议优先使用 getClassWithStructure 替代分别调用 getClassStructure + getClassCode。",
		jc.CodeInsightSkill,
	)
	if err != nil {
		return nil, err
	}
	t2, err := utils.InferTool(
		"resource_explorer",
		"资源探测器。用于获取 Manifest 安全信息、资源文件检索、主入口 Activity 等。\n"+
			"- getManifestDetail: **推荐** Manifest 结构化解析，一次返回所有组件(含exported/intent-filter/meta-data)、权限、application安全属性(debuggable/allowBackup等)、security_findings（自动检测安全问题）、deep_links（聚合所有深度链接）\n"+
			"- analyzeComponent: **新** 组件一站式分析，传入component_name一次返回Manifest元数据+类结构+反编译代码，替代3次独立调用\n"+
			"- searchResourceContent: 资源文件关键词搜索，在指定文件内检索匹配行+上下文\n"+
			"- getMainActivity: 主入口 Activity（返回类名+源码）\n"+
			"- getMainAppClasses: 主包下类列表\n"+
			"- getAllResourceNames: 搜索资源文件名\n"+
			"- getResourceFile: 按行范围读取资源文件原文",
		jc.ResourceExplorerSkill,
	)
	if err != nil {
		return nil, err
	}
	t3, err := utils.InferTool(
		"search_engine",
		"全局搜索引擎。后端自动等待所有搜索结果。搜索已优化：CodeIndex就绪时同步返回（毫秒级），未就绪时自动异步构建。\n"+
			"- searchMethod：函数名搜索，返回结构化结果（class_name/method_name/method_signature/access_flags）\n"+
			"- searchClass：类搜索。search_in=class_name（默认，快速类名匹配）；search_in=code（代码内容深度检索）\n"+
			"- searchString：全局字符串搜索\n"+
			"- scanCrypto：安全扫描（5大类：weak_crypto/hardcoded_secrets/ssl_tls/webview/data_leakage），返回含severity/category的结构化结果\n"+
			"- smartSearch：**推荐** 智能搜索，先快速类名匹配，无结果自动 fallback 到字符串搜索",
		jc.SearchEngineSkill,
	)
	if err != nil {
		return nil, err
	}
	t4, err := utils.InferTool(
		"get_call_chain",
		"调用链/交叉引用追踪工具（Xrefs）。逆向分析中寻找线索的核心能力。传入 class_name 查询类被谁引用，加 method_name 查询方法被谁调用，加 field_name 查询字段被谁读写。优先级：field > method > class。结果超过 2000 条时会自动截断并附加溢出警告，此时应缩小查询范围（指定更精确的 method 或 field）。",
		jc.XrefsSkill,
	)
	if err != nil {
		return nil, err
	}
	t5, err := utils.InferTool(
		"refactor_code",
		"逆向重构工具。当你分析混淆代码并推断出标识符真实含义后，使用此工具重命名类/方法/字段/局部变量。重要：所有 rename 操作执行后会自动清空缓存。Rename 改的是 JADX 显示名称，不改变 APK 实际字节码。renameVariable 支持 reg（寄存器编号）和 ssa（SSA 版本号）参数消歧义。exportMapping 导出格式为 {\"新名称\":\"原始混淆名\"}，编写 Frida/Xposed 脚本时必须用 value（原始混淆名）。",
		jc.RefactorSkill,
	)
	if err != nil {
		return nil, err
	}
	t6, err := utils.InferTool(
		"system_manager",
		"系统管理工具。核心用途：\n"+
			"1. 健康检查（推荐首步）：'systemStatus' 确认 health.status==\"UP\" 且 decompiler_ready==true，内存 usage_percent>85% 时建议先 clearCache。\n"+
			"2. APK 概览（已预加载缓存，毫秒级响应）：'getApkOverview' 获取包名、版本、权限声明、四大组件列表、SDK 版本等全貌信息，是制定分析策略的首要侦查手段。\n"+
			"3. 内存管理：'clearCache' 强制清理所有缓存（代码索引+搜索缓存+资源缓存）并触发 GC。大量 rename 操作后、内存不足时、或搜索结果异常时调用。清理后首次搜索会重建代码索引。",
		jc.SystemManagerSkill,
	)
	if err != nil {
		return nil, err
	}

	// 异步预热搜索索引，触发 Jadx 端 CodeIndexManager 构建
	go func() {
		time.Sleep(5 * time.Second)
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		_, _ = jc.callJadxAPI(ctx, "/searchEngine", map[string]string{
			"action": "searchString", "query": "MainActivity", "limit": "1",
		})
	}()

	return []tool.BaseTool{t1, t2, t3, t4, t5, t6}, nil
}
