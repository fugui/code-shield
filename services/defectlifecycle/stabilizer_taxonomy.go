package defectlifecycle

import (
	"regexp"
	"strings"
)

// 受控主类枚举常量定义（覆盖全量 10 类内置扫描任务语义，消灭自由文本漂移）
const (
	ClassNPD             = "NPD"           // 空指针解引用 / 未判空 (CWE-476, CWE-690)
	ClassMemLeak         = "MEM_LEAK"      // 堆内存与动态对象泄漏 (CWE-401)
	ClassResLeak         = "RES_LEAK"      // 文件句柄/套接字/RAII资源泄漏 (CWE-775)
	ClassUAF             = "UAF"           // 释放后使用 / 悬空(垂/挂)指针与引用 (CWE-416)
	ClassDoubleFree      = "DBL_FREE"      // 重复/双重释放 (CWE-415)
	ClassOOB             = "OOB"           // 缓冲区溢出 / 数组越界 / 栈溢出 / 内存破坏 (CWE-119, CWE-125, CWE-787)
	ClassDataRace        = "DATA_RACE"     // 并发数据竞争 (CWE-362)
	ClassDeadlock        = "DEADLOCK"      // 死锁风险 (CWE-833)
	ClassSignalSafe      = "SIGNAL_SAFE"   // 异步信号处理不安全 / 非可重入调用 (CWE-364, CWE-479)
	ClassIntOver         = "INT_OVER"      // 整数溢出 / 截断 (CWE-190)
	ClassUninit          = "UNINIT"        // 未初始化变量/内存使用 (CWE-457)
	ClassSecInject       = "SEC_INJECT"    // 注入 / 敏感硬编码等安全漏洞 (CWE-78, CWE-89, CWE-798)
	ClassLogicRisk       = "LOGIC_RISK"    // 逻辑漏洞 / 边界防御 / 回归风险 / 除零 / 健壮性
	ClassErrHandle       = "ERR_HANDLE"    // 异常控制 / 未捕获异常 / 返回值未检查 (CWE-252, CWE-391)
	ClassConcurrency     = "CONCUR"        // 并发安全 / 锁同步不当 / 跨线程限制
	ClassTypeConfuse     = "TYPE_CONF"     // 类型混淆 / 不安全转型 (CWE-843)
	ClassLifecycle       = "LIFECYCLE"     // 生命周期管理 / join 遗漏 / 回调销毁竞争
	ClassThreadPolicy    = "THREAD_POLICY" // 线程创建必要性 / 平台调度与定时器复用
	ClassFloatCmp        = "FLOAT_CMP"     // 浮点数比较精度与收敛误判
	ClassOrderDep        = "ORDER_DEP"     // 无序集合顺序稳定性风险
	ClassObservability   = "OBSERVABILITY" // 可观测性日志缺失 / 敏感日志暴露
	ClassInterfaceCompat = "IFACE_COMPAT"  // 接口破坏性变更 / 上下游参数失配
	ClassPerformance     = "PERFORMANCE"   // 运行效率 / CPU 与 I/O 瓶颈
	ClassMaintainability = "CODE_QUALITY"  // 质量控制 / 复杂度 / 可维护性与冗余坏味道
	ClassArchitecture    = "ARCHITECTURE"  // 架构高耦合 / 职责违背 / 三方依赖
	ClassUTEffect        = "UT_EFFECT"     // 单元测试断言有效性 / 提前返回 / 功能单一性
	ClassPass            = "PASS"          // 评估显式通过（无问题）
	ClassOther           = "OTHER"         // 兜底主类 (仅在明确其它类别或无规则命中时使用)
)

type TaxonomyRule struct {
	Pattern *regexp.Regexp
	Class   string
}

