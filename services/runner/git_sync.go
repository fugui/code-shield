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

// IsGitAuthOrPermissionError 判断 Git 输出或错误信息是否属于权限、鉴权或凭据类问题。
// 此类问题属于外部配置或凭证缺失，不能通过本地缓存清理重试解决，应快速阻断并向用户明确报告。
func IsGitAuthOrPermissionError(msg string) bool {
	lower := strings.ToLower(msg)
	authPatterns := []string{
		"permission denied",
		"authentication failed",
		"could not read username",
		"could not read password",
		"host key verification failed",
		"repository not found",
		"does not exist",
		"returned error: 401",
		"returned error: 403",
		"access denied",
		"not allowed to",
		"unauthorized",
		"invalid credentials",
		"key exchange failed",
	}
	for _, pattern := range authPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
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
	if absCodesPath, absErr := filepath.Abs(codesPath); absErr == nil {
		codesPath = absCodesPath
	}

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

		// 1. 权限或鉴权错误：属于外部凭据缺失或仓库无权访问，不可自愈，快速失败并明确报错
		if IsGitAuthOrPermissionError(errStr) {
			log.Printf("[GitSync] Authentication/permission failure detected for %s: %s (fast-fail without retry)\n", codesPath, errStr)
			break
		}

		// 2. 锁冲突：清理残留锁文件并短暂退避后重试
		if strings.Contains(errStr, ".lock") || strings.Contains(errStr, "File exists") || strings.Contains(errStr, "Another git process") {
			log.Printf("[GitSync] Git lock contention detected on attempt %d for %s, auto-healing locks and retrying...\n",
				attempt, codesPath)
			CleanStaleGitLocks(codesPath)
			time.Sleep(500 * time.Millisecond)
			continue
		}

		// 3. 终极自愈：只要非权限问题，本地缓存出现任何损坏、分叉或残缺异常（如对象损坏、分叉阻断、残缺半克隆目录等），
		// 在第 1 次尝试失败后，彻底销毁本地缓存目录，重新发起全新的 git clone
		if attempt == 1 {
			if _, statErr := os.Stat(codesPath); statErr == nil {
				log.Printf("[GitSync] Non-permission git failure detected on attempt 1 for %s (%s). Auto-healing: purging local cache and performing fresh clone...\n",
					codesPath, errStr)
				if rmErr := os.RemoveAll(codesPath); rmErr != nil {
					log.Printf("[GitSync] Failed to purge corrupted cache directory %s: %v\n", codesPath, rmErr)
					break
				}
				output, gitErr = ExecGitSync(ctx, codesPath, branch, repoURL)
				if gitErr == nil {
					log.Printf("[GitSync] Repository cache successfully auto-healed for %s\n", codesPath)
					break
				}
			}
		}
		break
	}

	if gitErr != nil {
		if models.DB != nil {
			_, _ = models.UpdateActiveTaskReport(models.DB, reportID, map[string]interface{}{
				"clone_status": "failed",
			})
		}
		return codesPath, fmt.Errorf("git operation failed: %s", string(output))
	}

	if models.DB != nil {
		_, _ = models.UpdateActiveTaskReport(models.DB, reportID, map[string]interface{}{
			"clone_status": "success",
		})
	}
	return codesPath, nil
}

