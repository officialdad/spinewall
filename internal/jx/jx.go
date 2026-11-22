// Package jx decodes JSON the way Python's json module does in the Python server.
//
// Two behaviours matter for parity. Numbers stay as json.Number, so an integer
// literal can be told apart from a float literal the way Python's int/float are.
// Bare NaN and Infinity are accepted, because Python's json.loads accepts them
// and the page's hostile input must not change the reply.
package jx

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

var errTrailing = errors.New("trailing data after JSON value")

// Decode parses one JSON value. It retries once with NaN and Infinity rewritten
// when the strict parser rejects them.
func Decode(data []byte) (any, error) {
	if v, err := decodeOnce(data); err == nil {
		return v, nil
	}
	return decodeOnce(sanitizeNumbers(data))
}

func decodeOnce(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	// Python's json.loads rejects a second value, so the rest must be whitespace.
	var rest any
	if err := dec.Decode(&rest); err != io.EOF {
		return nil, errTrailing
	}
	return v, nil
}

// sanitizeNumbers rewrites bare NaN to null and Infinity to a huge finite number.
// The clamp helper turns a huge number into the high bound, so this keeps the
// same result as Python for these two literals. Tokens inside strings are left alone.
func sanitizeNumbers(b []byte) []byte {
	out := make([]byte, 0, len(b))
	inString := false
	escaped := false
	for i := 0; i < len(b); {
		c := b[i]
		if inString {
			out = append(out, c)
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
			i++
			continue
		}
		if c == '"' {
			inString = true
			out = append(out, c)
			i++
			continue
		}
		if hasValueToken(b, i, "NaN") {
			out = append(out, "null"...)
			i += 3
			continue
		}
		if hasValueToken(b, i, "Infinity") {
			out = append(out, "1e308"...)
			i += 8
			continue
		}
		out = append(out, c)
		i++
	}
	return out
}

// hasValueToken reports whether tok sits at i as a whole token, not as part of a
// longer identifier and not where a key could be.
func hasValueToken(b []byte, i int, tok string) bool {
	if !bytes.HasPrefix(b[i:], []byte(tok)) {
		return false
	}
	if i > 0 && isIdentByte(b[i-1]) {
		return false
	}
	end := i + len(tok)
	if end < len(b) && isIdentByte(b[end]) {
		return false
	}
	return true
}

func isIdentByte(c byte) bool {
	return c == '_' || c == '.' || (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}
