package migration

import (
	"code-shield/models"
	assessmentprofiles "code-shield/services/engines/assessment/profiles"

	"gorm.io/gorm"
)

func backfillAssessmentOutcomes(db *gorm.DB) error {
	var findings []models.AnalysisFinding
	return db.Where("assessment_outcome = '' AND assessment_status <> ''").
		FindInBatches(&findings, 500, func(tx *gorm.DB, _ int) error {
			for _, finding := range findings {
				outcome, ok := assessmentprofiles.OutcomeForStatus(finding.AssessmentStatus)
				if !ok {
					continue
				}
				if err := tx.Model(&models.AnalysisFinding{}).
					Where("id = ? AND assessment_outcome = ''", finding.ID).
					Update("assessment_outcome", string(outcome)).Error; err != nil {
					return err
				}
			}
			return nil
		}).Error
}
