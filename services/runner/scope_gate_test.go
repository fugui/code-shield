package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"code-shield/models"
	"code-shield/services/engines"
	"code-shield/services/engines/planner"
	"code-shield/services/engines/profile"
	"time"
)

func fullReviewConfig() string {
	return `{"scan_profile":{"version":1,"name":"full_review","include_extensions":[".cpp"]}}`
}

func fullReviewSnapshot() ([]byte, engines.ParsedEngineProfile, error) {
	raw := []byte(`{"version":1,"name":"full_review","include_extensions":[".cpp"],"max_files":7}`)
	parsedProfile, err := engines.ParseProfileSnapshot(raw)
	return raw, parsedProfile, err
}

func taskReportSnapshot(t *testing.T, engineConfig string) models.TaskReport {
	t.Helper()
	parsedProfile, err := engines.ParseProfileConfig([]byte(engineConfig))
	if err != nil {
		t.Fatalf("ParseProfileConfig() error = %v", err)
	}
	snapshot, err := json.Marshal(parsedProfile.Profile)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	return models.TaskReport{ScanProfile: snapshot, ScanProfileHash: parsedProfile.Hash}
}

func TestRunScanProfileGateUsesReportSnapshot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ready.cpp"), []byte("void ready() { start(); }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	snapshot, parsedProfile, err := fullReviewSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	ctx := &TaskContext{
		CodesPath: root,
		Report: models.TaskReport{
			ScanProfile:     snapshot,
			ScanProfileHash: parsedProfile.Hash,
		},
		TaskType: models.TaskType{EngineConfig: []byte(`{"engine_config":"changed-after-enqueue"}`)},
	}

	_, primaryPlan, gotProfile, err := RunScanProfileGate(ctx)
	if err != nil {
		t.Fatalf("RunScanProfileGate() error = %v", err)
	}
	if gotProfile.Hash != parsedProfile.Hash || gotProfile.Profile.Name != profile.NameFullReview ||
		gotProfile.Config.MaxFiles != 7 || primaryPlan == nil || primaryPlan.PrimaryCount() != 1 {
		t.Fatalf("unexpected snapshot profile: got=%+v want=%+v", gotProfile, parsedProfile)
	}
}

func TestRunScanProfileGateRejectsMissingSnapshot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ready.cpp"), []byte("void ready() { start(); }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	ctx := &TaskContext{
		CodesPath: root,
		TaskType:  models.TaskType{EngineConfig: []byte(fullReviewConfig())},
	}

	_, _, _, err := RunScanProfileGate(ctx)
	if err == nil || !strings.Contains(err.Error(), "no scan profile execution snapshot") {
		t.Fatalf("RunScanProfileGate() error = %v, want missing snapshot", err)
	}
}

func TestRunScanProfileGateRejectsSnapshotHashMismatch(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ready.cpp"), []byte("void ready() { start(); }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	snapshot, parsedProfile, err := fullReviewSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	ctx := &TaskContext{
		CodesPath: root,
		Report: models.TaskReport{
			ScanProfile:     snapshot,
			ScanProfileHash: parsedProfile.Hash + "-mismatch",
		},
	}

	_, _, _, err = RunScanProfileGate(ctx)
	if err == nil || !strings.Contains(err.Error(), "scan profile hash mismatch") {
		t.Fatalf("RunScanProfileGate() error = %v, want hash mismatch", err)
	}
}

func TestRunScanProfileGateProceed(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ready.cpp"), []byte("void ready() { start(); }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	report := taskReportSnapshot(t, fullReviewConfig())
	ctx := &TaskContext{CodesPath: root, Report: report}

	scanPlan, primaryPlan, _, err := RunScanProfileGate(ctx)
	if err != nil {
		t.Fatalf("RunScanProfileGate() error = %v", err)
	}
	if scanPlan == nil || primaryPlan == nil || primaryPlan.PrimaryCount() != 1 {
		t.Fatalf("unexpected proceed result: plan=%#v primaryPlan=%#v", scanPlan, primaryPlan)
	}
	if ctx.Summary.ScopeDecision == nil || ctx.Summary.ScopeDecision.Decision != planner.DecisionProceed {
		t.Fatalf("unexpected decision: %+v", ctx.Summary.ScopeDecision)
	}
}

