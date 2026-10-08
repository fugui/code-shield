package runner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
