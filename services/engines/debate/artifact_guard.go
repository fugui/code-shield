package debate

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	IssueAliasNormalized           = "ALIAS_FIELD_NORMALIZED"
	IssuePathNormalized            = "ABSOLUTE_PATH_NORMALIZED"
	IssueLineRangeNormalized       = "LINE_RANGE_SPLIT_NORMALIZED"
	IssueSnippetEnriched           = "SNIPPET_ENRICHED"
	IssueCandidateIDRepaired       = "CANDIDATE_ID_REPAIRED"
	IssueCandidateQuarantined      = "CANDIDATE_QUARANTINED"
	IssueCategoryReviewRequired    = "CATEGORY_REVIEW_REQUIRED"
	IssueCategoryExtraFieldIgnored = "CATEGORY_REPAIR_EXTRA_FIELD_IGNORED"
	IssueUnresolved                = "ARTIFACT_ISSUE_UNRESOLVED"
)

type ArtifactIssue struct {
	Stage       string `json:"stage"`
	Schema      string `json:"schema"`
	JSONPath    string `json:"json_path,omitempty"`
	Code        string `json:"code"`
	CandidateID string `json:"candidate_id,omitempty"`
	Field       string `json:"field,omitempty"`
	Message     string `json:"message"`
	Recoverable bool   `json:"recoverable"`
	RawArtifact string `json:"-"`
}

type lineInterval struct {
	Start int
	End   int
}

var artifactLineRangePattern = regexp.MustCompile(`^\d+(?:-\d+)?$`)

// NormalizeHunterArtifactJSON coerces known, deterministic Hunter drift before
// strict validation. It never changes business semantics such as category,
// severity or the substance of a finding.
func NormalizeHunterArtifactJSON(raw []byte, repoRoot string, targetFiles []string) ([]byte, []ArtifactIssue) {
	cleaned := cleanJSONOutput(raw)
	issues := make([]ArtifactIssue, 0)
	syntaxRepaired := false
	if !json.Valid(cleaned) {
		if recovered, ok := repairJSONTrailingCommas(cleaned); ok {
			cleaned = recovered
			syntaxRepaired = true
		}
	}

	var root map[string]interface{}
	if err := json.Unmarshal(cleaned, &root); err != nil {
		return cleaned, nil
	}

	schema, _ := root["schema"].(string)
	if syntaxRepaired {
		issues = append(issues, jsonSyntaxIssue(schema))
	}
	normalized := false

	if rawCandidates, ok := root["candidates"].([]interface{}); ok {
		for i, rawCandidate := range rawCandidates {
			candidate, ok := rawCandidate.(map[string]interface{})
			if !ok {
				continue
			}
			normalizedCandidate, candidateIssues := normalizeHunterCandidateMap(candidate, schema, i, repoRoot, targetFiles)
			rawCandidates[i] = normalizedCandidate
			issues = append(issues, candidateIssues...)
			if len(candidateIssues) > 0 || !sameJSONValue(candidate, normalizedCandidate) {
				normalized = true
			}
		}
		if candidateMaps := interfaceMaps(rawCandidates); len(candidateMaps) > 0 {
			idIssues, changed := repairCandidateIDs(candidateMaps, schema)
			issues = append(issues, idIssues...)
			if changed {
				for i, candidate := range candidateMaps {
					rawCandidates[i] = candidate
				}
				normalized = true
			}
		}
	}

	if !normalized {
		return cleaned, issues
	}
	out, err := json.Marshal(root)
	if err != nil {
		return raw, issues
	}
	return out, issues
}

