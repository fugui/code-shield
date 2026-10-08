package planner

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"code-shield/services/coverage"
	"code-shield/services/engines/chunker"
	"code-shield/services/engines/profile"
)

type EntityPlan struct {
	Version      int          `json:"version"`
	Profile      string       `json:"profile"`
	EntityKind   string       `json:"entity_kind"`
	ManifestHash string       `json:"manifest_hash"`
	Entities     []TestEntity `json:"entities"`
}

type TestEntity struct {
	EntityID     string `json:"entity_id"`
	Language     string `json:"language"`
	FilePath     string `json:"file_path"`
	LineRange    string `json:"line_range"`
	EntityKind   string `json:"entity_kind"`
	Suite        string `json:"suite,omitempty"`
	Name         string `json:"name"`
	DisplayName  string `json:"display_name"`
	Parametrized bool   `json:"parametrized"`
	Hash         string `json:"hash"`
}

type sourceLine struct {
	Number int
	Text   string
}

var (
	cppTestPattern     = regexp.MustCompile(`^\s*TEST(?:_F|_P)?\s*\(\s*([A-Za-z0-9_]+)\s*,\s*([A-Za-z0-9_]+)\s*\)`)
	pythonClassPattern = regexp.MustCompile(`^\s*class\s+(Test[A-Za-z0-9_]*)`)
	pythonTestPattern  = regexp.MustCompile(`^\s*(?:async\s+)?def\s+(test_[A-Za-z0-9_]*)`)
	javaTestPattern    = regexp.MustCompile(`^\s*(?:public|protected|private)?\s*[A-Za-z0-9_<>,\[\]\s]+\s+([A-Za-z0-9_]+)\s*\(`)
	goTestPattern      = regexp.MustCompile(`^func\s+(Test[A-Za-z0-9_]*)\s*\(`)
	helperNamePattern  = regexp.MustCompile(`(?i)(setup|teardown|fixture|helper|mock|factory|utility|util)`)
)

func buildEntityScope(input Input) (ScopeDecision, *coverage.ScanPlan, PrimaryPlan, error) {
	plan, err := chunker.PlanFiles(input.CodesPath, input.Config, input.TargetScope)
	if err != nil {
		return ScopeDecision{
			Decision: DecisionFailed, Reason: ReasonPrimaryScopeFailed, ScopeProfile: input.Profile.Name,
			PrimaryUnit: profile.PrimaryUnitFile, Message: err.Error(),
		}, nil, nil, nil
	}

	decision := ScopeDecision{
		Decision: DecisionProceed, Reason: ReasonPrimaryScopeReady, ScopeProfile: input.Profile.Name,
		PrimaryUnit: "test_entity", CandidateFiles: len(plan.Selected), UnknownFiles: len(plan.Unknown),
		UnknownUnits: len(plan.Unknown), PlanManifestHash: manifestHash("test-entity-input|"+input.ProfileHash, plan.Selected),
	}
	if len(plan.Selected) == 0 {
		decision.Decision = DecisionSkipped
		decision.Reason = ReasonNoTestScope
		return decision, &plan, nil, nil
	}
	if decision.UnknownFiles > 0 {
		decision.Decision = DecisionProceedDegraded
		decision.Reason = ReasonPartialPrimaryScope
	}

	entityPlan, err := buildEntityPlan(input, plan.Selected)
	if err != nil {
		decision.Decision = DecisionFailed
		decision.Reason = ReasonEntityPlanFailed
		decision.Message = err.Error()
		return decision, &plan, nil, nil
	}
	decision.PrimaryUnits = len(entityPlan.Entities)
	decision.EntityUnits = len(entityPlan.Entities)
	decision.PlanManifestHash = entityPlan.ManifestHash
	if len(entityPlan.Entities) == 0 {
		decision.Decision = DecisionFailed
		decision.Reason = ReasonEntityPlanFailed
		decision.Message = "no test entities found in test scope"
	}
	return decision, &plan, entityPlan, nil
}

