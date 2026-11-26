package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"code-common/backend/gitsync"
	"code-shield/models"
)

// GetRepoSyncLock 获取指定仓库物理路径对应的读写锁，确保同一代码仓的 Git 同步操作与只读访问跨协同排队。
func GetRepoSyncLock(repoPath string) *sync.RWMutex {
	return gitsync.GetPathLock(repoPath)
}

// CleanStaleGitLocks 检查并清理由于历史进程崩溃或异常中断残留的 .git/*.lock 文件。
func CleanStaleGitLocks(codesPath string) {
	gitsync.CleanStaleGitLocks(codesPath)
}

// IsGitAuthOrPermissionError 判断 Git 输出或错误信息是否属于权限、鉴权或凭据类问题。
func IsGitAuthOrPermissionError(msg string) bool {
	return gitsync.IsAuthOrPermissionError(msg)
}

// ExecCommandWithProcessGroup 执行外部命令，并为其创建独立的 Linux 进程组 (Setpgid)。
func ExecCommandWithProcessGroup(ctx context.Context, dir string, name string, args ...string) ([]byte, int, error) {
	return gitsync.ExecCommandWithProcessGroup(ctx, dir, name, args...)
}

// PrepareAndSync 解析 URL，委托公共包 code-common/backend/gitsync 执行受两层锁保护的 Git 同步，并回写任务状态。
func PrepareAndSync(ctx context.Context, repo models.Repository, reportID uint, repoURL string) (string, error) {
	codesRoot := models.AppConfig.GetCodesDir()
	codesPath, err := gitsync.ResolveRepoLocalPath(codesRoot, repoURL)
	if err != nil {
		return "", fmt.Errorf("invalid repository URL: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(codesPath), 0755); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}

	UpdateTaskStatus(reportID, models.StatusCloning)

	branch := gitsync.ResolveBranchDefault(repo.Branch)

	_, gitErr := gitsync.Sync(ctx, gitsync.SyncOptions{
		RepoURL:        repoURL,
		LocalPath:      codesPath,
		CodesRoot:      codesRoot,
		Branch:         branch,
		Depth:          0,
		AutoHealCache:  true,
		CleanUntracked: true,
	})

	if gitErr != nil {
		if models.DB != nil {
			if _, dbErr := models.UpdateActiveTaskReport(models.DB, reportID, map[string]interface{}{
				"clone_status": "failed",
			}); dbErr != nil {
				return codesPath, fmt.Errorf("git operation failed (%w); failed to update clone_status: %v", gitErr, dbErr)
			}
		}
		return codesPath, fmt.Errorf("git operation failed: %w", gitErr)
	}

	if models.DB != nil {
		if _, dbErr := models.UpdateActiveTaskReport(models.DB, reportID, map[string]interface{}{
			"clone_status": "success",
		}); dbErr != nil {
			return codesPath, fmt.Errorf("failed to update clone_status: %w", dbErr)
		}
	}
	return codesPath, nil
}

// ExecGitSync 执行单次底层的 Git Clone / Fetch / Checkout / Reset。
func ExecGitSync(ctx context.Context, codesPath, branch, repoURL string) ([]byte, error) {
	output, _, err := gitsync.ExecGitSyncWithOptions(ctx, codesPath, branch, repoURL, 0, true)
	return output, err
}

// CleanExistingWorkspace 在同步前重置托管工作区，避免残留未跟踪文件污染后续扫描。
func CleanExistingWorkspace(ctx context.Context, codesPath string) error {
	return gitsync.CleanExistingWorkspace(ctx, codesPath)
}
