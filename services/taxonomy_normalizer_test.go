package services

import (
	"testing"
)

func TestSanitizeCategory(t *testing.T) {
	allowed := []string{
		"空指针解引用",
		"越界访问",
		"释放后使用",
		"双重释放",
		"数据竞争与多线程安全",
		"信号处理不安全",
		"未初始化变量",
		"资源或内存泄漏",
		"死锁风险",
		"其它缺陷",
	}

	cases := []struct {
		input    string
		expected string
	}{
		// 1. 全等匹配
		{input: "空指针解引用", expected: "空指针解引用"},
		// 2. CWE 映射
		{input: "CWE-476: NULL Pointer Dereference", expected: "空指针解引用"},
		{input: "CWE-416: Use After Free", expected: "释放后使用"},
		{input: "CWE-787: Out-of-bounds Write", expected: "越界访问"},
		{input: "CWE-362: Concurrent Execution Race Condition", expected: "数据竞争与多线程安全"},
		{input: "CWE-364: Signal Handler Race Condition", expected: "信号处理不安全"},
		{input: "CWE-401: Memory Leak", expected: "资源或内存泄漏"},
		// 3. 子串模糊吸附
		{input: "内存管理问题-空指针解引用/崩溃", expected: "空指针解引用"},
		{input: "越界读取", expected: "越界访问"},
		{input: "内存管理问题-释放后使用(UAF)", expected: "释放后使用"},
		{input: "并发问题-数据竞争", expected: "数据竞争与多线程安全"},
		// 4. 未知类型兜底
		{input: "某种前所未见的奇怪错误描述", expected: "其它缺陷"},
		{input: "", expected: "其它缺陷"},
	}

	for _, c := range cases {
		actual := SanitizeCategory(c.input, allowed)
		if actual != c.expected {
			t.Errorf("SanitizeCategory(%q) = %q, expected %q", c.input, actual, c.expected)
		}
	}

	// 测试真实 coredump-risk 任务受控白名单吸附能力
	coredumpAllowed := []string{
		"多线程并发问题-数据竞争",
		"多线程并发问题-锁同步不当",
		"多线程并发问题-死锁风险",
		"内存管理问题-空指针解引用",
		"内存管理问题-越界访问/缓冲区溢出",
		"内存管理问题-双重释放",
		"内存管理问题-释放后使用",
		"内存管理问题-内存泄漏(OOM风险)",
		"生命周期管理问题-悬挂指针与引用",
		"生命周期管理问题-回调对象销毁竞争",
		"时序与初始化问题-未初始化变量",
		"时序与初始化问题-释放与访问竞态",
		"运行时异常-除零错误",
		"运行时异常-栈溢出",
		"运行时异常-未捕获异常/致命退出",
		"运行时异常-信号处理不当",
		"第三方框架限制-Qt跨线程UI操作",
		"其它问题-其它崩溃隐患",
	}

	coredumpCases := []struct {
		input    string
		expected string
	}{
		{input: "CWE-476: NULL Pointer Dereference", expected: "内存管理问题-空指针解引用"},
		{input: "空指针解引用", expected: "内存管理问题-空指针解引用"},
		{input: "内存管理问题-空指针解引用", expected: "内存管理问题-空指针解引用"},
		{input: "CWE-416: Use After Free", expected: "内存管理问题-释放后使用"},
		{input: "CWE-787: Out-of-bounds Write", expected: "内存管理问题-越界访问/缓冲区溢出"},
		{input: "越界访问", expected: "内存管理问题-越界访问/缓冲区溢出"},
		{input: "数据竞争", expected: "多线程并发问题-数据竞争"},
		{input: "信号处理不当", expected: "运行时异常-信号处理不当"},
		{input: "未知奇怪错误", expected: "其它问题-其它崩溃隐患"},
	}

	for _, c := range coredumpCases {
		actual := SanitizeCategory(c.input, coredumpAllowed)
		if actual != c.expected {
			t.Errorf("Coredump SanitizeCategory(%q) = %q, expected %q", c.input, actual, c.expected)
		}
	}
}
