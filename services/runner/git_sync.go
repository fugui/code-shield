package runner

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"code-shield/models"
)

var (
	repoSyncMu    sync.Mutex
	repoSyncLocks = make(map[string]*sync.Mutex)
)

// GetRepoSyncLock 获取指定仓库物理路径对应的互斥锁，确保同一代码仓的 Git 同步操作严格串行排队
func GetRepoSyncLock(repoPath string) *sync.Mutex {
	repoSyncMu.Lock()
	defer repoSyncMu.Unlock()
	l, exists := repoSyncLocks[repoPath]
	if !exists {
		l = &sync.Mutex{}
		repoSyncLocks[repoPath] = l
	}
	return l
}

// CleanStaleGitLocks 检查并清理由于历史进程崩溃或异常中断残留的 .git/*.lock 文件
func CleanStaleGitLocks(codesPath string) {
	gitDir := filepath.Join(codesPath, ".git")
	if stat, err := os.Stat(gitDir); err != nil || !stat.IsDir() {
		return
	}

	lockFiles := []string{
		filepath.Join(gitDir, "index.lock"),
		filepath.Join(gitDir, "shallow.lock"),
		filepath.Join(gitDir, "config.lock"),
		filepath.Join(gitDir, "HEAD.lock"),
	}

	_ = filepath.Walk(filepath.Join(gitDir, "refs"), func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(path, ".lock") {
			lockFiles = append(lockFiles, path)
		}
		return nil
	})

	for _, lockPath := range lockFiles {
		if stat, err := os.Stat(lockPath); err == nil && !stat.IsDir() {
			log.Printf("[GitSync] Detected stale git lock: %s (modTime: %v), auto-cleaning to heal repository\n",
				lockPath, stat.ModTime())
			_ = os.Remove(lockPath)
		}
	}
}

// ExecCommandWithProcessGroup 执行外部命令，并为其创建独立的 Linux 进程组 (Setpgid)。
// 当 Context 被取消时，向整个进程组 (-pgid) 发送 SIGKILL 强杀，彻底杜绝孤儿进程占用 CPU。
func ExecCommandWithProcessGroup(ctx context.Context, dir string, name string, args ...string) ([]byte, int, error) {
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGKILL,
	}

	var outputBuf bytes.Buffer
	cmd.Stdout = &outputBuf
	cmd.Stderr = &outputBuf

	if err := cmd.Start(); err != nil {
		return nil, -1, err
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		exitCode := 0
		if cmd.ProcessState != nil {
			exitCode = cmd.ProcessState.ExitCode()
		}
		return outputBuf.Bytes(), exitCode, err
	case <-ctx.Done():
		if cmd.Process != nil {
			pgid := cmd.Process.Pid
			log.Printf("[ProcessGroup] Context canceled, killing entire process group %d (%s %v)\n", pgid, name, args)
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		}
		<-done
		return outputBuf.Bytes(), -1, ctx.Err()
	}
}

// PrepareAndSync 解析 URL，在仓库级互斥排队锁保护下执行 Git 同步与分支检出
func PrepareAndSync(ctx context.Context, repo models.Repository, reportID uint, repoURL string) (string, error) {
	u, err := url.Parse(repoURL)
	if err != nil {
		return "", fmt.Errorf("invalid repository URL: %w", err)
	}

	rawPath := strings.TrimSuffix(strings.TrimPrefix(u.Path, "/"), ".git")
	codesPath := filepath.Join(models.AppConfig.GetDataDir(), "codes", rawPath)

	if err := os.MkdirAll(filepath.Dir(codesPath), 0755); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}

	repoLock := GetRepoSyncLock(codesPath)
	repoLock.Lock()
	defer repoLock.Unlock()

	UpdateTaskStatus(reportID, models.StatusCloning)

	branch := strings.TrimSpace(repo.Branch)
	if branch == "" {
		branch = "master"
	}

	CleanStaleGitLocks(codesPath)

	var output []byte
	var gitErr error

	for attempt := 1; attempt <= 2; attempt++ {
		output, gitErr = ExecGitSync(ctx, codesPath, branch, repoURL)
		if gitErr == nil {
			break
		}

		errStr := string(output) + " " + gitErr.Error()
		if strings.Contains(errStr, ".lock") || strings.Contains(errStr, "File exists") || strings.Contains(errStr, "Another git process") {
			log.Printf("[GitSync] Git lock contention detected on attempt %d for %s, auto-healing locks and retrying...\n",
				attempt, codesPath)
			CleanStaleGitLocks(codesPath)
			time.Sleep(500 * time.Millisecond)
			continue
		}
		break
	}

	if gitErr != nil {
		if models.DB != nil {
			models.DB.Model(&models.TaskReport{}).Where("id = ?", reportID).Update("clone_status", "failed")
		}
		return codesPath, fmt.Errorf("git operation failed: %s", string(output))
	}

	if models.DB != nil {
		models.DB.Model(&models.TaskReport{}).Where("id = ?", reportID).Update("clone_status", "success")
	}
	return codesPath, nil
}

// ExecGitSync 执行单次底层的 Git Clone / Fetch / Checkout / Pull
func ExecGitSync(ctx context.Context, codesPath, branch, repoURL string) ([]byte, error) {
	var output []byte
	var gitErr error

	if stat, errStat := os.Stat(filepath.Join(codesPath, ".git")); errStat == nil && stat.IsDir() {
		log.Printf("[GitSync] Updating existing repository in %s for branch %s\n", codesPath, branch)
		remotesOut, _, _ := ExecCommandWithProcessGroup(ctx, codesPath, "git", "-C", codesPath, "remote")
		hasOrigin := strings.Contains(string(remotesOut), "origin")

		if hasOrigin {
			if out, _, err := ExecCommandWithProcessGroup(ctx, codesPath, "git", "-C", codesPath, "fetch", "--all", "--prune"); err != nil {
				output = out
				gitErr = err
			}
		}

		if gitErr == nil {
			if out, _, err := ExecCommandWithProcessGroup(ctx, codesPath, "git", "-C", codesPath, "checkout", "-f", branch); err != nil {
				if hasOrigin {
					if out2, _, err2 := ExecCommandWithProcessGroup(ctx, codesPath, "git", "-C", codesPath, "checkout", "-B", branch, "origin/"+branch); err2 != nil {
						output = out2
						gitErr = err2
					}
				} else {
					output = out
					gitErr = err
				}
			}
		}

		if gitErr == nil && hasOrigin {
			out, _, err := ExecCommandWithProcessGroup(ctx, codesPath, "git", "-C", codesPath, "pull", "origin", branch)
			if err != nil {
				output = out
				gitErr = err
			}
		}
	} else {
		log.Printf("[GitSync] Running git clone (branch: %s) %s %s\n", branch, repoURL, codesPath)
		output, _, gitErr = ExecCommandWithProcessGroup(ctx, "", "git", "clone", "-b", branch, repoURL, codesPath)
		if gitErr != nil {
			if branch == "master" {
				log.Printf("[GitSync] Clone with -b %s failed, trying fallback standard clone %s\n", branch, repoURL)
				outputFallback, _, fallbackErr := ExecCommandWithProcessGroup(ctx, "", "git", "clone", repoURL, codesPath)
				if fallbackErr == nil {
					gitErr = nil
					output = outputFallback
				}
			}
		}
	}

	return output, gitErr
}
