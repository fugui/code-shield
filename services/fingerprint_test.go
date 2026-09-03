package services

import (
	"testing"
)

func TestCalculateDefectFingerprint_AntiDrift(t *testing.T) {
	repoID := uint(1)
	taskTypeID := uint(10)
	filePath := "include/fmt/format.h"
	scopeSymbol := "parse_arg_id<char>"

	// 基础语句
	trigger1 := "c = *++it;"
	fp1 := CalculateDefectFingerprint(repoID, taskTypeID, filePath, trigger1, scopeSymbol)

	// 语句附带注释与前后空格
	trigger2 := "  c = *++it; // 先自增后解引用堆越界读"
	fp2 := CalculateDefectFingerprint(repoID, taskTypeID, filePath, trigger2, scopeSymbol)

	// 语句包含多余空格
	trigger3 := "c   =   *++it   ;"
	fp3 := CalculateDefectFingerprint(repoID, taskTypeID, filePath, trigger3, scopeSymbol)

	if fp1 != fp2 {
		t.Errorf("Fingerprint drifted with comments! fp1=%s, fp2=%s", fp1, fp2)
	}
	if fp1 != fp3 {
		t.Errorf("Fingerprint drifted with spaces! fp1=%s, fp3=%s", fp1, fp3)
	}

	// 传入不同分类时，应生成不同的强指纹以区分相同位置的不同缺陷
	fpCat1 := CalculateDefectFingerprint(repoID, taskTypeID, filePath, trigger1, scopeSymbol, "CWE-476")
	fpCat2 := CalculateDefectFingerprint(repoID, taskTypeID, filePath, trigger1, scopeSymbol, "CWE-125")
	if fpCat1 == fpCat2 {
		t.Errorf("Fingerprint should differ for different categories! fpCat1=%s, fpCat2=%s", fpCat1, fpCat2)
	}
}

func TestExtractScopeSymbol(t *testing.T) {
	// Go 语言
	goCode := "func (s *Scanner) ScanDirectory(ctx context.Context) error { ... }"
	scopeGo := ExtractScopeSymbol("scanner.go", goCode)
	if scopeGo != "(s *Scanner).ScanDirectory" {
		t.Errorf("Expected Go scope, got %q", scopeGo)
	}

	// Python 语言
	pyCode := "def analyze_code_block(snippet, options):\n    pass"
	scopePy := ExtractScopeSymbol("analyzer.py", pyCode)
	if scopePy != "analyze_code_block" {
		t.Errorf("Expected Python scope, got %q", scopePy)
	}

	// C++ 语言
	cppCode := "int buffered_file::fileno() const {\n    return file_->fileno();\n}"
	scopeCpp := ExtractScopeSymbol("posix.cc", cppCode)
	if scopeCpp != "buffered_file::fileno" {
		t.Errorf("Expected C++ scope, got %q", scopeCpp)
	}
}
