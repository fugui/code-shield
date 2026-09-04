package invoker

import (
	"code-shield/models"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeInvoker_BuildArgs(t *testing.T) {
	tempDir := t.TempDir()
	promptFile := filepath.Join(tempDir, "analysis_prompt.md")
	if err := os.WriteFile(promptFile, []byte("# Claude System Prompt"), 0644); err != nil {
		t.Fatalf("failed to write test prompt file: %v", err)
	}
	settingsFile := filepath.Join(tempDir, "settings.json")
	if err := os.WriteFile(settingsFile, []byte(`{}`), 0644); err != nil {
		t.Fatalf("failed to write test settings file: %v", err)
	}

	prevFormat := models.AppConfig.AI.OutputFormat
	prevDataDir := models.AppConfig.Server.DataDir
	t.Cleanup(func() {
		models.AppConfig.AI.OutputFormat = prevFormat
		models.AppConfig.Server.DataDir = prevDataDir
	})
	models.AppConfig.AI.OutputFormat = "json"
	models.AppConfig.Server.DataDir = tempDir

	invoker := &ClaudeInvoker{}
	req := AIRequest{
		WorkDir:    tempDir,
		PromptFile: promptFile,
		PromptMsg:  "执行测试扫描",
		InputFiles: []string{"main.go", "util.go"},
		OutputPath: filepath.Join(tempDir, "output.json.raw"),
		ModelName:  "glm5.1",
	}

	args, err := invoker.buildArgs(req)
	if err != nil {
		t.Fatalf("buildArgs failed: %v", err)
	}

	argsStr := strings.Join(args, " ")
	for _, expected := range []string{
		"-p",
		"--output-format stream-json",
		"--disable-slash-commands",
		"--model glm5.1",
		"--append-system-prompt",
		"# Claude System Prompt",
		"--settings " + settingsFile,
	} {
		if !strings.Contains(argsStr, expected) {
			t.Fatalf("expected args to contain %q, got %v", expected, args)
		}
	}

	// 验证未传递模型名时不注入 --model
	req.ModelName = ""
	argsNoModel, err := invoker.buildArgs(req)
	if err != nil {
		t.Fatalf("buildArgs failed: %v", err)
	}
	if strings.Contains(strings.Join(argsNoModel, " "), "--model") {
		t.Fatalf("args should not contain --model when ModelName is empty, got %v", argsNoModel)
	}
}
