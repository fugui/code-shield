package services

import (
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

	// ── 逐片执行 AI ──
	chunkDir := filepath.Join(os.TempDir(), fmt.Sprintf("chunks-%d", ctx.report.ID))
	os.MkdirAll(chunkDir, 0755)
	defer os.RemoveAll(chunkDir)

	var chunkReports []string

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
		if err := chunkCtx.executeAI(files, ""); err != nil {
			log.Printf("[TaskRunner] Chunk [%s] failed: %v\n", name, err)
			continue
		}

		if content, err := os.ReadFile(chunkCtx.reportPath); err == nil {
			chunkReports = append(chunkReports, fmt.Sprintf("### 模块: %s\n\n%s\n", name, string(content)))
		}
	}

	if len(chunkReports) == 0 {
		return fmt.Errorf("all %d chunks failed", len(chunks))
	}

	// ── 引擎后处理：合并分片报告并综合 ──
	log.Printf("[TaskRunner] Starting synthesis for %d chunks\n", len(chunkReports))

	synthesisPrompt := "以上是该项目各模块的检视结果。请以此为基础，生成一份全工程的综合报告，总结核心风险，剔除重复发现，并给出整体健康度评分。请务必保持输出格式为 Markdown。"

	synthesisInputPath := filepath.Join(chunkDir, "synthesis-input.md")
	os.WriteFile(synthesisInputPath, []byte(strings.Join(chunkReports, "\n\n")), 0644)

	// 综合报告写入 ctx.reportPath（外层管线准备好的路径）
	if err := ctx.executeAI([]string{synthesisInputPath}, synthesisPrompt); err != nil {
		return fmt.Errorf("synthesis failed: %w", err)
	}

	return nil
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
