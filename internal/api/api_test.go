package api

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/officialdad/spinewall/internal/state"
)

const port = 18777

// newTestServer builds a server over a temp store with a few models.
func newTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	root := t.TempDir()
	modelsDir := filepath.Join(root, "models")
	for _, m := range []struct{ id, version string }{{"alpha", "4.0.31"}, {"beta", "4.1.56"}} {
		dir := filepath.Join(modelsDir, m.id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		skel := make([]byte, 8)
		for i := range skel {
			skel[i] = 0xff
		}
		text := []byte(m.version)
		skel = append(skel, byte(len(text)+1))
		skel = append(skel, text...)
		if err := os.WriteFile(filepath.Join(dir, m.id+".skel"), skel, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, m.id+".atlas"), []byte("page.png\nbody\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "page.png"), []byte("\x89PNG\r\n\x1a\nx"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	paths := state.Paths{
		ModelsDir:       modelsDir,
		ConfigFile:      filepath.Join(root, "config.json"),
		StateFile:       filepath.Join(root, "state", "state.json"),
		PrefsFile:       filepath.Join(root, "state", "prefs.json"),
		LayoutFile:      filepath.Join(root, "state", "layout.json"),
		ScreensFile:     filepath.Join(root, "state", "screens.json"),
		ThumbDir:        filepath.Join(root, "cache", "thumbs"),
		LegacyStateFile: filepath.Join(root, "legacy.json"),
	}
	store := state.Open(paths, nil)
	return New(store, port, Theme{Page: []byte("<html>"), LocalHost: []byte("js")}), modelsDir
}

func do(t *testing.T, s *Server, method, path string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bodyReader(body))
	req.Host = "127.0.0.1:" + itoa(port)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if method == http.MethodPost && headers["Content-Type"] == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

type byteReader struct {
	data []byte
	pos  int
}

func bodyReader(b []byte) io.Reader { return &byteReader{data: b} }

func (r *byteReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func TestBadHostRefused(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/state", nil)
	req.Host = "evil.com"
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d", rec.Code)
	}
	if got := rec.Body.String(); !strings.Contains(got, "bad host") {
		t.Fatalf("body %s", got)
	}
}

func TestForeignOriginRefusedNoCORS(t *testing.T) {
	s, _ := newTestServer(t)
	rec := do(t, s, http.MethodGet, "/state", nil, map[string]string{"Origin": "http://evil.example"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("wrong CORS header")
	}
}

func TestAllowedOriginEchoed(t *testing.T) {
	s, _ := newTestServer(t)
	for _, origin := range []string{"null", "file://", "http://127.0.0.1:" + itoa(port), "http://localhost:" + itoa(port)} {
		rec := do(t, s, http.MethodGet, "/state", nil, map[string]string{"Origin": origin})
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status %d", origin, rec.Code)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != origin {
			t.Fatalf("%s echoed %q", origin, got)
		}
		if rec.Header().Get("Vary") != "Origin" {
			t.Fatalf("%s Vary %q", origin, rec.Header().Get("Vary"))
		}
	}
}

func TestCrossSiteFetchRefused(t *testing.T) {
	s, _ := newTestServer(t)
	rec := do(t, s, http.MethodGet, "/state", nil, map[string]string{"Sec-Fetch-Site": "cross-site"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestPreflight(t *testing.T) {
	s, _ := newTestServer(t)
	rec := do(t, s, http.MethodOptions, "/view", nil, map[string]string{"Origin": "null"})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Methods") != "GET, POST, OPTIONS" {
		t.Fatalf("methods %q", rec.Header().Get("Access-Control-Allow-Methods"))
	}
	if rec.Header().Get("Access-Control-Allow-Headers") != "Content-Type" {
		t.Fatalf("headers %q", rec.Header().Get("Access-Control-Allow-Headers"))
	}
}

func TestHeadNotAllowed(t *testing.T) {
	s, _ := newTestServer(t)
	rec := do(t, s, http.MethodHead, "/state", nil, nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestContentTypeMustBeJSON(t *testing.T) {
	s, _ := newTestServer(t)
	for _, ct := range []string{"text/plain", "application/x-www-form-urlencoded", "multipart/form-data"} {
		rec := do(t, s, http.MethodPost, "/view", []byte(`{"scale":5}`), map[string]string{"Content-Type": ct})
		if rec.Code != http.StatusUnsupportedMediaType {
			t.Fatalf("%s status %d", ct, rec.Code)
		}
	}
}

func TestCharsetAndCaseAccepted(t *testing.T) {
	s, _ := newTestServer(t)
	rec := do(t, s, http.MethodPost, "/view", []byte(`{"x":0.1}`), map[string]string{"Content-Type": "application/json; charset=utf-8"})
	if rec.Code != http.StatusOK {
		t.Fatalf("charset status %d", rec.Code)
	}
	rec = do(t, s, http.MethodPost, "/view", []byte(`{"x":0.1}`), map[string]string{"Content-Type": "Application/JSON"})
	if rec.Code != http.StatusOK {
		t.Fatalf("case status %d", rec.Code)
	}
}

func TestBodyOverCapIs413(t *testing.T) {
	s, _ := newTestServer(t)
	big := append([]byte(`{"x":"`), make([]byte, 1024*1024)...)
	big = append(big, []byte(`"}`)...)
	rec := do(t, s, http.MethodPost, "/view", big, nil)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestEmptyBodyIsEmptyObject(t *testing.T) {
	s, _ := newTestServer(t)
	rec := do(t, s, http.MethodPost, "/view", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestNonObjectJSONIs400(t *testing.T) {
	s, _ := newTestServer(t)
	for _, payload := range []string{"[1]", `"str"`, "5", "null", "true"} {
		rec := do(t, s, http.MethodPost, "/view", []byte(payload), nil)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s status %d", payload, rec.Code)
		}
	}
}

func TestNaNDoesNotReachState(t *testing.T) {
	s, _ := newTestServer(t)
	rec := do(t, s, http.MethodPost, "/view", []byte(`{"scale": NaN, "x": Infinity}`), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	stateRec := do(t, s, http.MethodGet, "/state", nil, nil)
	var reply struct {
		Scale float64 `json:"scale"`
		X     float64 `json:"x"`
	}
	if err := json.Unmarshal(stateRec.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Scale != 1 || reply.X != 2 {
		t.Fatalf("scale %v x %v", reply.Scale, reply.X)
	}
}

func TestModelFileServedAndHidden(t *testing.T) {
	s, _ := newTestServer(t)
	if rec := do(t, s, http.MethodGet, "/alpha/alpha.skel", nil, nil); rec.Code != http.StatusOK {
		t.Fatalf("skel status %d", rec.Code)
	}
	if rec := do(t, s, http.MethodGet, "/alpha/page.png", nil, nil); rec.Code != http.StatusOK {
		t.Fatalf("png status %d", rec.Code)
	}
	if rec := do(t, s, http.MethodGet, "/nope/x.png", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown status %d", rec.Code)
	}
}

func TestThemeServed(t *testing.T) {
	s, _ := newTestServer(t)
	page := do(t, s, http.MethodGet, "/index.html", nil, nil)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "<html>") {
		t.Fatalf("page status %d body %q", page.Code, page.Body.String())
	}
	js := do(t, s, http.MethodGet, "/local-host.js", nil, nil)
	if js.Code != http.StatusOK || js.Body.String() != "js" {
		t.Fatalf("js status %d body %q", js.Code, js.Body.String())
	}
	if rec := do(t, s, http.MethodGet, "/", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("root status %d", rec.Code)
	}
}

func TestNoDirectoryListing(t *testing.T) {
	s, _ := newTestServer(t)
	for _, path := range []string{"/", "/alpha", "/alpha/"} {
		rec := do(t, s, http.MethodGet, path, nil, nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s status %d", path, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "alpha.skel") {
			t.Fatalf("%s leaked a listing", path)
		}
	}
}

func TestOtherExtensionsNotServed(t *testing.T) {
	s, modelsDir := newTestServer(t)
	if err := os.WriteFile(filepath.Join(modelsDir, "alpha", "secret.sh"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modelsDir, "alpha", "model.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"secret.sh", "model.json"} {
		rec := do(t, s, http.MethodGet, "/alpha/"+name, nil, nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s status %d", name, rec.Code)
		}
	}
}

func TestTraversalNeverLeaks(t *testing.T) {
	s, _ := newTestServer(t)
	for _, path := range []string{
		"/alpha/..%2f..%2foutside.json",
		"/..%2foutside.json",
		"/alpha/%2e%2e/%2e%2e/outside.json",
		"/%2e%2e/outside.json",
	} {
		rec := do(t, s, http.MethodGet, path, nil, nil)
		if rec.Code == http.StatusOK && strings.Contains(rec.Body.String(), "secret") {
			t.Fatalf("%s leaked", path)
		}
	}
}

func TestNulByteIs404(t *testing.T) {
	s, _ := newTestServer(t)
	rec := do(t, s, http.MethodGet, "/alpha/%00.png", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d", rec.Code)
	}
	// The server must stay up.
	if rec := do(t, s, http.MethodGet, "/state", nil, nil); rec.Code != http.StatusOK {
		t.Fatalf("server down: %d", rec.Code)
	}
}

func TestThumbUploadAndFetch(t *testing.T) {
	s, _ := newTestServer(t)
	png := []byte("\x89PNG\r\n\x1a\nhello")
	body, _ := json.Marshal(map[string]string{"png": base64.StdEncoding.EncodeToString(png)})
	rec := do(t, s, http.MethodPost, "/thumb/alpha", body, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload status %d: %s", rec.Code, rec.Body.String())
	}
	fetch := do(t, s, http.MethodGet, "/thumb/alpha.png", nil, nil)
	if fetch.Code != http.StatusOK || fetch.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("fetch status %d ct %q", fetch.Code, fetch.Header().Get("Content-Type"))
	}
	if string(fetch.Body.Bytes()) != string(png) {
		t.Fatalf("bytes differ")
	}
}

func TestThumbRejections(t *testing.T) {
	s, _ := newTestServer(t)
	notPNG, _ := json.Marshal(map[string]string{"png": base64.StdEncoding.EncodeToString([]byte("GIF89a"))})
	if rec := do(t, s, http.MethodPost, "/thumb/alpha", notPNG, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("non-png status %d", rec.Code)
	}
	if rec := do(t, s, http.MethodPost, "/thumb/alpha", []byte(`{"png":"***not base64***"}`), nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad base64 status %d", rec.Code)
	}
	if rec := do(t, s, http.MethodPost, "/thumb/nope", notPNG, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown status %d", rec.Code)
	}
}
