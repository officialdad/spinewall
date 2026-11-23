package api

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
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

func legacyManager(t *testing.T, fail *atomic.Bool, hits *atomic.Int32) *runtime.Manager {
	t.Helper()
	download := []byte("var spine;\nspine = {};\n")
	sum := sha256.Sum256(download)
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if fail.Load() || r.URL.Path != "/raw/spine38.js" {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		w.Write(download)
	}))
	t.Cleanup(registry.Close)
	return runtime.New(runtime.Options{
		Dir:     t.TempDir(),
		BaseURL: registry.URL,
		Pins: []runtime.Pin{{
			Global: "spine38", Version: "3.8", Source: runtime.RawFile,
			Path: "raw/spine38.js", SHA256: hex.EncodeToString(sum[:]),
		}},
	})
}

// A model added after start can need a player the start fetch skipped. The
// page's request for it fetches it, and a failed fetch is retried on the next
// request.
func TestMissingPlayerIsFetchedOnRequestAndRetried(t *testing.T) {
	var fail atomic.Bool
	var hits atomic.Int32
	fail.Store(true)
	s, _ := newTestServer(t)
	s.theme.Runtime = legacyManager(t, &fail, &hits)

	if rec := do(t, s, http.MethodGet, "/spine-player38.js", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("failed fetch status %d", rec.Code)
	}
	fail.Store(false)
	rec := do(t, s, http.MethodGet, "/spine-player38.js", nil, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "var spine38 = (function () {") {
		t.Fatalf("retry status %d body %q", rec.Code, rec.Body.String())
	}
	before := hits.Load()
	do(t, s, http.MethodGet, "/spine-player38.js", nil, nil)
	if hits.Load() != before {
		t.Fatal("a cached player fetched again")
	}
}

func TestConcurrentMissesShareOneFetch(t *testing.T) {
	var fail atomic.Bool
	var hits atomic.Int32
	s, _ := newTestServer(t)
	s.theme.Runtime = legacyManager(t, &fail, &hits)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if rec := do(t, s, http.MethodGet, "/spine-player38.js", nil, nil); rec.Code != http.StatusOK {
				t.Errorf("status %d", rec.Code)
			}
		}()
	}
	wg.Wait()
	if hits.Load() != 1 {
		t.Fatalf("%d downloads, want 1", hits.Load())
	}
}

// The page's API base is the serving binary only when the binary serves it.
func TestServedPageUsesItsOwnPort(t *testing.T) {
	const page = `const API = "http://127.0.0.1:8777";`
	s, _ := newTestServer(t)
	s2 := New(s.store, 18999, Theme{Page: []byte(page)})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "127.0.0.1:18999"
	rec := httptest.NewRecorder()
	s2.ServeHTTP(rec, req)
	if body := rec.Body.String(); !strings.Contains(body, "http://127.0.0.1:18999") {
		t.Fatalf("custom port page %q", body)
	}
	s3 := New(s.store, 8777, Theme{Page: []byte(page)})
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "127.0.0.1:8777"
	rec = httptest.NewRecorder()
	s3.ServeHTTP(rec, req)
	if body := rec.Body.String(); body != page {
		t.Fatalf("default port page %q", body)
	}
}
