package debate

import (
	"bytes"
	"encoding/json"
)

const IssueJSONSyntaxNormalized = "JSON_SYNTAX_NORMALIZED"
const IssueRepairBaselineUnverified = "REPAIR_BASELINE_UNVERIFIED"

func repairJSONTrailingCommas(raw []byte) ([]byte, bool) {
	var out []byte
	inString, escaped := false, false

	for _, b := range raw {
		if inString {
			out = append(out, b)
			switch {
			case escaped:
				escaped = false
			case b == '\\':
				escaped = true
			case b == '"':
				inString = false
			}
			continue
		}

		switch b {
		case '"':
			inString = true
		case '}', ']':
			trimmed := bytes.TrimRight(out, " \t\r\n")
			if len(trimmed) > 0 && trimmed[len(trimmed)-1] == ',' {
				out = bytes.TrimRight(trimmed[:len(trimmed)-1], " \t\r\n")
			}
		}

		out = append(out, b)
	}

	return out, json.Valid(out)
}

func jsonSyntaxIssue(schema string) ArtifactIssue {
	return ArtifactIssue{
		Stage:       "hunter",
		Schema:      schema,
		JSONPath:    "$",
		Code:        IssueJSONSyntaxNormalized,
		Field:       "artifact",
		Message:     "trailing commas normalized",
		Recoverable: true,
	}
}
