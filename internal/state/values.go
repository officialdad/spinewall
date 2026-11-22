package state

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The helpers here reproduce Python builtins the server relies on. Parity with
// server/spinewall-server.py depends on these matching exactly.

// asObject returns value when it is a JSON object.
func asObject(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

// asList returns value when it is a JSON list.
func asList(v any) ([]any, bool) {
	l, ok := v.([]any)
	return l, ok
}

func isTrue(v any) bool {
	b, ok := v.(bool)
	return ok && b
}

// isDigits reports whether a json.Number string is an integer literal, so it
// maps to a Python int rather than a float.
func isIntLiteral(s string) bool {
	if s == "" {
		return false
	}
	return !strings.ContainsAny(s, ".eE")
}

// plainInt mirrors Python's plain_int: only a real int passes, never a bool, a
// float or a string.
func plainInt(v any, fallback int) int {
	switch t := v.(type) {
	case json.Number:
		if !isIntLiteral(t.String()) {
			return fallback
		}
		n, err := strconv.Atoi(t.String())
		if err != nil {
			return fallback
		}
		return n
	case int:
		return t
	case int64:
		return int(t)
	default:
		return fallback
	}
}

// parsePyFloat accepts the string forms Python's float() accepts, including the
// special infinity and NaN words.
func parsePyFloat(s string) (float64, bool) {
	lower := strings.ToLower(strings.TrimSpace(s))
	switch lower {
	case "inf", "+inf", "infinity", "+infinity":
		return math.Inf(1), true
	case "-inf", "-infinity":
		return math.Inf(-1), true
	case "nan", "+nan", "-nan":
		return math.NaN(), true
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

// clamp mirrors Python's clamp. A blank string, an unusable value or NaN falls
// back. Infinity clamps to the bound.
func clamp(v any, low, high, fallback float64) float64 {
	var f float64
	switch t := v.(type) {
	case nil:
		return fallback
	case bool:
		if t {
			f = 1
		} else {
			f = 0
		}
	case string:
		if strings.TrimSpace(t) == "" {
			return fallback
		}
		parsed, ok := parsePyFloat(t)
		if !ok {
			return fallback
		}
		f = parsed
	case json.Number:
		parsed, err := strconv.ParseFloat(t.String(), 64)
		if err != nil {
			return fallback
		}
		f = parsed
	case float64:
		f = t
	case int:
		f = float64(t)
	case int64:
		f = float64(t)
	default:
		return fallback
	}
	if math.IsNaN(f) {
		return fallback
	}
	return math.Max(low, math.Min(high, f))
}

// isPrintable mirrors Python's str.isprintable, which Go's unicode.IsPrint
// already matches: letters, marks, numbers, punctuation, symbols and the ASCII
// space are printable.
func isPrintable(s string) bool {
	for _, r := range s {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

// truncateRunes cuts a string to n code points, as Python's slice does.
func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}

// newUID returns a fresh 6 hex character id not in taken.
func newUID(taken map[string]bool) string {
	for {
		var b [3]byte
		if _, err := rand.Read(b[:]); err != nil {
			// crypto/rand failing is not expected. Fall back to a deterministic
			// walk so the server never spins.
			panic(fmt.Sprintf("spinewall: crypto/rand failed: %v", err))
		}
		uid := hex.EncodeToString(b[:])
		if !taken[uid] {
			return uid
		}
	}
}

// marshalJSON encodes v without HTML escaping, so replies read like Python's.
func marshalJSON(v any) ([]byte, error) {
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	out := sb.String()
	return []byte(strings.TrimSuffix(out, "\n")), nil
}
