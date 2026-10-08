package planner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"code-shield/services/coverage"
	"code-shield/services/engines"
	"code-shield/services/engines/profile"
)

func testInput(t *testing.T, root string, scanProfile profile.ScanProfile) Input {
	t.Helper()
	return Input{
		CodesPath:   root,
		Profile:     scanProfile,
		ProfileHash: profile.Hash(scanProfile),
		Config: engines.ChunkConfig{
			MaxFiles:        engines.DefaultChunkMaxFiles,
			Depth:           engines.DefaultChunkDepth,
			FileExtensions:  scanProfile.IncludeExtensions,
			ContentKeywords: scanProfile.ContentKeywords,
			ExcludePaths:    scanProfile.ExcludePaths,
		},
		TargetScope: scanProfile.TargetScope,
	}
}

func TestBuildFullReviewProceeds(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "session.cpp"), []byte("int start() { return 0; }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	scanProfile := profile.ScanProfile{Version: 1, Name: profile.NameFullReview, TargetScope: "business", IncludeExtensions: []string{".cpp"}}
	decision, plan, planValue, err := Build(testInput(t, root, scanProfile))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if decision.Decision != DecisionProceed || decision.PrimaryUnits != 1 {
		t.Fatalf("unexpected decision: %+v", decision)
	}
	if plan == nil || len(plan.Selected) != 1 || decision.PlanManifestHash == "" {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	filePlan, ok := planValue.(*FilePlan)
	if !ok || filePlan == nil || filePlan.PrimaryCount() != 1 || filePlan.Files[0] != "src/session.cpp" {
		t.Fatalf("unexpected file plan: %+v", planValue)
	}
}

func TestBuildFullReviewSkipsNoLanguageScope(t *testing.T) {
	scanProfile := profile.ScanProfile{Version: 1, Name: profile.NameFullReview, TargetScope: "business", IncludeExtensions: []string{".cpp"}}
	decision, _, _, err := Build(testInput(t, t.TempDir(), scanProfile))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if decision.Decision != DecisionSkipped || decision.Reason != ReasonNoLanguageScope {
		t.Fatalf("unexpected decision: %+v", decision)
	}
}

func TestBuildKeywordReviewDistinguishesNoMatch(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "session.cpp"), []byte("void start() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	scanProfile := profile.ScanProfile{
		Version: 1, Name: profile.NameKeywordReview, TargetScope: "business",
		IncludeExtensions: []string{".cpp"}, ContentKeywords: []string{"pthread_create"},
	}
	decision, _, _, err := Build(testInput(t, root, scanProfile))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if decision.Decision != DecisionSkipped || decision.Reason != ReasonNoKeywordMatch {
		t.Fatalf("unexpected decision: %+v", decision)
	}
}

