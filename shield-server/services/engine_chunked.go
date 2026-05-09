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

		// 过滤非源码文件
		if !isSourceFile(file) {
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

// sourceExtensions 定义需要分析的源码文件扩展名
var sourceExtensions = map[string]bool{
	// 通用编程语言
	".go": true, ".py": true, ".java": true, ".kt": true, ".scala": true,
	".js": true, ".ts": true, ".jsx": true, ".tsx": true, ".vue": true, ".svelte": true,
	".c": true, ".cpp": true, ".cc": true, ".cxx": true, ".h": true, ".hpp": true,
	".cs": true, ".rs": true, ".rb": true, ".php": true, ".swift": true, ".m": true,
	".dart": true, ".lua": true, ".r": true, ".pl": true, ".pm": true,
	// Shell / 脚本
	".sh": true, ".bash": true, ".zsh": true, ".bat": true, ".ps1": true,
	// 配置 / 标记语言（可能含安全相关配置）
	".yaml": true, ".yml": true, ".toml": true, ".ini": true,
	".xml": true, ".json": true, ".jsonc": true,
	// Web
	".html": true, ".css": true, ".scss": true, ".less": true,
	// 数据库
	".sql": true,
	// 其他
	".proto": true, ".graphql": true, ".gql": true,
	".tf": true, ".hcl": true,
	".dockerfile": true,
}

// isSourceFile 根据扩展名判断是否为源码文件
func isSourceFile(file string) bool {
	// 跳过 . 开头的目录（如 .github/, .vscode/, .idea/ 等）
	for _, part := range strings.Split(file, "/") {
		if strings.HasPrefix(part, ".") && part != "." {
			return false
		}
	}

	// 跳过常见的非源码目录
	lower := strings.ToLower(file)
	for _, skip := range []string{"vendor/", "node_modules/", "__pycache__/", "dist/", "build/"} {
		if strings.Contains(lower, skip) {
			return false
		}
	}

	ext := strings.ToLower(filepath.Ext(file))
	if ext == "" {
		// 无扩展名的特殊文件（如 Dockerfile, Makefile）
		base := strings.ToLower(filepath.Base(file))
		return base == "dockerfile" || base == "makefile" || base == "rakefile" || base == "gemfile"
	}

	return sourceExtensions[ext]
}

func init() {
	RegisterEngine("chunked", &ChunkedEngine{})
}
