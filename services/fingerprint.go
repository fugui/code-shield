package services

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// CalculateDefectFingerprint 计算抗代码行号与上下文抖动的确定性源码强指纹 (L1 物理强指纹)
// 公式: SHA256(RepoID + TaskTypeID + NormalizedPath + NormalizedScope + PhysicalToken)
// 【核心架构突破】：彻底剔除 Category 与大模型自然语言文本，确保纯物理源码客观生成！
func CalculateDefectFingerprint(repoID uint, taskTypeID uint, filePath string, triggerLine string, scopeSymbol string, _ ...string) string {
	// 1. 规范化相对路径 (统一正斜杠，小写)
	normPath := strings.ToLower(filepath.ToSlash(strings.TrimSpace(filePath)))

	// 2. 规范化作用域符号 (剥离外层命名空间，规范化 lambda)
	normScope := NormalizeScopeSymbol(scopeSymbol)
	if normScope == "" {
		normScope = filepath.Base(normPath)
	}

	// 3. 核心触发行物理 Token 清洗 (去除注释、所有空白字符、单双引号与分号)
	normTrigger := CleanSourceToken(triggerLine)

	// 4. 纯物理锚点特征计算 SHA-256 强指纹
	rawKey := fmt.Sprintf("repo:%d|task:%d|path:%s|scope:%s|token:%s",
		repoID, taskTypeID, normPath, normScope, normTrigger)

	hash := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(hash[:])
}

// CalculateTokenJaccard 计算两串代码 Token 的 2-gram Jaccard 相似度 [0.0, 1.0]
func CalculateTokenJaccard(s1, s2 string) float64 {
	t1 := CleanSourceToken(s1)
	t2 := CleanSourceToken(s2)
	if t1 == t2 {
		return 1.0
	}
	if len(t1) == 0 || len(t2) == 0 {
		return 0.0
	}

	// 字符级 2-gram 集合
	grams1 := make(map[string]bool)
	grams2 := make(map[string]bool)

	if len(t1) < 2 {
		grams1[t1] = true
	} else {
		for i := 0; i < len(t1)-1; i++ {
			grams1[t1[i:i+2]] = true
		}
	}

	if len(t2) < 2 {
		grams2[t2] = true
	} else {
		for i := 0; i < len(t2)-1; i++ {
			grams2[t2[i:i+2]] = true
		}
	}

	intersection := 0
	for g := range grams1 {
		if grams2[g] {
			intersection++
		}
	}

	union := len(grams1) + len(grams2) - intersection
	if union <= 0 {
		return 0.0
	}
	return float64(intersection) / float64(union)
}

// CalculateWeakScopeFingerprint 计算作用域弱指纹 (L2 弱指纹容错)
func CalculateWeakScopeFingerprint(repoID uint, taskTypeID uint, filePath string, scopeSymbol string, _ ...string) string {
	normPath := strings.ToLower(filepath.ToSlash(strings.TrimSpace(filePath)))
	normScope := NormalizeScopeSymbol(scopeSymbol)
	if normScope == "" {
		normScope = filepath.Base(normPath)
	}

	rawKey := fmt.Sprintf("repo:%d|task:%d|path:%s|scope:%s",
		repoID, taskTypeID, normPath, normScope)

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
