package state

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/officialdad/spinewall/internal/scan"
)

func TestClamp(t *testing.T) {
	if got := clamp(5, 0, 1, 9); got != 1 {
		t.Fatalf("clamp(5): %v", got)
	}
	if got := clamp(json.Number("-5"), 0, 1, 9); got != 0 {
		t.Fatalf("clamp(-5): %v", got)
	}
	if got := clamp("0.5", 0, 1, 9); got != 0.5 {
		t.Fatalf("clamp(0.5): %v", got)
	}
	for _, bad := range []any{"abc", "", "  ", nil, []any{}, map[string]any{}, math.NaN(), "nan"} {
		if got := clamp(bad, 0, 1, 9); got != 9 {
			t.Fatalf("clamp(%v): %v", bad, got)
		}
	}
	if got := clamp(math.Inf(1), 0, 1, 9); got != 1 {
		t.Fatalf("clamp(inf): %v", got)
	}
	if got := clamp("-inf", 0, 1, 9); got != 0 {
		t.Fatalf("clamp(-inf): %v", got)
	}
}

func TestPlainInt(t *testing.T) {
	if got := plainInt(true, 7); got != 7 {
		t.Fatalf("bool: %v", got)
	}
	if got := plainInt(json.Number("3"), 7); got != 3 {
		t.Fatalf("int: %v", got)
	}
	if got := plainInt(json.Number("3.5"), 7); got != 7 {
		t.Fatalf("float: %v", got)
	}
	if got := plainInt("3", 7); got != 7 {
		t.Fatalf("string: %v", got)
	}
}

func TestCleanPresetName(t *testing.T) {
	if got := cleanPresetName("  desk  "); got == nil || *got != "desk" {
		t.Fatalf("trim: %v", got)
	}
	for _, bad := range []any{"", "   ", "tab\there", "line\nbreak", string(make([]byte, 41))} {
		if got := cleanPresetName(bad); got != nil {
			t.Fatalf("bad %q: %v", bad, got)
		}
	}
	long := ""
	for i := 0; i < 41; i++ {
		long += "x"
	}
	if got := cleanPresetName(long); got != nil {
		t.Fatalf("41 chars: %v", *got)
	}
}

func TestCleanCharacters(t *testing.T) {
	ids := map[string]bool{"alpha": true, "beta": true}
	raw := []any{
		5,
		map[string]any{"model": "ghost"},
		map[string]any{"uid": "../bad", "model": "alpha"},
		map[string]any{"uid": "ok1", "model": "alpha", "x": "bad", "scale": 99, "animation": 7},
		map[string]any{"uid": "ok1", "model": "beta"},
	}
	got := cleanCharacters(raw, ids, nil, true)
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	if got[0].UID != "ok1" || got[0].X != 0 || got[0].Scale != 8 || got[0].Animation != nil {
		t.Fatalf("got %+v", got[0])
	}
}

func TestCleanCharactersNewUID(t *testing.T) {
	ids := map[string]bool{"alpha": true}
	got := cleanCharacters([]any{map[string]any{"model": "alpha"}}, ids, nil, true)
	if len(got) != 1 || len(got[0].UID) != 6 {
		t.Fatalf("got %+v", got)
	}
	if !uidPattern.MatchString(got[0].UID) {
		t.Fatalf("uid %q", got[0].UID)
	}
}

func TestCleanStage(t *testing.T) {
	ids := map[string]bool{"alpha": true, "gamma": true}
	first := "alpha"
	stage := cleanStage(map[string]any{
		"model": "gamma", "animation": 5, "rev": "7", "vrev": true,
		"scale": "big", "x": nil, "y": []any{1},
	}, ids, &first)
	if deref(stage.Model) != "gamma" || stage.Animation != nil {
		t.Fatalf("model/animation: %+v", stage)
	}
	if stage.Rev != 0 || stage.Vrev != 0 {
		t.Fatalf("rev: %d / %d", stage.Rev, stage.Vrev)
	}
	if stage.Scale != 1 || stage.X != 0 || stage.Y != 0 {
		t.Fatalf("view: %+v", stage)
	}
}

func TestCleanStageMissingModelFallsBack(t *testing.T) {
	ids := map[string]bool{"alpha": true}
	first := "alpha"
	stage := cleanStage(map[string]any{"model": "ghost", "rev": 9}, ids, &first)
	if deref(stage.Model) != "alpha" || stage.Rev != 0 {
		t.Fatalf("got %+v", stage)
	}
}

