package runtime

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// maxDownloadBytes caps a single download, so a bad response cannot fill memory.
	maxDownloadBytes = 16 << 20
	// maxExtractBytes caps one un-tarred file.
	maxExtractBytes = 32 << 20
	manifestName    = "manifest.json"
	cssName         = "spine-player.css"
)

// Manifest records the transformed bytes this cache holds. Files maps an output
// file name to its sha256. Pins maps a global to the download hash it came from,
// so a changed pin forces a refetch even when the cached bytes still hash well.
// CSSFrom names the global whose tarball supplied spine-player.css.
type Manifest struct {
	Files   map[string]string `json:"files"`
	Pins    map[string]string `json:"pins"`
	CSSFrom string            `json:"cssFrom"`
}

// Options configure a Manager. Zero values are useful defaults.
type Options struct {
	// Dir is the runtime cache directory. Empty means DefaultDir.
	Dir string
	// Offline refuses every network fetch and uses only the cache.
	Offline bool
	// BaseURL replaces both download hosts. It exists for tests; empty uses the
	// production npm and GitHub hosts.
	BaseURL string
	// Client is the HTTP client. Nil uses one with a 30 second timeout.
	Client *http.Client
	// Logf receives human-readable progress. Nil discards it.
	Logf func(string)
	// Pins replaces the checked-in table. It exists for tests; nil uses Pins.
	Pins []Pin
}

// Manager owns one runtime cache. It is safe for concurrent use.
type Manager struct {
	dir     string
	offline bool
	baseURL string
	client  *http.Client
	logf    func(string)
	pins    []Pin

	// fetchMu serialises fetches so concurrent misses share one download. mu
	// guards the in-memory state only and is never held across the network.
	fetchMu  sync.Mutex
	mu       sync.Mutex
	files    map[string][]byte
	manifest Manifest
	loaded   bool
}

// New builds a Manager from options.
func New(opts Options) *Manager {
	dir := opts.Dir
	if dir == "" {
		dir = DefaultDir()
	}
	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	logf := opts.Logf
	if logf == nil {
		logf = func(string) {}
	}
	pins := opts.Pins
	if pins == nil {
		pins = Pins
	}
	return &Manager{
		dir:     dir,
		offline: opts.Offline,
		baseURL: strings.TrimRight(opts.BaseURL, "/"),
		client:  client,
		logf:    logf,
		pins:    pins,
		files:   map[string][]byte{},
		manifest: Manifest{
			Files: map[string]string{},
			Pins:  map[string]string{},
		},
	}
}

// DefaultDir is the per-user runtime cache: $XDG_CACHE_HOME/spinewall/runtime,
// ~/.cache/spinewall/runtime when XDG_CACHE_HOME is unset.
func DefaultDir() string {
	cache, err := os.UserCacheDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "spinewall", "runtime")
	}
	return filepath.Join(cache, "spinewall", "runtime")
}

// Dir returns the cache directory.
func (m *Manager) Dir() string { return m.dir }

// FileName is the output file a global is served as, such as spine-player41.js.
func FileName(global string) string {
	return "spine-player" + strings.TrimPrefix(global, "spine") + ".js"
}

// IsRuntimeFile reports whether name is one of the files this cache serves.
func IsRuntimeFile(name string) bool {
	if name == cssName {
		return true
	}
	for _, pin := range Pins {
		if FileName(pin.Global) == name {
			return true
		}
	}
	return false
}

// Globals returns every runtime global the binary knows, in pin order.
func Globals() []string {
	out := make([]string, len(Pins))
	for i, pin := range Pins {
		out[i] = pin.Global
	}
	return out
}

// ServedNames returns every file the cache serves: each pinned player and the
// shared stylesheet, in pin order. The theme install links these into the
// theme folder so the file:// page finds them.
func ServedNames() []string {
	out := make([]string, 0, len(Pins)+1)
	for _, pin := range Pins {
		out = append(out, FileName(pin.Global))
	}
	return append(out, cssName)
}

// File returns the validated bytes of a served runtime file. A file that is not
// in the cache and validated at the last Ensure reports false.
func (m *Manager) File(name string) ([]byte, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.files[name]
	return data, ok
}

// Manifest returns a copy of the current cache manifest.
func (m *Manager) Manifest() Manifest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return copyManifest(m.manifest)
}

func copyManifest(in Manifest) Manifest {
	out := Manifest{
		Files:   map[string]string{},
		Pins:    map[string]string{},
		CSSFrom: in.CSSFrom,
	}
	for k, v := range in.Files {
		out.Files[k] = v
	}
	for k, v := range in.Pins {
		out.Pins[k] = v
	}
	return out
}

