package reports

func parseHunterCategoryDiagnostics(raw map[string]interface{}) CategoryHunterDiagnostics {
	result := CategoryHunterDiagnostics{}
	fields := []struct {
		key   string
		value *int
	}{
		{"model_candidates", &result.ModelCandidates},
		{"model_code_valid", &result.ModelCodeValid},
		{"model_code_invalid", &result.ModelCodeInvalid},
		{"model_code_absent", &result.ModelCodeAbsent},
		{"deterministic_repairs", &result.DeterministicRepairs},
		{"llm_repair_attempts", &result.LLMRepairAttempts},
		{"llm_repairs", &result.LLMRepairs},
		{"review_required", &result.ReviewRequired},
		{"category_only_quarantined", &result.CategoryOnlyQuarantined},
	}
	for _, field := range fields {
		if value, ok := raw[field.key].(float64); ok {
			*field.value = int(value)
		}
	}
	return result
}

func parseJudgeCategoryDiagnostics(raw map[string]interface{}) CategoryJudgeDiagnostics {
	result := CategoryJudgeDiagnostics{}
	fields := []struct {
		key   string
		value *int
	}{
		{"model_candidates", &result.ModelCandidates},
		{"model_code_valid", &result.ModelCodeValid},
		{"model_code_invalid", &result.ModelCodeInvalid},
		{"model_code_absent", &result.ModelCodeAbsent},
		{"review_required", &result.ReviewRequired},
		{"category_only_quarantined", &result.CategoryOnlyQuarantined},
	}
	for _, field := range fields {
		if value, ok := raw[field.key].(float64); ok {
			*field.value = int(value)
		}
	}
	return result
}