func normalizeHunterCandidateMap(candidate map[string]interface{}, schema string, index int, repoRoot string, targetFiles []string) (map[string]interface{}, []ArtifactIssue) {
	issues := make([]ArtifactIssue, 0)
	id := fmt.Sprintf("$.candidates[%d]", index)
	candidateID := stringValue(candidate["candidate_id"])

	type aliasRule struct {
		target string
		keys   []string
	}
	rules := []aliasRule{
		{target: "candidate_id", keys: []string{"id", "candidateId", "finding_id"}},
		{target: "file_path", keys: []string{"file", "path", "source_file", "filename"}},
		{target: "scope_symbol", keys: []string{"function", "symbol", "method"}},
		{target: "code_snippet", keys: []string{"snippet", "code"}},
		{target: "line_range", keys: []string{"line", "line_number", "lines"}},
		{target: "title", keys: []string{"name", "issue"}},
	}
	for _, rule := range rules {
		if strings.TrimSpace(stringValue(candidate[rule.target])) != "" {
			continue
		}
		for _, key := range rule.keys {
			value := candidate[key]
			if text := strings.TrimSpace(stringValue(value)); text != "" {
				candidate[rule.target] = text
				delete(candidate, key)
				candidateID = firstNonEmpty(candidateID, stringValue(candidate["candidate_id"]))
				issues = append(issues, ArtifactIssue{
					Stage: "hunter", Schema: schema, JSONPath: id, CandidateID: candidateID,
					Code: IssueAliasNormalized, Field: rule.target,
					Message: fmt.Sprintf("alias field %q normalized to %q", key, rule.target), Recoverable: true,
				})
				break
			}
		}
	}
	candidateID = strings.TrimSpace(stringValue(candidate["candidate_id"]))

	description := strings.TrimSpace(stringValue(candidate["description"]))
	if strings.TrimSpace(stringValue(candidate["trigger_condition"])) == "" && description != "" {
		candidate["trigger_condition"] = description
		issues = append(issues, ArtifactIssue{
			Stage: "hunter", Schema: schema, JSONPath: id, CandidateID: candidateID,
			Code: IssueAliasNormalized, Field: "trigger_condition",
			Message: "description used as trigger_condition fallback", Recoverable: true,
		})
	}
	if strings.TrimSpace(stringValue(candidate["title"])) == "" {
		if title := deriveTitle(description); title != "" {
			candidate["title"] = title
			issues = append(issues, ArtifactIssue{
				Stage: "hunter", Schema: schema, JSONPath: id, CandidateID: candidateID,
				Code: IssueAliasNormalized, Field: "title",
				Message: "title derived from description", Recoverable: true,
			})
		}
	}

	rawPath := strings.TrimSpace(stringValue(candidate["file_path"]))
	if rawPath != "" {
		if normalizedPath, changed := normalizeArtifactPath(rawPath, repoRoot, targetFiles); changed {
			candidate["file_path"] = normalizedPath
			issues = append(issues, ArtifactIssue{
				Stage: "hunter", Schema: schema, JSONPath: id, CandidateID: candidateID,
				Code: IssuePathNormalized, Field: "file_path",
				Message: fmt.Sprintf("file path normalized from %q to %q", rawPath, normalizedPath), Recoverable: true,
			})
		}
	}
	filePath := strings.TrimSpace(stringValue(candidate["file_path"]))

	if text := strings.TrimSpace(stringValue(candidate["trigger_line"])); text != "" &&
		!isJSONString(candidate["trigger_line"]) {
		candidate["trigger_line"] = text
	}

	intervals, additional, lineIssues := normalizeHunterLineRanges(candidate, schema, id, candidateID, filePath, repoRoot)
	issues = append(issues, lineIssues...)
	if len(intervals) > 0 {
		candidate["line_range"] = formatLineInterval(intervals[0])
		if len(additional) > 0 {
			candidate["additional_line_ranges"] = additional
			issues = append(issues, ArtifactIssue{
				Stage: "hunter", Schema: schema, JSONPath: id, CandidateID: candidateID,
				Code: IssueLineRangeNormalized, Field: "line_range",
				Message:     fmt.Sprintf("multi-range line anchor normalized; additional locations: %s", strings.Join(additional, ", ")),
				Recoverable: true,
			})
		}
	}

	if filePath != "" && len(intervals) > 0 && strings.TrimSpace(stringValue(candidate["code_snippet"])) == "" {
		if snippet := extractSourceSnippet(repoRoot, filePath, intervals[0], 40); snippet != "" {
			candidate["code_snippet"] = snippet
			issues = append(issues, ArtifactIssue{
				Stage: "hunter", Schema: schema, JSONPath: id, CandidateID: candidateID,
				Code: IssueSnippetEnriched, Field: "code_snippet",
				Message: "code snippet enriched from source", Recoverable: true,
			})
		}
	}

	if filePath != "" && len(intervals) > 0 && strings.TrimSpace(stringValue(candidate["trigger_line"])) == "" {
		if trigger := extractSourceLine(repoRoot, filePath, intervals[0].Start); trigger != "" {
			candidate["trigger_line"] = trigger
			issues = append(issues, ArtifactIssue{
				Stage: "hunter", Schema: schema, JSONPath: id, CandidateID: candidateID,
				Code: IssueSnippetEnriched, Field: "trigger_line",
				Message: "trigger_line enriched from source", Recoverable: true,
			})
		}
	}

	return candidate, issues
}

