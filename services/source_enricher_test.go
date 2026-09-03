package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanSourceToken(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{input: "  p->init(); // 初始化指针", expected: "p->init()"},
		{input: "  p->init(); /* 多行注释 */", expected: "p->init()"},
		{input: "pData = nullptr;", expected: "pdata=nullptr"},
		{input: "printf(\"Hello World!\\n\");", expected: "printf(helloworld!\\n)"},
		{input: "   ", expected: ""},
	}

	for _, c := range cases {
		actual := CleanSourceToken(c.input)
		if actual != c.expected {
			t.Errorf("CleanSourceToken(%q) = %q, expected %q", c.input, actual, c.expected)
		}
	}
}

func TestNormalizeScopeSymbol(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{input: "APP_COMMON::SensorDevice::ReadData", expected: "SensorDevice::ReadData"},
		{input: "LSTB_COMMON::SensorDevice::ReadData", expected: "SensorDevice::ReadData"},
		{input: "StandaloneFunction", expected: "StandaloneFunction"},
		{input: "Namespace::Class::operator()(signal handler)", expected: "Class::<lambda>"},
		{input: "Class::doWork::<lambda()>", expected: "doWork::<lambda>"},
		{input: "GetTriggerDelay / GetTriggerFreq", expected: "GetTriggerDelay / GetTriggerFreq"},
		{input: "GetTriggerFreq / GetTriggerDelay", expected: "GetTriggerDelay / GetTriggerFreq"},
		{input: "MyClass<int, double>::process", expected: "MyClass::process"},
	}

	for _, c := range cases {
		actual := NormalizeScopeSymbol(c.input)
		if actual != c.expected {
			t.Errorf("NormalizeScopeSymbol(%q) = %q, expected %q", c.input, actual, c.expected)
		}
	}
}

func TestLocateTriggerNearby(t *testing.T) {
	lines := []string{
		"void Foo() {",                     // line 1
		"    int a = 0;",                   // line 2
		"    // some comments",             // line 3
		"    Sensor* p = getSensor();",     // line 4
		"    p->Read(); // 触发行在这里",      // line 5
		"    delete p;",                    // line 6
		"}",                                // line 7
	}

	// 假设大模型给错了行号为 2，但 trigger 是 "p->Read();"
	calibrated := LocateTriggerNearby(lines, "p->read()", 2, 5)
	if calibrated != 5 {
		t.Errorf("expected calibrated line to be 5, got %d", calibrated)
	}

	// 假设大模型给错了行号为 7，但 trigger 是 "p->Read();"
	calibrated2 := LocateTriggerNearby(lines, "p->read()", 7, 5)
	if calibrated2 != 5 {
		t.Errorf("expected calibrated line to be 5, got %d", calibrated2)
	}
}

func TestLocateTriggerInLines(t *testing.T) {
	lines := []string{
		"void Foo() {",
		"    int a = 0;",
		"    char* buf = malloc(100);",
		"    free(buf);",
		"    buf[0] = 'a'; // UAF here", // line 5
		"}",
	}

	// 行号为空时的全文反查
	line := LocateTriggerInLines(lines, "buf[0]=a")
	if line != 5 {
		t.Errorf("expected line 5 for full text match, got %d", line)
	}
}

func TestEnrichSourceAnchor_EndToEnd(t *testing.T) {
	tmpDir := t.TempDir()
	sourceFile := "src/test_demo.cpp"
	fullPath := filepath.Join(tmpDir, sourceFile)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		t.Fatal(err)
	}

	code := `
#include <iostream>

namespace CORE_SYS::SUB_SYS {

void Worker::ProcessData() {
    int* ptr = nullptr;
    *ptr = 42; // trigger line here
}

}
`
	if err := os.WriteFile(fullPath, []byte(code), 0644); err != nil {
		t.Fatal(err)
	}

	// 测试 1: 正常行号
	anchor, err := EnrichSourceAnchor(tmpDir, sourceFile, "8", "*ptr = 42;")
	if err != nil {
		t.Fatalf("EnrichSourceAnchor failed: %v", err)
	}

	if anchor.NormalizedPath != "src/test_demo.cpp" {
		t.Errorf("unexpected path: %s", anchor.NormalizedPath)
	}
	if anchor.NormalizedScope != "Worker::ProcessData" {
		t.Errorf("unexpected scope: %s (expected Worker::ProcessData)", anchor.NormalizedScope)
	}
	if anchor.PhysicalToken != "*ptr=42" {
		t.Errorf("unexpected token: %s (expected *ptr=42)", anchor.PhysicalToken)
	}
	if anchor.StartLine != 8 {
		t.Errorf("unexpected start line: %d", anchor.StartLine)
	}

	// 测试 2: 行号偏移 3 行（大模型给了 5，真实在 8）
	anchorOffset, err := EnrichSourceAnchor(tmpDir, sourceFile, "5", "*ptr = 42;")
	if err != nil {
		t.Fatalf("EnrichSourceAnchor offset failed: %v", err)
	}
	if anchorOffset.StartLine != 8 {
		t.Errorf("expected offset line to be calibrated to 8, got %d", anchorOffset.StartLine)
	}

	// 测试 3: 行号为空（大模型给空字符串）
	anchorEmpty, err := EnrichSourceAnchor(tmpDir, sourceFile, "", "*ptr = 42;")
	if err != nil {
		t.Fatalf("EnrichSourceAnchor empty failed: %v", err)
	}
	if anchorEmpty.StartLine != 8 {
		t.Errorf("expected empty line to be found at 8, got %d", anchorEmpty.StartLine)
	}
}
