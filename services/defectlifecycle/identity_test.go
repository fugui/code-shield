package defectlifecycle

import (
	"os"
	"path/filepath"
	"testing"

	"code-shield/models"
)

func TestStatementShapeIsRenameAndFormattingInvariant(t *testing.T) {
	left := statementShape("int file_size = value + 1;")
	right := statementShape("int    length=length+2;")
	if left == "" || left != right {
		t.Fatalf("expected stable statement shape, left=%q right=%q", left, right)
	}
}

func TestIdentityKeysExcludeDisplayFields(t *testing.T) {
	base := models.AnalysisFinding{
		FilePath: "src/parser.cpp", LineNumber: "10", Category: "memory_leak",
		Title: "first title", Severity: "严重", TriggerLine: "delete buffer;",
	}
	variant := base
	variant.LineNumber = "90"
	variant.Title = "completely different title"
	variant.Severity = "一般"

	identities := BuildIdentities("", 1, 2, []models.AnalysisFinding{base, variant})
	if identities[0].K1 != identities[1].K1 || identities[0].K2 != identities[1].K2 {
		t.Fatalf("expected stable identity keys: %+v %+v", identities[0], identities[1])
	}
	if identities[0].DefectClassMajor != "memory_leak" {
		t.Fatalf("unexpected class major: %q", identities[0].DefectClassMajor)
	}
}

func TestObservationGroupsMergeSameStrongIdentity(t *testing.T) {
	repoRoot := t.TempDir()
	path := filepath.Join(repoRoot, "src", "merge.cpp")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("void work() {\n  delete buffer;\n}\n"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	findings := []models.AnalysisFinding{
		{FilePath: "src/a.cpp", LineNumber: "10", Category: "memory_leak", Severity: "严重", Title: "b", TriggerLine: "delete buffer;"},
		{FilePath: "src/a.cpp", LineNumber: "12", Category: "memory_leak", Severity: "致命", Title: "a", TriggerLine: "delete buffer;"},
	}
	groups := BuildObservationGroups(repoRoot, 7, findings)
	if len(groups) != 1 {
		t.Fatalf("expected one observation group, got %d", len(groups))
	}
	if len(groups[0].Findings) != 2 {
		t.Fatalf("expected two members, got %d", len(groups[0].Findings))
	}
	if groups[0].Representative.Title != "a" {
		t.Fatalf("expected highest severity representative, got %q", groups[0].Representative.Title)
	}
}

func TestObservationGroupsRejectDifferentClassOrLowConfidence(t *testing.T) {
	sameStatement := models.AnalysisFinding{FilePath: "src/a.cpp", LineNumber: "10", TriggerLine: "delete buffer;"}
	findings := []models.AnalysisFinding{
		{FilePath: "src/a.cpp", LineNumber: "10", Category: "memory_leak", TriggerLine: "delete buffer;"},
		{FilePath: "src/a.cpp", LineNumber: "11", Category: "integer_overflow", TriggerLine: "delete buffer;"},
		{FilePath: "src/a.cpp", LineNumber: "20", Category: "memory_leak", CodeSnippet: "delete buffer;"},
	}
	groups := BuildObservationGroups("", 8, findings)
	if len(groups) != 3 {
		t.Fatalf("expected separate observation groups, got %d", len(groups))
	}
	_ = sameStatement
}

func TestIdentitySnapsToPhysicalTrigger(t *testing.T) {
	repoRoot := t.TempDir()
	path := filepath.Join(repoRoot, "src", "sample.cpp")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create dir: %v", err)
	}
	content := "namespace demo {\nvoid work() {\n  int ready = 1;\n  delete buffer;\n}\n}\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	finding := models.AnalysisFinding{
		FilePath: "src/sample.cpp", LineNumber: "30", Category: "memory_leak",
		TriggerLine: "delete buffer;", ScopeSymbol: "demo::work",
	}
	identity := BuildIdentities(repoRoot, 1, 2, []models.AnalysisFinding{finding})[0]
	if identity.LineStart != 4 || identity.LineEnd != 4 {
		t.Fatalf("expected snapped line 4, got %d-%d", identity.LineStart, identity.LineEnd)
	}
	if identity.Confidence != AnchorHigh || identity.CleanToken != "deletebuffer" {
		t.Fatalf("unexpected physical identity: %+v", identity)
	}
}

func TestIdentityDistinguishesRepeatedStatementOccurrences(t *testing.T) {
	repoRoot := t.TempDir()
	path := filepath.Join(repoRoot, "src", "repeat.cpp")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create dir: %v", err)
	}
	content := "void work() {\n  release(a);\n  release(b);\n}\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	findings := []models.AnalysisFinding{
		{FilePath: "src/repeat.cpp", LineNumber: "2", Category: "resource_leak", TriggerLine: "release(a);"},
		{FilePath: "src/repeat.cpp", LineNumber: "3", Category: "resource_leak", TriggerLine: "release(b);"},
	}
	identities := BuildIdentities(repoRoot, 1, 2, findings)
	if identities[0].OccurrenceIndex != 1 || identities[1].OccurrenceIndex != 1 {
		t.Fatalf("expected one occurrence for the same statement, got %d and %d", identities[0].OccurrenceIndex, identities[1].OccurrenceIndex)
	}
	if identities[0].K2 == identities[1].K2 {
		t.Fatalf("expected distinct context keys for repeated statements")
	}
}

func TestDerivedIdentityEvidence(t *testing.T) {
	findings := []models.AnalysisFinding{
		{Category: "race", TriggerLine: "sim1GasAndVacuumStateRep.store(true);"},
		{Category: "race", TriggerLine: "heatingStateTraceMap.store(true);"},
		{Category: "null", TriggerLine: "const auto* value = container.Find(id); return value;"},
	}
	identities := BuildIdentities("", 1, 2, findings)

	if identities[0].ResourceIdentity == "" || identities[0].ResourceIdentity == identities[1].ResourceIdentity {
		t.Fatalf("expected distinct resource identities, got %q and %q", identities[0].ResourceIdentity, identities[1].ResourceIdentity)
	}
	if identities[2].ValidationChain == "" {
		t.Fatalf("expected non-empty validation chain hash")
	}
	plain := BuildIdentities("", 1, 2, []models.AnalysisFinding{{Category: "race", TriggerLine: "plainValue.store(true);"}})[0]
	if plain.ValidationChain != "" {
		t.Fatalf("expected empty validation chain for ordinary statement, got %q", plain.ValidationChain)
	}
	for _, identity := range identities {
		if identity.RootFamily == "" {
			t.Fatalf("expected non-empty root family for %+v", identity)
		}
	}
}

func TestSameStatementWithLineRangeMerges(t *testing.T) {
	repoRoot := t.TempDir()
	path := filepath.Join(repoRoot, "src", "duplicate.cpp")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create dir: %v", err)
	}
	content := "void work() {\n  int data[4];\n  data[4] = 1;\n}\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	findings := []models.AnalysisFinding{
		{FilePath: "src/duplicate.cpp", LineNumber: "3", Category: "memory_overflow", TriggerLine: "data[4] = 1;", ScopeSymbol: "work"},
		{FilePath: "src/duplicate.cpp", LineNumber: "3-4", Category: "memory_overflow", TriggerLine: "data[4] = 1;", ScopeSymbol: "work"},
	}
	groups := BuildObservationGroups(repoRoot, 1, findings)
	if len(groups) != 1 || len(groups[0].Findings) != 2 {
		t.Fatalf("expected same physical statement in one group, got %d groups and %d findings", len(groups), len(groups[0].Findings))
	}
}