func normalizeArtifactPath(rawPath, repoRoot string, targetFiles []string) (string, bool) {
	path := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rawPath), "file://"))
	path = filepath.ToSlash(path)
	if path == "" {
		return rawPath, false
	}
	clean := filepath.ToSlash(filepath.Clean(strings.TrimPrefix(path, "./")))
	if !filepath.IsAbs(clean) && clean != rawPath {
		return clean, clean != rawPath
	}
	if !filepath.IsAbs(clean) {
		return clean, clean != rawPath
	}
	if repoRoot != "" {
		absRoot, err := filepath.Abs(repoRoot)
		if err == nil {
			if rel, relErr := filepath.Rel(absRoot, clean); relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				rel = filepath.ToSlash(rel)
				return rel, rel != rawPath
			}
		}
	}
	for _, target := range targetFiles {
		target = filepath.ToSlash(strings.TrimSpace(target))
		if target != "" && strings.HasSuffix(clean, "/"+target) {
			return target, target != rawPath
		}
	}
	return rawPath, false
}

func normalizeHunterLineRanges(candidate map[string]interface{}, schema, jsonPath, candidateID, filePath, repoRoot string) ([]lineInterval, []string, []ArtifactIssue) {
	issues := make([]ArtifactIssue, 0)
	primary, hasPrimary := parseLineIntervalValue(candidate["primary_line_range"])
	if !hasPrimary {
		primary, hasPrimary = parseLineIntervalValue(candidate["anchor_line_range"])
	}
	intervals, parseIssues := parseIntervalsValue(candidate["line_range"], schema, jsonPath, candidateID, "line_range")
	issues = append(issues, parseIssues...)

	unique := map[lineInterval]struct{}{}
	if hasPrimary {
		intervals = append([]lineInterval{primary}, intervals...)
	}
	additionalRaw, ok := candidate["additional_line_ranges"]
	if ok {
		additionalIntervals, additionalIssues := parseIntervalsValue(additionalRaw, schema, jsonPath, candidateID, "additional_line_ranges")
		intervals = append(intervals, additionalIntervals...)
		issues = append(issues, additionalIssues...)
	}

	cleaned := make([]lineInterval, 0, len(intervals))
	for _, interval := range intervals {
		if _, exists := unique[interval]; exists {
			continue
		}
		unique[interval] = struct{}{}
		cleaned = append(cleaned, interval)
	}

	primary = lineInterval{}
	if len(cleaned) > 1 {
		if trigger, triggerErr := strconv.Atoi(strings.TrimSpace(stringValue(candidate["trigger_line"]))); triggerErr == nil {
			for _, interval := range cleaned {
				if trigger >= interval.Start && trigger <= interval.End {
					primary = interval
					break
				}
			}
		}
		if primary.Start == 0 {
			if found, ok := selectIntervalByTriggerSource(repoRoot, filePath, stringValue(candidate["trigger_line"]), cleaned); ok {
				primary = found
			}
		}
	}
	if primary.Start == 0 && len(cleaned) > 0 {
		primary = cleaned[0]
	}

	additional := make([]string, 0, max(0, len(cleaned)-1))
	for _, interval := range cleaned {
		if interval == primary {
			continue
		}
		additional = append(additional, formatLineInterval(interval))
	}
	return cleaned, additional, issues
}

