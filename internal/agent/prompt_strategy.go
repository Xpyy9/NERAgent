package agent

import (
	"NERAgent/internal/tools"
	"encoding/json"
	"fmt"
	"strings"
)

// ──────────────────────────────────────────
// 策略表内容（从 planPrompt.md 拆出）
// ──────────────────────────────────────────

const strategyGeneral = `## 通用分析路径

| 场景 | 切入工具链 | 核心思路 |
|------|-----------|---------|
| 功能定位 | getMainActivity → getClassWithStructure → getMethodWithCallers | 从主入口获取结构+源码，一次追踪方法调用者 |
| 安全审计 | getManifestDetail → smartSearch(敏感API) → getClassWithStructure | Manifest结构化解析(exported/权限/安全属性) → 智能搜索敏感API → 结构+源码一次获取 |
| 协议分析 | smartSearch(OkHttp/Retrofit) → getClassWithStructure(Interceptor) → getMethodWithCallers | 智能搜索定位 → 拦截器结构+源码 → 调用链追踪 |
| SDK分析 | getAllClasses(SDK包名前缀) → batchGetClassCode(核心类) → 分析初始化和回调 | 批量获取核心类源码，减少往返 |
| 混淆代码 | smartSearch(特征字符串) → getMethodWithCallers → refactor_code(rename) | 智能搜索定位 → 方法源码+调用者 → 重命名 |
`

const strategySecurity = `## 安全分析策略

| 分析类型 | 搜索关键字 | 检查要点 |
|---------|-----------|---------|
| 硬编码敏感信息 | searchString: password, secret, api_key, Bearer, -----BEGIN, jdbc: | static final String 密钥材料、SharedPreferences 明文凭证、BuildConfig 敏感值 |
| 签名算法 | scanCrypto + searchMethod: sign, signature, hmac, digest | 参数拼接 → 密钥获取 → 哈希/HMAC 计算 → 签名附加的完整流程 |
| 加密实现 | scanCrypto + searchMethod: encrypt, decrypt, cipher, AES, RSA | ECB模式、硬编码IV/密钥、弱密钥(DES/<128bit AES)、java.util.Random 代替 SecureRandom |
| Launch Anywhere | getManifestDetail → exported Activity 的 intent-filter | getParcelableExtra 获取 Intent 后直接 startActivity，无校验 |
| WebView 风险 | searchClass: WebView, WebViewClient | setJavaScriptEnabled+addJavascriptInterface、loadUrl 参数外部可控、文件协议攻击面 |
| 组件安全 | getManifestDetail → exported 组件 + 无 permission 保护 | Activity/Service/Receiver/Provider 各自的数据信任和权限校验 |
| 客户端 RCE | searchString: Runtime.getRuntime().exec, DexClassLoader, System.loadLibrary | 参数来源：0-click(Push/网络/Provider) vs 1-click(DeepLink/Intent/URL) |
| 任意文件读取 | searchMethod: openFile, openInputStream | ContentProvider.openFile 路径规范化(getCanonicalPath)、目录范围限制 |
`

const strategySpecial = `## 专项逆向策略

| 分析类型 | 搜索关键字 | 检查要点 |
|---------|-----------|---------|
| JSBridge 通信 | searchClass: JavascriptInterface, JsBridge, DSBridge; searchMethod: addJavascriptInterface, evaluateJavascript | @JavascriptInterface 方法是否校验来源 URL、是否暴露文件读写/命令执行/Intent 发送 |
| 第三方 SDK | getAllClasses: com.tencent/alibaba/baidu/umeng/firebase, cn.jpush, com.igexin, com.xiaomi.push | 初始化权限、数据回传、推送 SDK 消息处理链路(0-click 入口)、硬编码 appid/appkey |
| 自写算法 | searchMethod: encode, decode, transform, obfuscate, pack | 循环 XOR/位移→自写加密、256字节数组→S-Box、自定义64字符表→变种 Base64；用 getClassSmali 交叉验证 |
| Native SO | searchMethod: native; searchString: System.loadLibrary | Java→Native 参数传递中的敏感数据(密钥/明文)；SO 内部需 IDA/Ghidra 辅助 |
| 网络协议 | searchClass: OkHttpClient, Retrofit, HttpURLConnection; searchString: /api/, /v1/ | Interceptor 实现、请求签名/Token 注入、API base URL |
| 数据存储 | searchClass: SharedPreferences, SQLiteDatabase, Room | 明文存储敏感数据、SQL 拼接(注入风险)、MODE_WORLD_READABLE |
`

