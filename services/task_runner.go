package services

import (
	"sync"

	"code-shield/models"
	"code-shield/services/runner"
)

// 导出 Runner 领域的类型别名，确保对外部 handlers、cmd、cron_jobs 零破坏
type (
	TaskResult        = runner.TaskResult
	ChunkDetails      = runner.ChunkDetails
	AnalysisSummary   = runner.AnalysisSummary
	SynthesisSummary  = runner.SynthesisSummary
	MergingSummary    = runner.MergingSummary
	TaskSummaryReport = runner.TaskSummaryReport
	RunningTaskInfo   = runner.RunningTaskInfo
	taskContext       = runner.TaskContext
)

func init() {
	// 装配增量指纹比对实现至 Runner 流水线，避免子包循环依赖
	runner.FindingEnricher = func(repoID, reportID, taskTypeID uint, findings []models.AnalysisFinding, codesPath string) ([]models.AnalysisFinding, error) {
		return DiffAndEnrichFindings(repoID, reportID, taskTypeID, nil, findings, codesPath)
	}
}

// RunTaskSync 同步驱动单次扫描任务，委托至 runner 子包
func RunTaskSync(reportID uint, repoURL string, taskTypeID uint, autoNotify bool, runParams models.RunParams) error {
	return runner.RunTaskSync(reportID, repoURL, taskTypeID, autoNotify, runParams)
}

// CancelRunningTask 取消正在执行的任务，委托至 runner 子包
func CancelRunningTask(reportID uint) bool {
	return runner.CancelRunningTask(reportID)
}

// CancelAllRunningTasks 取消所有正在执行的任务，委托至 runner 子包
func CancelAllRunningTasks() {
	runner.CancelAllRunningTasks()
}

// GetRunningTasks 获取当前运行中任务列表快照，委托至 runner 子包
func GetRunningTasks() []RunningTaskInfo {
	return runner.GetRunningTasks()
}

// NotifyTaskResult 结果邮件与 Webhook 通告，委托至 runner 子包
func NotifyTaskResult(repo models.Repository, taskType models.TaskType, result TaskResult, specificRecipientEmail string, reportID uint, reportPath string) {
	runner.NotifyTaskResult(repo, taskType, result, specificRecipientEmail, reportID, reportPath)
}

// ResumeFailedChunks 失败分片断点续跑，委托至 runner 子包
func ResumeFailedChunks(reportID uint) error {
	return runner.ResumeFailedChunks(reportID)
}

// 门面清洗与辅助函数，保持单元测试透明兼容
func cleanJSONFromAI(raw []byte) []byte {
	return runner.CleanJSONFromAI(raw)
}

func fixUnescapedQuotes(s string) string {
	return runner.FixUnescapedQuotes(s)
}

func cleanAnalysisTempFiles(jsonPath string) {
	runner.CleanAnalysisTempFiles(jsonPath)
}

func cleanSynthesisTempFiles(reportPath string) {
	runner.CleanSynthesisTempFiles(reportPath)
}

func recoverAIOutput(expectedPath string) {
	runner.RecoverAIOutput(expectedPath)
}

func sanitizeMarkdownReport(raw []byte) []byte {
	return runner.SanitizeMarkdownReport(raw)
}

func toLineStr(v interface{}) string {
	return runner.ToLineStr(v)
}

func cleanStaleGitLocks(codesPath string) {
	runner.CleanStaleGitLocks(codesPath)
}

func getRepoSyncLock(repoPath string) *sync.Mutex {
	return runner.GetRepoSyncLock(repoPath)
}