func TestCleanStageClamps(t *testing.T) {
	ids := map[string]bool{"alpha": true}
	first := "alpha"
	stage := cleanStage(map[string]any{"model": "alpha", "scale": 100, "x": -50}, ids, &first)
	if stage.Scale != 8 || stage.X != -2 {
		t.Fatalf("got %+v", stage)
	}
	stage = cleanStage(map[string]any{"model": "alpha", "rev": -4, "vrev": -1}, ids, &first)
	if stage.Rev != 0 || stage.Vrev != 0 {
		t.Fatalf("rev: %+v", stage)
	}
}

func TestCleanPresets(t *testing.T) {
	ids := map[string]bool{"beta": true}
	junk := []any{
		"text", json.Number("7"), nil, map[string]any{"name": "no chars"},
		map[string]any{"name": "", "characters": []any{map[string]any{"model": "beta"}}},
		map[string]any{"name": "empty", "characters": []any{}},
		map[string]any{"name": 5, "characters": []any{map[string]any{"model": "beta"}}},
		map[string]any{"name": "bad model", "characters": []any{map[string]any{"model": 5}, map[string]any{"model": ""}, "x"}},
		map[string]any{"name": "good", "characters": []any{map[string]any{"model": "beta", "x": "oops", "scale": 99, "auto": 1}}},
		map[string]any{"name": "good", "characters": []any{map[string]any{"model": "beta"}}},
	}
	presets := cleanPresets(junk, ids)
	if len(presets) != 1 || presets[0].Name != "good" {
		t.Fatalf("presets: %+v", presets)
	}
	c := presets[0].Characters[0]
	if c.X != 0 || c.Scale != 8 || c.Auto {
		t.Fatalf("char: %+v", c)
	}
}

func TestCleanPrefs(t *testing.T) {
	ids := map[string]bool{"alpha": true}
	prefs := cleanPrefs(map[string]any{
		"favorites": []any{"alpha", 5, nil},
		"recent":    "x",
		"layout":    "tiles",
		"sort":      "recent",
		"variants":  "no",
		"kind":      5,
	}, ids)
	if len(prefs.Favorites) != 1 || prefs.Favorites[0] != "alpha" {
		t.Fatalf("favorites: %v", prefs.Favorites)
	}
	if len(prefs.Recent) != 0 {
		t.Fatalf("recent: %v", prefs.Recent)
	}
	if prefs.Layout != "grid" || prefs.Sort != "recent" || prefs.Kind != "character" {
		t.Fatalf("prefs: %+v", prefs)
	}
	if !prefs.Variants {
		t.Fatalf("variants: %v", prefs.Variants)
	}
}

// newTestStore builds a store over a temp directory with the given models.
func newTestStore(t *testing.T, models ...scan.Model) (*Store, Paths) {
	t.Helper()
	root := t.TempDir()
	modelsDir := filepath.Join(root, "models")
	if err := os.MkdirAll(modelsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	applyModel(t, modelsDir, "alpha", "4.0.31")
	applyModel(t, modelsDir, "beta", "4.1.56")
	applyModel(t, modelsDir, "gamma", "4.2.120")
	paths := Paths{
		ModelsDir:       modelsDir,
		ConfigFile:      filepath.Join(root, "config.json"),
		StateFile:       filepath.Join(root, "state", "state.json"),
		PrefsFile:       filepath.Join(root, "state", "prefs.json"),
		LayoutFile:      filepath.Join(root, "state", "layout.json"),
		ScreensFile:     filepath.Join(root, "state", "screens.json"),
		ThumbDir:        filepath.Join(root, "cache", "thumbs"),
		LegacyStateFile: filepath.Join(root, "home", ".local", "state", "spinewall.json"),
	}
	store := Open(paths, nil)
	return store, paths
}

func applyModel(t *testing.T, modelsDir, id, version string) {
	t.Helper()
	dir := filepath.Join(modelsDir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".skel"), binarySkel(version), 0o644); err != nil {
		t.Fatal(err)
	}
	atlas := "page.png\nsize: 1,1\nfilter: Linear,Linear\nbody\n  bounds: 0,0,1,1\n"
	if err := os.WriteFile(filepath.Join(dir, id+".atlas"), []byte(atlas), 0o644); err != nil {
		t.Fatal(err)
	}
}

