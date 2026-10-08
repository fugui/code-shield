package entityreview

import (
	"encoding/json"
	"fmt"
	"strings"

	"code-shield/services/engines/assessment"
)

func (Profile) Normalize(raw string, session *assessment.PromptRefSession) (assessment.AssessmentArtifact, error) {
	cleaned := assessment.CleanJSON([]byte(raw))
	if !json.Valid(cleaned) {
		return assessment.AssessmentArtifact{}, fmt.Errorf("%w: AI output is not valid JSON", assessment.ErrContractRepair)
	}
	artifact := assessment.AssessmentArtifact{}
	if err := json.Unmarshal(cleaned, &artifact); err != nil {
		return assessment.AssessmentArtifact{}, fmt.Errorf("%w: decode entity assessments: %v", assessment.ErrContractRepair, err)
	}
	if artifact.Schema != ArtifactSchemaV2 {
		return artifact, fmt.Errorf("%w: expected schema %q, got %q", assessment.ErrContractRepair, ArtifactSchemaV2, artifact.Schema)
	}
	for i := range artifact.Assessments {
		item := &artifact.Assessments[i]
		item.UnitRef = strings.TrimSpace(item.UnitRef)
		if item.UnitRef == "" {
			return artifact, fmt.Errorf("%w: assessments[%d].unit_ref is empty", assessment.ErrContractRepair, i)
		}
		if unitID, err := session.UnitIDForRef(item.UnitRef); err == nil {
			item.PrimaryUnitID = unitID
		}
	}
	return artifact, nil
}
