package governance

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
		// 未知类型必须兜底，禁止模糊吸附
		{input: "CWE-476: NULL Pointer Dereference", expected: "其它缺陷"},
		{input: "内存管理问题-空指针解引用/崩溃", expected: "其它缺陷"},
		{input: "越界读取", expected: "其它缺陷"},
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
		{input: "空指针解引用", expected: "其它问题-其它崩溃隐患"},
		{input: "内存管理问题-空指针解引用", expected: "内存管理问题-空指针解引用"},
		{input: "未知奇怪错误", expected: "其它问题-其它崩溃隐患"},
	}

	for _, c := range coredumpCases {
		actual := SanitizeCategory(c.input, coredumpAllowed)
		if actual != c.expected {
			t.Errorf("Coredump SanitizeCategory(%q) = %q, expected %q", c.input, actual, c.expected)
		}
	}
}