const strategyVuln = `## 扩展漏洞策略

| 漏洞类型 | 搜索关键字 | 判定条件 |
|---------|-----------|---------|
| DeepLink 滥用 | getManifestDetail → intent_filters 中的 data scheme | URI 参数直接拼入 loadUrl/startActivity/SQL 查询 |
| Fragment 注入 | searchString: PreferenceActivity | 未重写 isValidFragment() 或始终返回 true |
| PendingIntent 误用 | searchClass: PendingIntent | 空 Intent 基础 + 无 FLAG_IMMUTABLE + 隐式 Intent |
| Tapjacking | searchString: filterTouchesWhenObscured | 敏感页面(支付/授权)未设置触摸过滤 |
| allowBackup 泄露 | getManifestDetail → application.allowBackup | true 或缺省(API<31 默认 true)，未配置 fullBackupContent 排除 |
| 日志泄露 | searchString: Log.d, Log.v, System.out.println | release 代码中残留调试日志打印敏感信息 |
| 剪贴板泄露 | searchClass: ClipboardManager, ClipData | 敏感数据(密码/token)写入剪贴板 |
| SQL 注入 | searchMethod: rawQuery, execSQL | 字符串拼接而非参数化查询(?占位符)，尤其 exported Provider |
| ZipSlip | searchClass: ZipInputStream, ZipEntry | getName() 未校验 ../、未 getCanonicalPath 规范化 |
| 证书弱校验 | searchClass: X509TrustManager, HostnameVerifier | checkServerTrusted 空实现、verify 始终返回 true |
| Intent Scheme | searchString: intent://, Intent.parseUri | parseUri 后直接 startActivity 未过滤 |
| Task 劫持 | getManifestDetail → activities 的 taskAffinity + launchMode | taskAffinity 非空 + singleTask 组合(StrandHogg) |
`

// ──────────────────────────────────────────
// 策略匹配与动态组装
// ──────────────────────────────────────────

type strategyEntry struct {
	content  string
	keywords []string
}

var strategies = []strategyEntry{
	// 通用路径始终包含（keywords 为空）
	{strategyGeneral, nil},
	{strategySecurity, []string{
		"安全", "加密", "密钥", "硬编码", "sign", "encrypt", "crypto", "漏洞", "审计",
		"password", "secret", "rce", "webview", "launch anywhere", "签名",
	}},
	{strategySpecial, []string{
		"jsbridge", "sdk", "native", "so", "协议", "webview", "混淆", "算法",
		"okhttp", "retrofit", "网络", "存储", "sharepreference", "数据库",
	}},
	{strategyVuln, []string{
		"deeplink", "fragment", "pendingintent", "sql", "zip", "证书", "allowbackup",
		"日志", "inject", "劫持", "intent scheme", "tapjack", "剪贴板",
	}},
}

// BuildStrategySection 根据用户输入动态选择相关策略表
func BuildStrategySection(userInput string) string {
	input := strings.ToLower(userInput)
	var sb strings.Builder
	sb.WriteString("\n# 分析策略速查表\n\n")
	sb.WriteString(strategyGeneral) // 通用始终包含

	matched := false
	for _, s := range strategies[1:] { // 跳过通用
		for _, kw := range s.keywords {
			if strings.Contains(input, kw) {
				sb.WriteString("\n")
				sb.WriteString(s.content)
				matched = true
				break
			}
		}
	}

	// 无匹配时包含安全策略作为默认补充
	if !matched {
		sb.WriteString("\n")
		sb.WriteString(strategySecurity)
	}

	return sb.String()
}

// ──────────────────────────────────────────
// APK 特征画像：自动分类 + 重点方向
// ──────────────────────────────────────────

