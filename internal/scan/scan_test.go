package scan

import (
	"os"
	"path/filepath"
	"testing"
)

func binarySkel(version, layout string) []byte {
	text := []byte(version)
	if layout == "3x" {
		digest := []byte("hash1234")
		out := []byte{byte(len(digest) + 1)}
		out = append(out, digest...)
		out = append(out, byte(len(text)+1))
		out = append(out, text...)
		return append(out, make([]byte, 16)...)
	}
	out := make([]byte, 8)
	for i := range out {
		out[i] = 0xff
	}
	out = append(out, byte(len(text)+1))
	out = append(out, text...)
	return append(out, make([]byte, 16)...)
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadBinaryVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.skel")
	for _, version := range []string{"4.0.31", "4.1.56", "4.2.120"} {
		writeFile(t, path, binarySkel(version, "4x"))
		got := ReadBinaryVersion(path)
		if got == nil || *got != version {
			t.Fatalf("4x %s: got %v", version, got)
		}
	}
	writeFile(t, path, binarySkel("3.8.99", "3x"))
	if got := ReadBinaryVersion(path); got == nil || *got != "3.8.99" {
		t.Fatalf("3x: got %v", got)
	}
	for _, data := range [][]byte{{}, {0x01, 0x02, 0x03}, make([]byte, 64)} {
		writeFile(t, path, data)
		if got := ReadBinaryVersion(path); got != nil {
			t.Fatalf("garbage %v: got %v", data, got)
		}
	}
}

func TestReadJSONVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.json")
	cases := map[string]string{
		`{"skeleton": {"spine": "4.1.24"}}`:   "4.1.24",
		`{"skeleton": {"version": "3.8.75"}}`: "3.8.75",
		`{"skeleton": {"spine": 4.2}}`:        "4.2",
	}
	for body, want := range cases {
		writeFile(t, path, []byte(body))
		got, err := ReadJSONVersion(path)
		if err != nil {
			t.Fatalf("%s: %v", body, err)
		}
		if got == nil || *got != want {
			t.Fatalf("%s: got %v want %s", body, got, want)
		}
	}
	for _, body := range []string{"{}", "[]", `{"skeleton": 5}`, `{"skeleton": {}}`} {
		writeFile(t, path, []byte(body))
		if got, err := ReadJSONVersion(path); err != nil || got != nil {
			t.Fatalf("%s: got %v err %v", body, got, err)
		}
	}
}

