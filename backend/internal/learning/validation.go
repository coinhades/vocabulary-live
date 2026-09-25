package learning

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

func Hash(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func Decode(raw []byte, v any) bool {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(v) == nil && d.Decode(new(any)) == io.EOF
}
func Plain(s string, max int, empty bool) bool {
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > max || (!empty && strings.TrimSpace(s) == "") {
		return false
	}
	if strings.ContainsAny(s, "<>") || strings.Contains(strings.ToLower(s), "javascript:") || strings.Contains(s, "://") {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) && r != '\n' && r != '\t' || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}

var targetPattern = regexp.MustCompile(`^[\pL\pM][\pL\pM '\-]*$`)

func ValidTarget(s string) bool {
	return Plain(s, 80, false) && len(strings.Fields(s)) <= 10 && targetPattern.MatchString(s)
}
func stringSchema(max int) map[string]any { return map[string]any{"type": "string", "maxLength": max} }
func objectSchema(properties map[string]any) map[string]any {
	required := make([]string, 0, len(properties))
	for k := range properties {
		required = append(required, k)
	}
	// json.Marshal sorts map keys, and the required list is sorted for stable cache identity.
	slices.Sort(required)
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}
