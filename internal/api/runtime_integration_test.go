package api

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/officialdad/spinewall/internal/runtime"
)

// TestServedRuntimeMatchesCacheManifest is the served-file half of the cache
// manifest check: the bytes an HTTP client receives hash to the manifest entry.
func TestServedRuntimeMatchesCacheManifest(t *testing.T) {
	download := []byte("var spine;\nspine = {};\n")
	downloadSum := sha256.Sum256(download)
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/raw/spine38.js" {
			http.NotFound(w, r)
			return
		}
		w.Write(download)
	}))
	defer registry.Close()

	manager := runtime.New(runtime.Options{
		Dir:     t.TempDir(),
		BaseURL: registry.URL,
		Pins: []runtime.Pin{{
			Global: "spine38", Version: "3.8", Source: runtime.RawFile,
			Path: "raw/spine38.js", SHA256: hex.EncodeToString(downloadSum[:]),
		}},
	})
	if err := manager.Ensure([]string{"spine38"}); err != nil {
		t.Fatal(err)
	}

	s, _ := newTestServer(t)
	s.theme.Runtime = manager
	rec := do(t, s, http.MethodGet, "/spine-player38.js", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	got := sha256.Sum256(rec.Body.Bytes())
	want := manager.Manifest().Files["spine-player38.js"]
	if hex.EncodeToString(got[:]) != want {
		t.Fatalf("served hash %s != manifest %s", hex.EncodeToString(got[:]), want)
	}
}
