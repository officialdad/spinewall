package cli

import (
	"net/http"
	"strings"
)

// unreserved holds the characters urllib.parse.quote leaves alone with safe="".
// Everything else is percent-encoded, so a preset name may carry a slash, a
// space or a plus sign and still reach the server as one path segment.
const upperhex = "0123456789ABCDEF"

// quote mirrors urllib.parse.quote(s, safe=""). It encodes every byte outside
// the unreserved set as %XX in upper case, matching the Python CLI byte for byte.
func quote(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9',
			c == '_', c == '.', c == '-', c == '~':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(upperhex[c>>4])
			b.WriteByte(upperhex[c&0x0f])
		}
	}
	return b.String()
}

// newGet builds a GET for base at an already-encoded path, with an optional
// raw query. The path goes into URL.Opaque so net/http
// sends the percent escapes as written: a decoded Path would re-split a %2F and
// the server reads EscapedPath, which must keep a name's slash inside one segment.
func newGet(base, path, rawQuery string) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodGet, base, nil)
	if err != nil {
		return nil, err
	}
	req.URL.Opaque = path
	req.URL.RawQuery = rawQuery
	return req, nil
}
