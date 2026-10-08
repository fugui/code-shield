package assessment

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

func CleanJSON(raw []byte) []byte {
	content := strings.TrimSpace(string(raw))
	if strings.HasPrefix(content, "```") {
		if index := strings.Index(content, "\n"); index != -1 {
			content = content[index+1:]
		}
		content = strings.TrimSuffix(content, "```")
		content = strings.TrimSpace(content)
	}
	if !strings.HasPrefix(content, "{") {
		if start := strings.Index(content, "{"); start != -1 {
			if end := strings.LastIndex(content, "}"); end > start {
				content = content[start : end+1]
			}
		}
	}
	if !json.Valid([]byte(content)) {
		content = string(RepairJSONEscapes([]byte(content)))
	}
	return []byte(content)
}

// RepairJSONEscapes fixes common AI mistakes inside JSON string values:
// unescaped ASCII double quotes (") that terminate the string prematurely.
// It walks the raw JSON byte-by-byte, tracking whether we are inside a string,
// and replaces bare `"` that appear between a value's opening and closing
// structural quotes with `\"`.
func RepairJSONEscapes(raw []byte) []byte {
	var out strings.Builder
	out.Grow(len(raw) + 16)
	inString := false
	i := 0
	for i < len(raw) {
		c := raw[i]
		if c == '"' {
			if inString {
				// Check if the next non-space char suggests this is a
				// structural closing quote (followed by , } ] : etc.)
				// or an embedded quote that should be escaped.
				j := i + 1
				for j < len(raw) && (raw[j] == ' ' || raw[j] == '\t' || raw[j] == '\n' || raw[j] == '\r') {
					j++
				}
				if j < len(raw) {
					switch raw[j] {
					case ',', '}', ']', ':', '"':
						// This is a structural closing quote or part of a key.
						out.WriteByte('"')
						inString = false
					default:
						// Likely an embedded quote inside a string value.
						out.WriteString(`\"`)
					}
				} else {
					// End of input; treat as closing.
					out.WriteByte('"')
					inString = false
				}
			} else {
				out.WriteByte('"')
				inString = true
			}
			i++
		} else if c == '\\' && inString {
			// Copy escape sequences as-is (\" \\ \/ \n \t etc.)
			if i+1 < len(raw) {
				out.WriteByte(raw[i])
				out.WriteByte(raw[i+1])
				i += 2
			} else {
				out.WriteByte(raw[i])
				i++
			}
		} else {
			_, size := utf8.DecodeRune(raw[i:])
			out.Write(raw[i : i+size])
			i += size
		}
	}
	return []byte(out.String())
}