// 分类归约规则引擎：确定性优先级匹配（显式兜底 -> PASS -> CWE -> 细粒度专项关键词 -> 领域粗类）
var taxonomyRules = []TaxonomyRule{
	// 0. 显式“其它/其他”兜底类与无问题评估先行收敛，避免误命中子关键词
	{Pattern: regexp.MustCompile(`^(?i)(其它|其他|other$|uncategorized$)`), Class: ClassOther},
	{Pattern: regexp.MustCompile(`^无问题$`), Class: ClassPass},

	// 1. CWE 显式编号强映射
	{Pattern: regexp.MustCompile(`(?i)\bCWE-(476|690)\b`), Class: ClassNPD},
	{Pattern: regexp.MustCompile(`(?i)\bCWE-401\b`), Class: ClassMemLeak},
	{Pattern: regexp.MustCompile(`(?i)\bCWE-775\b`), Class: ClassResLeak},
	{Pattern: regexp.MustCompile(`(?i)\bCWE-416\b`), Class: ClassUAF},
	{Pattern: regexp.MustCompile(`(?i)\bCWE-415\b`), Class: ClassDoubleFree},
	{Pattern: regexp.MustCompile(`(?i)\bCWE-(119|120|125|787)\b`), Class: ClassOOB},
	{Pattern: regexp.MustCompile(`(?i)\bCWE-362\b`), Class: ClassDataRace},
	{Pattern: regexp.MustCompile(`(?i)\bCWE-833\b`), Class: ClassDeadlock},
	{Pattern: regexp.MustCompile(`(?i)\bCWE-(364|479)\b`), Class: ClassSignalSafe},
	{Pattern: regexp.MustCompile(`(?i)\bCWE-(78|89|77|798)\b`), Class: ClassSecInject},
	{Pattern: regexp.MustCompile(`(?i)\bCWE-190\b`), Class: ClassIntOver},
	{Pattern: regexp.MustCompile(`(?i)\bCWE-457\b`), Class: ClassUninit},
	{Pattern: regexp.MustCompile(`(?i)\bCWE-(252|391)\b`), Class: ClassErrHandle},
	{Pattern: regexp.MustCompile(`(?i)\bCWE-843\b`), Class: ClassTypeConfuse},

	// 2. 信号处理安全专项
	{Pattern: regexp.MustCompile(`(?i)signal.?handler|信号处理|异步信号|async.?signal|非重入`), Class: ClassSignalSafe},

	// 3. 精确子类与中英文核心术语（细粒度词先于粗粒度类别前缀）
	{Pattern: regexp.MustCompile(`(?i)use.?after.?free|释放后使用|dangling.?pointer|悬空指针|悬垂指针|悬挂指针|悬空引用|野指针|释放与访问竞态`), Class: ClassUAF},
	{Pattern: regexp.MustCompile(`(?i)null.?pointer|空指针|未判空|null.?deref|NPD|NPE`), Class: ClassNPD},
	{Pattern: regexp.MustCompile(`(?i)double.?free|重复释放|二次释放|双重释放`), Class: ClassDoubleFree},
	{Pattern: regexp.MustCompile(`(?i)memor?y?.?leak|内存泄[漏露]|堆内存.*未释放|动态内存.*未释放|智能指针|cjson.*(?:未释放|泄漏)`), Class: ClassMemLeak},
	{Pattern: regexp.MustCompile(`(?i)resource.?leak|资源泄[漏露]|资源.*未释放|文件.*未关|fd.?leak|句柄.*未释放|raii`), Class: ClassResLeak},
	{Pattern: regexp.MustCompile(`(?i)out.?of.?bound|buffer.?overflow|数组越界|缓冲区溢出|OOB|下标越界|越界读|越界写|越界访问|栈溢出|内存破坏`), Class: ClassOOB},
	{Pattern: regexp.MustCompile(`(?i)data.?race|数据竞争|并发竞争|竞态|race.?condition`), Class: ClassDataRace},
	{Pattern: regexp.MustCompile(`(?i)deadlock|死锁`), Class: ClassDeadlock},
	{Pattern: regexp.MustCompile(`(?i)integer.?overflow|整数溢出|算术溢出`), Class: ClassIntOver},
	{Pattern: regexp.MustCompile(`(?i)uninit|未初始化|uninitialized`), Class: ClassUninit},
	{Pattern: regexp.MustCompile(`(?i)inject|注入|sql.?inject|command.?inject|xss|敏感硬编码|hardcod`), Class: ClassSecInject},
	{Pattern: regexp.MustCompile(`(?i)error.?handle|异常处理|异常安全|异常控制|未检查返回值|check.?return|未捕获异常|致命退出|析构崩溃`), Class: ClassErrHandle},
	{Pattern: regexp.MustCompile(`(?i)type.?confus|类型混淆|unsafe.?cast`), Class: ClassTypeConfuse},
	{Pattern: regexp.MustCompile(`(?i)thread.?safe|并发同步|线程安全|并发安全|锁同步|跨线程`), Class: ClassConcurrency},
	{Pattern: regexp.MustCompile(`(?i)生命周期|join遗漏|未安全退出|回调对象销毁`), Class: ClassLifecycle},
	{Pattern: regexp.MustCompile(`(?i)频繁创建|未使用平台调度组|未使用平台定时器`), Class: ClassThreadPolicy},
	{Pattern: regexp.MustCompile(`(?i)浮点|阈值误判|收敛控制误判|数据匹配与统计误判|非关键指标比较`), Class: ClassFloatCmp},
	{Pattern: regexp.MustCompile(`(?i)顺序不稳定|键值索引错位|有序输入被重排|顺序依赖`), Class: ClassOrderDep},
	{Pattern: regexp.MustCompile(`(?i)可观测性|关键分支日志缺失|敏感日志暴露`), Class: ClassObservability},
	{Pattern: regexp.MustCompile(`(?i)接口兼容性|破坏性变更|参数失配`), Class: ClassInterfaceCompat},
	{Pattern: regexp.MustCompile(`(?i)性能与运行效率|cpu与i/o瓶颈`), Class: ClassPerformance},
	{Pattern: regexp.MustCompile(`(?i)断言有效性|空测试|预期结果不完整|永真断言|无效捕获|mock遗漏|提前返回|非法退出|静默跳过|功能单一性|面条测试|路径混合`), Class: ClassUTEffect},
	{Pattern: regexp.MustCompile(`(?i)可维护性|质量控制|冗余代码|坏味道|重构问题|复杂度过高|重复代码`), Class: ClassMaintainability},
	{Pattern: regexp.MustCompile(`(?i)架构与设计|高耦合|职责违背|三方依赖`), Class: ClassArchitecture},
	{Pattern: regexp.MustCompile(`(?i)logic.?risk|死循环|必假条件|逻辑漏洞|除零错误|边界处理|边界防御|回归风险|健壮性与稳定性`), Class: ClassLogicRisk},
}

