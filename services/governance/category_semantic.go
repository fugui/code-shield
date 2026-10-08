package governance

import (
	"encoding/json"
	"fmt"
	"time"

	"code-shield/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	CategoryAliasDeprecated = "deprecated"
	CategoryAliasInline     = "inline"

	CategorySampleActive     = "ACTIVE"
	CategorySampleSuperseded = "SUPERSEDED"

	CategoryReviewInitial = "initial"
	CategoryReviewSecond  = "second"

	CategoryReviewConfirmed = "CONFIRMED"
	CategoryReviewRejected  = "REJECTED"
	CategoryReviewDisputed  = "DISPUTED"
)

func RecordCategoryAliasHit(db *gorm.DB, usage models.CategoryAliasUsage) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	if usage.AliasKind != CategoryAliasDeprecated && usage.AliasKind != CategoryAliasInline {
		return fmt.Errorf("alias kind %q is invalid", usage.AliasKind)
	}
	if usage.AliasLabel == "" || usage.TargetCode == "" || usage.TaxonomyHash == "" {
		return fmt.Errorf("alias taxonomy hash, label, and target code are required")
	}
	now := time.Now()
	if usage.LastSeenAt.IsZero() {
		usage.LastSeenAt = now
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var existing models.CategoryAliasUsage
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("taxonomy_hash = ? AND alias_kind = ? AND alias_label = ?", usage.TaxonomyHash, usage.AliasKind, usage.AliasLabel).
			First(&existing).Error
		if err == nil {
			return tx.Model(&existing).Updates(map[string]any{
				"hit_count":      gorm.Expr("hit_count + 1"),
				"last_report_id": usage.LastReportID,
				"last_seen_at":   usage.LastSeenAt,
			}).Error
		}
		if err != gorm.ErrRecordNotFound {
			return err
		}
		usage.HitCount = 1
		usage.FirstSeenAt = now
		return tx.Create(&usage).Error
	})
}

func RecordCategoryRegressionRun(db *gorm.DB, sample models.CategoryRegressionSample) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	if sample.SampleKey == "" || sample.TaskType == "" || sample.ExpectedCode == "" ||
		sample.TaxonomyHash == "" || sample.PromptHash == "" || sample.ModelBackend == "" {
		return fmt.Errorf("regression sample metadata is incomplete")
	}
	if len(sample.CandidateFacts) == 0 {
		return fmt.Errorf("regression candidate facts are required")
	}
	var facts map[string]any
	if err := json.Unmarshal(sample.CandidateFacts, &facts); err != nil {
		return fmt.Errorf("decode regression candidate facts: %w", err)
	}
	if len(facts) == 0 {
		return fmt.Errorf("regression candidate facts are empty")
	}
	now := time.Now()
	sample.LastRunAt = &now
	if sample.Outcome == "" {
		if sample.ActualCode == sample.ExpectedCode {
			sample.Outcome = "PASS"
		} else {
			sample.Outcome = "FAIL"
		}
	}
	if sample.Status == "" {
		sample.Status = CategorySampleActive
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var existing models.CategoryRegressionSample
		err := tx.Where("sample_key = ?", sample.SampleKey).First(&existing).Error
		if err == nil {
			updates := map[string]any{
				"actual_code":         sample.ActualCode,
				"outcome":             sample.Outcome,
				"last_run_at":         sample.LastRunAt,
				"last_failure_reason": sample.LastFailureReason,
				"failure_count":       existing.FailureCount,
			}
			if sample.Outcome == "FAIL" || sample.Outcome == "ERROR" {
				updates["failure_count"] = existing.FailureCount + 1
			}
			return tx.Model(&existing).Updates(updates).Error
		}
		if err != gorm.ErrRecordNotFound {
			return err
		}
		if sample.Outcome == "FAIL" || sample.Outcome == "ERROR" {
			sample.FailureCount = 1
		}
		return tx.Create(&sample).Error
	})
}

