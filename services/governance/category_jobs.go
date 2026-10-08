package governance

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"code-shield/models"
	"code-shield/services/dispatcher"
	"code-shield/services/invoker"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type regressionJSONSample struct {
	TaskType       string            `json:"task_type"`
	CandidateID    string            `json:"candidate_id"`
	PairKey        string            `json:"pair_key"`
	CandidateFacts map[string]string `json:"candidate_facts"`
	ExpectedCode   string            `json:"expected_code"`
	TaxonomyHash   string            `json:"taxonomy_hash"`
	PromptHash     string            `json:"prompt_hash"`
	ModelBackend   string            `json:"model_backend"`
}

type regressionDimension struct {
	TaxonomyHash string
	PromptHash   string
	ModelBackend string
}

type CategoryRegressionExecutor func(models.CategoryRegressionSample) (string, error)

const categoryRegressionDailyRunLimit = 200

func SeedCategoryRegressionSamples(db *gorm.DB, path string) (int, error) {
	if db == nil {
		return 0, fmt.Errorf("database is required")
	}
	file, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open category regression corpus: %w", err)
	}
	defer file.Close()

	synced := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}
		var source regressionJSONSample
		if err := json.Unmarshal([]byte(text), &source); err != nil {
			return synced, fmt.Errorf("decode regression line %d: %w", line, err)
		}
		if source.TaskType == "" || source.CandidateID == "" || source.ExpectedCode == "" ||
			source.TaxonomyHash == "" || source.PromptHash == "" || source.ModelBackend == "" ||
			len(source.CandidateFacts) == 0 {
			return synced, fmt.Errorf("incomplete metadata on regression line %d", line)
		}
		facts, marshalErr := json.Marshal(source.CandidateFacts)
		if marshalErr != nil {
			return synced, fmt.Errorf("encode regression line %d: %w", line, marshalErr)
		}
		sample := models.CategoryRegressionSample{
			SampleKey:      source.CandidateID,
			TaskType:       source.TaskType,
			PairKey:        regressionPairKey(source),
			CandidateFacts: datatypes.JSON(facts),
			ExpectedCode:   source.ExpectedCode,
			Status:         CategorySampleActive,
			TaxonomyHash:   source.TaxonomyHash,
			PromptHash:     source.PromptHash,
			ModelBackend:   source.ModelBackend,
		}
		var existing models.CategoryRegressionSample
		err := db.Where("sample_key = ?", sample.SampleKey).First(&existing).Error
		switch {
		case err == nil:
			updates := map[string]any{
				"task_type":       sample.TaskType,
				"pair_key":        sample.PairKey,
				"candidate_facts": sample.CandidateFacts,
				"expected_code":   sample.ExpectedCode,
				"taxonomy_hash":   sample.TaxonomyHash,
				"prompt_hash":     sample.PromptHash,
				"model_backend":   sample.ModelBackend,
			}
			if err := db.Model(&existing).Updates(updates).Error; err != nil {
				return synced, err
			}
		case err == gorm.ErrRecordNotFound:
			if err := db.Create(&sample).Error; err != nil {
				return synced, err
			}
		default:
			return synced, err
		}
		synced++
	}
	if err := scanner.Err(); err != nil {
		return synced, fmt.Errorf("scan category regression corpus: %w", err)
	}
	return synced, nil
}

func regressionPairKey(source regressionJSONSample) string {
	if source.PairKey != "" {
		return source.PairKey
	}
	if prefix, ok := strings.CutPrefix(source.CandidateID, "regression/"); ok {
		pair, _, _ := strings.Cut(prefix, "-case-")
		return pair
	}
	return "anchor:" + source.ExpectedCode
}

func RunCategoryRegressionTrend(db *gorm.DB, executor CategoryRegressionExecutor) (int, error) {
	if db == nil {
		return 0, fmt.Errorf("database is required")
	}
	if executor == nil {
		executor = categoryRegressionBackendExecutor
	}
	batchSize := models.AppConfig.Governance.CategoryRegressionBatchSize
	if batchSize <= 0 {
		batchSize = 50
	}
	maxFailures := models.AppConfig.Governance.CategoryRegressionMaxFailures
	if maxFailures <= 0 {
		maxFailures = 5
	}
	var samples []models.CategoryRegressionSample
	if err := db.Where("status = ?", CategorySampleActive).Order("id asc").Limit(batchSize).Find(&samples).Error; err != nil {
		return 0, err
	}
	now := time.Now()
	periodStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	var runsToday int64
	if err := db.Model(&models.CategoryRegressionSample{}).
		Where("last_run_at >= ?", periodStart).
		Count(&runsToday).Error; err != nil {
		return 0, err
	}
	remainingToday := categoryRegressionDailyRunLimit - int(runsToday)
	if remainingToday <= 0 {
		return 0, nil
	}
	runCount := 0
	executionFailures := 0
	for _, sample := range samples {
		if runCount >= remainingToday {
			break
		}
		actual, err := executor(sample)
		if err != nil {
			sample.Outcome = "ERROR"
			sample.LastFailureReason = err.Error()
			executionFailures++
		} else {
			sample.ActualCode = actual
			sample.LastFailureReason = ""
		}
		if err := RecordCategoryRegressionRun(db, sample); err != nil {
			return runCount, err
		}
		runCount++
		if executionFailures >= maxFailures {
			return runCount, fmt.Errorf("category regression stopped after %d execution failures", executionFailures)
		}
	}
	return runCount, nil
}