func binarySkel(version string) []byte {
	text := []byte(version)
	out := make([]byte, 8)
	for i := range out {
		out[i] = 0xff
	}
	out = append(out, byte(len(text)+1))
	out = append(out, text...)
	return append(out, make([]byte, 16)...)
}

func TestStoreDefaultStage(t *testing.T) {
	store, _ := newTestStore(t)
	reply, status, msg := store.StateFor("", false, true)
	if status != 0 {
		t.Fatalf("status %d msg %s", status, msg)
	}
	if deref(reply.Model) != "alpha" {
		t.Fatalf("model: %v", reply.Model)
	}
	if reply.Edit != 0 {
		t.Fatalf("edit: %d", reply.Edit)
	}
}

func TestCommandZoomClamp(t *testing.T) {
	store, _ := newTestStore(t)
	for i := 0; i < 60; i++ {
		store.CommandReply("", false, "zoom-in", Arg{})
	}
	reply, _, _ := store.StateFor("", false, true)
	if reply.Scale != 8 {
		t.Fatalf("scale: %v", reply.Scale)
	}
}

func TestSetModelSameIsNoOp(t *testing.T) {
	store, _ := newTestStore(t)
	before, _, _ := store.StateFor("", false, true)
	store.CommandReply("", false, "set-model", StringArg(deref(before.Model)))
	after, _, _ := store.StateFor("", false, true)
	if after.Rev != before.Rev {
		t.Fatalf("rev bumped: %d -> %d", before.Rev, after.Rev)
	}
}

func TestAddCharacterStartsMulti(t *testing.T) {
	store, _ := newTestStore(t)
	reply, status, msg := store.CommandReply("", false, "add-character", StringArg("gamma"))
	if status != 0 {
		t.Fatalf("%s", msg)
	}
	if len(reply.Characters) != 2 || reply.Characters[0].Model != "alpha" || reply.Characters[1].Model != "gamma" {
		t.Fatalf("chars: %+v", reply.Characters)
	}
	if reply.Crev != 1 {
		t.Fatalf("crev: %d", reply.Crev)
	}
}

func TestRemoveDownToOneFolds(t *testing.T) {
	store, _ := newTestStore(t)
	reply, _, _ := store.CommandReply("", false, "add-character", StringArg("gamma"))
	first := reply.Characters[0].UID
	before := reply
	reply, status, msg := store.CommandReply("", false, "remove-character", StringArg(first))
	if status != 0 {
		t.Fatalf("%s", msg)
	}
	if len(reply.Characters) != 0 {
		t.Fatalf("chars: %+v", reply.Characters)
	}
	if deref(reply.Model) != "gamma" || reply.Scale != 0.7 {
		t.Fatalf("folded: %+v", reply)
	}
	if reply.Rev <= before.Rev || reply.Vrev <= before.Vrev || reply.Crev <= before.Crev {
		t.Fatalf("revisions did not rise")
	}
}

func TestPresetSaveLoad(t *testing.T) {
	store, _ := newTestStore(t)
	store.CommandReply("", false, "set-model", StringArg("gamma"))
	store.CommandReply("", false, "save-preset", StringArg("desk"))
	store.CommandReply("", false, "set-model", StringArg("beta"))
	reply, _, _ := store.CommandReply("", false, "load-preset", StringArg("desk"))
	if deref(reply.Model) != "gamma" {
		t.Fatalf("model: %v", reply.Model)
	}
}

func TestRenamePresetSplitsBeforeDecode(t *testing.T) {
	store, _ := newTestStore(t)
	store.CommandReply("", false, "save-preset", StringArg("a/b"))
	store.CommandReply("", false, "rename-preset", PairArg("a/b", "c/d e"))
	prefs := store.PrefsCopy()
	if len(prefs.Presets) != 1 || prefs.Presets[0].Name != "c/d e" {
		t.Fatalf("presets: %+v", prefs.Presets)
	}
}