func TestBuildKeywordOccurrencesIsDeterministic(t *testing.T) {
	root := t.TempDir()
	content := "namespace Session {\nvoid start() { pthread_create(nullptr, nullptr, worker, nullptr); }\n}\n"
	if err := os.WriteFile(filepath.Join(root, "session.cpp"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	scanProfile := profile.ScanProfile{
		Version: 1, Name: profile.NameKeywordReview, TargetScope: "business",
		IncludeExtensions: []string{".cpp"}, ContentKeywords: []string{"pthread_create"},
		PrimaryUnit: profile.PrimaryUnitKeywordOccurrence,
	}
	input := testInput(t, root, scanProfile)
	decision, _, planValue, err := Build(input)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if decision.Decision != DecisionProceed || decision.KeywordOccurrenceUnits != 1 {
		t.Fatalf("unexpected decision: %+v", decision)
	}
	keywordPlan, ok := planValue.(*KeywordOccurrencePlan)
	if !ok || keywordPlan == nil || len(keywordPlan.Occurrences) != 1 || keywordPlan.Occurrences[0].EnclosingScope != "start" {
		t.Fatalf("unexpected occurrence plan: %+v", planValue)
	}
	firstHash := keywordPlan.ManifestHash
	_, _, repeatedValue, err := Build(input)
	if err != nil {
		t.Fatalf("Build() repeat error = %v", err)
	}
	repeated, ok := repeatedValue.(*KeywordOccurrencePlan)
	if !ok || repeated == nil || repeated.ManifestHash != firstHash || len(repeated.Occurrences) != len(keywordPlan.Occurrences) {
		t.Fatalf("plan is not deterministic: %+v", repeatedValue)
	}
	units := keywordPlan.PrimaryUnits()
	if len(units) != len(keywordPlan.Occurrences) {
		t.Fatalf("PrimaryUnits() returned %d units, want %d", len(units), len(keywordPlan.Occurrences))
	}
	for i, unit := range units {
		if unit.ID != keywordPlan.Occurrences[i].OccurrenceID || unit.Kind != coverage.PlanUnitKeywordOccurrence {
			t.Fatalf("PrimaryUnits()[%d] = %+v, want occurrence identity", i, unit)
		}
		if unit.DisplayName == "" || strings.Contains(unit.DisplayName, "sha256:") {
			t.Fatalf("PrimaryUnits()[%d].DisplayName = %q, want human readable value", i, unit.DisplayName)
		}
	}
}

func TestBuildDegradesPartialUnknownPrimaryScope(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ready.cpp"), []byte("void ready() { start(); }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	unknown := filepath.Join(root, "unknown.cpp")
	if err := os.WriteFile(unknown, []byte("void unknown_start() { start(); }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unknown, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(unknown, 0644) })
	scanProfile := profile.ScanProfile{
		Version: 1, Name: profile.NameFullReview, TargetScope: "business", IncludeExtensions: []string{".cpp"},
		ContentKeywords: []string{"start"},
	}
	decision, _, _, err := Build(testInput(t, root, scanProfile))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if decision.Decision != DecisionProceedDegraded || decision.Reason != ReasonPartialPrimaryScope || decision.UnknownFiles != 1 {
		t.Fatalf("unexpected decision: %+v", decision)
	}
}

func TestBuildChangeAndEntityPlansFailExplicitly(t *testing.T) {
	changeProfile := profile.ScanProfile{
		Version: 1, Name: profile.NameChangeReview, Languages: []string{profile.LanguageCPP, profile.LanguagePython, profile.LanguageJava},
		ScopePolicy: "changed_hunks", ContextPolicy: "changed_files", BasePolicy: &profile.BasePolicy{Strategy: "since_days", SinceDays: 7},
	}
	decision, _, _, err := Build(testInput(t, t.TempDir(), changeProfile))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if decision.Decision != DecisionFailed || decision.Reason != ReasonInvalidBase || decision.Message != "ChangePlan was not precomputed" {
		t.Fatalf("unexpected change decision: %+v", decision)
	}
	entityProfile := profile.ScanProfile{Version: 1, Name: profile.NameTestEntityReview, TargetScope: "test", EntityKind: "test_case"}
	entityRoot := t.TempDir()
	javaTest := "public class SessionTest {\n  @Test\n  public void handlesPacketBoundary() {\n  }\n}\n"
	if err := os.WriteFile(filepath.Join(entityRoot, "SessionTest.java"), []byte(javaTest), 0644); err != nil {
		t.Fatal(err)
	}
	decision, _, entityPlanValue, err := Build(testInput(t, entityRoot, entityProfile))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if decision.Decision != DecisionProceed || decision.EntityUnits != 1 {
		t.Fatalf("unexpected entity decision: %+v", decision)
	}
	entityPlan, ok := entityPlanValue.(*EntityPlan)
	if !ok || entityPlan == nil || len(entityPlan.Entities) != 1 || entityPlan.Entities[0].Name != "handlesPacketBoundary" {
		t.Fatalf("unexpected entity plan type: %T", entityPlanValue)
	}
}

func TestBuildEntityPlansAcrossLanguages(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"session_test.cc":  "TEST(SessionTest, HandlesPacketBoundary) {\n}\n\nTEST(SessionTest, SetupHelper) {\n}\n",
		"test_session.py":  "class TestSession:\n    def setup_method(self):\n        pass\n\n    def test_handles_packet(self):\n        pass\n",
		"SessionTest.java": "public class SessionTest {\n  @Test\n  public void handlesPacketBoundary() {\n  }\n\n  private void setupHelper() {\n  }\n}\n",
		"session_test.go":  "func TestHandlesPacketBoundary(t *testing.T) {\n}\n\nfunc testHelper(t *testing.T) {\n}\n",
	}
	for path, content := range files {
		if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	scanProfile := profile.ScanProfile{
		Version: 1, Name: profile.NameTestEntityReview, TargetScope: "test", EntityKind: "test_case",
		Languages: []string{profile.LanguageCPP, profile.LanguagePython, profile.LanguageJava, profile.LanguageGo},
	}
	decision, _, planValue, err := Build(testInput(t, root, scanProfile))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if decision.Decision != DecisionProceed || decision.EntityUnits != 4 {
		t.Fatalf("unexpected decision: %+v", decision)
	}
	entityPlan, ok := planValue.(*EntityPlan)
	if !ok {
		t.Fatalf("unexpected entity plan type: %T", planValue)
	}
	units := entityPlan.PrimaryUnits()
	if len(units) != len(entityPlan.Entities) {
		t.Fatalf("PrimaryUnits() returned %d units, want %d", len(units), len(entityPlan.Entities))
	}
	for i, unit := range units {
		if unit.ID != entityPlan.Entities[i].EntityID || unit.Kind != coverage.PlanUnitEntity {
			t.Fatalf("PrimaryUnits()[%d] = %+v, want entity identity", i, unit)
		}
	}
	entities := make(map[string]TestEntity, len(entityPlan.Entities))
	for _, entity := range entityPlan.Entities {
		entities[entity.Language] = entity
	}
	if entities[profile.LanguageCPP].Suite != "SessionTest" || entities[profile.LanguagePython].Suite != "TestSession" ||
		entities[profile.LanguageJava].Name != "handlesPacketBoundary" || entities[profile.LanguageGo].Name != "TestHandlesPacketBoundary" {
		t.Fatalf("unexpected entities: %+v", entityPlan.Entities)
	}
}