func TestRunScanProfileGateProceedDegraded(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ready.cpp"), []byte("void ready() { start(); }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "missing.cpp"), filepath.Join(root, "unknown.cpp")); err != nil {
		t.Fatal(err)
	}
	report := taskReportSnapshot(t, `{"scan_profile":{"version":1,"name":"keyword_review","content_keywords":["start"],"primary_unit":"file"}}`)
	ctx := &TaskContext{CodesPath: root, Report: report}

	_, primaryPlan, _, err := RunScanProfileGate(ctx)
	if err != nil {
		t.Fatalf("RunScanProfileGate() error = %v", err)
	}
	decision := ctx.Summary.ScopeDecision
	if primaryPlan == nil || decision == nil || decision.Decision != planner.DecisionProceedDegraded ||
		decision.Reason != planner.ReasonPartialPrimaryScope || decision.UnknownFiles != 1 {
		t.Fatalf("unexpected degraded result: decision=%+v primaryPlan=%#v", decision, primaryPlan)
	}
}

func TestRunScanProfileGateSkipped(t *testing.T) {
	root := t.TempDir()
	summaryPath := filepath.Join(root, "summary.json")
	report := taskReportSnapshot(t, fullReviewConfig())
	ctx := &TaskContext{CodesPath: root, JsonPath: summaryPath, Report: report}

	_, _, _, err := RunScanProfileGate(ctx)
	if !errors.Is(err, ErrSkipped) {
		baseline := "<nil>"
		if ctx.Summary.ScopeDecision != nil && ctx.Summary.ScopeDecision.ChangeBaseline != nil {
			baseline = fmt.Sprintf("%+v", *ctx.Summary.ScopeDecision.ChangeBaseline)
		}
		t.Fatalf("RunScanProfileGate() error = %v, want ErrSkipped; decision=%+v baseline=%s", err, ctx.Summary.ScopeDecision, baseline)
	}
	decision := ctx.Summary.ScopeDecision
	if ctx.Summary.Status != models.StatusSkipped || decision == nil ||
		decision.Decision != planner.DecisionSkipped || decision.Reason != planner.ReasonNoLanguageScope {
		t.Fatalf("unexpected skipped result: status=%q decision=%+v", ctx.Summary.Status, decision)
	}
	summary, readErr := os.ReadFile(summaryPath)
	if readErr != nil || !strings.Contains(string(summary), planner.ReasonNoLanguageScope) {
		t.Fatalf("skipped summary not written: readErr=%v content=%s", readErr, summary)
	}
}

