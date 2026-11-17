package debate

import "testing"

func TestSortHunterCandidatesForCapDeterministicPriority(t *testing.T) {
	runA := []HunterCandidate{
		{CandidateID: "H-003", FilePath: "src/b.cpp", LineRange: "50", Category: "其它缺陷"},
		{CandidateID: "H-002", FilePath: "src/b.cpp", LineRange: "20", Category: "内存管理问题-内存泄漏"},
		{CandidateID: "H-001", FilePath: "src/a.cpp", LineRange: "30", Category: "内存管理问题-空指针解引用"},
		{CandidateID: "H-004", FilePath: "src/a.cpp", LineRange: "10", Category: "内存管理问题-释放后使用"},
	}
	runB := []HunterCandidate{
		runA[2], runA[0], runA[3], runA[1],
	}

	sortHunterCandidatesForCap(runA)
	sortHunterCandidatesForCap(runB)

	for i := range runA {
		if runA[i].CandidateID != runB[i].CandidateID {
			t.Fatalf("sort mismatch at %d: %s vs %s", i, runA[i].CandidateID, runB[i].CandidateID)
		}
	}
	// 高危 UAF/NPD 按文件/行号排序在前，其次 MEM_LEAK，最后 OTHER
	wantOrder := []string{"H-004", "H-001", "H-002", "H-003"}
	for i, want := range wantOrder {
		if runA[i].CandidateID != want {
			t.Fatalf("index %d: got %s, want %s (full=%+v)", i, runA[i].CandidateID, want, runA)
		}
	}
}

func TestAssembleFindingFromJudgePropagatesTargetSymbol(t *testing.T) {
	finding := assembleFindingFromJudge(nil, JudgeFinalVerdict{
		CandidateID:         "H-001",
		Verdict:             "CONFIRMED",
		SeverityPreliminary: "严重",
		Category:            "空指针解引用",
		FilePath:            "src/a.cpp",
		LineRange:           "12",
		TriggerLine:         "*pSession = 1;",
		ScopeSymbol:         "Process",
		TargetSymbol:        "pSession",
		Title:               "null ptr",
	}, HunterCandidate{CandidateID: "H-001", TargetSymbol: "fallbackPtr"}, ChallengerDefenseCase{})
	if finding.TargetSymbol != "pSession" {
		t.Fatalf("expected Judge target_symbol 'pSession', got %q", finding.TargetSymbol)
	}

	fallbackFinding := assembleFindingFromJudge(nil, JudgeFinalVerdict{
		CandidateID:         "H-002",
		Verdict:             "CONFIRMED",
		SeverityPreliminary: "严重",
		Category:            "空指针解引用",
		FilePath:            "src/a.cpp",
		LineRange:           "20",
		TriggerLine:         "*fallbackPtr = 1;",
		ScopeSymbol:         "Process",
		Title:               "null ptr 2",
	}, HunterCandidate{CandidateID: "H-002", TargetSymbol: "fallbackPtr"}, ChallengerDefenseCase{})
	if fallbackFinding.TargetSymbol != "fallbackPtr" {
		t.Fatalf("expected fallback Hunter target_symbol 'fallbackPtr', got %q", fallbackFinding.TargetSymbol)
	}
}