// Ensure makes the named globals available, plus the stylesheet. It validates
// cached bytes against the manifest, refetches a missing or corrupt file, and
// records the result. In offline mode a missing or corrupt file is an error.
func (m *Manager) Ensure(globals []string) error {
	m.fetchMu.Lock()
	defer m.fetchMu.Unlock()

	want := map[string]bool{}
	for _, global := range globals {
		if _, ok := m.pinFor(global); ok {
			want[global] = true
		}
	}

	m.mu.Lock()
	m.loadLocked()
	m.mu.Unlock()

	var problems []string
	for _, pin := range m.pins {
		if !want[pin.Global] || m.valid(pin) {
			continue
		}
		if m.offline {
			problems = append(problems, fmt.Sprintf("%s is not cached and the runtime is offline", FileName(pin.Global)))
			continue
		}
		if err := m.fetch(pin); err != nil {
			problems = append(problems, err.Error())
		}
	}

	// A fetched 4.x tarball writes the stylesheet. When every wanted player was
	// already cached, or only 3.x players are in use, fetch the cssPin tarball so
	// a deleted or absent stylesheet still comes back.
	if !m.validCSS() {
		if pin, ok := m.pinFor(cssPin); ok {
			if m.offline {
				problems = append(problems, cssName+" is not cached and the runtime is offline")
			} else if err := m.fetch(pin); err != nil {
				problems = append(problems, err.Error())
			}
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("runtime cache %s incomplete: %s", m.dir, strings.Join(problems, "; "))
	}
	return nil
}

// Serve returns a runtime file, fetching it first when the cache lacks it or
// holds a corrupt copy. A failed fetch is retried on the next miss.
func (m *Manager) Serve(name string) ([]byte, bool) {
	if data, ok := m.File(name); ok {
		return data, true
	}
	var globals []string
	for _, pin := range m.pins {
		if FileName(pin.Global) == name {
			globals = append(globals, pin.Global)
		}
	}
	if len(globals) == 0 && name != cssName {
		return nil, false
	}
	if err := m.Ensure(globals); err != nil {
		m.logf("spinewall: " + err.Error())
	}
	return m.File(name)
}

// FetchAll downloads every pinned runtime and records the cache.
func (m *Manager) FetchAll() error {
	return m.Ensure(m.globalsLocked())
}

// Verify checks every file the manifest names against its recorded hash. It
// does not use the network. A missing or empty cache is an error.
func (m *Manager) Verify() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loadLocked()
	if len(m.manifest.Files) == 0 {
		return fmt.Errorf("runtime cache %s is empty; run 'spinewall runtime fetch'", m.dir)
	}
	var problems []string
	for name, want := range m.manifest.Files {
		data, err := os.ReadFile(filepath.Join(m.dir, name))
		if err != nil {
			problems = append(problems, name+": missing")
			continue
		}
		if got := hashHex(data); got != want {
			problems = append(problems, fmt.Sprintf("%s: hash %s does not match manifest %s", name, got, want))
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("runtime cache %s is corrupt: %s", m.dir, strings.Join(problems, "; "))
	}
	for name := range m.manifest.Files {
		if data, err := os.ReadFile(filepath.Join(m.dir, name)); err == nil {
			m.files[name] = data
		}
	}
	return nil
}

func (m *Manager) globalsLocked() []string {
	out := make([]string, len(m.pins))
	for i, pin := range m.pins {
		out[i] = pin.Global
	}
	return out
}

func (m *Manager) pinFor(global string) (Pin, bool) {
	for _, pin := range m.pins {
		if pin.Global == global {
			return pin, true
		}
	}
	return Pin{}, false
}

// loadLocked reads the manifest once. A missing or malformed manifest is an
// empty cache, so the next Ensure refetches.
func (m *Manager) loadLocked() {
	if m.loaded {
		return
	}
	m.loaded = true
	data, err := os.ReadFile(filepath.Join(m.dir, manifestName))
	if err != nil {
		return
	}
	var man Manifest
	if err := json.Unmarshal(data, &man); err != nil {
		m.logf(fmt.Sprintf("spinewall: ignoring unreadable runtime manifest: %v", err))
		return
	}
	if man.Files == nil {
		man.Files = map[string]string{}
	}
	if man.Pins == nil {
		man.Pins = map[string]string{}
	}
	m.manifest = man
}

// validLocked reports whether a pin's output is cached, matches the manifest,
// and still comes from the current pin. It loads the bytes for serving.
func (m *Manager) validLocked(pin Pin) bool {
	name := FileName(pin.Global)
	want, ok := m.manifest.Files[name]
	if !ok || m.manifest.Pins[pin.Global] != pin.SHA256 {
		return false
	}
	data, err := os.ReadFile(filepath.Join(m.dir, name))
	if err != nil || hashHex(data) != want {
		return false
	}
	m.files[name] = data
	return true
}

func (m *Manager) valid(pin Pin) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.validLocked(pin)
}

func (m *Manager) validCSS() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.validCSSLocked()
}

func (m *Manager) validCSSLocked() bool {
	want, ok := m.manifest.Files[cssName]
	if !ok || m.manifest.CSSFrom == "" {
		return false
	}
	pin, ok := m.pinFor(m.manifest.CSSFrom)
	if !ok || m.manifest.Pins[m.manifest.CSSFrom] != pin.SHA256 {
		return false
	}
	data, err := os.ReadFile(filepath.Join(m.dir, cssName))
	if err != nil || hashHex(data) != want {
		return false
	}
	m.files[cssName] = data
	return true
}

