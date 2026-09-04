package governance

import (
	"log"
	"strings"
)

// DefaultFallbackCategory 通用兜底分类
const DefaultFallbackCategory = "其它缺陷"

// SanitizeCategory 将大模型自由发散的 Category 字符串收敛至受控白名单字典
// 匹配策略：
// 1. 全等精确匹配 (大小写无关)
// 2. 最长公共子串 / 双向包含吸附
// 3. CWE 编号映射 (如包含 CWE-476 / 125 / 787 / 416 / 362 等)
// 4. 兜底收敛为 "其它缺陷" 并记录告警日志
func SanitizeCategory(rawCategory string, allowedCategories []string) string {
	trimmed := strings.TrimSpace(rawCategory)
	if trimmed == "" {
		return DefaultFallbackCategory
	}

	if len(allowedCategories) == 0 {
		return trimmed
	}

	// 1. 全等匹配 (不区分大小写)
	for _, allowed := range allowedCategories {
		if strings.EqualFold(trimmed, allowed) {
			return allowed
		}
	}

	// 2. 常见 CWE 编号与核心特征词优先映射速查
	cweUpper := strings.ToUpper(trimmed)

	// 信号处理优先
	if strings.Contains(cweUpper, "CWE-364") || strings.Contains(cweUpper, "SIGNAL") || strings.Contains(trimmed, "信号") {
		for _, allowed := range allowedCategories {
			if strings.Contains(allowed, "信号") {
				return allowed
			}
		}
	}

	if strings.Contains(cweUpper, "CWE-476") || strings.Contains(cweUpper, "NULL POINTER") || strings.Contains(trimmed, "空指针") {
		for _, allowed := range allowedCategories {
			if strings.Contains(allowed, "空指针") {
				return allowed
			}
		}
	}

	if strings.Contains(cweUpper, "CWE-416") || strings.Contains(cweUpper, "USE AFTER FREE") || strings.Contains(cweUpper, "UAF") || strings.Contains(trimmed, "释放后使用") || strings.Contains(trimmed, "悬垂指针") {
		for _, allowed := range allowedCategories {
			if strings.Contains(allowed, "释放后使用") || strings.Contains(allowed, "悬垂指针") {
				return allowed
			}
		}
	}

	if strings.Contains(cweUpper, "CWE-415") || strings.Contains(cweUpper, "DOUBLE FREE") || strings.Contains(trimmed, "双重释放") {
		for _, allowed := range allowedCategories {
			if strings.Contains(allowed, "双重释放") {
				return allowed
			}
		}
	}

	if strings.Contains(cweUpper, "CWE-787") || strings.Contains(cweUpper, "CWE-125") || strings.Contains(cweUpper, "BUFFER OVERFLOW") ||
		strings.Contains(cweUpper, "OUT OF BOUNDS") || strings.Contains(cweUpper, "OUT-OF-BOUNDS") || strings.Contains(trimmed, "越界") || strings.Contains(trimmed, "溢出") {
		for _, allowed := range allowedCategories {
			if strings.Contains(allowed, "越界") || strings.Contains(allowed, "溢出") {
				return allowed
			}
		}
	}

	if strings.Contains(cweUpper, "CWE-362") || strings.Contains(cweUpper, "RACE CONDITION") || strings.Contains(trimmed, "竞争") || strings.Contains(trimmed, "数据争用") {
		for _, allowed := range allowedCategories {
			if strings.Contains(allowed, "竞争") || strings.Contains(allowed, "争用") {
				return allowed
			}
		}
	}

	if strings.Contains(cweUpper, "CWE-667") || strings.Contains(cweUpper, "DEADLOCK") || strings.Contains(trimmed, "死锁") {
		for _, allowed := range allowedCategories {
			if strings.Contains(allowed, "死锁") {
				return allowed
			}
		}
	}

	if strings.Contains(cweUpper, "CWE-401") || strings.Contains(cweUpper, "MEMORY LEAK") || strings.Contains(trimmed, "内存泄露") || strings.Contains(trimmed, "内存泄漏") {
		for _, allowed := range allowedCategories {
			if strings.Contains(allowed, "泄漏") || strings.Contains(allowed, "泄露") {
				return allowed
			}
		}
	}

	if strings.Contains(cweUpper, "ASSERT") || strings.Contains(trimmed, "断言") {
		for _, allowed := range allowedCategories {
			if strings.Contains(allowed, "断言") {
				return allowed
			}
		}
	}

	// 3. 双向包含/最长匹配启发式吸附
	bestAllowed := ""
	maxLen := 0
	for _, allowed := range allowedCategories {
		if strings.Contains(trimmed, allowed) || strings.Contains(allowed, trimmed) {
			if len(allowed) > maxLen {
				maxLen = len(allowed)
				bestAllowed = allowed
			}
		}
	}
	if bestAllowed != "" {
		return bestAllowed
	}

	// 4. 无法收敛，降级为兜底分类并记警告日志
	fallback := DefaultFallbackCategory
	for _, allowed := range allowedCategories {
		if strings.Contains(allowed, "其它") || strings.Contains(allowed, "其他") {
			fallback = allowed
			break
		}
	}
	log.Printf("[TaxonomyWarn] Unrecognized category %q, falling back to task category %q", rawCategory, fallback)
	return fallback
}
