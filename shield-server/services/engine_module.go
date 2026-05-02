package services

import (
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
		return ctx.executeAI(nil, "")
	}

	// ── 逐模块执行 AI ──
	moduleDir := filepath.Join(os.TempDir(), fmt.Sprintf("modules-%d", ctx.report.ID))
	os.MkdirAll(moduleDir, 0755)
	defer os.RemoveAll(moduleDir)

	var moduleReports []string

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
		if err := modCtx.executeAI(files, ""); err != nil {
			log.Printf("[TaskRunner] Module [%s] failed: %v\n", mod, err)
			continue
		}

		if content, err := os.ReadFile(modCtx.reportPath); err == nil {
			moduleReports = append(moduleReports, fmt.Sprintf("### 模块: %s\n\n%s\n", mod, string(content)))
		}
	}

	if len(moduleReports) == 0 {
		return fmt.Errorf("all %d modules failed", len(modules))
	}

	// ── 引擎后处理：合并模块报告并综合 ──
	log.Printf("[TaskRunner] Starting synthesis for %d modules\n", len(moduleReports))

	synthesisPrompt := "以上是该项目各模块的检视结果。请以此为基础，生成一份全工程的综合报告，总结核心风险，剔除重复发现，并给出整体健康度评分。请务必保持输出格式为 Markdown。"

	synthesisInputPath := filepath.Join(moduleDir, "synthesis-input.md")
	os.WriteFile(synthesisInputPath, []byte(strings.Join(moduleReports, "\n\n")), 0644)

	if err := ctx.executeAI([]string{synthesisInputPath}, synthesisPrompt); err != nil {
		return fmt.Errorf("synthesis failed: %w", err)
	}

	return nil
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
		if excludeSet[entry.Name()] {
			continue
		}
		modules = append(modules, entry.Name())
	}

	return modules, nil
}

func init() {
	RegisterEngine("module", &ModuleEngine{})
}
