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
		EngineMode:         "debate_full",
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
	if strings.Contains(prompt, `"findings":`) {
		t.Errorf("hunter prompt must not contain findings output contract")
	}
	if !strings.Contains(prompt, "candidates") || !strings.Contains(prompt, "code-shield.candidates.v1") {
		t.Errorf("hunter prompt must contain generated candidates contract")
	}

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

func TestPromptAssembler_StripsLegacyOutputContracts(t *testing.T) {
	assembler := &PromptAssembler{}
	root := t.TempDir()
	promptPath := filepath.Join(root, "analysis_prompt.md")
	content := "# 领域规则\n重点检查浮点数比较。\n\n## 输出格式与约束\n```json\n{\"findings\":[]}\n```\n\n## 保留规则\n必须收集触发证据。"
	if err := os.WriteFile(promptPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write prompt: %v", err)
	}

	cleaned := assembler.loadTaskDomainPrompt(promptPath, root)
	if strings.Contains(cleaned, "findings") || strings.Contains(cleaned, "输出格式") {
		t.Fatalf("legacy output contract was not stripped:\n%s", cleaned)
	}
	if !strings.Contains(cleaned, "重点检查浮点数比较") || !strings.Contains(cleaned, "保留规则") {
		t.Fatalf("domain rules were damaged:\n%s", cleaned)
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
	promptFallback, err := assembler.BuildChallengerPrompt(ctxFallback, bundle, hunterOut, nil)
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
	promptCustom, err := assembler.BuildChallengerPrompt(ctxCustom, bundle, hunterOut, nil)
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

	evidencePacks := []JudgeCaseEvidencePack{
		{
			CandidateID: "H-001",
			Claim: JudgeCandidateView{
				CandidateID: "H-001",
				Title:       "可疑问题",
			},
			WorkspaceRoot:        "/repo",
			TargetFiles:          bundle.AllFiles,
			AllowedEvidencePaths: bundle.AllFiles,
		},
	}
	judgePrompt, err := assembler.BuildJudgePrompt(ctx, bundle, hunterOut, challOut, evidencePacks)
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

func TestPromptAssembler_PrefersAnalysisPromptContent(t *testing.T) {
	assembler := &PromptAssembler{}
	content := "重点检查并发竞态。\n\n## 输出格式\n```json\n{\"findings\":[]}\n```\n\n## 保留规则\n必须给出触发位置。"
	ctx := &engines.EngineContext{
		TaskTypeName:          "并发检测",
		AnalysisPromptContent: content,
		AnalysisPromptPath:    filepath.Join(t.TempDir(), "missing.md"),
	}

	prompt := assembler.BuildHunterPrompt(ctx, chunker.SemanticBundle{Name: "bundle-1"})
	if !strings.Contains(prompt, "重点检查并发竞态") || !strings.Contains(prompt, "必须给出触发位置") {
		t.Fatalf("analysis prompt content was not injected:\n%s", prompt)
	}
	loaded := assembler.loadTaskDomainPromptContent(content)
	if strings.Contains(loaded, "findings") || strings.Contains(loaded, "输出格式") {
		t.Fatalf("legacy output contract was not stripped:\n%s", loaded)
	}
}

func TestPromptAssembler_TruncatesAnalysisPromptContent(t *testing.T) {
	assembler := &PromptAssembler{}
	content := strings.Repeat("A", MaxPromptRuleBytes+1000)

	loaded := assembler.loadTaskDomainPromptContent(content)
	if !strings.Contains(loaded, "规则文件过长，后续内容已被物理安全截断") {
		t.Fatalf("content was not truncated:\n%s", loaded)
	}
	if len(loaded) > MaxPromptRuleBytes+500 {
		t.Fatalf("content length = %d, want bounded", len(loaded))
	}
}

func TestPromptAssembler_FallsBackToLegacyTaskPromptPath(t *testing.T) {
	assembler := &PromptAssembler{}
	dataDir := t.TempDir()
	oldDataDir := models.AppConfig.Server.DataDir
	models.AppConfig.Server.DataDir = dataDir
	t.Cleanup(func() { models.AppConfig.Server.DataDir = oldDataDir })

	promptPath := filepath.Join(dataDir, "tasks", "analysis_prompt.md")
	if err := os.MkdirAll(filepath.Dir(promptPath), 0755); err != nil {
		t.Fatalf("create tasks dir: %v", err)
	}
	if err := os.WriteFile(promptPath, []byte("legacy path domain rule"), 0644); err != nil {
		t.Fatalf("write legacy prompt: %v", err)
	}

	domainRules := assembler.loadTaskDomainPrompt(promptPath, filepath.Join(dataDir, "tasks"))
	if domainRules != "legacy path domain rule" {
		t.Fatalf("legacy prompt path fallback = %q, want %q", domainRules, "legacy path domain rule")
	}
}
