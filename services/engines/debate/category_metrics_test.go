package debate

import (
	"testing"

	"code-shield/services/engines"
)

func TestCategoryExecutionMetricsCoverStages(t *testing.T) {
	taxonomy := testTaxonomy()
	hunter := &HunterOutput{Candidates: []HunterCandidate{
		{CandidateID: "H-001", CategoryCode: "RACE_RELEASE_ACCESS", Category: taxonomy.Categories[0].Label},
		{CandidateID: "H-002", CategoryCode: "BAD", Category: "invalid"},
		{CandidateID: "H-003", CategoryCode: "", Category: ""},
		{CandidateID: "H-004", CategoryCode: "", Category: "释放后使用"},
	}}
	hunter.Candidates[0].ReviewRequired = false
	hunter.Candidates[1].ReviewRequired = true
	hunter.Candidates[2].ReviewRequired = true
	addHunterCategoryMetrics(hunter, taxonomy)
	hunterMetrics := engines.CategoryHunterMetrics{
		ModelCandidates: 4, ModelCodeValid: 1, ModelCodeInvalid: 1, ModelCodeAbsent: 2,
		DeterministicRepairs: 1, ReviewRequired: 2,
	}
	if hunter.CategoryMetrics != hunterMetrics {
		t.Fatalf("hunter metrics = %#v, want %#v", hunter.CategoryMetrics, hunterMetrics)
	}

	judge := &JudgeOutput{FinalVerdicts: []JudgeFinalVerdict{
		{CandidateID: "H-001", CategoryCode: "MEM_USE_AFTER_FREE", Category: taxonomy.Categories[1].Label},
		{CandidateID: "H-002", CategoryCode: "BAD", Category: "invalid"},
		{CandidateID: "H-003", CategoryCode: "", Category: ""},
		{CandidateID: "H-004", CategoryCode: "", Category: "旧释放竞态"},
		{CandidateID: "H-005", Verdict: "REJECTED", CategoryCode: "BAD", Category: "invalid"},
	}}
	judge.FinalVerdicts[1].ReviewRequired = true
	judge.FinalVerdicts[2].ReviewRequired = true
	addJudgeCategoryMetrics(judge, taxonomy)
	judgeMetrics := engines.CategoryJudgeMetrics{
		ModelCandidates: 5, ModelCodeValid: 1, ModelCodeInvalid: 2, ModelCodeAbsent: 2,
		DeterministicRepairs: 1, ReviewRequired: 2, Rejected: 1,
	}
	if judge.CategoryMetrics != judgeMetrics {
		t.Fatalf("judge metrics = %#v, want %#v", judge.CategoryMetrics, judgeMetrics)
	}

	details := []engines.ChunkDetails{{Category: engines.CategoryStageMetrics{Hunter: hunter.CategoryMetrics, Judge: judge.CategoryMetrics}}}
	aggregate := engines.AggregateAnalysisMetrics(details)
	if aggregate.Category.Hunter != hunter.CategoryMetrics || aggregate.Category.Judge != judge.CategoryMetrics {
		t.Fatalf("aggregate category metrics mismatch: %#v", aggregate.Category)
	}
}
