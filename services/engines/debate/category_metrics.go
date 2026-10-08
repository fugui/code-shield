package debate

import (
	"code-shield/models"
	"code-shield/services/engines"
)

func countHunterCategories(candidates []HunterCandidate, taxonomy models.CategoryTaxonomy) engines.CategoryHunterMetrics {
	metrics := engines.CategoryHunterMetrics{ModelCandidates: len(candidates)}
	for i := range candidates {
		candidate := candidates[i]
		switch ResolveCategory(candidate.CategoryCode, candidate.GetCategory(), taxonomy).Status {
		case CategoryResolutionValid:
			metrics.ModelCodeValid++
		case CategoryResolutionCodeAuthority:
			metrics.ModelCodeValid++
			metrics.DeterministicRepairs++
		case CategoryResolutionNeedsRepair:
			if candidate.CategoryCode == "" {
				metrics.ModelCodeAbsent++
			} else {
				metrics.ModelCodeInvalid++
			}
		default:
			if candidate.CategoryCode == "" {
				metrics.ModelCodeAbsent++
			} else {
				metrics.ModelCodeInvalid++
			}
			metrics.DeterministicRepairs++
		}
	}
	return metrics
}

func countJudgeCategories(verdicts []JudgeFinalVerdict, taxonomy models.CategoryTaxonomy) engines.CategoryJudgeMetrics {
	metrics := engines.CategoryJudgeMetrics{ModelCandidates: len(verdicts)}
	for i := range verdicts {
		verdict := verdicts[i]
		if verdict.Verdict == "REJECTED" {
			metrics.Rejected++
		}
		switch ResolveCategory(verdict.CategoryCode, verdict.Category, taxonomy).Status {
		case CategoryResolutionValid:
			metrics.ModelCodeValid++
		case CategoryResolutionCodeAuthority:
			metrics.ModelCodeValid++
			metrics.DeterministicRepairs++
		case CategoryResolutionNeedsRepair:
			if verdict.CategoryCode == "" {
				metrics.ModelCodeAbsent++
			} else {
				metrics.ModelCodeInvalid++
			}
		default:
			if verdict.CategoryCode == "" {
				metrics.ModelCodeAbsent++
			} else {
				metrics.ModelCodeInvalid++
			}
			metrics.DeterministicRepairs++
		}
	}
	return metrics
}

func addHunterCategoryMetrics(output *HunterOutput, taxonomy models.CategoryTaxonomy) {
	if output == nil {
		return
	}
	output.CategoryMetrics = countHunterCategories(output.Candidates, taxonomy)
	refreshHunterReviewMetrics(output)
}

func refreshHunterReviewMetrics(output *HunterOutput) {
	if output == nil {
		return
	}
	output.CategoryMetrics.ReviewRequired = 0
	for i := range output.Candidates {
		if output.Candidates[i].ReviewRequired {
			output.CategoryMetrics.ReviewRequired++
		}
	}
}

func addHunterRepairMetrics(output *HunterOutput, attempts int, successes int) {
	if output == nil {
		return
	}
	output.CategoryMetrics.LLMRepairAttempts += attempts
	output.CategoryMetrics.LLMRepairs += successes
	refreshHunterReviewMetrics(output)
}

func addJudgeCategoryMetrics(output *JudgeOutput, taxonomy models.CategoryTaxonomy) {
	if output == nil {
		return
	}
	output.CategoryMetrics = countJudgeCategories(output.FinalVerdicts, taxonomy)
	refreshJudgeReviewMetrics(output)
}

func refreshJudgeReviewMetrics(output *JudgeOutput) {
	if output == nil {
		return
	}
	output.CategoryMetrics.ReviewRequired = 0
	output.CategoryMetrics.Rejected = 0
	for i := range output.FinalVerdicts {
		verdict := output.FinalVerdicts[i]
		if verdict.ReviewRequired {
			output.CategoryMetrics.ReviewRequired++
		}
		if verdict.Verdict == "REJECTED" {
			output.CategoryMetrics.Rejected++
		}
	}
}

func addJudgeRepairMetrics(output *JudgeOutput, attempts int, successes int) {
	if output == nil {
		return
	}
	output.CategoryMetrics.LLMRepairAttempts += attempts
	output.CategoryMetrics.LLMRepairs += successes
	refreshJudgeReviewMetrics(output)
}
