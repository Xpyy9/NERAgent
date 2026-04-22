package tools

import (
	"net/http"
	"sync"
	"time"
)

// cachedEntry 缓存条目
type cachedEntry struct {
	result    string
	timestamp time.Time
}

type JadxClient struct {
	BaseURL    string `json:"jadxUrl"`
	HTTPClient *http.Client

	overviewMu    sync.RWMutex
	overviewCache string
	overviewDone  bool

	resultCache sync.Map // key: string (cacheKey), value: cachedEntry
}

type CodeInsightInput struct {
	Action    string `json:"action" jsonschema:"description=操作：getAllClasses(类名列表) / getClassCode(源码) / getClassWithStructure(推荐,结构+源码一次返回) / batchGetClassCode(批量获取多个类源码) / getMethodWithCallers(方法源码+调用者)"`
	Keyword   string `json:"keyword,omitempty" jsonschema:"description=模糊过滤关键字，仅 getAllClasses 生效"`
	CodeName  string `json:"code_name,omitempty" jsonschema:"description=目标类名或函数名，仅 getClassCode/getClassWithStructure 必填。支持：精确方法签名 / 类名.方法名 / 完整类名"`
	CodeNames string `json:"code_names,omitempty" jsonschema:"description=逗号分隔的类名列表(最多5个)，仅 batchGetClassCode 必填"`
	ClassName string `json:"class_name,omitempty" jsonschema:"description=全限定类名，getClassWithStructure/getMethodWithCallers 使用"`
	MethodName string `json:"method_name,omitempty" jsonschema:"description=方法短名，仅 getMethodWithCallers 必填"`
	Offset    int    `json:"offset,omitempty" jsonschema:"description=分页偏移量，默认0"`
	Limit     int    `json:"limit,omitempty" jsonschema:"description=单页数量，默认50，最大500"`
}

type ResourceExplorerInput struct {
	Action       string `json:"action" jsonschema:"description=操作：getManifestDetail(推荐,Manifest结构化解析) / searchResourceContent(资源文件关键词搜索) / getMainActivity(主入口) / getMainAppClasses(主包类列表) / getAllResourceNames(资源文件名) / getResourceFile(按行读取资源内容)"`
	Keyword      string `json:"keyword,omitempty" jsonschema:"description=关键字。getAllResourceNames 时模糊过滤文件名；searchResourceContent 时为搜索内容"`
	FileName     string `json:"file_name,omitempty" jsonschema:"description=资源文件名，getResourceFile/searchResourceContent 必填"`
	ContextLines int    `json:"context_lines,omitempty" jsonschema:"description=搜索上下文行数(默认3,最大10)，仅 searchResourceContent 可选"`
	StartLine    int    `json:"startLine,omitempty" jsonschema:"description=起始行号，仅 getResourceFile 可选，单次不超过250行"`
	EndLine      int    `json:"endLine,omitempty" jsonschema:"description=结束行号，配合 startLine 使用"`
	Offset       int    `json:"offset,omitempty" jsonschema:"description=分页偏移量，默认0"`
	Limit        int    `json:"limit,omitempty" jsonschema:"description=单页数量，默认50(getMainAppClasses默认100)，最大500"`
}

type SearchEngineInput struct {
	Action     string `json:"action" jsonschema:"description=操作：searchMethod(函数名搜索) / searchClass(类搜索) / searchString(字符串搜索) / scanCrypto(加密特征扫描) / smartSearch(智能搜索:先类名后字符串自动fallback)"`
	MethodName string `json:"method_name,omitempty" jsonschema:"description=函数名，仅 searchMethod 必填"`
	ClassName  string `json:"class_name,omitempty" jsonschema:"description=类名，仅 searchClass 必填"`
	Package    string `json:"package,omitempty" jsonschema:"description=包名过滤，仅 searchClass 可选"`
	SearchIn   string `json:"search_in,omitempty" jsonschema:"description=搜索范围：class_name(默认,快速) / code(深度搜索) / class_name,code(兼搜)"`
	Query      string `json:"query,omitempty" jsonschema:"description=搜索字符串，仅 searchString 必填"`
	Offset     int    `json:"offset,omitempty" jsonschema:"description=分页偏移量，默认0"`
	Limit      int    `json:"limit,omitempty" jsonschema:"description=单页数量，默认50，最大500"`
}

type XrefsInput struct {
	ClassName  string `json:"class_name" jsonschema:"description=必填，目标全限定类名"`
	MethodName string `json:"method_name,omitempty" jsonschema:"description=查询方法调用者时填写"`
	FieldName  string `json:"field_name,omitempty" jsonschema:"description=查询字段读写者时填写"`
	Offset     int    `json:"offset,omitempty" jsonschema:"description=分页偏移量，默认0"`
	Limit      int    `json:"limit,omitempty" jsonschema:"description=单页数量，默认50，最大500"`
}

type RefactorInput struct {
	Action       string `json:"action" jsonschema:"description=操作：renameClass / renameMethod / renameField / renameVariable / exportMapping"`
	ClassName    string `json:"class_name,omitempty" jsonschema:"description=全限定类名。renameClass时为旧类名，其他rename操作为目标所在类"`
	MethodName   string `json:"method_name,omitempty" jsonschema:"description=方法短名(如onCreate)。renameMethod时为要重命名的方法，renameVariable时为变量所在方法"`
	FieldName    string `json:"field_name,omitempty" jsonschema:"description=字段名，仅 renameField 必填"`
	VariableName string `json:"variable_name,omitempty" jsonschema:"description=局部变量名，仅 renameVariable 必填"`
	NewName      string `json:"new_name,omitempty" jsonschema:"description=新名称，除 exportMapping 外均必填"`
	Reg          string `json:"reg,omitempty" jsonschema:"description=寄存器编号，renameVariable 同名变量消歧义用"`
	Ssa          string `json:"ssa,omitempty" jsonschema:"description=SSA版本号，renameVariable 消歧义用"`
}

type SystemManagerInput struct {
	Action string `json:"action" jsonschema:"description=操作：systemStatus(健康检查) / clearCache(清理缓存+GC) / getApkOverview(APK概览,已预缓存)"`
}