func parseIntervalsValue(value interface{}, schema, jsonPath, candidateID, field string) ([]lineInterval, []ArtifactIssue) {
	intervals := make([]lineInterval, 0)
	issues := make([]ArtifactIssue, 0)
	appendInterval := func(raw interface{}) {
		if text, isText := raw.(string); isText {
			if spaceIntervals, split := splitSpaceSeparatedRanges(text); split {
				intervals = append(intervals, spaceIntervals...)
				issues = append(issues, spaceSplitIssue(schema, jsonPath, candidateID, field, text))
				return
			}
		}
		interval, ok := parseLineIntervalValue(raw)
		if ok {
			intervals = append(intervals, interval)
			return
		}
		message := fmt.Sprintf("invalid line range %v", raw)
		if encoded, encodeErr := json.Marshal(raw); encodeErr == nil {
			message = fmt.Sprintf("invalid line range %s", string(encoded))
		}
		issues = append(issues, ArtifactIssue{
			Stage: "hunter", Schema: schema, JSONPath: jsonPath, CandidateID: candidateID,
			Code: IssueUnresolved, Field: field,
			Message: message, Recoverable: false,
		})
	}

	switch typed := value.(type) {
	case nil:
	case string:
		if spaceIntervals, split := splitSpaceSeparatedRanges(typed); split {
			intervals = append(intervals, spaceIntervals...)
			issues = append(issues, spaceSplitIssue(schema, jsonPath, candidateID, field, typed))
			break
		}
		parts := strings.FieldsFunc(typed, func(r rune) bool {
			return r == ',' || r == '，' || r == ';' || r == '；' || r == '、' || r == '\n' || r == '\t'
		})
		for _, part := range parts {
			interval, ok := parseLineIntervalString(strings.TrimSpace(part))
			if ok {
				intervals = append(intervals, interval)
			} else if strings.TrimSpace(part) != "" {
				issues = append(issues, ArtifactIssue{
					Stage: "hunter", Schema: schema, JSONPath: jsonPath, CandidateID: candidateID,
					Code: IssueUnresolved, Field: field,
					Message: fmt.Sprintf("invalid line range %q", part), Recoverable: false,
				})
			}
		}
	case []interface{}:
		if interval, ok := parseJSONNumberIntervalPair(typed); ok {
			intervals = append(intervals, interval)
			return intervals, issues
		}
		for _, item := range typed {
			if nested, ok := item.([]interface{}); ok {
				if interval, pairOk := parseJSONNumberIntervalPair(nested); pairOk {
					intervals = append(intervals, interval)
					continue
				}
			}
			appendInterval(item)
		}
	case map[string]interface{}:
		appendInterval(typed)
	default:
		appendInterval(typed)
	}
	return intervals, issues
}

func parseJSONNumberIntervalPair(items []interface{}) (lineInterval, bool) {
	if len(items) != 2 {
		return lineInterval{}, false
	}
	for _, item := range items {
		switch item.(type) {
		case float64, json.Number:
		default:
			return lineInterval{}, false
		}
	}
	start, startErr := numberValue(items[0])
	end, endErr := numberValue(items[1])
	if startErr != nil || endErr != nil {
		return lineInterval{}, false
	}
	return makeLineInterval(start, end)
}

