package state

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/officialdad/spinewall/internal/jx"
	"github.com/officialdad/spinewall/internal/scan"
)

// Paths holds every file and directory the server reads or writes.
type Paths struct {
	ModelsDir       string
	ConfigFile      string
	StateFile       string
	PrefsFile       string
	LayoutFile      string
	ScreensFile     string
	ThumbDir        string
	LegacyStateFile string
}

// Store owns all mutable state behind one mutex.
//
// The Python server uses a re-entrant lock and swaps a module-global STATE with
// on_stage. Go passes the *Stage explicitly and the setters assume the caller
// holds the lock, so no setter takes it again.
type Store struct {
	mu    sync.Mutex
	paths Paths
	logf  func(string)

	models    []scan.Model
	skipped   map[string]string
	ids       []string
	idsSet    map[string]bool
	modelDirs map[string]string
	anims     map[string][]string
	edit      int
	input     map[string]int
	prefs     Prefs

	defaultStage *Stage
	screens      map[string]*Stage
	screenOrder  []string
	lastChosen   *Stage
}

const (
	screenMax      = 16
	inputStepMax   = 1000
	inputTotalMax  = int64(1_000_000_000_000)
	reportAnimsMax = 500
)

var inputKeys = []string{"wheel", "pointerdown", "pointermove", "pointerup", "fired", "keydown"}

// Open loads a store from the paths. It scans models, reads the state files
// and is ready to serve when it returns.
func Open(paths Paths, logf func(string)) *Store {
	if logf == nil {
		logf = func(string) {}
	}
	s := &Store{
		paths:     paths,
		logf:      logf,
		anims:     map[string][]string{},
		input:     newInput(),
		modelDirs: map[string]string{},
		screens:   map[string]*Stage{},
		skipped:   map[string]string{},
	}
	s.rescanLocked()
	s.defaultStage = s.loadDefaultStage()
	s.lastChosen = s.defaultStage
	s.loadScreens()
	s.prefs = loadPrefs(paths.PrefsFile, s.idsSet)
	return s
}

func newInput() map[string]int {
	m := map[string]int{}
	for _, k := range inputKeys {
		m[k] = 0
	}
	return m
}

// Lock and Unlock let the HTTP layer hold the one lock across a whole request.
func (s *Store) Lock()   { s.mu.Lock() }
func (s *Store) Unlock() { s.mu.Unlock() }

// rescanLocked rescans models. The main directory wins a duplicate id.
func (s *Store) rescanLocked() {
	result := scan.Scan(s.paths.ModelsDir, s.paths.ConfigFile, s.logf)
	s.models = result.Models
	s.skipped = result.Skipped
	s.ids = result.IDs
	s.idsSet = map[string]bool{}
	for _, id := range s.ids {
		s.idsSet[id] = true
	}
	s.modelDirs = result.Dirs
}

// Skipped returns the skip reasons.
func (s *Store) Skipped() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.skipped))
	for k, v := range s.skipped {
		out[k] = v
	}
	return out
}

// ModelDir returns a model's directory, or false when the id is unknown.
func (s *Store) ModelDir(id string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir, ok := s.modelDirs[id]
	return dir, ok
}

// loadDefaultStage reads state.json, falling back to the legacy file only when
// the new file does not exist, and merges layout.json over it.
func (s *Store) loadDefaultStage() *Stage {
	saved := readJSONObject(s.paths.StateFile)
	if saved == nil {
		if _, err := os.Stat(s.paths.StateFile); os.IsNotExist(err) {
			saved = readJSONObject(s.paths.LegacyStateFile)
		}
	}
	merged := map[string]any{}
	for k, v := range saved {
		merged[k] = v
	}
	for k, v := range readJSONObject(s.paths.LayoutFile) {
		merged[k] = v
	}
	return cleanStage(merged, s.idsSet, scan.FirstCharacterID(s.models))
}

// loadScreens reads screens.json and keeps valid keys in file order, up to the cap.
func (s *Store) loadScreens() {
	keys, values := readOrderedObject(s.paths.ScreensFile)
	for _, key := range keys {
		if _, ok := cleanScreenKey(key); !ok || key == "" {
			continue
		}
		if len(s.screens) >= screenMax {
			break
		}
		s.screens[key] = cleanStage(values[key], s.idsSet, scan.FirstCharacterID(s.models))
		s.screenOrder = append(s.screenOrder, key)
	}
}

// allStages returns the default stage then each screen in insertion order.
func (s *Store) allStages() []*Stage {
	out := []*Stage{s.defaultStage}
	for _, key := range s.screenOrder {
		out = append(out, s.screens[key])
	}
	return out
}

// screenStage returns the stage a key names, creating a copy of the last
// chosen stage when create is true and there is room.
func (s *Store) screenStage(key string, create bool) *Stage {
	if key == "" {
		return s.defaultStage
	}
	if stage, ok := s.screens[key]; ok {
		return stage
	}
	if create && len(s.screens) < screenMax {
		stage := s.lastChosen.clone()
		s.screens[key] = stage
		s.screenOrder = append(s.screenOrder, key)
		s.saveScreens()
		return stage
	}
	return nil
}

// noteChosenStage records the stage a page changed last.
func (s *Store) noteChosenStage(stage *Stage) { s.lastChosen = stage }