// fetch downloads one pin, verifies it, transforms it, writes the output, and
// updates the manifest. The caller holds fetchMu. The network and the file
// writes run outside mu, so cached files keep serving during a slow download.
func (m *Manager) fetch(pin Pin) error {
	m.logf(fmt.Sprintf("spinewall: fetching Spine %s (%s)", pin.Version, pin.Global))
	data, err := m.download(pin)
	if err != nil {
		return err
	}
	js, css, err := transform(pin, data)
	if err != nil {
		return err
	}

	name := FileName(pin.Global)
	if err := writeAtomic(filepath.Join(m.dir, name), js); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[name] = js
	m.manifest.Files[name] = hashHex(js)
	m.manifest.Pins[pin.Global] = pin.SHA256

	// The stylesheet comes from whichever 4.x tarball is fetched first. Once a
	// valid one is present, a later fetch leaves it alone so the hash stays fixed.
	if pin.Source == Tarball && len(css) > 0 && !m.validCSSLocked() {
		if err := writeAtomic(filepath.Join(m.dir, cssName), css); err != nil {
			return err
		}
		m.files[cssName] = css
		m.manifest.Files[cssName] = hashHex(css)
		m.manifest.CSSFrom = pin.Global
	}

	return m.writeManifestLocked()
}

func (m *Manager) download(pin Pin) ([]byte, error) {
	url := m.url(pin)
	resp, err := m.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", pin.Global, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: %s answered %d", pin.Global, url, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDownloadBytes+1))
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", pin.Global, err)
	}
	if len(data) > maxDownloadBytes {
		return nil, fmt.Errorf("fetch %s: download exceeds %d bytes", pin.Global, maxDownloadBytes)
	}
	if got := hashHex(data); got != pin.SHA256 {
		return nil, fmt.Errorf("fetch %s: hash %s does not match pin %s", pin.Global, got, pin.SHA256)
	}
	return data, nil
}

func (m *Manager) url(pin Pin) string {
	base := m.baseURL
	if base == "" {
		if pin.Source == Tarball {
			base = npmBase
		} else {
			base = rawBase
		}
	}
	return base + "/" + pin.Path
}

func (m *Manager) writeManifestLocked() error {
	data, err := json.MarshalIndent(m.manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeAtomic(filepath.Join(m.dir, manifestName), data)
}

// transform turns a verified download into the bytes the page loads. A 4.x
// tarball yields a renamed player and the stylesheet. A 3.x file is wrapped in a
// function so its private `spine` global becomes spine37 or spine38.
func transform(pin Pin, data []byte) (js, css []byte, err error) {
	switch pin.Source {
	case Tarball:
		rawJS, rawCSS, err := extractTarball(data)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", pin.Global, err)
		}
		renamed, err := renameGlobal(rawJS, pin.Global)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", pin.Global, err)
		}
		return renamed, rawCSS, nil
	case RawFile:
		return wrapLegacy(data, pin.Global), nil, nil
	default:
		return nil, nil, fmt.Errorf("%s: unknown source", pin.Global)
	}
}

func extractTarball(data []byte) (js, css []byte, err error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, nil, fmt.Errorf("not a gzip tarball: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("read tarball: %w", err)
		}
		switch hdr.Name {
		case "package/dist/iife/spine-player.js":
			js, err = readCapped(tr)
		case "package/dist/spine-player.css":
			css, err = readCapped(tr)
		}
		if err != nil {
			return nil, nil, err
		}
	}
	if js == nil {
		return nil, nil, fmt.Errorf("tarball has no package/dist/iife/spine-player.js")
	}
	return js, css, nil
}

func readCapped(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxExtractBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxExtractBytes {
		return nil, fmt.Errorf("tarball entry exceeds %d bytes", maxExtractBytes)
	}
	return data, nil
}

// renameGlobal replaces the first line that starts with `var spine = ` with the
// runtime's own global. The line is required; a player without it fails loudly,
// exactly as scripts/install.sh does.
func renameGlobal(js []byte, global string) ([]byte, error) {
	const marker = "var spine = "
	for start := 0; start < len(js); {
		end := bytes.IndexByte(js[start:], '\n')
		lineEnd := len(js)
		if end >= 0 {
			lineEnd = start + end + 1
		}
		if bytes.HasPrefix(js[start:], []byte(marker)) {
			out := make([]byte, 0, len(js)+len(global)-len("spine"))
			out = append(out, js[:start]...)
			out = append(out, "var "+global+" = "...)
			out = append(out, js[start+len(marker):]...)
			return out, nil
		}
		start = lineEnd
	}
	return nil, fmt.Errorf("player has no 'var spine = ' line to rename")
}

// wrapLegacy keeps a 3.x player's private `spine` global inside a function and
// exposes it as spine37 or spine38, the same shape install.sh builds.
func wrapLegacy(js []byte, global string) []byte {
	var b bytes.Buffer
	b.Grow(len(js) + 48)
	fmt.Fprintf(&b, "var %s = (function () {\n", global)
	b.Write(js)
	b.WriteString("\nreturn spine;\n})();\n")
	return b.Bytes()
}

func hashHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