func splitSpaceSeparatedRanges(text string) ([]lineInterval, bool) {
	fields := strings.Fields(text)
	if len(fields) <= 1 {
		return nil, false
	}
	intervals := make([]lineInterval, 0, len(fields))
	for _, part := range fields {
		interval, ok := parseLineIntervalString(part)
		if !ok {
			return nil, false
		}
		intervals = append(intervals, interval)
	}
	return intervals, true
}

func spaceSplitIssue(schema, jsonPath, candidateID, field, text string) ArtifactIssue {
	return ArtifactIssue{
		Stage: "hunter", Schema: schema, JSONPath: jsonPath, CandidateID: candidateID,
		Code: IssueLineRangeNormalized, Field: field,
		Message:     fmt.Sprintf("space-separated line ranges split: %q", text),
		Recoverable: true,
	}
}

func parseLineIntervalValue(value interface{}) (lineInterval, bool) {
	switch typed := value.(type) {
	case string:
		return parseLineIntervalString(typed)
	case float64:
		return lineInterval{Start: int(typed), End: int(typed)}, validInterval(lineInterval{Start: int(typed), End: int(typed)})
	case map[string]interface{}:
		start, startErr := numberValue(typed["start"])
		end, endErr := numberValue(typed["end"])
		if startErr != nil || endErr != nil {
			return lineInterval{}, false
		}
		return makeLineInterval(start, end)
	default:
		return parseLineIntervalString(fmt.Sprint(value))
	}
}

func parseLineIntervalString(raw string) (lineInterval, bool) {
	raw = strings.TrimSpace(raw)
	if !artifactLineRangePattern.MatchString(raw) {
		return lineInterval{}, false
	}
	parts := strings.SplitN(raw, "-", 2)
	start, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return lineInterval{}, false
	}
	end := start
	if len(parts) == 2 {
		end, err = strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			return lineInterval{}, false
		}
	}
	return makeLineInterval(start, end)
}

func makeLineInterval(start, end int) (lineInterval, bool) {
	interval := lineInterval{Start: start, End: end}
	if interval.Start > interval.End {
		interval.Start, interval.End = interval.End, interval.Start
	}
	return interval, validInterval(interval)
}

func validInterval(interval lineInterval) bool {
	return interval.Start > 0 && interval.End > 0 && interval.Start <= interval.End &&
		interval.Start <= math.MaxInt32 && interval.End <= math.MaxInt32
}

func numberValue(value interface{}) (int, error) {
	switch typed := value.(type) {
	case float64:
		return int(typed), nil
	case json.Number:
		return strconv.Atoi(typed.String())
	case string:
		return strconv.Atoi(strings.TrimSpace(typed))
	default:
		return 0, fmt.Errorf("not a number")
	}
}

func formatLineInterval(interval lineInterval) string {
	if interval.Start == interval.End {
		return strconv.Itoa(interval.Start)
	}
	return fmt.Sprintf("%d-%d", interval.Start, interval.End)
}

func selectIntervalByTriggerSource(repoRoot, filePath, trigger string, intervals []lineInterval) (lineInterval, bool) {
	if repoRoot == "" || filePath == "" || strings.TrimSpace(trigger) == "" {
		return lineInterval{}, false
	}
	fullPath, err := safeArtifactSourcePath(repoRoot, filePath)
	if err != nil {
		return lineInterval{}, false
	}
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return lineInterval{}, false
	}
	needle := strings.TrimSpace(trigger)
	for lineNumber, line := range strings.Split(string(content), "\n") {
		if !strings.Contains(line, needle) {
			continue
		}
		for _, interval := range intervals {
			if lineNumber+1 >= interval.Start && lineNumber+1 <= interval.End {
				return interval, true
			}
		}
	}
	return lineInterval{}, false
}

