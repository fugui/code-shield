package runner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"code-shield/models"
)

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{
		"-c", "user.name=Code Shield Test",
		"-c", "user.email=shield-test@example.com",
	}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func readFileForTest(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func TestExecGitSyncCleansExistingWorkspaceBeforePull(t *testing.T) {
	tempDir := t.TempDir()
	originPath := filepath.Join(tempDir, "origin")
	workPath := filepath.Join(tempDir, "work")

	if err := os.MkdirAll(originPath, 0755); err != nil {
		t.Fatal(err)
	}
	runGit(t, originPath, "init", "--initial-branch=master")
	if err := os.WriteFile(filepath.Join(originPath, ".gitignore"), []byte("ignored-cache.txt\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(originPath, "source.txt"), []byte("initial\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, originPath, "add", ".")
	runGit(t, originPath, "commit", "-m", "initial")

	runGit(t, tempDir, "clone", originPath, workPath)

	if err := os.WriteFile(filepath.Join(originPath, "source.txt"), []byte("updated on origin\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(originPath, "added-on-origin.txt"), []byte("from origin\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, originPath, "add", "added-on-origin.txt")
	runGit(t, originPath, "commit", "-am", "update source")

	if err := os.WriteFile(filepath.Join(workPath, "source.txt"), []byte("modified locally\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workPath, "added-on-origin.txt"), []byte("scanner residue\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workPath, "tmp"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workPath, "tmp", "scanner-output.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workPath, "ignored-cache.txt"), []byte("ignored"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := ExecGitSync(context.Background(), workPath, "master", originPath); err != nil {
		t.Fatalf("ExecGitSync failed: %v", err)
	}

	if got := readFileForTest(t, filepath.Join(workPath, "source.txt")); got != "updated on origin\n" {
		t.Fatalf("source was not reset/pulled, got %q", got)
	}
	if got := readFileForTest(t, filepath.Join(workPath, "added-on-origin.txt")); got != "from origin\n" {
		t.Fatalf("workspace was not cleaned before pull, got %q", got)
	}
	if _, err := os.Stat(filepath.Join(workPath, "tmp", "scanner-output.json")); !os.IsNotExist(err) {
		t.Fatalf("untracked scanner artifact was not cleaned, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(workPath, "ignored-cache.txt")); err != nil {
		t.Fatalf("ignored cache should be preserved without git clean -x: %v", err)
	}

	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = workPath
	if out, err := cmd.CombinedOutput(); err != nil || strings.TrimSpace(string(out)) != "" {
		t.Fatalf("workspace is not clean after sync: err=%v output=%q", err, string(out))
	}
}

func TestIsGitAuthOrPermissionError(t *testing.T) {
	authErrors := []string{
		"Permission denied (publickey)",
		"fatal: Authentication failed for 'https://git.example.com/repo.git'",
		"fatal: could not read Username for 'https://git.example.com': terminal prompts disabled",
		"fatal: could not read Password for 'https://git.example.com': terminal prompts disabled",
		"Host key verification failed.",
		"remote: Repository not found.",
		"fatal: repository 'https://git.example.com/repo.git/' does not exist",
		"error: The requested URL returned error: 401",
		"error: The requested URL returned error: 403",
		"remote: Access denied",
		"remote: You are not allowed to access this repository",
		"remote: Unauthorized",
	}

	for _, msg := range authErrors {
		if !IsGitAuthOrPermissionError(msg) {
			t.Errorf("expected %q to be identified as auth/permission error", msg)
		}
	}

	nonAuthErrors := []string{
		"fatal: Need to specify how to reconcile divergent branches.",
		"fatal: index file corrupt",
		"fatal: bad object HEAD",
		"error: inflate: data stream error",
		"fatal: corrupt loose object",
		"fatal: refusing to merge unrelated histories",
	}

	for _, msg := range nonAuthErrors {
		if IsGitAuthOrPermissionError(msg) {
			t.Errorf("expected %q to NOT be identified as auth/permission error", msg)
		}
	}
}

func TestExecGitSync_DivergentBranchesAutoRecovery(t *testing.T) {
	tempDir := t.TempDir()
	originPath := filepath.Join(tempDir, "origin")
	workPath := filepath.Join(tempDir, "work")

	if err := os.MkdirAll(originPath, 0755); err != nil {
		t.Fatal(err)
	}
	runGit(t, originPath, "init", "--initial-branch=master")
	if err := os.WriteFile(filepath.Join(originPath, "file.txt"), []byte("commit 1 on origin\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, originPath, "add", ".")
	runGit(t, originPath, "commit", "-m", "commit 1")

	// Clone origin to work
	runGit(t, tempDir, "clone", originPath, workPath)

	// In work repo: create a local commit (commit 2)
	if err := os.WriteFile(filepath.Join(workPath, "file.txt"), []byte("commit 2 on local\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, workPath, "commit", "-am", "commit 2 local")

	// In origin repo: create a different commit (commit 3), causing branches to diverge!
	if err := os.WriteFile(filepath.Join(originPath, "file.txt"), []byte("commit 3 on origin\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, originPath, "commit", "-am", "commit 3 origin")

	// Now run ExecGitSync on workPath
	// In the old implementation (git pull), this would fail with:
	// "fatal: Need to specify how to reconcile divergent branches."
	if _, err := ExecGitSync(context.Background(), workPath, "master", originPath); err != nil {
		t.Fatalf("ExecGitSync should auto-recover from divergent branches, got error: %v", err)
	}

	// Verify workPath has cleanly aligned with origin's commit 3
	if got := readFileForTest(t, filepath.Join(workPath, "file.txt")); got != "commit 3 on origin\n" {
		t.Fatalf("expected work repo to align with origin's commit 3, got %q", got)
	}

	// Verify status is completely clean
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = workPath
	if out, err := cmd.CombinedOutput(); err != nil || strings.TrimSpace(string(out)) != "" {
		t.Fatalf("workspace should be clean after divergent branch sync: err=%v, out=%s", err, string(out))
	}
}

func TestExecGitSync_RemoteForcePushAutoRecovery(t *testing.T) {
	tempDir := t.TempDir()
	originPath := filepath.Join(tempDir, "origin")
	workPath := filepath.Join(tempDir, "work")

	if err := os.MkdirAll(originPath, 0755); err != nil {
		t.Fatal(err)
	}
	runGit(t, originPath, "init", "--initial-branch=master")
	if err := os.WriteFile(filepath.Join(originPath, "file.txt"), []byte("base commit\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, originPath, "add", ".")
	runGit(t, originPath, "commit", "-m", "base")

	// Clone origin to work
	runGit(t, tempDir, "clone", originPath, workPath)

	// Origin adds commit B
	if err := os.WriteFile(filepath.Join(originPath, "file.txt"), []byte("commit B\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, originPath, "commit", "-am", "commit B")

	// Work syncs commit B
	if _, err := ExecGitSync(context.Background(), workPath, "master", originPath); err != nil {
		t.Fatalf("sync commit B failed: %v", err)
	}

	// Simulate force-push on origin: reset to base, make commit C
	runGit(t, originPath, "reset", "--hard", "HEAD~1")
	if err := os.WriteFile(filepath.Join(originPath, "file.txt"), []byte("commit C after force-push\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, originPath, "commit", "-am", "commit C rewrite")

	// Work syncs again. Under git pull this would fail or merge. Under fetch + reset it must align to commit C!
	if _, err := ExecGitSync(context.Background(), workPath, "master", originPath); err != nil {
		t.Fatalf("ExecGitSync should handle remote force-push/rebase, got error: %v", err)
	}

	if got := readFileForTest(t, filepath.Join(workPath, "file.txt")); got != "commit C after force-push\n" {
		t.Fatalf("expected work repo to align with force-pushed commit C, got %q", got)
	}
}

func TestPrepareAndSync_CorruptedCacheAutoHealing(t *testing.T) {
	tempDir := t.TempDir()
	originPath := filepath.Join(tempDir, "origin")

	if err := os.MkdirAll(originPath, 0755); err != nil {
		t.Fatal(err)
	}
	runGit(t, originPath, "init", "--initial-branch=master")
	if err := os.WriteFile(filepath.Join(originPath, "hello.txt"), []byte("hello world\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, originPath, "add", ".")
	runGit(t, originPath, "commit", "-m", "initial")

	// Setup AppConfig storage
	models.AppConfig.Server.DataDir = tempDir

	repo := models.Repository{
		URL:    originPath,
		Branch: "master",
	}

	// 1. First sync: should clone successfully
	codesPath, err := PrepareAndSync(context.Background(), repo, 0, originPath)
	if err != nil {
		t.Fatalf("first PrepareAndSync failed: %v", err)
	}
	if got := readFileForTest(t, filepath.Join(codesPath, "hello.txt")); got != "hello world\n" {
		t.Fatalf("expected hello world, got %q", got)
	}

	// 2. Corrupt git metadata (corrupt HEAD to invalid ref)
	if err := os.WriteFile(filepath.Join(codesPath, ".git", "HEAD"), []byte("garbage corrupt content\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// 3. Second sync: should detect corruption (non-permission failure), purge cache, re-clone and heal!
	healedPath, err := PrepareAndSync(context.Background(), repo, 0, originPath)
	if err != nil {
		t.Fatalf("second PrepareAndSync should auto-heal corrupted cache, got: %v", err)
	}

	if got := readFileForTest(t, filepath.Join(healedPath, "hello.txt")); got != "hello world\n" {
		t.Fatalf("expected auto-healed repo to have hello world, got %q", got)
	}
}
