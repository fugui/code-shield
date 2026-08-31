package rules

// ThreadGovernanceRule 线程调度组治理规则
type ThreadGovernanceRule struct {
	ForbiddenPattern string
	Suggestion       string
	Severity         string
}

// BuiltinThreadRules 内置线程治理规则库
var BuiltinThreadRules = []ThreadGovernanceRule{
	{
		ForbiddenPattern: "std::thread",
		Suggestion:       "禁止直接创建裸线程 std::thread，必须使用平台统一的 DispatchGroup / ThreadPool 进行纳管调度",
		Severity:         "高",
	},
	{
		ForbiddenPattern: "pthread_create",
		Suggestion:       "禁止直接调用 POSIX pthread_create，必须使用平台统一的调度组机制",
		Severity:         "高",
	},
	{
		ForbiddenPattern: "QThread",
		Suggestion:       "禁止直接继承或实例化 QThread，优先使用平台异步工作线程池",
		Severity:         "高",
	},
}

// UnorderedCollectionRule 无序集合保序规则
type UnorderedCollectionRule struct {
	TypePattern string
	Language    string
	Suggestion  string
}

// BuiltinUnorderedRules 多语言无序集合排查规则库
var BuiltinUnorderedRules = []UnorderedCollectionRule{
	{
		TypePattern: "std::unordered_map",
		Language:    "cpp",
		Suggestion:  "在签名计算、RPC 序列化或数据哈希场景下，严禁使用无序哈希表，请改用 std::map 或按 Key 排序后再导出",
	},
	{
		TypePattern: "map[",
		Language:    "go",
		Suggestion:  "Go map 遍历顺序具有随机性，在序列化、签名或哈希时必须提取 Key 显式排序后输出",
	},
	{
		TypePattern: "HashSet",
		Language:    "java",
		Suggestion:  "Java HashSet 为无序集合，在涉及外部协议导出或数据指纹计算时改用 LinkedHashSet 或 TreeSet",
	},
	{
		TypePattern: "set(",
		Language:    "python",
		Suggestion:  "Python set 为无序集合，在保证输出确定性场景下请使用 sorted(set)",
	},
}

// FloatComparisonRule 浮点比较规则
type FloatComparisonRule struct {
	ForbiddenOp string
	Suggestion  string
}

// BuiltinFloatRules 浮点精度比较规则库
var BuiltinFloatRules = []FloatComparisonRule{
	{
		ForbiddenOp: "==",
		Suggestion:  "浮点数由于 IEEE 754 精度损失严禁直接使用 == 比较，请使用 fabs(a - b) < EPSILON 进行容差比较",
	},
	{
		ForbiddenOp: "!=",
		Suggestion:  "浮点数严禁直接使用 != 比较，请使用 fabs(a - b) >= EPSILON 进行容差判断",
	},
}

// UTEffectivenessRule 单测有效性规则
type UTEffectivenessRule struct {
	AntiPattern string
	Category    string
	Suggestion  string
}

// BuiltinUTRules 单测质量与有效性规则库
var BuiltinUTRules = []UTEffectivenessRule{
	{
		AntiPattern: "EmptyAssertion",
		Category:    "无效单测",
		Suggestion:  "测试用例中无任何 ASSERT 或 EXPECT 断言，属于空跑用例，需补充状态或返回值断言",
	},
	{
		AntiPattern: "TautologicalAssert",
		Category:    "永真断言",
		Suggestion:  "断言条件恒为真（如 assert(1 == 1)），无法验证被测函数真实逻辑，需修正断言目标",
	},
	{
		AntiPattern: "TimeSleepInTest",
		Category:    "Flaky 隐患",
		Suggestion:  "单测中包含 hardcoded time.Sleep()，易受宿主机负载影响引发偶发失败，请改用信号量或条件等待轮询",
	},
}
