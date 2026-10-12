package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"code-shield/models"
)

func TestBuildSynthesisPrompt_StaticAndIncremental(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. 测试静态分析任务装配
	staticDir := filepath.Join(tmpDir, "static_task")
	_ = os.MkdirAll(staticDir, 0755)
	staticMeta := `{
		"name": "coredump_risk",
		"display_name": "Coredump 风险分析",
		"synthesis_config": {
			"report_type": "static",
			"overview_guidelines": "重点突出内存非法访问与崩溃隐患。",
			"recommendation_guidelines": "推广 RAII 与开启 ASan 动态检测。"
		}
	}`
	_ = os.WriteFile(filepath.Join(staticDir, "meta.json"), []byte(staticMeta), 0644)

	staticTask := &models.TaskType{
		Name:         "coredump_risk",
		DisplayName:  "Coredump 风险分析",
		EngineConfig: []byte(`{}`),
	}
	cfg := LoadTaskSynthesisConfig(staticDir)
	if cfg.ReportType != "static" {
		t.Fatalf("expected static report type, got %s", cfg.ReportType)
	}

	// 2. 测试增量变更任务装配
	incDir := filepath.Join(tmpDir, "inc_task")
	_ = os.MkdirAll(incDir, 0755)
	incMeta := `{
		"name": "change_review",
		"display_name": "近期变更检视",
		"synthesis_config": {
			"report_type": "incremental",
			"overview_guidelines": "重点分析公共接口变更。",
			"recommendation_guidelines": "关注增量回归风险。"
		}
	}`
	_ = os.WriteFile(filepath.Join(incDir, "meta.json"), []byte(incMeta), 0644)

	incTask := &models.TaskType{
		Name:        "change_review",
		DisplayName: "近期变更检视",
	}
	incCfg := LoadTaskSynthesisConfig(incDir)
	if incCfg.ReportType != "incremental" {
		t.Fatalf("expected incremental report type, got %s", incCfg.ReportType)
	}

	// 验证静态模板 Prompt 内容
	staticPrompt := BuildSynthesisPromptWithDir(staticTask, staticDir)
	if !strings.Contains(staticPrompt, "Coredump 风险分析 综合评估报告生成指令") {
		t.Errorf("static prompt title mismatch: %s", staticPrompt)
	}
	if !strings.Contains(staticPrompt, "致命：N，严重：N，一般：N，建议：N") {
		t.Errorf("static prompt must contain machine stats rule")
	}
	if !strings.Contains(staticPrompt, "重点突出内存非法访问与崩溃隐患") {
		t.Errorf("static prompt must contain overview guidelines")
	}
	if !strings.Contains(staticPrompt, "推广 RAII 与开启 ASan 动态检测") {
		t.Errorf("static prompt must contain recommendation guidelines")
	}

	// 验证增量模板 Prompt 内容
	incPrompt := BuildSynthesisPromptWithDir(incTask, incDir)
	if !strings.Contains(incPrompt, "近期变更检视 综合报告生成指令（增量变更检视）") {
		t.Errorf("incremental prompt title mismatch: %s", incPrompt)
	}
	if !strings.Contains(incPrompt, "【特别说明】对外接口变更") {
		t.Errorf("incremental prompt must contain API change highlight")
	}
	if !strings.Contains(incPrompt, "致命：N，严重：N，一般：N，建议：N") {
		t.Errorf("incremental prompt must contain machine stats rule")
	}

	// 3. 测试现有真实任务目录
	realTask := &models.TaskType{
		Name:        "coredump_risk",
		DisplayName: "Coredump 风险分析",
	}
	realPrompt := BuildSynthesisPrompt(realTask)
	if !strings.Contains(realPrompt, "致命：N，严重：N，一般：N，建议：N") {
		t.Errorf("real prompt must contain machine stats rule")
	}
	if !strings.Contains(realPrompt, "Coredump") {
		t.Errorf("real prompt must contain coredump guidelines")
	}
}
