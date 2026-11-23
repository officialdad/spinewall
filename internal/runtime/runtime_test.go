package runtime

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// registry is an httptest server that answers the paths the test pins name and
// counts hits, so a refetch can be told apart from a cache hit.
type registry struct {
	server *httptest.Server
	mu     sync.Mutex
	hits   map[string]int
}

func newRegistry(t *testing.T, blobs map[string][]byte) *registry {
	t.Helper()
	r := &registry{hits: map[string]int{}}
	r.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		r.hits[req.URL.Path]++
		r.mu.Unlock()
		data, ok := blobs[req.URL.Path]
		if !ok {
			http.NotFound(w, req)
			return
		}
		w.Write(data)
	}))
	t.Cleanup(r.server.Close)
	return r
}

func (r *registry) hitCount(path string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.hits[path]
}

// makeTarball builds the npm package shape the fetcher expects.
func makeTarball(t *testing.T, js, css []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, entry := range []struct {
		name string
		data []byte
	}{
		{"package/dist/iife/spine-player.js", js},
		{"package/dist/spine-player.css", css},
	} {
		if err := tw.WriteHeader(&tar.Header{Name: entry.name, Mode: 0o644, Size: int64(len(entry.data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(entry.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

const (
	tar41Path = "/npm/spine41.tgz"
	tar40Path = "/npm/spine40.tgz"
	tar42Path = "/npm/spine42.tgz"
	raw38Path = "/raw/spine38.js"
	cssBody   = ".spine-player{color:black}\n"
	js41Body  = "var spine = (() => {\n  return {version: \"4.1\"};\n})();\nconsole.log(spine);\n"
	js40Body  = "window.spine = {};\n"
	js38Body  = "var spine;\nspine = {version: \"3.8\"};\n"
)

// fixture returns a manager wired to a test registry with pins whose hashes are
// computed from the served bytes, so a correct fetch is provable.
func fixture(t *testing.T) (*Manager, *registry) {
	t.Helper()
	css := []byte(cssBody)
	js41 := []byte(js41Body)
	js40 := []byte(js40Body)
	js38 := []byte(js38Body)
	tar41 := makeTarball(t, js41, css)
	tar40 := makeTarball(t, js40, css)
	tar42 := makeTarball(t, js41, css)
	reg := newRegistry(t, map[string][]byte{
		tar41Path: tar41,
		tar40Path: tar40,
		tar42Path: tar42,
		raw38Path: js38,
	})
	pins := []Pin{
		{Global: "spine40", Version: "4.0.31", Source: Tarball, Path: "npm/spine40.tgz", SHA256: digest(tar40)},
		{Global: "spine41", Version: "4.1.56", Source: Tarball, Path: "npm/spine41.tgz", SHA256: digest(tar41)},
		{Global: "spine42", Version: "4.2.120", Source: Tarball, Path: "npm/spine42.tgz", SHA256: digest(tar42)},
		{Global: "spine38", Version: "3.8", Source: RawFile, Path: "raw/spine38.js", SHA256: digest(js38)},
	}
	m := New(Options{Dir: t.TempDir(), BaseURL: reg.server.URL, Pins: pins})
	return m, reg
}

func TestFetch4xRenamesTheGlobalAndCopiesCSS(t *testing.T) {
	m, _ := fixture(t)
	if err := m.Ensure([]string{"spine41"}); err != nil {
		t.Fatal(err)
	}
	js, ok := m.File("spine-player41.js")
	if !ok {
		t.Fatal("spine-player41.js was not cached")
	}
	if !bytes.HasPrefix(js, []byte("var spine41 = ")) {
		t.Fatalf("player does not start with the renamed global: %q", js[:20])
	}
	if bytes.Contains(js, []byte("var spine = ")) {
		t.Fatal("the original global name survived the rename")
	}
	if !bytes.Contains(js, []byte("console.log(spine);")) {
		t.Fatal("the rename touched more than the declaration line")
	}
	css, ok := m.File(cssName)
	if !ok || string(css) != cssBody {
		t.Fatalf("stylesheet %q, ok %v", css, ok)
	}
	man := m.Manifest()
	if man.Files["spine-player41.js"] != digest(js) {
		t.Fatal("manifest hash does not match the cached player")
	}
	if man.Files[cssName] != digest(css) {
		t.Fatal("manifest hash does not match the cached stylesheet")
	}
}

func TestFetch3xWrapsInAFunctionScope(t *testing.T) {
	m, _ := fixture(t)
	if err := m.Ensure([]string{"spine38"}); err != nil {
		t.Fatal(err)
	}
	js, ok := m.File("spine-player38.js")
	if !ok {
		t.Fatal("spine-player38.js was not cached")
	}
	if !bytes.HasPrefix(js, []byte("var spine38 = (function () {\n")) {
		t.Fatalf("legacy player is not wrapped: %q", js[:40])
	}
	if !bytes.HasSuffix(js, []byte("\nreturn spine;\n})();\n")) {
		t.Fatalf("legacy wrapper has no return: %q", js[len(js)-30:])
	}
	if !bytes.Contains(js, []byte("var spine;")) {
		t.Fatal("the original legacy body is missing")
	}
	// A 3.x-only cache still needs the stylesheet the page links.
	if css, ok := m.File(cssName); !ok || len(css) == 0 {
		t.Fatal("a 3.x-only fetch did not provide the stylesheet")
	}
}

func TestMissingStylesheetIsRestoredWhenPlayersAreCached(t *testing.T) {
	m, reg := fixture(t)
	if err := m.Ensure([]string{"spine41"}); err != nil {
		t.Fatal(err)
	}
	cssPath := filepath.Join(m.Dir(), cssName)
	if err := os.Remove(cssPath); err != nil {
		t.Fatal(err)
	}
	before := reg.hitCount(tar42Path)
	if err := m.Ensure([]string{"spine41"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cssPath); err != nil {
		t.Fatalf("the stylesheet was not restored: %v", err)
	}
	if reg.hitCount(tar42Path) == before {
		t.Fatal("the stylesheet was restored without fetching its source")
	}
}

func TestWrongHashAbortsWithoutWriting(t *testing.T) {
	js := []byte(js41Body)
	tar := makeTarball(t, js, []byte(cssBody))
	reg := newRegistry(t, map[string][]byte{tar41Path: tar})
	m := New(Options{
		Dir:     t.TempDir(),
		BaseURL: reg.server.URL,
		Pins: []Pin{{
			Global: "spine41", Version: "4.1.56", Source: Tarball, Path: "npm/spine41.tgz",
			SHA256: "0000000000000000000000000000000000000000000000000000000000000000",
		}},
	})
	err := m.Ensure([]string{"spine41"})
	if err == nil {
		t.Fatal("a wrong hash was accepted")
	}
	if _, ok := m.File("spine-player41.js"); ok {
		t.Fatal("a wrong-hash download reached the cache")
	}
	if _, statErr := os.Stat(filepath.Join(m.Dir(), "spine-player41.js")); !os.IsNotExist(statErr) {
		t.Fatalf("a wrong-hash download was written to disk: %v", statErr)
	}
}

func TestMissingSpineLineAborts(t *testing.T) {
	js := []byte(js40Body) // no `var spine = ` line to rename
	tar := makeTarball(t, js, []byte(cssBody))
	reg := newRegistry(t, map[string][]byte{tar40Path: tar})
	m := New(Options{
		Dir:     t.TempDir(),
		BaseURL: reg.server.URL,
		Pins: []Pin{{
			Global: "spine40", Version: "4.0.31", Source: Tarball, Path: "npm/spine40.tgz",
			SHA256: digest(tar),
		}},
	})
	err := m.Ensure([]string{"spine40"})
	if err == nil {
		t.Fatal("a player without the rename target was accepted")
	}
	if _, ok := m.File("spine-player40.js"); ok {
		t.Fatal("an unrenamable player reached the cache")
	}
}

func TestCorruptCacheByteIsRefetched(t *testing.T) {
	m, reg := fixture(t)
	if err := m.Ensure([]string{"spine41"}); err != nil {
		t.Fatal(err)
	}
	cached := filepath.Join(m.Dir(), "spine-player41.js")
	good, err := os.ReadFile(cached)
	if err != nil {
		t.Fatal(err)
	}
	corrupt := append([]byte(nil), good...)
	corrupt[0] ^= 0xff
	if err := os.WriteFile(cached, corrupt, 0o644); err != nil {
		t.Fatal(err)
	}

	before := reg.hitCount(tar41Path)
	if err := m.Ensure([]string{"spine41"}); err != nil {
		t.Fatal(err)
	}
	if reg.hitCount(tar41Path) == before {
		t.Fatal("a corrupt cached file was served without a refetch")
	}
	repaired, err := os.ReadFile(cached)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(repaired, good) {
		t.Fatal("the refetched file does not match the original")
	}
	if m.Manifest().Files["spine-player41.js"] != digest(good) {
		t.Fatal("the manifest does not match the repaired file")
	}
}

func TestVerifyReportsCorruption(t *testing.T) {
	m, _ := fixture(t)
	if err := m.Ensure([]string{"spine38"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Verify(); err != nil {
		t.Fatalf("a fresh cache did not verify: %v", err)
	}
	path := filepath.Join(m.Dir(), "spine-player38.js")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] ^= 0xff
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.Verify(); err == nil {
		t.Fatal("a corrupt cache verified")
	}
}

func TestOfflineUsesTheSeedDirectory(t *testing.T) {
	m, _ := fixture(t)
	if err := m.Ensure([]string{"spine41"}); err != nil {
		t.Fatal(err)
	}
	seeded := New(Options{Dir: m.Dir(), Offline: true, Pins: m.pins})
	if err := seeded.Ensure([]string{"spine41"}); err != nil {
		t.Fatalf("offline seed failed: %v", err)
	}
	if _, ok := seeded.File("spine-player41.js"); !ok {
		t.Fatal("the seeded player was not loaded")
	}
}

func TestOfflineRefusesAnUncachedRuntime(t *testing.T) {
	m := New(Options{Dir: t.TempDir(), Offline: true})
	if err := m.Ensure([]string{"spine41"}); err == nil {
		t.Fatal("offline mode fetched or accepted a missing runtime")
	}
}

func TestManifestPinsMatchTheTable(t *testing.T) {
	m, _ := fixture(t)
	if err := m.Ensure([]string{"spine41", "spine38"}); err != nil {
		t.Fatal(err)
	}
	man := m.Manifest()
	for _, global := range []string{"spine41", "spine38"} {
		pin, _ := m.pinFor(global)
		if man.Pins[global] != pin.SHA256 {
			t.Fatalf("%s manifest pin %q != %q", global, man.Pins[global], pin.SHA256)
		}
	}
}

func TestProductionPinsAreWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, pin := range Pins {
		if pin.Global == "" || pin.Version == "" || pin.Path == "" {
			t.Fatalf("incomplete pin %+v", pin)
		}
		if seen[pin.Global] {
			t.Fatalf("duplicate global %s", pin.Global)
		}
		seen[pin.Global] = true
		if len(pin.SHA256) != 64 {
			t.Fatalf("%s pin hash is not 64 hex digits", pin.Global)
		}
		if _, err := hex.DecodeString(pin.SHA256); err != nil {
			t.Fatalf("%s pin hash is not hex: %v", pin.Global, err)
		}
	}
	for _, global := range []string{"spine37", "spine38", "spine40", "spine41", "spine42"} {
		if _, ok := pinFor(global); !ok {
			t.Fatalf("no pin for %s", global)
		}
	}
}

// TestRealNetworkFetch is the opt-in replacement for the old install.sh network
// test. It downloads every production pin and checks each output against the
// manifest, then proves the cache serves offline with no registry.
func TestRealNetworkFetch(t *testing.T) {
	if os.Getenv("SPINEWALL_TEST_NETWORK") != "1" {
		t.Skip("set SPINEWALL_TEST_NETWORK=1 to download the real Spine runtimes")
	}
	dir := t.TempDir()
	m := New(Options{Dir: dir, Logf: func(line string) { t.Log(line) }})
	if err := m.FetchAll(); err != nil {
		t.Fatal(err)
	}
	man := m.Manifest()
	for _, pin := range Pins {
		name := FileName(pin.Global)
		data, ok := m.File(name)
		if !ok {
			t.Fatalf("%s was not cached", name)
		}
		if len(data) < 10000 {
			t.Fatalf("%s is suspiciously small: %d bytes", name, len(data))
		}
		if man.Files[name] != digest(data) {
			t.Fatalf("%s manifest hash does not match its bytes", name)
		}
		if man.Pins[pin.Global] != pin.SHA256 {
			t.Fatalf("%s manifest pin does not match the checked-in pin", pin.Global)
		}
	}
	if css, ok := m.File(cssName); !ok || len(css) == 0 {
		t.Fatal("the stylesheet was not cached")
	}

	offline := New(Options{Dir: dir, Offline: true})
	if err := offline.Ensure(Globals()); err != nil {
		t.Fatalf("the real cache did not serve offline: %v", err)
	}
}