func categoryRegressionBackendExecutor(sample models.CategoryRegressionSample) (string, error) {
	backend := models.AppConfig.SchemaRepairResource()
	rawInvoker, ok := invoker.GetRawInvoker(backend)
	if !ok || rawInvoker == nil {
		return "", fmt.Errorf("semantic regression backend %q is unavailable", backend)
	}
	promptBytes, err := json.Marshal(map[string]any{
		"instructions": "根据 candidate_facts 选择唯一受控分类短码，只返回 category_code 和 classification_rationale。",
		"sample":       sample,
	})
	if err != nil {
		return "", err
	}
	outputFile, err := os.CreateTemp("", "category-regression-*.json")
	if err != nil {
		return "", err
	}
	outputPath := outputFile.Name()
	defer os.Remove(outputPath)
	if err := outputFile.Close(); err != nil {
		return "", err
	}
	temperature := 0.0
	request := invoker.AIRequest{
		ParentContext:  context.Background(),
		PromptMsg:      string(promptBytes),
		OutputPath:     outputPath,
		TimeoutSeconds: models.AppConfig.CategoryRepairTimeoutSeconds(),
		Temperature:    &temperature,
		ResponseFormat: "json",
		WorkContext: &invoker.LLMWorkContext{
			TaskType: sample.TaskType,
			Stage:    "semantic_regression",
			SubTask:  sample.SampleKey,
			TierName: "system_tool",
		},
	}
	if err := dispatcher.WrapInvoker(rawInvoker).Invoke(request); err != nil {
		return "", err
	}
	output, err := os.ReadFile(outputPath)
	if err != nil {
		return "", fmt.Errorf("read semantic regression output: %w", err)
	}
	var response map[string]any
	if err := json.Unmarshal(output, &response); err != nil {
		return "", fmt.Errorf("decode semantic regression output: %w", err)
	}
	if response == nil {
		return "", fmt.Errorf("semantic regression output must be a JSON object")
	}
	rawCode, ok := response["category_code"]
	if !ok {
		return "", fmt.Errorf("semantic regression output is missing category_code")
	}
	code, ok := rawCode.(string)
	if !ok || strings.TrimSpace(code) == "" {
		return "", fmt.Errorf("semantic regression category_code must be a non-empty string")
	}
	if err := validateRegressionCategory(sample, strings.TrimSpace(code)); err != nil {
		return "", err
	}
	return strings.TrimSpace(code), nil
}

func validateRegressionCategory(sample models.CategoryRegressionSample, categoryCode string) error {
	if models.DB == nil {
		return fmt.Errorf("semantic regression task taxonomy is unavailable")
	}
	var taskType models.TaskType
	if err := models.DB.Where("name = ?", sample.TaskType).First(&taskType).Error; err != nil {
		return fmt.Errorf("load semantic regression task taxonomy %q: %w", sample.TaskType, err)
	}
	taxonomy := taskType.GetCategoryTaxonomy()
	if taxonomy == nil {
		return fmt.Errorf("semantic regression task %q has no taxonomy", sample.TaskType)
	}
	if taxonomy.Hash != "" {
		if taskType.TaxonomyHash == "" || sample.TaxonomyHash == "" || taskType.TaxonomyHash != sample.TaxonomyHash {
			return fmt.Errorf("semantic regression taxonomy hash mismatch for task %q", sample.TaskType)
		}
	}
	for _, category := range taxonomy.Categories {
		if category.Code == categoryCode {
			return nil
		}
	}
	return fmt.Errorf("semantic regression category_code %q is not in task %q taxonomy", categoryCode, sample.TaskType)
}

func BuildMonthlyCategoryConfusionMatrices(db *gorm.DB, periodStart, periodEnd time.Time) ([]models.CategoryConfusionMatrix, error) {
	if db == nil || periodStart.IsZero() || periodEnd.IsZero() || !periodEnd.After(periodStart) {
		return nil, fmt.Errorf("confusion matrix period is required")
	}
	var dimensions []regressionDimension
	if err := db.Model(&models.CategoryRegressionSample{}).
		Select("DISTINCT taxonomy_hash, prompt_hash, model_backend").
		Where("status = ? AND last_run_at >= ? AND last_run_at < ?", CategorySampleActive, periodStart, periodEnd).
		Order("taxonomy_hash, prompt_hash, model_backend").
		Scan(&dimensions).Error; err != nil {
		return nil, err
	}
	matrices := make([]models.CategoryConfusionMatrix, 0, len(dimensions))
	for _, dimension := range dimensions {
		matrix, err := BuildCategoryConfusionMatrix(
			db, dimension.TaxonomyHash, dimension.PromptHash, dimension.ModelBackend, periodStart, periodEnd,
		)
		if err != nil {
			return matrices, err
		}
		matrices = append(matrices, matrix)
	}
	return matrices, nil
}

func RunCategoryGovernanceJobs(db *gorm.DB, corpusPath string, executor CategoryRegressionExecutor) (int, int, error) {
	synced, err := SeedCategoryRegressionSamples(db, corpusPath)
	if err != nil {
		return synced, 0, err
	}
	runCount, err := RunCategoryRegressionTrend(db, executor)
	if err != nil {
		return synced, runCount, err
	}
	now := time.Now()
	periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	periodEnd := periodStart.AddDate(0, 1, 0)
	if _, err := BuildMonthlyCategoryConfusionMatrices(db, periodStart, periodEnd); err != nil {
		return synced, runCount, err
	}
	return synced, runCount, nil
}

func DefaultCategoryRegressionCorpusPath() string {
	return filepath.Join("testdata", "category-regression.jsonl")
}
