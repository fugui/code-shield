package governance

import (
	"strings"
)

type NormalizedCategory struct {
	Category string
	Detail   string
	Governed bool
}

// NormalizeCategory applies the task taxonomy as the only mutable normalization
// surface. It never invents a task-specific enum when the task has none.
func NormalizeCategory(raw string, allowed []string) NormalizedCategory {
	detail := strings.TrimSpace(raw)
	normalized := SanitizeCategory(detail, allowed)
	governed := len(allowed) > 0
	return NormalizedCategory{
		Category: normalized,
		Detail:   detail,
		Governed: governed,
	}
}
