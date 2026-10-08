package governance

import (
	"log"
	"strings"
)

// DefaultFallbackCategory 通用兜底分类
const DefaultFallbackCategory = "其它缺陷"

// SanitizeCategory normalizes a free-form category using exact canonical label
// matching only. It never guesses through keywords, CWE text, or substrings.
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

	for _, allowed := range allowedCategories {
		if strings.EqualFold(trimmed, allowed) {
			return allowed
		}
	}

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
