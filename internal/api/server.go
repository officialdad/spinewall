// Package api serves the Spinewall HTTP API on 127.0.0.1.
//
// It is a drop-in for server/spinewall-server.py: same paths, status codes,
// JSON shapes, guards and file-serving rules. The Python server is the
// reference; the Go server must answer identically.
package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/officialdad/spinewall/internal/jx"
	"github.com/officialdad/spinewall/internal/runtime"
	"github.com/officialdad/spinewall/internal/state"
)

const (
	bodyMaxBytes  = 1024 * 1024
	thumbMaxBytes = 512 * 1024
	modelPathDeep = 2
)

// servedExtensions are the model file types the page fetches. model.json is read
// by the server, never by the page.
var servedExtensions = []string{".skel", ".json", ".atlas", ".png", ".jpg", ".jpeg", ".webp", ".txt"}

// hiddenFiles are never served even though their extension matches.
var hiddenFiles = []string{"model.json"}

var pngMagic = []byte("\x89PNG\r\n\x1a\n")

// Server answers the API for one store.
type Server struct {
	store *state.Store
	port  int
	theme Theme
}

// RuntimeProvider serves a validated runtime file, such as spine-player41.js.
// It is the internal/runtime.Manager in production and a stub in tests.
type RuntimeProvider interface {
	File(name string) ([]byte, bool)
}

// Theme holds the embedded page bytes and the runtime file provider.
type Theme struct {
	Page      []byte
	LocalHost []byte
	Runtime   RuntimeProvider
}

// New builds a Server.
func New(store *state.Store, port int, theme Theme) *Server {
	return &Server{store: store, port: port, theme: theme}
}

func (s *Server) allowedOrigins() []string {
	return []string{
		"null",
		"file://",
		fmt.Sprintf("http://127.0.0.1:%d", s.port),
		fmt.Sprintf("http://localhost:%d", s.port),
	}
}

// ServeHTTP wires the guards, CORS, and method dispatch.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	origin, status, msg := s.guard(r)
	if status != 0 {
		writeJSON(w, status, map[string]any{"error": msg})
		return
	}
	if origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
	}
	w.Header().Set("Cache-Control", "no-cache")

	switch r.Method {
	case http.MethodGet:
		s.handleGet(w, r)
	case http.MethodPost:
		s.handlePost(w, r)
	case http.MethodOptions:
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

// guard refuses DNS rebinding and cross-site requests. It returns the echoed
// origin, or a status and message when the request must be refused.
func (s *Server) guard(r *http.Request) (string, int, string) {
	host := r.Host
	if host != fmt.Sprintf("127.0.0.1:%d", s.port) && host != fmt.Sprintf("localhost:%d", s.port) {
		return "", http.StatusForbidden, "bad host"
	}
	origin := r.Header.Get("Origin")
	if origin != "" {
		for _, o := range s.allowedOrigins() {
			if o == origin {
				return origin, 0, ""
			}
		}
		return "", http.StatusForbidden, "origin not allowed"
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return "", http.StatusForbidden, "cross-site request"
	}
	return "", 0, ""
}

// route returns the decoded screen parameter. badScreen is true when a screen
// parameter is present but unusable, which is a 400.
func route(r *http.Request) (screen string, hasScreen, badScreen bool) {
	values := parseQuery(r.URL.RawQuery)
	if list, ok := values["screen"]; ok && len(list) > 0 {
		key, ok := state.ParseScreen(list[0])
		if !ok {
			return "", false, true
		}
		return key, true, false
	}
	return "", false, false
}

// parseQuery mirrors urllib.parse.parse_qs with keep_blank_values: it splits on
// & and =, turns + into a space, and treats a key with no value as empty.
func parseQuery(raw string) map[string][]string {
	out := map[string][]string{}
	if raw == "" {
		return out
	}
	for _, pair := range strings.Split(raw, "&") {
		if pair == "" {
			continue
		}
		key, value, _ := strings.Cut(pair, "=")
		out[pyUnquotePlus(key)] = append(out[pyUnquotePlus(key)], pyUnquotePlus(value))
	}
	return out
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	// Route on the escaped path, like the Python server's self.path. Percent
	// escapes are decoded per segment, so a preset name may hold a slash.
	path := r.URL.EscapedPath()
	screen, hasScreen, badScreen := route(r)
	if badScreen {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad screen"})
		return
	}

	switch {
	case path == "/state":
		reply, status, msg := s.store.StateFor(screen, hasScreen, true)
		if status != 0 {
			writeJSON(w, status, map[string]any{"error": msg})
			return
		}
		writeJSON(w, http.StatusOK, reply)
	case path == "/prefs":
		writeJSON(w, http.StatusOK, s.store.PrefsCopy())
	case path == "/presets":
		writeJSON(w, http.StatusOK, s.store.PresetsReply())
	case path == "/screens":
		writeJSON(w, http.StatusOK, s.store.ScreensReply())
	case path == "/models":
		writeJSON(w, http.StatusOK, s.store.ModelsCopy())
	case path == "/rescan":
		writeJSON(w, http.StatusOK, s.store.Rescan())
	case path == "/info":
		writeJSON(w, http.StatusOK, s.store.InfoReply())
	case strings.HasPrefix(path, "/thumb/") && strings.HasSuffix(path, ".png"):
		s.serveThumb(w, path[7:len(path)-4])
	case strings.HasPrefix(path, "/edit/"):
		edit := s.store.SetEdit(strings.HasSuffix(path, "/1"))
		writeJSON(w, http.StatusOK, map[string]any{"on": edit})
	case strings.HasPrefix(path, "/cmd/"):
		s.handleCommand(w, path[5:], screen, hasScreen)
	case path == "/" || path == "/index.html":
		s.serveBytes(w, "text/html; charset=utf-8", s.theme.Page)
	case path == "/local-host.js":
		s.serveBytes(w, "text/javascript; charset=utf-8", s.theme.LocalHost)
	case runtime.IsRuntimeFile(strings.TrimPrefix(path, "/")):
		s.serveRuntimeFile(w, strings.TrimPrefix(path, "/"))
	default:
		s.handleModelFile(w, path)
	}
}

// serveRuntimeFile answers a cached Spine player or stylesheet. A file the
// manager has not validated is a 404, never corrupt bytes.
func (s *Server) serveRuntimeFile(w http.ResponseWriter, name string) {
	if s.theme.Runtime == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "runtime not installed"})
		return
	}
	data, ok := s.theme.Runtime.File(name)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "runtime not installed"})
		return
	}
	contentType := "text/javascript; charset=utf-8"
	if strings.HasSuffix(name, ".css") {
		contentType = "text/css; charset=utf-8"
	}
	s.serveBytes(w, contentType, data)
}