func (plan *EntityPlan) PrimaryCount() int {
	return len(plan.Entities)
}

func (plan *EntityPlan) PrimaryManifestHash() string {
	return plan.ManifestHash
}

func (plan *EntityPlan) PrimaryUnits() []coverage.PlanUnit {
	units := make([]coverage.PlanUnit, 0, len(plan.Entities))
	for _, entity := range plan.Entities {
		startLine, endLine := parseEntityLineRange(entity.LineRange)
		units = append(units, coverage.PlanUnit{
			ID:          entity.EntityID,
			Kind:        coverage.PlanUnitEntity,
			Path:        entity.FilePath,
			DisplayName: entity.DisplayName,
			StartLine:   startLine,
			EndLine:     endLine,
		})
	}
	return units
}

func parseEntityLineRange(lineRange string) (int, int) {
	parts := strings.SplitN(lineRange, "-", 2)
	startLine, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || startLine < 1 {
		return 0, 0
	}
	endLine := startLine
	if len(parts) == 2 {
		if parsed, parseErr := strconv.Atoi(strings.TrimSpace(parts[1])); parseErr == nil && parsed >= startLine {
			endLine = parsed
		}
	}
	return startLine, endLine
}

func buildEntityPlan(input Input, selected []coverage.PlannedFile) (*EntityPlan, error) {
	result := &EntityPlan{Version: 1, Profile: input.Profile.Name, EntityKind: "test_case"}
	for _, selectedFile := range selected {
		language := entityLanguage(selectedFile.Path)
		if language == "" {
			continue
		}
		lines, err := readLines(filepath.Join(input.CodesPath, selectedFile.Path))
		if err != nil {
			return nil, err
		}
		entities := extractEntities(language, selectedFile.Path, lines)
		result.Entities = append(result.Entities, entities...)
	}

	result.ManifestHash = manifestHash("test-entity-plan|"+input.ProfileHash, result.Entities)
	return result, nil
}

func readLines(path string) ([]sourceLine, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []sourceLine
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, sourceLine{Number: len(lines) + 1, Text: scanner.Text()})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

func extractEntities(language, path string, lines []sourceLine) []TestEntity {
	switch language {
	case profile.LanguageCPP:
		return extractCPPTests(language, path, lines)
	case profile.LanguagePython:
		return extractPythonTests(language, path, lines)
	case profile.LanguageJava:
		return extractJavaTests(language, path, lines)
	case profile.LanguageGo:
		return extractGoTests(language, path, lines)
	default:
		return nil
	}
}

func extractCPPTests(language, path string, lines []sourceLine) []TestEntity {
	var entities []TestEntity
	for _, line := range lines {
		match := cppTestPattern.FindStringSubmatch(line.Text)
		if match == nil {
			continue
		}
		if helperNamePattern.MatchString(match[2]) {
			continue
		}
		entities = append(entities, newEntity(language, path, line.Number, entityEndLine(lines, line.Number, "{", "}"), match[1], match[2], false, ""))
	}
	return entities
}

func extractPythonTests(language, path string, lines []sourceLine) []TestEntity {
	var entities []TestEntity
	suite := ""
	suiteIndent := -1
	for index, line := range lines {
		trimmed := strings.TrimSpace(line.Text)
		indent := len(line.Text) - len(strings.TrimLeft(line.Text, " \t"))
		if suiteIndent >= 0 && indent <= suiteIndent && trimmed != "" {
			suite = ""
			suiteIndent = -1
		}
		if match := pythonClassPattern.FindStringSubmatch(line.Text); match != nil {
			suite = match[1]
			suiteIndent = indent
			continue
		}
		if !strings.HasPrefix(trimmed, "def test_") && !strings.HasPrefix(trimmed, "async def test_") {
			continue
		}
		match := pythonTestPattern.FindStringSubmatch(line.Text)
		if match == nil || helperNamePattern.MatchString(strings.TrimPrefix(match[1], "test_")) {
			continue
		}
		parametrized := index > 0 && strings.Contains(strings.TrimSpace(lines[index-1].Text), "parametrize")
		entities = append(entities, newEntity(language, path, line.Number, pythonEndLine(lines[index:], indent), suite, match[1], parametrized, ""))
	}
	return entities
}

