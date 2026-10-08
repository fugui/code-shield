package planner

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"code-shield/services/engines/profile"
)

func runGitCommand(t *testing.T, repoPath string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repoPath}, arguments...)...)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	command.Env = append(os.Environ(), "TZ=UTC", "LC_ALL=C")
	if err := command.Run(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(arguments, " "), err, stderr.String())
	}
}

func commitAt(t *testing.T, repoPath, message, commitTime string) string {
	t.Helper()
	runGitCommand(t, repoPath, "add", "-A")
	command := exec.Command(
		"git", "-C", repoPath,
		"-c", "user.name=Test", "-c", "user.email=test@example.com",
		"commit", "-m", message,
	)
	command.Env = append(
		os.Environ(),
		"TZ=UTC", "LC_ALL=C",
		"GIT_AUTHOR_DATE="+commitTime,
		"GIT_COMMITTER_DATE="+commitTime,
	)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("commit %q: %v: %s", message, err, stderr.String())
	}
	output, err := exec.Command("git", "-C", repoPath, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(output))
}

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for path, content := range files {
		fullPath := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestResolveChangeBaselineUsesCommitBeforeCutoff(t *testing.T) {
	repo := t.TempDir()
	runGitCommand(t, repo, "init", "-q", "--initial-branch=main")
	writeFiles(t, repo, map[string]string{"base.txt": "base\n"})
	base := commitAt(t, repo, "base", "2025-01-01T12:00:00Z")
	writeFiles(t, repo, map[string]string{"boundary.txt": "boundary\n"})
	_ = commitAt(t, repo, "boundary", "2025-01-02T12:00:00Z")
	writeFiles(t, repo, map[string]string{"head.txt": "head\n"})
	head := commitAt(t, repo, "head", "2025-01-03T12:00:00Z")

	baseline, err := ResolveChangeBaseline(
		context.Background(), repo,
		profile.BasePolicy{Strategy: "since_days", SinceDays: 7},
		time.Date(2025, 1, 9, 12, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	if baseline.BaseCommit != base || baseline.HeadCommit != head {
		t.Fatalf("unexpected baseline: base=%q head=%q", baseline.BaseCommit, baseline.HeadCommit)
	}
	if !baseline.BaseCommitTime.Equal(time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected base commit time: %s", baseline.BaseCommitTime)
	}
	if !baseline.CutoffAt.Equal(time.Date(2025, 1, 2, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected cutoff: %s", baseline.CutoffAt)
	}
}

func TestResolveChangeBaselineUsesEmptyTreeWhenNoPriorCommit(t *testing.T) {
	repo := t.TempDir()
	runGitCommand(t, repo, "init", "-q", "--initial-branch=main")
	writeFiles(t, repo, map[string]string{"only.txt": "only\n"})
	head := commitAt(t, repo, "only", "2025-01-03T12:00:00Z")

	baseline, err := ResolveChangeBaseline(
		context.Background(), repo,
		profile.BasePolicy{Strategy: "since_days", SinceDays: 7},
		time.Date(2025, 1, 9, 12, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	if baseline.BaseCommit != emptyTreeObjectID || baseline.HeadCommit != head {
		t.Fatalf("unexpected empty tree baseline: %+v", baseline)
	}
}

func TestResolveChangeBaselineFailsInsufficientShallowHistory(t *testing.T) {
	full := t.TempDir()
	runGitCommand(t, full, "init", "-q", "--initial-branch=main")
	writeFiles(t, full, map[string]string{"base.txt": "base\n"})
	_ = commitAt(t, full, "base", "2025-01-01T12:00:00Z")
	writeFiles(t, full, map[string]string{"head.txt": "head\n"})
	_ = commitAt(t, full, "head", "2025-01-03T12:00:00Z")

	shallow := filepath.Join(t.TempDir(), "shallow")
	runGitCommand(t, full, "clone", "-q", "--depth=1", "file://"+full, shallow)
	_, err := ResolveChangeBaseline(
		context.Background(), shallow,
		profile.BasePolicy{Strategy: "since_days", SinceDays: 7},
		time.Date(2025, 1, 9, 12, 0, 0, 0, time.UTC),
	)
	if !errors.Is(err, ErrInsufficientHistory) {
		t.Fatalf("ResolveChangeBaseline() error = %v, want ErrInsufficientHistory", err)
	}
}

func TestBuildChangePlanCoversAllChangeKinds(t *testing.T) {
	repo := t.TempDir()
	runGitCommand(t, repo, "init", "-q", "--initial-branch=main")
	writeFiles(t, repo, map[string]string{
		"modified.py":  "value = 1\n",
		"deleted.java": "class Deleted {\n}\n",
		"renamed.cc":   "int old_name() { return 0; }\n",
	})
	base := commitAt(t, repo, "base", "2025-01-01T12:00:00Z")
	writeFiles(t, repo, map[string]string{"modified.py": "value = 2\n"})
	if err := os.Remove(filepath.Join(repo, "deleted.java")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(repo, "renamed.cc"), filepath.Join(repo, "renamed.hpp")); err != nil {
		t.Fatal(err)
	}
	writeFiles(t, repo, map[string]string{"added.cpp": "int added() { return 1; }\n"})
	head := commitAt(t, repo, "changes", "2025-01-03T12:00:00Z")

	baseline := ChangeBaseline{
		Version: 1, Strategy: "since_days", SinceDays: 7,
		CutoffAt:   time.Date(2025, 1, 2, 12, 0, 0, 0, time.UTC),
		BaseCommit: base, BaseCommitTime: time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
		HeadCommit: head,
	}
	scanProfile := profile.ScanProfile{
		Version: 1, Name: profile.NameChangeReview,
		Languages:   []string{profile.LanguageCPP, profile.LanguagePython, profile.LanguageJava},
		ScopePolicy: "changed_hunks", ContextPolicy: "changed_files",
	}
	plan, err := BuildChangePlan(context.Background(), repo, scanProfile, profile.Hash(scanProfile), baseline)
	if err != nil {
		t.Fatal(err)
	}
	if plan.PrimaryCount() != 4 {
		t.Fatalf("PrimaryCount() = %d, want 4; files=%+v", plan.PrimaryCount(), plan.Files)
	}
	kinds := make(map[string]string)
	for _, file := range plan.Files {
		kinds[file.Path] = file.ChangeKind
	}
	want := map[string]string{
		"added.cpp":    ChangeKindAdded,
		"deleted.java": ChangeKindDeleted,
		"modified.py":  ChangeKindModified,
		"renamed.hpp":  ChangeKindRenamed,
	}
	if len(kinds) != len(want) {
		t.Fatalf("unexpected paths: %+v", kinds)
	}
	for path, kind := range want {
		if kinds[path] != kind {
			t.Fatalf("path %s kind = %q, want %q", path, kinds[path], kind)
		}
	}
	renamed := findChangeFile(t, plan, "renamed.hpp")
	if renamed.Evidence == nil ||
		len(renamed.Evidence.RenameMap) != 1 ||
		renamed.Evidence.RenameMap[0].From != "renamed.cc" ||
		renamed.Evidence.RenameMap[0].To != "renamed.hpp" {
		t.Fatalf("missing rename map: %+v", renamed)
	}
	for _, file := range plan.Files {
		for _, hunk := range file.Hunks {
			if hunk.ID == "" || hunk.ContentHash == "" {
				t.Fatalf("unstable hunk identity in %+v", hunk)
			}
		}
	}
}

func TestBuildChangePlanHandlesBinaryAndEmptyDiff(t *testing.T) {
	repo := t.TempDir()
	runGitCommand(t, repo, "init", "-q", "--initial-branch=main")
	writeFiles(t, repo, map[string]string{"sample.cpp": "int sample() { return 0; }\n"})
	base := commitAt(t, repo, "base", "2025-01-01T12:00:00Z")

	scanProfile := profile.ScanProfile{
		Version: 1, Name: profile.NameChangeReview,
		Languages:   []string{profile.LanguageCPP, profile.LanguagePython, profile.LanguageJava},
		ScopePolicy: "changed_hunks", ContextPolicy: "changed_files",
	}
	baseline := ChangeBaseline{Version: 1, Strategy: "since_days", SinceDays: 7, BaseCommit: base, HeadCommit: base}
	emptyPlan, err := BuildChangePlan(context.Background(), repo, scanProfile, profile.Hash(scanProfile), baseline)
	if err != nil {
		t.Fatal(err)
	}
	if emptyPlan.PrimaryCount() != 0 || emptyPlan.ManifestHash == "" {
		t.Fatalf("unexpected empty diff plan: %+v", emptyPlan)
	}

	if err := os.WriteFile(filepath.Join(repo, "sample.bin.cpp"), []byte{0, 1}, 0644); err != nil {
		t.Fatal(err)
	}
	binaryBase := commitAt(t, repo, "binary base", "2025-01-02T12:00:00Z")
	if err := os.WriteFile(filepath.Join(repo, "sample.bin.cpp"), []byte{0, 2}, 0644); err != nil {
		t.Fatal(err)
	}
	binaryHead := commitAt(t, repo, "binary head", "2025-01-03T12:00:00Z")
	binaryBaseline := ChangeBaseline{Version: 1, Strategy: "since_days", SinceDays: 7, BaseCommit: binaryBase, HeadCommit: binaryHead}
	binaryPlan, err := BuildChangePlan(context.Background(), repo, scanProfile, profile.Hash(scanProfile), binaryBaseline)
	if err != nil {
		t.Fatal(err)
	}
	if len(binaryPlan.Files) != 1 || binaryPlan.Files[0].Path != "sample.bin.cpp" ||
		!binaryPlan.Files[0].Binary || len(binaryPlan.Files[0].Hunks) != 0 {
		t.Fatalf("unexpected binary plan: %+v", binaryPlan.Files)
	}
	if binaryPlan.Files[0].Evidence == nil || len(binaryPlan.Files[0].Evidence.BaseFileSnippet) != 0 ||
		len(binaryPlan.Files[0].Evidence.HeadFileSnippet) != 0 {
		t.Fatalf("binary evidence must not decode content: %+v", binaryPlan.Files[0].Evidence)
	}
}

func TestBuildChangePlanAttachesEvidence(t *testing.T) {
	repo := t.TempDir()
	runGitCommand(t, repo, "init", "-q", "--initial-branch=main")
	writeFiles(t, repo, map[string]string{
		"src/session.cpp":      "int first() { return 0; }\n\nint second() { return 0; }\n",
		"src/legacy.cpp":       "int legacy() { return 0; }\n",
		"src/session_test.cpp": "int test_session() { return 0; }\n",
	})
	base := commitAt(t, repo, "base", "2025-01-01T12:00:00Z")
	writeFiles(t, repo, map[string]string{
		"src/session.cpp":      "int first() { return 1; }\n\nint second() { return 2; }\n",
		"src/session_test.cpp": "int test_session() { return 1; }\n",
	})
	if err := os.Remove(filepath.Join(repo, "src", "legacy.cpp")); err != nil {
		t.Fatal(err)
	}
	head := commitAt(t, repo, "changes", "2025-01-03T12:00:00Z")

	scanProfile := profile.ScanProfile{
		Version: 1, Name: profile.NameChangeReview,
		Languages:   []string{profile.LanguageCPP, profile.LanguagePython, profile.LanguageJava},
		ScopePolicy: "changed_hunks", ContextPolicy: "changed_files",
	}
	baseline := ChangeBaseline{
		Version: 1, Strategy: "since_days", SinceDays: 7,
		BaseCommit: base, HeadCommit: head,
	}
	plan, err := BuildChangePlan(context.Background(), repo, scanProfile, profile.Hash(scanProfile), baseline)
	if err != nil {
		t.Fatal(err)
	}
	if plan.PrimaryCount() != 4 {
		t.Fatalf("PrimaryCount() = %d, want 4", plan.PrimaryCount())
	}

	modified := findChangeFile(t, plan, "src/session.cpp")
	if len(modified.Hunks) != 2 {
		t.Fatalf("session hunks = %d, want 2", len(modified.Hunks))
	}
	for _, hunk := range modified.Hunks {
		evidence := hunk.Evidence
		if evidence == nil {
			t.Fatalf("missing evidence for %s", hunk.ID)
		}
		if evidence.Anchor.HunkID != hunk.ID || evidence.Anchor.BaseCommit != base || evidence.Anchor.HeadCommit != head {
			t.Fatalf("invalid evidence anchor: %+v", evidence.Anchor)
		}
		if len(evidence.BaseFileSnippet) == 0 || len(evidence.HeadFileSnippet) == 0 || len(evidence.ChangedHunk) == 0 {
			t.Fatalf("incomplete hunk evidence: %+v", evidence)
		}
		if evidence.TestRelationship != "production_file" {
			t.Fatalf("unexpected test relationship: %q", evidence.TestRelationship)
		}
	}
	deleted := findChangeFile(t, plan, "src/legacy.cpp")
	if len(deleted.Hunks) == 0 || deleted.Hunks[0].Evidence == nil || len(deleted.Hunks[0].Evidence.DeletedFileContext) == 0 {
		t.Fatalf("missing deleted context: %+v", deleted)
	}
	testFile := findChangeFile(t, plan, "src/session_test.cpp")
	if len(testFile.Hunks) == 0 || testFile.Hunks[0].Evidence == nil ||
		testFile.Hunks[0].Evidence.TestRelationship != "test_file" {
		t.Fatalf("missing test relationship: %+v", testFile)
	}
}

func TestBuildChangePlanBundlesOneFileThenHunkGroups(t *testing.T) {
	repo := t.TempDir()
	runGitCommand(t, repo, "init", "-q", "--initial-branch=main")
	writeFiles(t, repo, map[string]string{
		"src/session.cpp": "int one() { return 0; }\nint two() { return 0; }\nint three() { return 0; }\n",
		"src/other.py":    "value = 0\n",
	})
	base := commitAt(t, repo, "base", "2025-01-01T12:00:00Z")
	writeFiles(t, repo, map[string]string{
		"src/session.cpp": "int one() { return 1; }\nint two() { return 0; }\nint three() { return 2; }\n",
		"src/other.py":    "value = 1\n",
	})
	head := commitAt(t, repo, "changes", "2025-01-03T12:00:00Z")
	baseline := ChangeBaseline{Version: 1, Strategy: "since_days", SinceDays: 7, BaseCommit: base, HeadCommit: head}
	scanProfile := profile.ScanProfile{
		Version: 1, Name: profile.NameChangeReview,
		Languages:   []string{profile.LanguageCPP, profile.LanguagePython, profile.LanguageJava},
		ScopePolicy: "changed_hunks", ContextPolicy: "changed_files",
	}
	plan, err := BuildChangePlan(context.Background(), repo, scanProfile, profile.Hash(scanProfile), baseline)
	if err != nil {
		t.Fatal(err)
	}
	if plan.PrimaryCount() != 3 {
		t.Fatalf("PrimaryCount() = %d, want 3", plan.PrimaryCount())
	}

	defaultBundles, err := plan.BuildBundles(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(defaultBundles) != 2 {
		t.Fatalf("default bundle count = %d, want 2; names=%v", len(defaultBundles), bundleNames(defaultBundles))
	}
	splitBundles, err := plan.BuildBundles(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(splitBundles) != 3 {
		t.Fatalf("split bundle count = %d, want 3; names=%v", len(splitBundles), bundleNames(splitBundles))
	}
	for _, bundle := range splitBundles {
		if len(bundle.PrimaryUnits) != 1 || bundle.ManifestHash == "" {
			t.Fatalf("invalid split bundle: %+v", bundle)
		}
		if len(bundle.PrimaryUnits[0].Evidence) == 0 {
			t.Fatalf("split bundle missing evidence: %+v", bundle.PrimaryUnits[0])
		}
	}
}

func findChangeFile(t *testing.T, plan *ChangePlan, path string) ChangeFile {
	t.Helper()
	for _, file := range plan.Files {
		if file.Path == path {
			return file
		}
	}
	t.Fatalf("change file %q not found", path)
	return ChangeFile{}
}

func bundleNames(bundles []ChangeBundle) []string {
	names := make([]string, 0, len(bundles))
	for _, bundle := range bundles {
		names = append(names, bundle.Name)
	}
	return names
}

func TestBuildChangePlanIsDeterministic(t *testing.T) {
	repo := t.TempDir()
	runGitCommand(t, repo, "init", "-q", "--initial-branch=main")
	writeFiles(t, repo, map[string]string{"session.cpp": "int session() { return 0; }\n"})
	base := commitAt(t, repo, "base", "2025-01-01T12:00:00Z")
	writeFiles(t, repo, map[string]string{"session.cpp": "int session() { return 1; }\n"})
	head := commitAt(t, repo, "head", "2025-01-03T12:00:00Z")
	baseline := ChangeBaseline{Version: 1, Strategy: "since_days", SinceDays: 7, BaseCommit: base, HeadCommit: head}
	scanProfile := profile.ScanProfile{
		Version: 1, Name: profile.NameChangeReview,
		Languages:   []string{profile.LanguageCPP, profile.LanguagePython, profile.LanguageJava},
		ScopePolicy: "changed_hunks", ContextPolicy: "changed_files",
	}

	first, err := BuildChangePlan(context.Background(), repo, scanProfile, profile.Hash(scanProfile), baseline)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildChangePlan(context.Background(), repo, scanProfile, profile.Hash(scanProfile), baseline)
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, err := changePlanJSON(first)
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, err := changePlanJSON(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstJSON, secondJSON) || first.ManifestHash != second.ManifestHash {
		t.Fatalf("ChangePlan is not byte-stable:\nfirst=%s\nsecond=%s", firstJSON, secondJSON)
	}
}