// serveBytes answers with embedded theme bytes. Only the page and local host
// paths serve, so a directory path still answers 404 as the file rules require.
func (s *Server) serveBytes(w http.ResponseWriter, contentType string, data []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", fmt.Sprint(len(data)))
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

func (s *Server) handleCommand(w http.ResponseWriter, rest, screen string, hasScreen bool) {
	name, rawArg, _ := strings.Cut(rest, "/")
	var arg state.Arg
	if name == "rename-preset" {
		// A preset name may hold a slash, so the two names split before they decode.
		parts := strings.Split(rawArg, "/")
		decoded := make([]string, len(parts))
		all := true
		for i, part := range parts {
			decoded[i] = pyUnquote(part)
			if decoded[i] == "" {
				all = false
			}
		}
		if len(decoded) == 2 && all {
			arg = state.PairArg(decoded[0], decoded[1])
		}
	} else {
		arg = state.StringArg(pyUnquote(rawArg))
	}
	reply, status, msg := s.store.CommandReply(screen, hasScreen, name, arg)
	if status != 0 {
		writeJSON(w, status, map[string]any{"error": msg})
		return
	}
	writeJSON(w, http.StatusOK, reply)
}

func (s *Server) serveThumb(w http.ResponseWriter, rawID string) {
	id := pyUnquote(rawID)
	path, ok := s.store.ThumbPath(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "no thumbnail"})
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "no thumbnail"})
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Length", fmt.Sprint(len(data)))
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

func (s *Server) handlePost(w http.ResponseWriter, r *http.Request) {
	screen, hasScreen, badScreen := route(r)
	if badScreen {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad screen"})
		return
	}
	// A cross-site form can send text/plain without a preflight. JSON forces one.
	contentType := r.Header.Get("Content-Type")
	if semi := strings.IndexByte(contentType, ';'); semi >= 0 {
		contentType = contentType[:semi]
	}
	if strings.ToLower(strings.TrimSpace(contentType)) != "application/json" {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]any{"error": "Content-Type must be application/json"})
		return
	}
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}
	s.handlePostBody(w, r.URL.EscapedPath(), screen, hasScreen, body)
}

func (s *Server) handlePostBody(w http.ResponseWriter, path, screen string, hasScreen bool, body map[string]any) {
	switch {
	case path == "/report":
		status, msg := s.store.Report(screen, hasScreen, body)
		if status != 0 {
			writeJSON(w, status, map[string]any{"error": msg})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case strings.HasPrefix(path, "/thumb/"):
		s.uploadThumb(w, path[7:], body)
	case strings.HasPrefix(path, "/fav/"):
		id := pyUnquote(path[5:])
		if !s.store.ToggleFavorite(id) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "unknown model"})
			return
		}
		writeJSON(w, http.StatusOK, s.store.PrefsCopy())
	case path == "/prefs":
		if !s.store.PrefsPost(body) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad prefs"})
			return
		}
		writeJSON(w, http.StatusOK, s.store.PrefsCopy())
	case path == "/layout":
		characters, crev, status, msg := s.store.LayoutPost(screen, hasScreen, body)
		if status != 0 {
			writeJSON(w, status, map[string]any{"error": msg})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"characters": characters, "crev": crev})
	case path == "/view":
		status, msg := s.store.ViewPost(screen, hasScreen, body["scale"], body["x"], body["y"])
		if status != 0 {
			writeJSON(w, status, map[string]any{"error": msg})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case path == "/input":
		s.store.AddInput(body)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
	}
}

