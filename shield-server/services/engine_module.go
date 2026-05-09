package services

import (
	"code-shield/models"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// ModuleConfig 定义模块引擎的配置参数
type ModuleConfig struct {
	// Exclude 排除的目录名（如 vendor, node_modules, .git 等）
	Exclude []string `json:"exclude"`
}

// ModuleEngine 按顶层子目录（模块）拆分代码仓，逐模块提交给 AI 分析后汇总。
// 适用于一个代码仓包含多个独立模块的场景，根目录下只有 Makefile/README 等公共文件。
type ModuleEngine struct{}

func (e *ModuleEngine) Run(ctx *taskContext) error {
	// ── 引擎前处理：扫描顶层子目录作为模块 ──
	cfg := ModuleConfig{
		Exclude: []string{".git", ".github", ".vscode", "vendor", "node_modules", "__pycache__"},
	}
	if len(ctx.taskType.EngineConfig) > 0 {
		json.Unmarshal(ctx.taskType.EngineConfig, &cfg)
	}

	modules, err := scanModules(ctx.codesPath, cfg)
	if err != nil {
		return err
	}

	log.Printf("[TaskRunner] Module mode: Found %d modules for repo %s: %v\n", len(modules), ctx.repo.Name, modules)

	if len(modules) == 0 {
		// 没有子目录模块，回退到 single 模式
		log.Println("[TaskRunner] No modules found, falling back to single engine")
		findings, err := ctx.executeAnalysis(nil)
		if err != nil {
			return err
		}
		return ctx.executeSynthesis(findings)
	}

	// ── 逐模块执行分析阶段 ──
	moduleDir := filepath.Join(filepath.Dir(ctx.reportPath), fmt.Sprintf("modules-%d", ctx.report.ID))
	os.MkdirAll(moduleDir, 0755)

	var allFindings []models.AnalysisFinding

	for _, mod := range modules {
		safeName := strings.ReplaceAll(mod, "/", "-")
		modCtx := &taskContext{
			report:     ctx.report,
			taskType:   ctx.taskType,
			repo:       ctx.repo,
			codesPath:  ctx.codesPath,
			reportPath: filepath.Join(moduleDir, fmt.Sprintf("module-%s.md", safeName)),
			jsonPath:   filepath.Join(moduleDir, fmt.Sprintf("module-%s.json", safeName)),
		}
		modCtx.report.ChunkName = mod

		// 收集该模块下的所有文件（相对路径）
		var files []string
		filepath.Walk(filepath.Join(ctx.codesPath, mod), func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(ctx.codesPath, path)
			files = append(files, rel)
			return nil
		})

		log.Printf("[TaskRunner] Processing module [%s] (%d files)\n", mod, len(files))

		// Phase 1: 分析阶段
		findings, err := modCtx.executeAnalysis(files)
		if err != nil {
			log.Printf("[TaskRunner] Module [%s] analysis failed: %v\n", mod, err)
			continue
		}
		allFindings = append(allFindings, findings...)
	}

	if len(allFindings) == 0 && len(modules) > 0 {
		log.Printf("[TaskRunner] Warning: all %d modules produced no findings\n", len(modules))
	}

	// ── Phase 2: 综合阶段 ──
	log.Printf("[TaskRunner] Starting synthesis for %d findings from %d modules\n", len(allFindings), len(modules))
	return ctx.executeSynthesis(allFindings)
}

// scanModules 扫描代码仓根目录下的顶层子目录作为模块
func scanModules(codesPath string, cfg ModuleConfig) ([]string, error) {
	entries, err := os.ReadDir(codesPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	excludeSet := make(map[string]bool, len(cfg.Exclude))
	for _, e := range cfg.Exclude {
		excludeSet[e] = true
	}

	var modules []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if excludeSet[entry.Name()] || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		modules = append(modules, entry.Name())
	}

	return modules, nil
}

func init() {
	RegisterEngine("module", &ModuleEngine{})
}
