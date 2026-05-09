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
	// ── 引擎前处理：解析配置并扫描分片 ──
	cfg := ChunkConfig{MaxFiles: 50, Depth: 1}
	if len(ctx.taskType.EngineConfig) > 0 {
		json.Unmarshal(ctx.taskType.EngineConfig, &cfg)
	}

	chunks, err := scanAndChunk(ctx.codesPath, cfg)
	if err != nil {
		return err
	}

	log.Printf("[TaskRunner] Chunked mode: Found %d chunks for repo %s\n", len(chunks), ctx.repo.Name)

	// ── 逐片执行分析阶段 ──
	chunkDir := filepath.Join(filepath.Dir(ctx.reportPath), fmt.Sprintf("chunks-%d", ctx.report.ID))
	os.MkdirAll(chunkDir, 0755)

	var allFindings []models.AnalysisFinding

	for name, files := range chunks {
		safeName := strings.ReplaceAll(name, "/", "-")
		chunkCtx := &taskContext{
			report:     ctx.report,
			taskType:   ctx.taskType,
			repo:       ctx.repo,
			codesPath:  ctx.codesPath,
			reportPath: filepath.Join(chunkDir, fmt.Sprintf("chunk-%s.md", safeName)),
			jsonPath:   filepath.Join(chunkDir, fmt.Sprintf("chunk-%s.json", safeName)),
		}
		chunkCtx.report.ChunkName = name

		log.Printf("[TaskRunner] Processing chunk [%s] (%d files)\n", name, len(files))

		// Phase 1: 分析阶段
		findings, err := chunkCtx.executeAnalysis(files)
		if err != nil {
			log.Printf("[TaskRunner] Chunk [%s] analysis failed: %v\n", name, err)
			continue
		}
		allFindings = append(allFindings, findings...)
	}

	if len(allFindings) == 0 && len(chunks) > 0 {
		log.Printf("[TaskRunner] Warning: all %d chunks produced no findings\n", len(chunks))
	}

	// ── Phase 2: 综合阶段 ──
	log.Printf("[TaskRunner] Starting synthesis for %d findings from %d chunks\n", len(allFindings), len(chunks))
	return ctx.executeSynthesis(allFindings)
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
