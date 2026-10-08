package assessment

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const AssessmentStage = "specialized_assessment"

type ArtifactIssue struct {
	Stage       string `json:"stage"`
	Schema      string `json:"schema"`
	JSONPath    string `json:"json_path"`
	Field       string `json:"field"`
	Code        string `json:"code"`
	Message     string `json:"message"`
	Recoverable bool   `json:"recoverable"`
}

type ArtifactContract struct {
	SchemaID   string         `json:"schema_id"`
	Stage      string         `json:"stage"`
	JSONSchema map[string]any `json:"json_schema"`
	Rendered   string         `json:"rendered"`
	SchemaHash string         `json:"schema_hash"`
}

type ArtifactContractRegistry struct {
	contracts map[string]ArtifactContract
}

var ErrArtifactContractRegistry = errors.New("artifact contract registry")

func NewArtifactContractRegistry() *ArtifactContractRegistry {
	return &ArtifactContractRegistry{contracts: make(map[string]ArtifactContract)}
}

func (registry *ArtifactContractRegistry) Register(name string, contract ArtifactContract) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%w: contract profile name is empty", ErrArtifactContractRegistry)
	}
	if contract.SchemaID == "" || contract.Stage == "" || contract.JSONSchema == nil || contract.SchemaHash == "" {
		return fmt.Errorf("%w: profile %q contract is incomplete", ErrArtifactContractRegistry, name)
	}
	registry.contracts[name] = contract
	return nil
}

func (registry *ArtifactContractRegistry) Get(name string) (ArtifactContract, error) {
	contract, exists := registry.contracts[name]
	if !exists {
		return ArtifactContract{}, fmt.Errorf("%w: artifact contract %q is not registered", ErrArtifactContractRegistry, name)
	}
	return contract, nil
}

func (registry *ArtifactContractRegistry) Names() []string {
	names := make([]string, 0, len(registry.contracts))
	for name := range registry.contracts {
		names = append(names, name)
	}
	return names
}

func ArtifactContractForOutput(stage string, contract OutputContract, rendered string) (ArtifactContract, error) {
	if strings.TrimSpace(stage) == "" {
		return ArtifactContract{}, fmt.Errorf("%w: artifact stage is empty", ErrArtifactContractRegistry)
	}
	if contract.SchemaID == "" {
		return ArtifactContract{}, fmt.Errorf("%w: artifact schema id is empty", ErrArtifactContractRegistry)
	}
	outcomes := append([]string(nil), contract.AllowedOutcomes...)
	if len(outcomes) == 0 {
		outcomes = []string{
			string(OutcomePass), string(OutcomeDefect), string(OutcomeNotTarget), string(OutcomeNeedsHuman),
		}
	}
	jsonSchema := map[string]any{
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		"type":                 "object",
		"required":             []string{"schema", "assessments"},
		"additionalProperties": false,
		"properties": map[string]any{
			"schema":      map[string]any{"type": "string", "const": contract.SchemaID},
			"assessments": assessmentsJSONSchema(outcomes),
		},
	}
	for _, forbidden := range contract.ForbiddenTopLevel {
		jsonSchema["properties"].(map[string]any)[forbidden] = map[string]any{"not": map[string]any{}}
	}
	schemaHash, err := schemaHash(jsonSchema)
	if err != nil {
		return ArtifactContract{}, err
	}
	if strings.TrimSpace(rendered) == "" {
		rendered = RenderArtifactContract(jsonSchema, contract.AllowedCategories, outcomes)
	}
	return ArtifactContract{
		SchemaID:   contract.SchemaID,
		Stage:      stage,
		JSONSchema: jsonSchema,
		Rendered:   rendered,
		SchemaHash: schemaHash,
	}, nil
}

func assessmentsJSONSchema(outcomes []string) map[string]any {
	return map[string]any{
		"type":     "array",
		"minItems": 1,
		"items": map[string]any{
			"type":                 "object",
			"required":             []string{"unit_ref", "outcome"},
			"additionalProperties": false,
			"properties": map[string]any{
				"unit_ref":        map[string]any{"type": "string"},
				"primary_unit_id": map[string]any{"type": "string"},
				"outcome":         map[string]any{"type": "string", "enum": outcomes},
				"summary":         map[string]any{"type": "string"},
				"reason":          map[string]any{"type": "string"},
				"evidence":        map[string]any{"type": "object"},
				"domain":          map[string]any{"type": "object"},
				"issues":          issuesJSONSchema(),
			},
		},
	}
}

func issuesJSONSchema() map[string]any {
	return map[string]any{
		"type": "array",
		"items": map[string]any{
			"type":                 "object",
			"required":             []string{"category", "severity", "detail"},
			"additionalProperties": false,
			"properties": map[string]any{
				"code":       map[string]any{"type": "string"},
				"field":      map[string]any{"type": "string"},
				"category":   map[string]any{"type": "string"},
				"severity":   map[string]any{"type": "string"},
				"detail":     map[string]any{"type": "string"},
				"suggestion": map[string]any{"type": "string"},
				"evidence":   map[string]any{"type": "object"},
			},
		},
	}
}

func RenderArtifactContract(jsonSchema map[string]any, allowedCategories, allowedOutcomes []string) string {
	var sb strings.Builder
	sb.WriteString("Output Contract:\n")
	sb.WriteString("1. Return exactly one JSON object.\n")
	sb.WriteString("2. Do not use Markdown fences, comments, logs, or explanations.\n")
	sb.WriteString("3. Escape all double quotes inside JSON strings.\n")
	if schemaBytes, err := json.MarshalIndent(jsonSchema, "", "  "); err == nil {
		sb.WriteString("\nJSON Schema:\n```json\n")
		sb.Write(schemaBytes)
		sb.WriteString("\n```\n")
	}
	if len(allowedOutcomes) > 0 {
		sb.WriteString(fmt.Sprintf("\nAllowed outcomes: %s\n", strings.Join(allowedOutcomes, ", ")))
	}
	if len(allowedCategories) > 0 {
		sb.WriteString(fmt.Sprintf("Allowed issue categories: %s\n", strings.Join(allowedCategories, ", ")))
	}
	return sb.String()
}

func schemaHash(jsonSchema map[string]any) (string, error) {
	encoded, err := json.Marshal(jsonSchema)
	if err != nil {
		return "", fmt.Errorf("encode artifact JSON schema: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}