func extractJavaTests(language, path string, lines []sourceLine) []TestEntity {
	var entities []TestEntity
	suite := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line.Text)
		if strings.HasPrefix(trimmed, "class ") {
			suite = strings.Trim(strings.TrimPrefix(trimmed, "class "), "{ ")
			if fields := strings.Fields(suite); len(fields) > 0 {
				suite = fields[0]
			}
			continue
		}
		if !strings.Contains(trimmed, "@Test") && !strings.Contains(trimmed, "@ParameterizedTest") {
			continue
		}
		for index := range lines {
			if lines[index].Number <= line.Number {
				continue
			}
			match := javaTestPattern.FindStringSubmatch(lines[index].Text)
			if match == nil || helperNamePattern.MatchString(match[1]) {
				continue
			}
			entities = append(entities, newEntity(language, path, lines[index].Number, entityEndLine(lines, lines[index].Number, "{", "}"), suite, match[1], strings.Contains(trimmed, "@ParameterizedTest"), ""))
			break
		}
	}
	return entities
}

func extractGoTests(language, path string, lines []sourceLine) []TestEntity {
	var entities []TestEntity
	for _, line := range lines {
		match := goTestPattern.FindStringSubmatch(line.Text)
		if match == nil || helperNamePattern.MatchString(strings.TrimPrefix(match[1], "Test")) {
			continue
		}
		entities = append(entities, newEntity(language, path, line.Number, entityEndLine(lines, line.Number, "{", "}"), "", match[1], false, ""))
	}
	return entities
}

func newEntity(language, path string, start, end int, suite, name string, parametrized bool, context string) TestEntity {
	entity := TestEntity{
		Language: language, FilePath: path, LineRange: strconv.Itoa(start) + "-" + strconv.Itoa(end),
		EntityKind: "test_case", Suite: suite, Name: name, Parametrized: parametrized,
	}
	if suite != "" {
		entity.DisplayName = suite + "." + name
	} else {
		entity.DisplayName = name
	}
	if context != "" {
		entity.Hash = contextHash(path, context)
	} else {
		entity.Hash = contextHash(path, entity.LineRange)
	}
	entity.EntityID = hash("test-entity|" + entity.FilePath + "\x1f" + entity.DisplayName + "\x1f" + entity.LineRange + "\x1f" + entity.Hash)
	return entity
}

func entityEndLine(lines []sourceLine, start int, open, close string) int {
	depth := 0
	opened := false
	for _, line := range lines {
		if line.Number < start {
			continue
		}
		text := strings.TrimSpace(line.Text)
		if !opened && strings.Contains(text, open) {
			opened = true
		}
		if opened {
			depth += strings.Count(text, open) - strings.Count(text, close)
			if depth <= 0 && line.Number > start {
				return line.Number
			}
		}
	}
	return start
}

func pythonEndLine(lines []sourceLine, indent int) int {
	if len(lines) == 0 {
		return 1
	}
	start := lines[0].Number
	for _, line := range lines[1:] {
		if strings.TrimSpace(line.Text) == "" {
			continue
		}
		if len(line.Text)-len(strings.TrimLeft(line.Text, " \t")) <= indent {
			return line.Number - 1
		}
	}
	if len(lines) == 0 {
		return start
	}
	return lines[len(lines)-1].Number
}

func entityLanguage(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".c", ".cc", ".cpp", ".cxx", ".h", ".hpp", ".hxx":
		return profile.LanguageCPP
	case ".py":
		return profile.LanguagePython
	case ".java":
		return profile.LanguageJava
	case ".go":
		return profile.LanguageGo
	default:
		return ""
	}
}