func TestRunScanProfileGateFailed(t *testing.T) {
	invalidProfile, err := json.Marshal(profile.ScanProfile{Version: 1, Name: "unknown"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := &TaskContext{
		CodesPath: t.TempDir(),
		Report:    models.TaskReport{ScanProfile: invalidProfile, ScanProfileHash: "invalid"},
	}

	_, _, _, gateErr := RunScanProfileGate(ctx)
	if gateErr == nil || !strings.Contains(gateErr.Error(), "unsupported scan_profile.name") {
		t.Fatalf("RunScanProfileGate() error = %v, want unsupported profile", gateErr)
	}
	if ctx.Summary.Status != models.StatusFailed {
		t.Fatalf("Summary.Status = %q, want %q", ctx.Summary.Status, models.StatusFailed)
	}
}

func TestRunScanProfileGateActivatesChangeReview(t *testing.T) {
	root := t.TempDir()
	runGitCommand(t, root, "init", "-q", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(root, "session.cpp"), []byte("int session() { return 0; }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitCommit(t, root, "base", time.Now().AddDate(-2, 0, 0).Format(time.RFC3339))
	if err := os.WriteFile(filepath.Join(root, "session.cpp"), []byte("int session() { return 1; }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitCommit(t, root, "change", time.Now().Add(-time.Hour).Format(time.RFC3339))

	report := taskReportSnapshot(t, `{"scan_profile":{"version":1,"name":"change_review","languages":["cpp","python","java"],"scope_policy":"changed_hunks","context_policy":"changed_files","base_policy":{"strategy":"since_days","since_days":7}}}`)
	ctx := &TaskContext{CodesPath: root, Summary: TaskSummaryReport{StartTime: time.Now()}, Report: report}

	scanPlan, primaryPlan, _, err := RunScanProfileGate(ctx)
	if err != nil {
		t.Fatalf("RunScanProfileGate() error = %v", err)
	}
	decision := ctx.Summary.ScopeDecision
	if primaryPlan == nil || primaryPlan.PrimaryCount() != 1 {
		t.Fatalf("unexpected primary plan: %#v", primaryPlan)
	}
	if scanPlan == nil || len(scanPlan.Selected) != 1 || scanPlan.Selected[0].Path != "session.cpp" ||
		!scanPlan.Selected[0].DiffTouched || len(scanPlan.Selected[0].HunkRanges) != 1 ||
		scanPlan.Selected[0].HunkRanges[0] != "1" {
		t.Fatalf("unexpected changed-only scan plan: %+v", scanPlan)
	}
	if decision == nil || decision.Decision != planner.DecisionProceed ||
		decision.Reason != planner.ReasonPrimaryScopeReady ||
		decision.ChangeHunks != 1 || decision.PlanManifestHash == "" {
		t.Fatalf("unexpected change decision: %+v", decision)
	}
	if decision.ChangeBaseline == nil || decision.ChangeBaseline.BaseCommit == "" ||
		decision.ChangeBaseline.HeadCommit == "" || decision.ChangeBaseline.DiffManifestHash == "" {
		t.Fatalf("change baseline not prepared: %+v", decision.ChangeBaseline)
	}
	if decision.ChangeOverview == nil || decision.ChangeOverview.ModifiedFiles != 1 ||
		decision.ChangeOverview.ChangedHunks != 1 || len(decision.ChangeOverview.Files) != 1 ||
		decision.ChangeOverview.Files[0].HunkRanges[0] != "1" {
		t.Fatalf("change overview not prepared: %+v", decision.ChangeOverview)
	}
	if ctx.Report.BaseCommit != decision.ChangeBaseline.BaseCommit ||
		ctx.Report.DiffManifestHash != decision.ChangeBaseline.DiffManifestHash {
		t.Fatalf("baseline not projected to report: %+v", ctx.Report)
	}
}

func TestRunScanProfileGateSkipsEmptyChangeReview(t *testing.T) {
	root := t.TempDir()
	runGitCommand(t, root, "init", "-q", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(root, "session.cpp"), []byte("int session() { return 0; }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitCommit(t, root, "base", time.Now().AddDate(0, 0, -8).UTC().Format(time.RFC3339))

	report := taskReportSnapshot(t, changeReviewConfig())
	ctx := &TaskContext{CodesPath: root, Summary: TaskSummaryReport{StartTime: time.Now()}, Report: report}

	_, _, _, err := RunScanProfileGate(ctx)
	if !errors.Is(err, ErrSkipped) {
		baseline := "<nil>"
		if ctx.Summary.ScopeDecision != nil && ctx.Summary.ScopeDecision.ChangeBaseline != nil {
			baseline = fmt.Sprintf("%+v", *ctx.Summary.ScopeDecision.ChangeBaseline)
		}
		t.Fatalf("RunScanProfileGate() error = %v, want ErrSkipped; decision=%+v baseline=%s", err, ctx.Summary.ScopeDecision, baseline)
	}
	decision := ctx.Summary.ScopeDecision
	if ctx.Summary.Status != models.StatusSkipped || decision == nil ||
		decision.Decision != planner.DecisionSkipped || decision.Reason != planner.ReasonNoDiff {
		t.Fatalf("unexpected empty diff decision: status=%q decision=%+v", ctx.Summary.Status, decision)
	}
	if decision.ChangeBaseline == nil || decision.ChangeBaseline.BaseCommit == "" ||
		decision.ChangeBaseline.HeadCommit == "" {
		t.Fatalf("empty diff baseline not prepared: %+v", decision.ChangeBaseline)
	}
}

func changeReviewConfig() string {
	return `{"scan_profile":{"version":1,"name":"change_review","languages":["cpp","python","java"],"scope_policy":"changed_hunks","context_policy":"changed_files","base_policy":{"strategy":"since_days","since_days":7}}}`
}

func runGitCommand(t *testing.T, repoPath string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repoPath}, arguments...)...)
	command.Env = append(os.Environ(), "TZ=UTC", "LC_ALL=C")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(arguments, " "), err, output)
	}
}

func runGitCommit(t *testing.T, repoPath, message, commitTime string) {
	t.Helper()
	runGitCommand(t, repoPath, "add", "-A")
	command := exec.Command(
		"git", "-C", repoPath,
		"-c", "user.name=Test", "-c", "user.email=test@example.com",
		"commit", "-m", message,
	)
	command.Env = append(
		os.Environ(), "TZ=UTC", "LC_ALL=C",
		"GIT_AUTHOR_DATE="+commitTime, "GIT_COMMITTER_DATE="+commitTime,
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, output)
	}
}