// ExecGitSync 执行单次底层的 Git Clone / Fetch / Checkout / Reset
func ExecGitSync(ctx context.Context, codesPath, branch, repoURL string) ([]byte, error) {
	var output []byte
	var gitErr error

	if stat, errStat := os.Stat(filepath.Join(codesPath, ".git")); errStat == nil && stat.IsDir() {
		log.Printf("[GitSync] Updating existing repository in %s for branch %s\n", codesPath, branch)
		if cleanErr := CleanExistingWorkspace(ctx, codesPath); cleanErr != nil {
			return nil, fmt.Errorf("clean existing workspace: %w", cleanErr)
		}
		remotesOut, _, _ := ExecCommandWithProcessGroup(ctx, codesPath, "git", "-C", codesPath, "remote")
		hasOrigin := strings.Contains(string(remotesOut), "origin")

		if hasOrigin {
			// 保证 origin URL 与传入的最新 repoURL 严格一致
			_, _, _ = ExecCommandWithProcessGroup(ctx, codesPath, "git", "-C", codesPath, "remote", "set-url", "origin", repoURL)

			if out, _, err := ExecCommandWithProcessGroup(ctx, codesPath, "git", "-C", codesPath, "fetch", "--all", "--prune"); err != nil {
				output = out
				gitErr = err
			}
		}

		if gitErr == nil {
			if hasOrigin {
				targetRef := "origin/" + branch
				// 如果默认 branch 为 master，但远端分支实际仅存在 main，智能适配目标引用
				if branch == "master" {
					if _, _, refErr := ExecCommandWithProcessGroup(ctx, codesPath, "git", "-C", codesPath, "rev-parse", "--verify", targetRef); refErr != nil {
						if _, _, mainErr := ExecCommandWithProcessGroup(ctx, codesPath, "git", "-C", codesPath, "rev-parse", "--verify", "origin/main"); mainErr == nil {
							targetRef = "origin/main"
							branch = "main"
						}
					}
				}

				// 使用 checkout -B 强制检出并直接对齐 targetRef
				// 该机制天然免疫 force-push/rebase 导致的 divergent branches，并彻底避免生成虚假 merge commit
				if out, _, err := ExecCommandWithProcessGroup(ctx, codesPath, "git", "-C", codesPath, "checkout", "-B", branch, targetRef); err != nil {
					// 远端可能不存在对应的分支（例如纯本地测试分支），降级尝试 checkout -f
					if outFallback, _, errFallback := ExecCommandWithProcessGroup(ctx, codesPath, "git", "-C", codesPath, "checkout", "-f", branch); errFallback != nil {
						output = out
						gitErr = err
					} else {
						output = outFallback
					}
				} else {
					output = out
				}

				// 确保本地工作区与索引同 targetRef 严格保持 100% 一致
				if gitErr == nil {
					if resetOut, _, resetErr := ExecCommandWithProcessGroup(ctx, codesPath, "git", "-C", codesPath, "reset", "--hard", targetRef); resetErr == nil {
						output = resetOut
					}
					// 清理未跟踪的文件残留（保留 .gitignore 中的扫描缓存）
					_, _, _ = ExecCommandWithProcessGroup(ctx, codesPath, "git", "-C", codesPath, "clean", "-fd")
				}
			} else {
				if out, _, err := ExecCommandWithProcessGroup(ctx, codesPath, "git", "-C", codesPath, "checkout", "-f", branch); err != nil {
					output = out
					gitErr = err
				} else {
					output = out
				}
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

// CleanExistingWorkspace resets scanner-owned worktrees before synchronization.
// The scanner owns these cache directories and no human edits them, so untracked
// artifacts from a previous run must not leak into the next coverage manifest.
func CleanExistingWorkspace(ctx context.Context, codesPath string) error {
	if _, _, err := ExecCommandWithProcessGroup(ctx, codesPath, "git", "-C", codesPath, "rev-parse", "--verify", "HEAD"); err != nil {
		return fmt.Errorf("resolve current HEAD: %w", err)
	}
	if out, _, err := ExecCommandWithProcessGroup(ctx, codesPath, "git", "-C", codesPath, "reset", "--hard"); err != nil {
		return fmt.Errorf("reset worktree: %s: %w", string(out), err)
	}
	if out, _, err := ExecCommandWithProcessGroup(ctx, codesPath, "git", "-C", codesPath, "clean", "-fd"); err != nil {
		return fmt.Errorf("clean untracked files: %s: %w", string(out), err)
	}
	return nil
}