func TestPrefsPostCannotWritePresets(t *testing.T) {
	store, _ := newTestStore(t)
	ok := store.PrefsPost(map[string]any{
		"presets": []any{map[string]any{"name": "x", "characters": []any{map[string]any{"model": "beta"}}}},
		"sort":    "recent",
	})
	if !ok {
		t.Fatalf("sort change should be accepted")
	}
	if len(store.PrefsCopy().Presets) != 0 {
		t.Fatalf("presets written by prefs post")
	}
	if store.PrefsCopy().Sort != "recent" {
		t.Fatalf("sort: %s", store.PrefsCopy().Sort)
	}
}

func TestScreensCopyLastChosen(t *testing.T) {
	store, _ := newTestStore(t)
	store.CommandReply("", false, "set-model", StringArg("gamma"))
	left, status, msg := store.StateFor("left", true, true)
	if status != 0 {
		t.Fatalf("%s", msg)
	}
	if deref(left.Model) != "gamma" {
		t.Fatalf("copy model: %v", left.Model)
	}
	screens := store.ScreensReply()
	if len(screens) != 1 || screens[0]["screen"] != "left" {
		t.Fatalf("screens: %+v", screens)
	}
}

func TestUnknownScreenCommandIs404(t *testing.T) {
	store, _ := newTestStore(t)
	_, status, msg := store.CommandReply("left", true, "set-model", StringArg("gamma"))
	if status != 404 || msg != "unknown screen" {
		t.Fatalf("status %d msg %s", status, msg)
	}
	if len(store.ScreensReply()) != 0 {
		t.Fatalf("a screen was created")
	}
}

func TestScreenCap(t *testing.T) {
	store, _ := newTestStore(t)
	for i := 0; i < 16; i++ {
		if _, status, _ := store.StateFor("s"+string(rune('a'+i)), true, true); status != 0 {
			t.Fatalf("screen %d status %d", i, status)
		}
	}
	_, status, msg := store.StateFor("one-more", true, true)
	if status != 400 || msg != "at most 16 screens" {
		t.Fatalf("cap: status %d msg %s", status, msg)
	}
}

func TestParallelAddsLoseNothing(t *testing.T) {
	store, _ := newTestStore(t)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store.CommandReply("", false, "add-character", StringArg("gamma"))
		}()
	}
	wg.Wait()
	reply, _, _ := store.StateFor("", false, true)
	if len(reply.Characters) != 13 {
		t.Fatalf("characters: %d", len(reply.Characters))
	}
	if reply.Crev != 12 {
		t.Fatalf("crev: %d", reply.Crev)
	}
	seen := map[string]bool{}
	for _, c := range reply.Characters {
		if seen[c.UID] {
			t.Fatalf("duplicate uid %s", c.UID)
		}
		seen[c.UID] = true
	}
}

func TestParallelViewKeepsFilesValid(t *testing.T) {
	store, paths := newTestStore(t)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store.CommandReply("", false, "zoom-in", Arg{})
		}()
	}
	wg.Wait()
	reply, _, _ := store.StateFor("", false, true)
	if reply.Vrev != 16 {
		t.Fatalf("vrev: %d", reply.Vrev)
	}
	data, err := os.ReadFile(paths.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	var parsed any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("state file is not valid JSON: %v", err)
	}
}

func TestLayoutMergeAndReplace(t *testing.T) {
	store, _ := newTestStore(t)
	store.CommandReply("", false, "add-character", StringArg("gamma"))
	state, _, _ := store.StateFor("", false, true)
	uid := state.Characters[0].UID
	chars, _, status, msg := store.LayoutPost("", false, map[string]any{"characters": []any{map[string]any{"uid": uid, "x": 0.9}}})
	if status != 0 {
		t.Fatalf("%s", msg)
	}
	if len(chars) != 2 || chars[0].X != 0.9 {
		t.Fatalf("chars: %+v", chars)
	}
	replaced, _, status, msg := store.LayoutPost("", false, map[string]any{
		"replace":    true,
		"characters": []any{map[string]any{"model": "beta"}, map[string]any{"model": "gamma"}, map[string]any{"model": "bogus"}},
	})
	if status != 0 {
		t.Fatalf("%s", msg)
	}
	if len(replaced) != 2 || replaced[0].Model != "beta" || replaced[1].Model != "gamma" {
		t.Fatalf("replaced: %+v", replaced)
	}
	for _, c := range replaced {
		if c.UID == "" {
			t.Fatalf("a replaced character has no uid")
		}
	}
}