// save writes the state file for the default stage, or every screen otherwise.
func (s *Store) save(stage *Stage) {
	if stage == s.defaultStage {
		writeJSONAtomic(s.paths.StateFile, stateFileViewOf(stage))
	} else {
		s.saveScreens()
	}
}

// saveLayout writes the layout file for the default stage, or every screen otherwise.
func (s *Store) saveLayout(stage *Stage) {
	if stage == s.defaultStage {
		writeJSONAtomic(s.paths.LayoutFile, map[string]any{
			"characters": stage.Characters,
			"crev":       stage.Crev,
		})
	} else {
		s.saveScreens()
	}
}

func (s *Store) saveScreens() {
	screens := orderedObject{}
	for _, key := range s.screenOrder {
		if stage, ok := s.screens[key]; ok {
			screens = append(screens, orderedEntry{Key: key, Value: stage})
		}
	}
	writeJSONAtomic(s.paths.ScreensFile, screens)
}

// stateFileView is the state file shape: every stage key except the layout ones.
type stateFileView struct {
	Model     *string `json:"model"`
	Animation *string `json:"animation"`
	Rev       int     `json:"rev"`
	Scale     float64 `json:"scale"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Vrev      int     `json:"vrev"`
	Auto      bool    `json:"auto"`
	Preset    *string `json:"preset"`
}

func stateFileViewOf(stage *Stage) stateFileView {
	return stateFileView{
		Model:     stage.Model,
		Animation: stage.Animation,
		Rev:       stage.Rev,
		Scale:     stage.Scale,
		X:         stage.X,
		Y:         stage.Y,
		Vrev:      stage.Vrev,
		Auto:      stage.Auto,
		Preset:    stage.Preset,
	}
}

func (s *Store) savePrefs() {
	writeJSONAtomic(s.paths.PrefsFile, s.prefs)
}

// rescan assumes the lock is held.
func (s *Store) rescan() {
	s.rescanLocked()
	for _, stage := range s.allStages() {
		s.dropMissingModels(stage)
	}
}

func (s *Store) dropMissingModels(stage *Stage) {
	fallback := scan.FirstCharacterID(s.models)
	if stage.Model != nil && !s.idsSet[*stage.Model] {
		if fallback == nil || *stage.Model != *fallback {
			stage.Model = cloneStr(fallback)
			stage.Animation = nil
			stage.Rev++
			s.save(stage)
		}
	}
	kept := make([]Character, 0, len(stage.Characters))
	for _, c := range stage.Characters {
		if s.idsSet[c.Model] {
			kept = append(kept, c)
		}
	}
	if len(kept) != len(stage.Characters) {
		s.setCharacters(stage, kept)
	}
}

// ---- file IO ----

func readJSONObject(path string) map[string]any {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	v, err := jx.Decode(data)
	if err != nil {
		return nil
	}
	obj, ok := asObject(v)
	if !ok {
		return nil
	}
	return obj
}

// readOrderedObject reads a JSON object and returns its keys in file order,
// plus each value as its decoded form. Missing or non-object files return none.
func readOrderedObject(path string) ([]string, map[string]any) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, map[string]any{}
	}
	v, err := jx.Decode(data)
	if err != nil {
		return nil, map[string]any{}
	}
	obj, ok := asObject(v)
	if !ok {
		return nil, map[string]any{}
	}
	keys, err := orderedKeys(data)
	if err != nil {
		return nil, map[string]any{}
	}
	values := map[string]any{}
	kept := make([]string, 0, len(keys))
	for _, k := range keys {
		if _, dup := values[k]; dup {
			continue
		}
		values[k] = obj[k]
		kept = append(kept, k)
	}
	return kept, values
}

// orderedKeys returns an object's top-level keys in source order.
func orderedKeys(data []byte) ([]string, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, errNotObject
	}
	var keys []string
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := keyTok.(string)
		keys = append(keys, key)
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return nil, err
		}
	}
	return keys, nil
}

type orderedEntry struct {
	Key   string
	Value any
}

// orderedObject marshals to a JSON object keeping the entry order.
type orderedObject []orderedEntry

func (o orderedObject) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, entry := range o {
		if i > 0 {
			buf.WriteByte(',')
		}
		key, err := marshalJSON(entry.Key)
		if err != nil {
			return nil, err
		}
		buf.Write(key)
		buf.WriteByte(':')
		value, err := marshalJSON(entry.Value)
		if err != nil {
			return nil, err
		}
		buf.Write(value)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func writeJSONAtomic(path string, v any) {
	data, err := marshalJSON(v)
	if err != nil {
		writeFailed(err)
		return
	}
	writeAtomic(path, data)
}

func writeAtomic(path string, data []byte) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeFailed(err)
		return
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		writeFailed(err)
		return
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		writeFailed(err)
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		writeFailed(err)
		return
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		writeFailed(err)
	}
}

// writeFailed logs a write error and keeps the server up. The Python server
// lets an unwritable directory surface through the request, but a state write
// never changes the reply shape, so logging is enough for parity.
func writeFailed(err error) {
	os.Stderr.WriteString("spinewall: write failed: " + err.Error() + "\n")
}

var errNotObject = jsonError("not a JSON object")

type jsonError string

func (e jsonError) Error() string { return string(e) }
