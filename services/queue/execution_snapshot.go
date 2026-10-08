package queue

import (
	"code-shield/models"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const executionSnapshotVersion = 1

type executionSnapshotResult struct {
	Snapshot models.ExecutionContextSnapshot
	Encoded  json.RawMessage
}

func buildTaskTypeRevision(taskType models.TaskType) (models.TaskTypeRevision, error) {
	if _, err := models.ParseAssessmentConfig(taskType.AssessmentConfig); err != nil {
		return models.TaskTypeRevision{}, err
	}
	promptContent, err := taskPackageContent(models.AppConfig.GetAbsPath(taskType.AnalysisPromptFile()))
	if err != nil {
		return models.TaskTypeRevision{}, fmt.Errorf("load analysis prompt: %w", err)
	}
	postprocessContent, err := taskPackageContent(models.AppConfig.GetAbsPath(taskType.PostprocessScript()))
	if err != nil {
		return models.TaskTypeRevision{}, fmt.Errorf("load postprocess: %w", err)
	}

	taxonomyValue := taskType.GetCategoryTaxonomy()
	categories := taskType.GetAllowedCategories()
	taxonomySchemaVersion := 0
	var taxonomyHash string
	var taxonomyRaw datatypes.JSON
	if taxonomyValue != nil {
		taxonomyValue.Hash = ""
		taxonomyJSON, marshalErr := json.Marshal(taxonomyValue)
		if marshalErr != nil {
			return models.TaskTypeRevision{}, fmt.Errorf("canonicalize taxonomy: %w", marshalErr)
		}
		taxonomySchemaVersion = taxonomyValue.SchemaVersion
		taxonomyHash = canonicalJSONHash(taxonomyValue)
		taxonomyRaw = datatypes.JSON(canonicalJSONRaw(taxonomyJSON))
	} else {
		taxonomySchemaVersion = 1
		taxonomyHash = canonicalJSONHash(categories)
	}
	revision := models.TaskTypeRevision{
		TaskTypeID:            taskType.ID,
		EngineMode:            taskType.EngineMode,
		EngineConfig:          datatypes.JSON(canonicalJSONRaw(json.RawMessage(taskType.EngineConfig))),
		EngineConfigHash:      engineConfigHash(json.RawMessage(taskType.EngineConfig)),
		AssessmentConfig:      datatypes.JSON(canonicalJSONRaw(json.RawMessage(taskType.AssessmentConfig))),
		AssessmentConfigHash:  canonicalHash(json.RawMessage(taskType.AssessmentConfig)),
		PromptContent:         promptContent,
		PromptContentHash:     contentHash(promptContent),
		Categories:            datatypes.JSON(marshalSnapshotProjection(categories)),
		CategorySchemaHash:    canonicalJSONHash(categories),
		Taxonomy:              taxonomyRaw,
		TaxonomySchemaVersion: taxonomySchemaVersion,
		TaxonomyHash:          taxonomyHash,
		DomainFamily:          taskType.GetDomainFamily(),
		DefenseDimensions:     datatypes.JSON(canonicalJSONRaw(json.RawMessage(taskType.DefenseDimensions))),
		GovernanceMode:        models.ResolveGovernanceMode(taskType.GovernanceMode),
		DomainLabel:           taskType.GetDomainLabel(),
		TargetSemantics:       datatypes.JSON(canonicalJSONRaw(json.RawMessage(taskType.TargetSemantics))),
		DisplaySemantics:      datatypes.JSON(canonicalJSONRaw(json.RawMessage(taskType.DisplaySemantics))),
		PostprocessContent:    postprocessContent,
		PostprocessHash:       contentHash(postprocessContent),
	}
	revision.AggregateHash = revisionAggregateHash(revision)
	return revision, nil
}

func ensureTaskTypeRevision(tx *gorm.DB, revision models.TaskTypeRevision) (models.TaskTypeRevision, error) {
	var existing models.TaskTypeRevision
	err := tx.Where("task_type_id = ? AND aggregate_hash = ?", revision.TaskTypeID, revision.AggregateHash).
		First(&existing).Error
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return models.TaskTypeRevision{}, fmt.Errorf("load task type revision: %w", err)
	}

	var revisionCount int64
	if err := tx.Model(&models.TaskTypeRevision{}).
		Where("task_type_id = ?", revision.TaskTypeID).
		Count(&revisionCount).Error; err != nil {
		return models.TaskTypeRevision{}, fmt.Errorf("count task type revisions: %w", err)
	}
	revision.Revision = int(revisionCount) + 1
	if err := tx.Create(&revision).Error; err != nil {
		return models.TaskTypeRevision{}, fmt.Errorf("create task type revision: %w", err)
	}
	return revision, nil
}

