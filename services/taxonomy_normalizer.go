package services

import (
	"code-shield/services/governance"
)

// DefaultFallbackCategory 通用兜底分类
const DefaultFallbackCategory = governance.DefaultFallbackCategory

// SanitizeCategory 将大模型自由发散的 Category 字符串收敛至受控白名单字典，委托给 governance 子包
func SanitizeCategory(rawCategory string, allowedCategories []string) string {
	return governance.SanitizeCategory(rawCategory, allowedCategories)
}