func extractSourceSnippet(repoRoot, filePath string, interval lineInterval, maxLines int) string {
	fullPath, err := safeArtifactSourcePath(repoRoot, filePath)
	if err != nil {
		return ""
	}
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(content), "\n")
	if maxLines <= 0 {
		maxLines = 40
	}
	start := max(1, interval.Start)
	end := min(len(lines), interval.End)
	if end < start || end-start+1 > maxLines {
		end = min(len(lines), start+maxLines-1)
	}
	if end < start {
		return ""
	}
	return strings.TrimSpace(strings.Join(lines[start-1:end], "\n"))
}

func extractSourceLine(repoRoot, filePath string, lineNumber int) string {
	fullPath, err := safeArtifactSourcePath(repoRoot, filePath)
	if err != nil {
		return ""
	}
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(content), "\n")
	if lineNumber < 1 || lineNumber > len(lines) {
		return ""
	}
	return strings.TrimSpace(lines[lineNumber-1])
}

func safeArtifactSourcePath(repoRoot, relativePath string) (string, error) {
	if repoRoot == "" || relativePath == "" {
		return "", fmt.Errorf("empty source root or path")
	}
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return "", err
	}
	fullPath := filepath.Join(root, filepath.FromSlash(relativePath))
	rel, err := filepath.Rel(root, fullPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes workspace root")
	}
	return fullPath, nil
}

func deriveTitle(description string) string {
	description = strings.TrimSpace(description)
	if description == "" {
		return ""
	}
	if idx := strings.IndexAny(description, "。.!；;\n"); idx >= 0 {
		description = description[:idx]
	}
	runes := []rune(strings.TrimSpace(description))
	if len(runes) > 100 {
		runes = append(runes[:100], []rune("...")...)
	}
	return string(runes)
}

func stringValue(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		if typed == math.Trunc(typed) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case json.Number:
		return typed.String()
	default:
		return ""
	}
}

func isJSONString(value interface{}) bool {
	_, ok := value.(string)
	return ok
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func repairCandidateIDs(candidates []map[string]interface{}, schema string) ([]ArtifactIssue, bool) {
	issues := make([]ArtifactIssue, 0)
	seen := map[string]struct{}{}
	changed := false
	nextID := 1
	for i := range candidates {
		id := strings.TrimSpace(stringValue(candidates[i]["candidate_id"]))
		jsonPath := fmt.Sprintf("$.candidates[%d].candidate_id", i)
		if id == "" {
			id = fmt.Sprintf("H-%03d", nextID)
			candidates[i]["candidate_id"] = id
			changed = true
			issues = append(issues, ArtifactIssue{
				Stage: "hunter", Schema: schema, JSONPath: jsonPath, CandidateID: id,
				Code: IssueCandidateIDRepaired, Field: "candidate_id",
				Message: "missing candidate_id was generated", Recoverable: true,
			})
		}
		for {
			if _, exists := seen[id]; !exists {
				break
			}
			id = fmt.Sprintf("%s-%d", id, nextID)
		}
		if candidates[i]["candidate_id"] != id {
			candidates[i]["candidate_id"] = id
			changed = true
			issues = append(issues, ArtifactIssue{
				Stage: "hunter", Schema: schema, JSONPath: jsonPath, CandidateID: id,
				Code: IssueCandidateIDRepaired, Field: "candidate_id",
				Message: "duplicate candidate_id was renumbered", Recoverable: true,
			})
		}
		seen[id] = struct{}{}
		nextID++
	}
	return issues, changed
}

func sameJSONValue(left, right interface{}) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && string(leftJSON) == string(rightJSON)
}

func equalJSONValue(left, right interface{}) bool {
	return sameJSONValue(left, right)
}

func interfaceMaps(values []interface{}) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(values))
	for _, value := range values {
		if value, ok := value.(map[string]interface{}); ok {
			result = append(result, value)
		}
	}
	return result
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
