package services

import (
	"code-shield/models"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ChunkConfig 定义分片引擎的配置参数
type ChunkConfig struct {
	MaxFiles int `json:"max_files"`
	Depth    int `json:"depth"`
}

// ChunkedEngine 将代码仓按目录结构拆分成多个分片，逐个提交给 AI 分析后汇总。
type ChunkedEngine struct{}

func (e *ChunkedEngine) Run(ctx *taskContext) error {
	// 1. 解析配置
	cfg := ChunkConfig{MaxFiles: 50, Depth: 1}
	if len(ctx.taskType.EngineConfig) > 0 {
		json.Unmarshal(ctx.taskType.EngineConfig, &cfg)
	}

	// 2. 扫描并分片
	chunks, err := scanAndChunk(ctx.codesPath, cfg)
	if err != nil {
		ctx.markFailed(err.Error())
		return err
	}

	log.Printf("[TaskRunner] Chunked mode: Found %d chunks for repo %s\n", len(chunks), ctx.repo.Name)

	var subResults []TaskResult
	var subReports []string

	// 3. 逐片处理
	for name, files := range chunks {
		subReport := models.TaskReport{
			RepoID:     ctx.repo.ID,
			TaskTypeID: ctx.taskType.ID,
			ParentID:   ctx.report.ID,
			ChunkName:  name,
			Status:     models.StatusAnalyzing,
		}
		models.DB.Create(&subReport)

		subCtx := &taskContext{
			report:     subReport,
			taskType:   ctx.taskType,
			repo:       ctx.repo,
			codesPath:  ctx.codesPath,
			autoNotify: false, // 单片不发通知
		}

		log.Printf("[TaskRunner] Processing chunk [%s] (%d files)\n", name, len(files))
		if err := subCtx.executeAI(files, ""); err != nil {
			log.Printf("[TaskRunner] Chunk [%s] failed: %v\n", name, err)
			models.DB.Model(&subReport).Update("status", models.StatusFailed)
			continue
		}

		res := subCtx.runPostProcess()
		subResults = append(subResults, res)

		if content, err := os.ReadFile(subCtx.reportPath); err == nil {
			subReports = append(subReports, fmt.Sprintf("### 模块: %s (评分: %d)\n\n%s\n", name, res.Score, string(content)))
		}

		subCtx.finalize(res)
	}

	// 4. 综合报告
	updateTaskStatus(ctx.report.ID, "synthesizing")
	log.Printf("[TaskRunner] Starting synthesis for %d chunks\n", len(subResults))

	synthesisPrompt := "以上是该项目各模块的检视结果。请以此为基础，生成一份全工程的综合报告，总结核心风险，剔除重复发现，并给出整体健康度评分。请务必保持输出格式为 Markdown。"

	synthesisInputPath := filepath.Join(os.TempDir(), fmt.Sprintf("synthesis-input-%d.md", ctx.report.ID))
	os.WriteFile(synthesisInputPath, []byte(strings.Join(subReports, "\n\n")), 0644)
	defer os.Remove(synthesisInputPath)

	ctx.report.ChunkName = "total"
	if err := ctx.executeAI([]string{synthesisInputPath}, synthesisPrompt); err != nil {
		ctx.markFailed("Synthesis failed: " + err.Error())
		return err
	}

	// 聚合指标
	finalResult := TaskResult{Metrics: make(map[string]int)}
	totalScore := 0
	for _, r := range subResults {
		totalScore += r.Score
		for k, v := range r.Metrics {
			finalResult.Metrics[k] += v
		}
	}
	if len(subResults) > 0 {
		finalResult.Score = totalScore / len(subResults)
	}

	finalResult.Summary = fmt.Sprintf("已完成对 %d 个模块的分片检视。整体平均分为 %d。", len(subResults), finalResult.Score)

	return ctx.finalize(finalResult)
}

// scanAndChunk 扫描 git 仓库中的文件并按目录深度分组
func scanAndChunk(codesPath string, cfg ChunkConfig) (map[string][]string, error) {
	cmd := exec.Command("git", "-C", codesPath, "ls-files")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files failed: %w", err)
	}

	files := strings.Split(strings.TrimSpace(string(output)), "\n")
	chunks := make(map[string][]string)

	for _, file := range files {
		if file == "" {
			continue
		}

		parts := strings.Split(file, string(filepath.Separator))
		chunkName := "root"
		if len(parts) > 1 {
			depth := cfg.Depth
			if depth >= len(parts) {
				depth = len(parts) - 1
			}
			chunkName = filepath.Join(parts[:depth]...)
		}

		chunks[chunkName] = append(chunks[chunkName], file)
	}

	return chunks, nil
}

func init() {
	RegisterEngine("chunked", &ChunkedEngine{})
}