func revisionAggregateHash(revision models.TaskTypeRevision) string {
	values := map[string]string{
		"engine_mode":             revision.EngineMode,
		"engine_config_hash":      revision.EngineConfigHash,
		"assessment_config_hash":  revision.AssessmentConfigHash,
		"prompt_content_hash":     revision.PromptContentHash,
		"category_schema_hash":    revision.CategorySchemaHash,
		"taxonomy_schema_version": strconv.Itoa(revision.TaxonomySchemaVersion),
		"taxonomy_hash":           revision.TaxonomyHash,
		"domain_family":           revision.DomainFamily,
		"domain_label":            revision.DomainLabel,
		"defense_dimensions":      contentHash(string(revision.DefenseDimensions)),
		"target_semantics":        contentHash(string(revision.TargetSemantics)),
		"display_semantics":       contentHash(string(revision.DisplaySemantics)),
		"governance_mode":         revision.GovernanceMode,
		"postprocess_hash":        revision.PostprocessHash,
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+values[key])
	}
	return contentHash(strings.Join(parts, "\n"))
}

func buildExecutionSnapshot(revision models.TaskTypeRevision, scanProfileRaw json.RawMessage, scanProfileHash string) (executionSnapshotResult, error) {
	var categories []string
	if err := json.Unmarshal(revision.Categories, &categories); err != nil {
		return executionSnapshotResult{}, fmt.Errorf("decode category snapshot: %w", err)
	}
	var taxonomy models.CategoryTaxonomy
	if len(revision.Taxonomy) > 0 {
		if err := json.Unmarshal(revision.Taxonomy, &taxonomy); err != nil {
			return executionSnapshotResult{}, fmt.Errorf("decode taxonomy snapshot: %w", err)
		}
		taxonomy.Hash = revision.TaxonomyHash
	}
	revisionID := ""
	if revision.ID > 0 {
		revisionID = strconv.FormatUint(uint64(revision.ID), 10)
	}
	snapshot := models.ExecutionContextSnapshot{
		EngineMode:            revision.EngineMode,
		ScanProfile:           scanProfileRaw,
		ScanProfileHash:       scanProfileHash,
		EngineConfig:          canonicalJSONRaw(json.RawMessage(revision.EngineConfig)),
		EngineConfigHash:      revision.EngineConfigHash,
		AssessmentConfig:      canonicalJSONRaw(json.RawMessage(revision.AssessmentConfig)),
		AssessmentConfigHash:  revision.AssessmentConfigHash,
		PromptContent:         revision.PromptContent,
		PromptContentHash:     revision.PromptContentHash,
		Categories:            append([]string(nil), categories...),
		CategorySchemaHash:    revision.CategorySchemaHash,
		Taxonomy:              taxonomy,
		TaxonomySchemaVersion: revision.TaxonomySchemaVersion,
		TaxonomyHash:          revision.TaxonomyHash,
		DomainFamily:          revision.DomainFamily,
		DomainLabel:           revision.DomainLabel,
		DefenseDimensions:     canonicalJSONRaw(json.RawMessage(revision.DefenseDimensions)),
		TargetSemantics:       canonicalJSONRaw(json.RawMessage(revision.TargetSemantics)),
		DisplaySemantics:      canonicalJSONRaw(json.RawMessage(revision.DisplaySemantics)),
		GovernanceMode:        revision.GovernanceMode,
		PostprocessContent:    revision.PostprocessContent,
		PostprocessHash:       revision.PostprocessHash,
		TaskTypeRevisionID:    revisionID,
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return executionSnapshotResult{}, fmt.Errorf("encode execution snapshot: %w", err)
	}
	return executionSnapshotResult{Snapshot: snapshot, Encoded: encoded}, nil
}

func taskPackageContent(path string) (string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func canonicalJSONRaw(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return append(json.RawMessage(nil), raw...)
	}
	normalized, err := json.Marshal(value)
	if err != nil {
		return append(json.RawMessage(nil), raw...)
	}
	return normalized
}

func canonicalJSONHash(value any) string {
	normalized, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return contentHash(string(normalized))
}

func canonicalHash(raw json.RawMessage) string {
	return contentHash(string(canonicalJSONRaw(raw)))
}

func contentHash(content string) string {
	if content == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(content))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func marshalSnapshotProjection(categories []string) []byte {
	if len(categories) == 0 {
		return []byte("[]")
	}
	encoded, err := json.Marshal(categories)
	if err != nil {
		return []byte("[]")
	}
	return encoded
}
