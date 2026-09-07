package debate

import (
	"code-shield/models"
	"code-shield/services/engines"
	"code-shield/services/engines/chunker"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPromptAssembler_DomainFamilies(t *testing.T) {
	assembler := &PromptAssembler{}

	families := []string{
		models.DomainFamilyMemoryCrash,
		models.DomainFamilyArchitectureGov,
		models.DomainFamilyNumericalDeterminism,
		models.DomainFamilyTestEngineering,
		models.DomainFamilySecurityInjection,
		models.DomainFamilyComprehensive,
		"unknown_family",
	}

	for _, fam := range families {
		dims := assembler.getDefenseDimensionsForFamily(fam)
		if strings.TrimSpace(dims) == "" {
			t.Errorf("expected non-empty defense dimensions for family: %s", fam)
		}
	}
}

func TestPromptAssembler_BuildHunterPrompt(t *testing.T) {
	assembler := &PromptAssembler{}

	tmpDir := t.TempDir()
	tasksDir := filepath.Join(tmpDir, "tasks", "test-task")
	_ = os.MkdirAll(tasksDir, 0755)
	promptPath := filepath.Join(tasksDir, "analysis_prompt.md")
	_ = os.WriteFile(promptPath, []byte("# 领域专业规则\n重点排查死锁与资源竞争。"), 0644)

	ctx := &engines.EngineContext{
		TaskTypeName:       "死锁与竞争专项",
		AnalysisPromptPath: promptPath,
		AllowedCategories:  []string{"并发安全-死锁", "并发安全-竞态"},
		DomainFamily:       models.DomainFamilyMemoryCrash,
	}

	bundle := chunker.SemanticBundle{
		Name:     "bundle-1",
		AllFiles: []string{"src/mutex.cc", "include/mutex.h"},
		MacroContext: map[string]string{
			"ENABLE_THREADS": "1",
		},
		NegativeRules: []string{"test/* 目录免扫"},
	}

	prompt := assembler.BuildHunterPrompt(ctx, bundle)

	if !strings.Contains(prompt, "死锁与竞争专项") {
		t.Errorf("expected prompt to contain task name")
	}
	if !strings.Contains(prompt, "重点排查死锁与资源竞争") {
		t.Errorf("expected prompt to contain domain rules from file")
	}
	if !strings.Contains(prompt, "并发安全-死锁") {
		t.Errorf("expected prompt to contain allowed categories")
	}
	if !strings.Contains(prompt, "ENABLE_THREADS = 1") {
		t.Errorf("expected prompt to contain macro context")
	}
	if !strings.Contains(prompt, "test/* 目录免扫") {
		t.Errorf("expected prompt to contain negative rules")
	}

	ctx.CodesPath = "/path/to/repo"
	promptWithWorkDir := assembler.BuildHunterPrompt(ctx, bundle)
	if !strings.Contains(promptWithWorkDir, "当前分析运行目录为代码仓根目录：/path/to/repo") {
		t.Errorf("expected prompt to contain workdir explanation")
	}
}

func TestPromptAssembler_ChallengerDefensePriority(t *testing.T) {
	assembler := &PromptAssembler{}

	hunterOut := &HunterOutput{
		Candidates: []HunterCandidate{
			{
				CandidateID:      "H-001",
				FilePath:         "src/db.go",
				LineRange:        "10-12",
				TriggerLine:      "query := fmt.Sprintf(...)",
				Category:         "SQL注入-拼接",
				AttackHypothesis: "攻击者可注入恶意 SQL",
			},
		},
	}

	bundle := chunker.SemanticBundle{Name: "bundle-1"}

	// 1. 无任务专属维度，应回退至族群默认模板
	ctxFallback := &engines.EngineContext{
		TaskTypeName: "SQL注入检测",
		DomainFamily: models.DomainFamilySecurityInjection,
	}
	promptFallback, err := assembler.BuildChallengerPrompt(ctxFallback, bundle, hunterOut)
	if err != nil {
		t.Fatalf("BuildChallengerPrompt failed: %v", err)
	}
	if !strings.Contains(promptFallback, "ParametrizedQuery") {
		t.Errorf("expected prompt to contain default security injection dimension ParametrizedQuery")
	}

	// 2. 有任务专属自定义维度，必须 100% 优先使用任务专属维度
	ctxCustom := &engines.EngineContext{
		TaskTypeName: "SQL注入检测",
		DomainFamily: models.DomainFamilySecurityInjection,
		DefenseDimensions: []models.DefenseDimension{
			{
				Dimension:   "CustomWhitelistChecker",
				Description: "业务经过了私有网关签名鉴权与用户名白名单",
			},
		},
	}
	promptCustom, err := assembler.BuildChallengerPrompt(ctxCustom, bundle, hunterOut)
	if err != nil {
		t.Fatalf("BuildChallengerPrompt custom failed: %v", err)
	}
	if !strings.Contains(promptCustom, "CustomWhitelistChecker") {
		t.Errorf("expected prompt to contain custom defense dimension")
	}
	if strings.Contains(promptCustom, "ParametrizedQuery") {
		t.Errorf("expected custom dimensions to override default family dimensions")
	}
}

func TestPromptAssembler_JudgePromptCategories(t *testing.T) {
	assembler := &PromptAssembler{}

	hunterOut := &HunterOutput{
		Candidates: []HunterCandidate{
			{CandidateID: "H-001", Title: "可疑问题"},
		},
	}
	challOut := &ChallengerOutput{
		DefenseCases: []ChallengerDefenseCase{
			{CandidateID: "H-001", DefenseVerdict: "DEFENSE_SUCCESSFUL"},
		},
	}

	ctx := &engines.EngineContext{
		TaskTypeName:      "单测有效性分析",
		AllowedCategories: []string{"断言有效性-空测试", "断言有效性-永真断言"},
	}

	bundle := chunker.SemanticBundle{Name: "bundle-1"}

	judgePrompt, err := assembler.BuildJudgePrompt(ctx, bundle, hunterOut, challOut)
	if err != nil {
		t.Fatalf("BuildJudgePrompt failed: %v", err)
	}

	if !strings.Contains(judgePrompt, "断言有效性-空测试") || !strings.Contains(judgePrompt, "断言有效性-永真断言") {
		t.Errorf("expected judge prompt to contain allowed categories whitelist")
	}
}

func TestPromptAssembler_SecurityPathAndTruncation(t *testing.T) {
	assembler := &PromptAssembler{}
	tmpDir := t.TempDir()

	// 1. 路径穿越测试
	tasksDir := filepath.Join(tmpDir, "tasks")
	_ = os.MkdirAll(tasksDir, 0755)

	outsideDir := filepath.Join(tmpDir, "outside")
	_ = os.MkdirAll(outsideDir, 0755)
	outsideFile := filepath.Join(outsideDir, "secret.md")
	_ = os.WriteFile(outsideFile, []byte("敏感配置"), 0644)

	content := assembler.loadTaskDomainPrompt(outsideFile, tasksDir)
	if content != "" {
		t.Errorf("expected path outside tasks dir to be blocked, got: %q", content)
	}

	// 2. 超长截断测试
	bigFile := filepath.Join(tasksDir, "big_prompt.md")
	bigContent := strings.Repeat("A", MaxPromptRuleBytes+5000)
	_ = os.WriteFile(bigFile, []byte(bigContent), 0644)

	loaded := assembler.loadTaskDomainPrompt(bigFile, tasksDir)
	if !strings.Contains(loaded, "规则文件过长，后续内容已被物理安全截断") {
		t.Errorf("expected big file to be truncated with warning")
	}
	if len(loaded) > MaxPromptRuleBytes+500 {
		t.Errorf("expected length to be strictly bounded")
	}
}
