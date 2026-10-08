package defectlifecycle

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func TestIdentityDoesNotDependOnGORM(t *testing.T) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "identity.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse identity.go: %v", err)
	}
	for _, imported := range file.Imports {
		if imported.Path.Value == `"gorm.io/gorm"` {
			t.Fatalf("identity.go must not import GORM")
		}
	}
}

func TestMatchingDoesNotDependOnGORM(t *testing.T) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "matching.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse matching.go: %v", err)
	}
	for _, imported := range file.Imports {
		if imported.Path.Value == `"gorm.io/gorm"` {
			t.Fatalf("matching.go must not import GORM")
		}
	}
}

func TestWorkbenchDoesNotReadCampaignFindings(t *testing.T) {
	source, err := os.ReadFile("../../handlers/workbench.go")
	if err != nil {
		t.Fatalf("read workbench handler: %v", err)
	}
	if strings.Contains(string(source), "CampaignFinding") {
		t.Fatal("workbench handler must not read CampaignFinding")
	}
}

func TestGenericCampaignRejectsDefectGovernanceModes(t *testing.T) {
	source, err := os.ReadFile("../governance/campaign.go")
	if err != nil {
		t.Fatalf("read campaign governance: %v", err)
	}
	if !strings.Contains(string(source), "GovernanceModeEntityAssessment") {
		t.Fatal("generic campaign must only own entity assessment")
	}
}

func TestDefectAssignmentIsEmbeddedInDefect(t *testing.T) {
	source, err := os.ReadFile("../../models/models.go")
	if err != nil {
		t.Fatalf("read models: %v", err)
	}
	if strings.Contains(string(source), "type DefectAssignment struct") {
		t.Fatal("current assignee must be a field on Defect, not a separate 1:1 table")
	}
}

func TestCampaignDepartmentProjectionDoesNotUseCampaignFindings(t *testing.T) {
	source, err := os.ReadFile("../../handlers/campaign_generic.go")
	if err != nil {
		t.Fatalf("read campaign handlers: %v", err)
	}
	start := strings.Index(string(source), "func FetchCampaignDeptSummaries")
	end := strings.Index(string(source), "func GetDynamicCampaignTrends")
	if start < 0 || end < start {
		t.Fatal("cannot locate department projection")
	}
	if strings.Contains(string(source)[start:end], "campaign_findings") {
		t.Fatal("department projection must be derived from campaign repo summaries")
	}
}