func (s *Server) uploadThumb(w http.ResponseWriter, rawID string, body map[string]any) {
	id := pyUnquote(rawID)
	path, known := s.store.ThumbPath(id)
	encoded, isStr := body["png"].(string)
	var png []byte
	if isStr && known {
		// Python's base64 with validate=True rejects stray characters.
		if decoded, err := base64.StdEncoding.Strict().DecodeString(encoded); err == nil {
			png = decoded
		}
	}
	if !known || !strings.HasPrefix(string(png), string(pngMagic)) || len(png) > thumbMaxBytes {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad thumbnail"})
		return
	}
	if err := state.WriteThumb(path, png); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad thumbnail"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleModelFile serves a model file at exactly /<id>/<file>, or answers 404.
func (s *Server) handleModelFile(w http.ResponseWriter, path string) {
	parts := splitPath(path)
	if len(parts) != modelPathDeep {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
		return
	}
	id, name := parts[0], parts[1]
	if strings.HasPrefix(name, ".") || containsString(hiddenFiles, name) || !servedExtension(name) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
		return
	}
	// A NUL byte in the path must answer 404, never reach the filesystem.
	if strings.ContainsRune(name, 0) || strings.ContainsRune(id, 0) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
		return
	}
	if strings.ContainsAny(name, `/\`) || strings.ContainsAny(id, `/\`) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
		return
	}
	dir, ok := s.store.ModelDir(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
		return
	}
	full := filepath.Join(dir, name)
	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
		return
	}
	data, err := os.ReadFile(full)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
		return
	}
	if ct := contentTypeFor(name); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Content-Length", fmt.Sprint(len(data)))
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

func splitPath(path string) []string {
	parts := []string{}
	for _, p := range strings.Split(path, "/") {
		if p == "" {
			continue
		}
		parts = append(parts, pyUnquote(p))
	}
	return parts
}

func servedExtension(name string) bool {
	lower := strings.ToLower(name)
	for _, ext := range servedExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

func contentTypeFor(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".json":
		return "application/json"
	case ".atlas", ".skel", ".txt":
		if ct := mime.TypeByExtension(ext); ct != "" {
			return ct
		}
		return "application/octet-stream"
	}
	return ""
}

func containsString(list []string, value string) bool {
	for _, s := range list {
		if s == value {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	data, err := marshalNoEscape(v)
	if err != nil {
		http.Error(w, "encode error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", fmt.Sprint(len(data)))
	w.WriteHeader(status)
	w.Write(data)
}

func marshalNoEscape(v any) ([]byte, error) {
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return []byte(strings.TrimSuffix(sb.String(), "\n")), nil
}

// pyUnquote mirrors urllib.parse.unquote: percent escapes are decoded, a bad
// escape is left as-is, and + stays +. The Python server never errors on a bad
// escape, so neither does this.
func pyUnquote(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	return percentDecode(s, false)
}

// pyUnquotePlus mirrors urllib.parse.unquote_plus: like pyUnquote but + becomes
// a space. Query values use this.
func pyUnquotePlus(s string) string {
	return percentDecode(s, true)
}

// percentDecode decodes %XX escapes. A malformed escape is copied through
// unchanged, as Python does.
func percentDecode(s string, plusAsSpace bool) string {
	var sb strings.Builder
	sb.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '+' && plusAsSpace {
			sb.WriteByte(' ')
			continue
		}
		if c == '%' && i+2 < len(s) {
			hi, ok1 := hexVal(s[i+1])
			lo, ok2 := hexVal(s[i+2])
			if ok1 && ok2 {
				sb.WriteByte(hi<<4 | lo)
				i += 2
				continue
			}
		}
		sb.WriteByte(c)
	}
	return sb.String()
}

func hexVal(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// decodeBody parses a POST body to a JSON object. It sends the error reply and
// returns nil, false when the body is unusable.
func decodeBody(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
	length := int64(0)
	if cl := r.Header.Get("Content-Length"); cl != "" {
		n, err := strconv.ParseInt(cl, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad Content-Length"})
			return nil, false
		}
		length = n
	}
	if length < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad Content-Length"})
		return nil, false
	}
	if length > bodyMaxBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "body too large"})
		return nil, false
	}
	reader := io.LimitReader(r.Body, bodyMaxBytes+1)
	data, err := io.ReadAll(reader)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "body must be a JSON object"})
		return nil, false
	}
	if len(data) > bodyMaxBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "body too large"})
		return nil, false
	}
	// Python reads `json.loads(body or b"{}")`, so an empty body is an empty object.
	if len(strings.TrimSpace(string(data))) == 0 {
		return map[string]any{}, true
	}
	v, err := jx.Decode(data)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "body must be a JSON object"})
		return nil, false
	}
	obj, ok := v.(map[string]any)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "body must be a JSON object"})
		return nil, false
	}
	return obj, true
}