func TestReadAtlasRegions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "r.atlas")
	writeFile(t, path, []byte("page.png\nsize: 1,1\nbody\n  bounds: 0,0,1,1\nhead\n\nsecond.WEBP\narm\n"))
	got, err := ReadAtlasRegions(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"body", "head", "arm"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestClassifyKind(t *testing.T) {
	cases := []struct {
		names []string
		want  string
	}{
		{nil, "character"},
		{[]string{"bg_a", "bg_b", "body"}, "scene"},
		{[]string{"bg_a", "body"}, "character"},
		{[]string{"F_Coat_Back", "back", "back_hair"}, "character"},
		{[]string{"bg"}, "scene"},
		{[]string{"BG_sky"}, "scene"},
		{[]string{"background1"}, "scene"},
		{[]string{"Scene_01"}, "scene"},
		{[]string{"diorama"}, "scene"},
		{[]string{"a_bg_1", "b_bg_2"}, "scene"},
	}
	for _, c := range cases {
		if got := ClassifyKind(c.names); got != c.want {
			t.Fatalf("%v: got %s want %s", c.names, got, c.want)
		}
	}
}

func TestAssignCharacters(t *testing.T) {
	models := []Model{{ID: "hero_01"}, {ID: "hero_02"}, {ID: "solo_01"}, {ID: "alpha"}, {ID: "named", Character: strPtr("custom")}}
	AssignCharacters(models)
	want := []string{"hero", "hero", "solo_01", "alpha", "custom"}
	for i, w := range want {
		if models[i].Character == nil || *models[i].Character != w {
			t.Fatalf("%s: got %v want %s", models[i].ID, models[i].Character, w)
		}
	}
}

func TestFirstCharacterID(t *testing.T) {
	models := []Model{{ID: "bg", Kind: "scene"}, {ID: "who", Kind: "character"}}
	if id := FirstCharacterID(models); id == nil || *id != "who" {
		t.Fatalf("got %v", id)
	}
	if id := FirstCharacterID(models[:1]); id == nil || *id != "bg" {
		t.Fatalf("scene-only got %v", id)
	}
	if id := FirstCharacterID(nil); id != nil {
		t.Fatalf("empty got %v", id)
	}
}

func strPtr(s string) *string { return &s }

func substr(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}

func TestScanModelSkipReasons(t *testing.T) {
	dir := t.TempDir()
	// A model with no atlas.
	mk := filepath.Join(dir, "noatlas")
	writeFile(t, filepath.Join(mk, "noatlas.skel"), binarySkel("4.1.56", "4x"))
	if _, reason := ScanModel(mk); reason != "needs one skeleton (.skel or .json) and one .atlas file" {
		t.Fatalf("noatlas reason: %q", reason)
	}
	// A 3.7 binary must ask for a JSON export.
	mk = filepath.Join(dir, "legacy")
	writeFile(t, filepath.Join(mk, "legacy.skel"), binarySkel("3.7.94", "4x"))
	writeFile(t, filepath.Join(mk, "legacy.atlas"), []byte("page.png\n"))
	if _, reason := ScanModel(mk); reason == "" || !substr(reason, "JSON export") {
		t.Fatalf("legacy reason: %q", reason)
	}
}

func TestScanModelOverrides(t *testing.T) {
	dir := t.TempDir()
	mk := filepath.Join(dir, "named")
	writeFile(t, filepath.Join(mk, "named.skel"), binarySkel("4.2.120", "4x"))
	writeFile(t, filepath.Join(mk, "named.atlas"), []byte("page.png\nbody\n"))
	writeFile(t, filepath.Join(mk, "model.json"), []byte(`{"name":"Pretty Name","kind":"scene","hideSlots":["bg",7,""],"skin":"outfit"}`))
	model, reason := ScanModel(mk)
	if model == nil {
		t.Fatalf("reason: %s", reason)
	}
	if model.Name != "Pretty Name" || model.Kind != "scene" || model.Skin != "outfit" {
		t.Fatalf("got %+v", model)
	}
	if len(model.HideSlots) != 1 || model.HideSlots[0] != "bg" {
		t.Fatalf("hideSlots: %v", model.HideSlots)
	}
}

func TestScanDuplicateWinsMain(t *testing.T) {
	root := t.TempDir()
	models := filepath.Join(root, "models")
	extra := filepath.Join(root, "extra")
	configFile := filepath.Join(root, "config.json")
	for _, d := range []string{models, extra} {
		mk := filepath.Join(d, "dup")
		writeFile(t, filepath.Join(mk, "dup.skel"), binarySkel("4.1.56", "4x"))
		writeFile(t, filepath.Join(mk, "dup.atlas"), []byte("page.png\nbody\n"))
	}
	writeFile(t, configFile, []byte(`{"additional_model_dirs":["`+extra+`"]}`))
	result := Scan(models, configFile, func(string) {})
	if len(result.Models) != 1 || result.Models[0].ID != "dup" {
		t.Fatalf("models: %+v", result.Models)
	}
	if result.Dirs["dup"] != filepath.Join(models, "dup") {
		t.Fatalf("main dir did not win: %v", result.Dirs)
	}
	found := false
	for _, reason := range result.Skipped {
		if substr(reason, "duplicate model ID") {
			found = true
		}
	}
	if !found {
		t.Fatalf("skipped: %v", result.Skipped)
	}
}

func TestLoadExtraDirs(t *testing.T) {
	root := t.TempDir()
	models := filepath.Join(root, "models")
	extra := filepath.Join(root, "extra")
	if err := os.MkdirAll(models, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(extra, 0o755); err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(root, "config.json")

	writeFile(t, configFile, []byte("{not json"))
	logs := []string{}
	if dirs := LoadExtraDirs(models, configFile, func(s string) { logs = append(logs, s) }); len(dirs) != 0 {
		t.Fatalf("bad json dirs: %v", dirs)
	}
	if len(logs) != 1 || !substr(logs[0], "not valid JSON") {
		t.Fatalf("logs: %v", logs)
	}

	writeFile(t, configFile, []byte(`{"additional_model_dirs":"`+extra+`"}`))
	logs = nil
	LoadExtraDirs(models, configFile, func(s string) { logs = append(logs, s) })
	if len(logs) != 1 || !substr(logs[0], "must be a list") {
		t.Fatalf("logs: %v", logs)
	}

	writeFile(t, configFile, []byte(`{"additional_model_dirs":["relative/dir","`+filepath.Join(root, "absent")+`",5,null]}`))
	logs = nil
	LoadExtraDirs(models, configFile, func(s string) { logs = append(logs, s) })
	if len(logs) != 4 {
		t.Fatalf("skipped logs: %v", logs)
	}

	writeFile(t, configFile, []byte(`{"additional_model_dirs":["`+extra+`","`+models+`"]}`))
	dirs := LoadExtraDirs(models, configFile, func(string) {})
	if len(dirs) != 1 || dirs[0] != extra {
		t.Fatalf("dirs: %v", dirs)
	}
}
