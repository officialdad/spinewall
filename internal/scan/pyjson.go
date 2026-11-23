package scan

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/officialdad/spinewall/internal/jx"
)

// decode parses JSON with Python-compatible number and NaN handling.
func decode(data []byte) (any, error) {
	return jx.Decode(data)
}

// truthy mirrors Python's truthiness for the JSON value types a scan sees.
func truthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return t != ""
	case json.Number:
		f, err := strconv.ParseFloat(t.String(), 64)
		return err != nil || f != 0
	case []any:
		return len(t) > 0
	case map[string]any:
		return len(t) > 0
	default:
		return true
	}
}

// pyStr renders a decoded JSON value the way Python's str() renders it.
// Only the version field uses this, and a non-string version never maps to a
// runtime, so a close match is enough.
func pyStr(v any) string {
	switch t := v.(type) {
	case nil:
		return "None"
	case string:
		return t
	case bool:
		if t {
			return "True"
		}
		return "False"
	case json.Number:
		return t.String()
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return pyReprAny(v)
	}
}

// pyReprAny renders a decoded JSON value the way Python's repr() renders it.
func pyReprAny(v any) string {
	switch t := v.(type) {
	case nil:
		return "None"
	case string:
		return "'" + t + "'"
	case bool:
		if t {
			return "True"
		}
		return "False"
	case json.Number:
		return t.String()
	case []any:
		parts := make([]string, len(t))
		for i := range t {
			parts[i] = pyReprAny(t[i])
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = pyReprAny(k) + ": " + pyReprAny(t[k])
		}
		return "{" + strings.Join(parts, ", ") + "}"
	default:
		return ""
	}
}
