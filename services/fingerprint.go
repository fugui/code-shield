package services

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// CalculateDefectFingerprint 计算抗代码行号与上下文抖动的通用缺陷强指纹 (L1 强指纹)
// 公式: SHA256(RepoID + TaskTypeID + NormalizedPath + ScopeSymbol + NormalizedTriggerLine)
func CalculateDefectFingerprint(repoID uint, taskTypeID uint, filePath string, triggerLine string, scopeSymbol string) string {
	// 1. 规范化相对路径 (统一正斜杠，小写)
	normPath := strings.ToLower(filepath.ToSlash(strings.TrimSpace(filePath)))

	// 2. 核心触发行规范化 (去除注释、所有空白字符、单双引号与分号)
	normTrigger := NormalizeTriggerLine(triggerLine)

	// 3. 规范化作用域符号 (若为空，提取路径基名容错)
	normScope := strings.TrimSpace(scopeSymbol)
	if normScope == "" {
		normScope = filepath.Base(normPath)
	}

	// 4. 组合特征计算 SHA-256 强指纹
	rawKey := fmt.Sprintf("repo:%d|task:%d|path:%s|scope:%s|trigger:%s",
		repoID, taskTypeID, normPath, normScope, normTrigger)

	hash := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(hash[:])
}

// CalculateWeakScopeFingerprint 计算作用域弱指纹 (L2 弱指纹容错)
// 用于当核心触发行发生微调重构时，仍能继承历史反馈与生命周期
func CalculateWeakScopeFingerprint(repoID uint, taskTypeID uint, filePath string, scopeSymbol string, category string) string {
	normPath := strings.ToLower(filepath.ToSlash(strings.TrimSpace(filePath)))
	normScope := strings.TrimSpace(scopeSymbol)
	normCat := strings.ToLower(strings.TrimSpace(category))

	rawKey := fmt.Sprintf("repo:%d|task:%d|path:%s|scope:%s|cat:%s",
		repoID, taskTypeID, normPath, normScope, normCat)

	hash := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(hash[:])
}

// NormalizeTriggerLine 对引发漏洞的核心关键单一语句进行 Token 级规范化
func NormalizeTriggerLine(triggerLine string) string {
	trimmed := strings.TrimSpace(triggerLine)
	if trimmed == "" {
		return ""
	}

	// 移除 C/C++//Go/Java 单行与多行注释
	reComments := regexp.MustCompile(`(//.*?$|/\*.*?\*/|#.*?$)`)
	cleanLine := reComments.ReplaceAllString(trimmed, "")

	// 移除所有空白符与换行符
	reWhitespace := regexp.MustCompile(`\s+`)
	cleanLine = reWhitespace.ReplaceAllString(cleanLine, "")

	// 移除双引号、单引号与末尾分号
	cleanLine = strings.ReplaceAll(cleanLine, "\"", "")
	cleanLine = strings.ReplaceAll(cleanLine, "'", "")
	cleanLine = strings.TrimSuffix(cleanLine, ";")

	return strings.ToLower(cleanLine)
}

// ExtractScopeSymbol 多语言 AST 与正则作用域符号提取器
func ExtractScopeSymbol(filePath string, codeSnippet string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	snippet := strings.TrimSpace(codeSnippet)
	if snippet == "" {
		return ""
	}

	switch ext {
	case ".go":
		// Go: func (s *Receiver) MethodName(...) 或 func FunctionName(...)
		reGoFunc := regexp.MustCompile(`func\s+(\([^)]+\)\s+)?([A-Za-z0-9_]+)`)
		if m := reGoFunc.FindStringSubmatch(snippet); len(m) >= 3 {
			recv := strings.TrimSpace(m[1])
			if recv != "" {
				return recv + "." + m[2]
			}
			return m[2]
		}
	case ".java", ".kt", ".scala":
		// Java: public void methodName(...) 或 class ClassName
		reJavaMethod := regexp.MustCompile(`(?:public|protected|private|static|\s)+[\w<>\[\]]+\s+([A-Za-z0-9_]+)\s*\([^)]*\)`)
		if m := reJavaMethod.FindStringSubmatch(snippet); len(m) >= 2 {
			return m[1]
		}
	case ".py":
		// Python: def method_name(...) 或 class ClassName:
		rePy := regexp.MustCompile(`(?:def|class)\s+([A-Za-z0-9_]+)`)
		if m := rePy.FindStringSubmatch(snippet); len(m) >= 2 {
			return m[1]
		}
	default:
		// C/C++: Namespace::Class::Method 或 function_name(...)
		reCpp := regexp.MustCompile(`(?:[A-Za-z0-9_]+::)*[A-Za-z0-9_]+\s*\([^)]*\)`)
		if m := reCpp.FindString(snippet); m != "" {
			idx := strings.Index(m, "(")
			if idx != -1 {
				return strings.TrimSpace(m[:idx])
			}
			return strings.TrimSpace(m)
		}
	}

	return ""
}