func SupersedeCategoryRegressionSample(db *gorm.DB, sampleKey string, supersededBy string, reason string) error {
	if sampleKey == "" || supersededBy == "" || reason == "" {
		return fmt.Errorf("superseded sample key, replacement, and reason are required")
	}
	return db.Model(&models.CategoryRegressionSample{}).
		Where("sample_key = ?", sampleKey).
		Updates(map[string]any{
			"status":            CategorySampleSuperseded,
			"superseded_by":     supersededBy,
			"superseded_reason": reason,
		}).Error
}

func ReviewCategoryRegressionSample(db *gorm.DB, review models.CategoryRegressionReview) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	if review.SampleID == 0 || review.Reviewer == "" {
		return fmt.Errorf("review sample and reviewer are required")
	}
	if review.Stage != CategoryReviewInitial && review.Stage != CategoryReviewSecond {
		return fmt.Errorf("review stage %q is invalid", review.Stage)
	}
	if review.Outcome != CategoryReviewConfirmed && review.Outcome != CategoryReviewRejected && review.Outcome != CategoryReviewDisputed {
		return fmt.Errorf("review outcome %q is invalid", review.Outcome)
	}
	if review.Outcome == CategoryReviewDisputed && review.DisputeReason == "" {
		return fmt.Errorf("disputed review requires a reason")
	}
	var sample models.CategoryRegressionSample
	if err := db.First(&sample, review.SampleID).Error; err != nil {
		return err
	}
	if review.Stage == CategoryReviewSecond {
		var initialCount int64
		if err := db.Model(&models.CategoryRegressionReview{}).
			Where("sample_id = ? AND stage = ?", review.SampleID, CategoryReviewInitial).
			Count(&initialCount).Error; err != nil {
			return err
		}
		if initialCount == 0 {
			return fmt.Errorf("second review requires an initial review")
		}
	}
	return db.Create(&review).Error
}

func BuildCategoryConfusionMatrix(
	db *gorm.DB,
	taxonomyHash string,
	promptHash string,
	modelBackend string,
	periodStart time.Time,
	periodEnd time.Time,
) (models.CategoryConfusionMatrix, error) {
	if db == nil {
		return models.CategoryConfusionMatrix{}, fmt.Errorf("database is required")
	}
	if taxonomyHash == "" || promptHash == "" || modelBackend == "" || !periodEnd.After(periodStart) {
		return models.CategoryConfusionMatrix{}, fmt.Errorf("confusion matrix period and dimensions are required")
	}
	var samples []models.CategoryRegressionSample
	if err := db.Where(
		"taxonomy_hash = ? AND prompt_hash = ? AND model_backend = ? AND status = ? AND last_run_at >= ? AND last_run_at < ?",
		taxonomyHash, promptHash, modelBackend, CategorySampleActive, periodStart, periodEnd,
	).Find(&samples).Error; err != nil {
		return models.CategoryConfusionMatrix{}, err
	}
	type matrixCell struct {
		Expected string `json:"expected"`
		Actual   string `json:"actual"`
		Count    int    `json:"count"`
	}
	cells := make([]matrixCell, 0)
	cellIndex := make(map[string]int)
	errorCount := 0
	for _, sample := range samples {
		key := sample.ExpectedCode + "\x00" + sample.ActualCode
		index, ok := cellIndex[key]
		if !ok {
			index = len(cells)
			cellIndex[key] = index
			cells = append(cells, matrixCell{Expected: sample.ExpectedCode, Actual: sample.ActualCode})
		}
		cells[index].Count++
		if sample.ActualCode != sample.ExpectedCode {
			errorCount++
		}
	}
	matrixRaw, err := json.Marshal(cells)
	if err != nil {
		return models.CategoryConfusionMatrix{}, err
	}
	matrix := models.CategoryConfusionMatrix{
		TaxonomyHash: taxonomyHash,
		PromptHash:   promptHash,
		ModelBackend: modelBackend,
		PeriodStart:  periodStart,
		PeriodEnd:    periodEnd,
		Matrix:       matrixRaw,
		SampleCount:  len(samples),
		ErrorCount:   errorCount,
	}
	if err := db.Create(&matrix).Error; err != nil {
		return models.CategoryConfusionMatrix{}, err
	}
	return matrix, nil
}