// ReduceControlledDefectClass 将不稳定的 category 与 title 归约为受控主类。
// 当调用方已提供明确的非空 category 时仅按词典匹配 category，严禁因未命中而回退扫描 title 自由文本串类。
func ReduceControlledDefectClass(category string, title string) string {
	cat := strings.TrimSpace(category)
	if cat != "" {
		for _, rule := range taxonomyRules {
			if rule.Pattern.MatchString(cat) {
				return rule.Class
			}
		}
		return ClassOther
	}
	tit := strings.TrimSpace(title)
	if tit == "" {
		return ClassOther
	}
	for _, rule := range taxonomyRules {
		if rule.Pattern.MatchString(tit) {
			return rule.Class
		}
	}
	return ClassOther
}

// CalibratePhysicalLine 触发词引导的双轨行号精准校准 (消灭 68% 行号漂移与 15% 行号缺失)
func CalibratePhysicalLine(lines []string, rawLineNumber int, triggerLine string) int {
	cleanTrigger := CleanSourceToken(triggerLine)
	total := len(lines)
	if total == 0 {
		if rawLineNumber > 0 {
			return rawLineNumber
		}
		return 1
	}

	// 情况 A: 行号存在且在合理范围内，优先在 [L-15, L+15] 内近距离寻找
	if rawLineNumber > 0 && rawLineNumber <= total {
		calibrated := LocateTriggerNearby(lines, cleanTrigger, rawLineNumber, 15)
		if calibrated > 0 && calibrated <= total {
			if cleanTrigger != "" {
				tok := CleanSourceToken(lines[calibrated-1])
				if tok == cleanTrigger || strings.Contains(tok, cleanTrigger) || strings.Contains(cleanTrigger, tok) {
					return calibrated
				}
				// 附近未找到，尝试全文反查
				if found := findCleanTriggerInLines(lines, cleanTrigger); found > 0 {
					return found
				}
			}
			return calibrated
		}
	}

	// 情况 B: 行号越界 (rawLineNumber > total) 或行号为空 (rawLineNumber <= 0)
	// 尝试全文反查 cleanTrigger
	if cleanTrigger != "" {
		if found := findCleanTriggerInLines(lines, cleanTrigger); found > 0 {
			return found
		}
	}

	// 回退保底：必须夹紧在 [1, total] 范围内
	if rawLineNumber > 0 {
		if rawLineNumber > total {
			return total
		}
		return rawLineNumber
	}

	return 1
}

// CanonicalizeScopeSymbol 规范化函数符号，若缺失则从物理代码行逆向反查
func CanonicalizeScopeSymbol(rawScope string, lines []string, targetLine int, filePath string) string {
	norm := NormalizeScopeSymbol(rawScope)
	if norm != "" {
		return norm
	}

	// 若大模型未输出作用域，直接反查物理源文件声明
	if len(lines) > 0 && targetLine > 0 {
		detectedScope, _ := ExtractScopeAndBodyFromLines(filePath, lines, targetLine)
		return NormalizeScopeSymbol(detectedScope)
	}

	return ""
}