type apkClassifyRule struct {
	typeName   string
	permKeys   []string // permissions 关键词
	pkgKeys    []string // package_name 关键词
	focusAreas []string
}

var classifyRules = []apkClassifyRule{
	{
		typeName: "金融类",
		permKeys: []string{"FINGERPRINT", "USE_BIOMETRIC", "BIND_NFC_SERVICE"},
		pkgKeys:  []string{"pay", "wallet", "bank", "finance", "money"},
		focusAreas: []string{
			"加密实现（AES/RSA 密钥强度、ECB 模式、硬编码密钥）",
			"证书校验（X509TrustManager/HostnameVerifier 实现）",
			"密钥存储（KeyStore 使用、SharedPreferences 明文存储）",
			"Root/模拟器检测绕过",
			"签名算法（请求签名流程、HMAC 密钥来源）",
		},
	},
	{
		typeName: "社交类",
		permKeys: []string{"CAMERA", "CONTACTS", "RECORD_AUDIO", "READ_CALL_LOG"},
		pkgKeys:  []string{"chat", "message", "social", "im.", "messenger"},
		focusAreas: []string{
			"WebView/JSBridge 安全（addJavascriptInterface、URL 校验）",
			"文件上传/下载路径校验（目录穿越）",
			"隐私数据存储（聊天记录/通讯录明文存储）",
			"DeepLink 滥用（URI 参数注入）",
			"推送 SDK 消息处理（0-click 攻击面）",
		},
	},
	{
		typeName: "游戏类",
		permKeys: []string{"WAKE_LOCK", "VIBRATE"},
		pkgKeys:  []string{"game", "unity", "cocos", "unreal"},
		focusAreas: []string{
			"Native SO 接口（Java→Native 敏感数据传递）",
			"内购/支付校验逻辑",
			"反作弊/反调试检测",
			"本地存档文件保护",
		},
	},
	{
		typeName: "电商类",
		permKeys: []string{"ACCESS_FINE_LOCATION", "RECEIVE_SMS"},
		pkgKeys:  []string{"shop", "mall", "store", "commerce", "jd.", "taobao", "pinduoduo"},
		focusAreas: []string{
			"签名算法与请求防篡改",
			"WebView 混合页面安全（JSBridge 暴露面）",
			"DeepLink/Intent Scheme 组件跳转",
			"用户数据存储（token/session 明文）",
			"第三方 SDK 数据回传",
		},
	},
}

// BuildApkProfile 根据已缓存的 APK overview 自动分类并生成 prompt 段
func BuildApkProfile() string {
	overview := tools.GetCachedOverview()
	if overview == "" {
		return ""
	}

	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(overview), &obj); err != nil {
		return ""
	}

	pkgName, _ := obj["package_name"].(string)
	pkgLower := strings.ToLower(pkgName)

	// 提取权限列表
	var perms []string
	if permArr, ok := obj["permissions"].([]interface{}); ok {
		for _, p := range permArr {
			if s, ok := p.(string); ok {
				perms = append(perms, strings.ToUpper(s))
			}
		}
	}
	permStr := strings.Join(perms, " ")

	// 匹配规则
	var matched *apkClassifyRule
	bestScore := 0
	for i := range classifyRules {
		rule := &classifyRules[i]
		score := 0
		for _, kw := range rule.permKeys {
			if strings.Contains(permStr, kw) {
				score += 2
			}
		}
		for _, kw := range rule.pkgKeys {
			if strings.Contains(pkgLower, kw) {
				score += 3
			}
		}
		if score > bestScore {
			bestScore = score
			matched = rule
		}
	}

	if matched == nil || bestScore < 2 {
		return "" // 无法分类，不注入
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# APK 特征画像\n\n"))
	sb.WriteString(fmt.Sprintf("**APK 类型**: %s（包名: %s）\n\n", matched.typeName, pkgName))
	sb.WriteString("**建议重点分析方向**:\n")
	for _, area := range matched.focusAreas {
		sb.WriteString(fmt.Sprintf("- %s\n", area))
	}
	sb.WriteString("\n请根据以上 APK 类型特征，优先分析与该类型最相关的安全风险。\n")
	return sb.String()
}
