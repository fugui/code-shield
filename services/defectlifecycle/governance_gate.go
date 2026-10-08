package defectlifecycle

import (
	"code-shield/models"
	"code-shield/services/coverage"
	"strings"
)

func ledgerObservationGroups(groups []ObservationGroup) []ObservationGroup {
	ledgerGroups := make([]ObservationGroup, 0, len(groups))
	for _, group := range groups {
		if !groupContainsLedgerFinding(group) {
			continue
		}
		ledgerGroups = append(ledgerGroups, group)
	}
	return ledgerGroups
}

func passObservationGroups(groups []ObservationGroup) []ObservationGroup {
	passGroups := make([]ObservationGroup, 0)
	for _, group := range groups {
		if isPassOnlyObservationGroup(group) {
			passGroups = append(passGroups, group)
		}
	}
	return passGroups
}

func groupContainsLedgerFinding(group ObservationGroup) bool {
	for _, finding := range group.Findings {
		if !isPassAssessment(finding) {
			return true
		}
	}
	return false
}

func isPassAssessment(finding models.AnalysisFinding) bool {
	switch strings.ToLower(strings.TrimSpace(finding.AssessmentStatus)) {
	case "valid", "pass", "not_target", "not_thread_creation", "safe", "not_change_related":
		return true
	}
	switch strings.ToLower(strings.TrimSpace(finding.AssessmentOutcome)) {
	case "pass", "not_target":
		return true
	}
	return strings.EqualFold(strings.TrimSpace(finding.Severity), "合格") &&
		strings.EqualFold(strings.TrimSpace(finding.Category), "无问题")
}

func isPassOnlyObservationGroup(group ObservationGroup) bool {
	return len(group.Findings) > 0 && !groupContainsLedgerFinding(group)
}

func coverageLifecycleBlocked(scanCoverage *coverage.Coverage) bool {
	if scanCoverage == nil {
		return true
	}
	return scanCoverage.Summary().CoverageDegraded
}
