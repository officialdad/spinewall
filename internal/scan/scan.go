// Package scan finds Spine models on disk the same way the Python server does.
//
// The Python server is the reference. Every rule here, including the order of
// checks and the reason strings, mirrors server/spinewall-server.py so the two
// servers pick the same models and report the same skips.
package scan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// RuntimeByVersion maps a model's Spine editor version to the player that can load it.
var RuntimeByVersion = map[string]string{
	"4.0": "spine40",
	"4.1": "spine41",
	"4.2": "spine42",
	"3.8": "spine38",
	"3.7": "spine37",
}

// Kinds are the values model.json may name for a model's kind.
var Kinds = map[string]bool{"character": true, "scene": true}

var (
	sceneName      = regexp.MustCompile(`(?i)^(bg|background|scene|diorama)|bg_`)
	versionPattern = regexp.MustCompile(`^\d+\.\d+(\.\d+)?`)
	variantSuffix  = regexp.MustCompile(`^(.+?)[_-]\d+$`)
)

// Model is one scanned model directory. Field order matches the Python dict.
type Model struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Group            *string  `json:"group"`
	Character        *string  `json:"character"`
	Skeleton         string   `json:"skeleton"`
	Atlas            string   `json:"atlas"`
	Version          *string  `json:"version"`
	Runtime          string   `json:"runtime"`
	Skin             string   `json:"skin"`
	Kind             string   `json:"kind"`
	HideSlots        []string `json:"hideSlots"`
	DefaultAnimation string   `json:"defaultAnimation"`
}

// Result is one scan: the models, what was skipped and why, and the id to directory map.
type Result struct {
	Models  []Model
	Skipped map[string]string
	Dirs    map[string]string
	IDs     []string
}

// ReadBinaryVersion reads the version string from a binary .skel header.
//
// 4.x stores an 8-byte hash then a string. 3.x stores the hash as a string too,
// so the version starts after it. Both layouts are tried, as the Python does.
func ReadBinaryVersion(path string) *string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	head := make([]byte, 64)
	n, err := io.ReadFull(f, head)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return nil
	}
	head = head[:n]
	starts := []int{8}
	if n > 0 && head[0] > 0 && head[0] < 0x80 {
		starts = append(starts, int(head[0]))
	}
	for _, start := range starts {
		length := 0
		if start < n {
			length = int(head[start]) - 1
		}
		if length <= 0 || length >= 32 {
			continue
		}
		lo := start + 1
		if lo > n {
			continue
		}
		hi := lo + length
		if hi > n {
			hi = n
		}
		text := asciiReplace(head[lo:hi])
		if versionPattern.MatchString(text) {
			v := text
			return &v
		}
	}
	return nil
}

func asciiReplace(b []byte) string {
	var sb strings.Builder
	for _, c := range b {
		if c < 0x80 {
			sb.WriteByte(c)
		} else {
			sb.WriteRune('\uFFFD')
		}
	}
	return sb.String()
}

// decodeJSONHead decodes a JSON file into target when the top level is an
// object, and reports whether it was an object. Python treats a non-object top
// level as "no data" rather than an error, so a valid array or scalar returns
// false, nil. Malformed JSON still returns an error.
func decodeJSONHead(data []byte, target any) (bool, error) {
	trimmed := bytesTrimLeft(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		var probe any
		if err := json.Unmarshal(data, &probe); err != nil {
			return false, err
		}
		return false, nil
	}
	if err := json.Unmarshal(data, target); err != nil {
		return false, err
	}
	return true, nil
}

func bytesTrimLeft(data []byte) []byte {
	return bytes.TrimLeft(data, " \t\r\n")
}

// ReadJSONVersion reads skeleton.spine or skeleton.version from a JSON export.
//
// It decodes only the header, so a large skeleton's bone and animation arrays
// are skipped instead of built into maps. That keeps a 974-model scan fast.
func ReadJSONVersion(path string) (*string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var head struct {
		Skeleton json.RawMessage `json:"skeleton"`
	}
	isObject, err := decodeJSONHead(data, &head)
	if err != nil {
		return nil, err
	}
	if !isObject || len(head.Skeleton) == 0 || string(head.Skeleton) == "null" {
		return nil, nil
	}
	var skeleton struct {
		Spine   json.RawMessage `json:"spine"`
		Version json.RawMessage `json:"version"`
	}
	if bytesTrimLeft(head.Skeleton)[0] != '{' {
		return nil, nil
	}
	if err := json.Unmarshal(head.Skeleton, &skeleton); err != nil {
		return nil, err
	}
	raw := skeleton.Spine
	if !rawTruthy(raw) {
		raw = skeleton.Version
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	s := pyStr(v)
	return &s, nil
}

// rawTruthy reports whether a raw JSON value is truthy in Python's sense.
func rawTruthy(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return false
	}
	return truthy(v)
}

// ReadAtlasRegions lists the region names in an .atlas file.
//
// A region is an unindented line without a colon. A page line ends in an image
// extension and is skipped.
func ReadAtlasRegions(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, line := range strings.Split(string(data), "\n") {
		text := strings.TrimRight(line, "\r\n")
		if text == "" || text[0] == ' ' || text[0] == '\t' || strings.Contains(text, ":") {
			continue
		}
		lower := strings.ToLower(text)
		if strings.HasSuffix(lower, ".png") || strings.HasSuffix(lower, ".jpg") ||
			strings.HasSuffix(lower, ".jpeg") || strings.HasSuffix(lower, ".webp") {
			continue
		}
		names = append(names, text)
	}
	return names, nil
}

// ReadJSONSlots lists the slot names in a JSON export.
//
// Like ReadJSONVersion, it decodes only the slots array, so the large bones and
// animation arrays never become maps.
func ReadJSONSlots(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var head struct {
		Slots []struct {
			Name *string `json:"name"`
		} `json:"slots"`
	}
	isObject, err := decodeJSONHead(data, &head)
	if err != nil {
		return nil, err
	}
	if !isObject {
		return nil, nil
	}
	var names []string
	for _, s := range head.Slots {
		if s.Name != nil {
			names = append(names, *s.Name)
		}
	}
	return names, nil
}

// ClassifyKind decides character or scene from region names.
//
// Bare "back" is left out on purpose: F_Coat_Back and similar are clothing parts.
func ClassifyKind(names []string) string {
	if len(names) == 0 {
		return "character"
	}
	hits := 0
	for _, n := range names {
		if sceneName.MatchString(n) {
			hits++
		}
	}
	if hits*2 > len(names) {
		return "scene"
	}
	return "character"
}

// textOr returns value when it is a non-empty string, else def.
func textOr(value any, def string) string {
	if s, ok := value.(string); ok && s != "" {
		return s
	}
	return def
}

// textOrNil returns a pointer to a non-empty string, else nil.
func textOrNil(value any) *string {
	if s, ok := value.(string); ok && s != "" {
		return &s
	}
	return nil
}

// isPlainFilename reports whether an override names a file in the model directory.
func isPlainFilename(value any) bool {
	s, ok := value.(string)
	if !ok {
		return false
	}
	return s != "" && s != "." && s != ".." && !strings.Contains(s, "/") && !strings.Contains(s, "\\")
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// ScanModel reads one model directory. It returns (model, "") or (nil, reason).
func ScanModel(dir string) (*Model, string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err.Error()
	}
	files := make([]string, 0, len(entries))
	for _, e := range entries {
		files = append(files, e.Name())
	}
	sort.Strings(files)

	overrides := map[string]any{}
	if contains(files, "model.json") {
		data, err := os.ReadFile(filepath.Join(dir, "model.json"))
		if err != nil {
			return nil, err.Error()
		}
		root, err := decode(data)
		if err != nil {
			return nil, "JSONDecodeError: " + err.Error()
		}
		obj, ok := root.(map[string]any)
		if !ok {
			return nil, "model.json must be a JSON object"
		}
		overrides = obj
	}
	for _, key := range []string{"skeleton", "atlas"} {
		if v, ok := overrides[key]; ok && v != nil && !isPlainFilename(v) {
			return nil, fmt.Sprintf("model.json %s must be a file name in the model directory", key)
		}
	}

	var skeleton string
	if s := textOr(overrides["skeleton"], ""); s != "" {
		skeleton = s
	} else {
		skeleton = firstWithSuffix(files, ".skel", "")
		if skeleton == "" {
			skeleton = firstWithSuffix(files, ".json", "model.json")
		}
	}
	var atlas string
	if a := textOr(overrides["atlas"], ""); a != "" {
		atlas = a
	} else {
		atlas = firstWithSuffix(files, ".atlas", "")
	}
	if skeleton == "" || atlas == "" {
		return nil, "needs one skeleton (.skel or .json) and one .atlas file"
	}
	skelPath := filepath.Join(dir, skeleton)
	atlasPath := filepath.Join(dir, atlas)
	if !isFile(skelPath) || !isFile(atlasPath) {
		return nil, "the skeleton or atlas named in model.json does not exist"
	}

	var version *string
	if strings.HasSuffix(skeleton, ".json") {
		v, err := ReadJSONVersion(skelPath)
		if err != nil {
			return nil, "JSONDecodeError: " + err.Error()
		}
		version = v
	} else {
		version = ReadBinaryVersion(skelPath)
	}
	versionStr := ""
	if version != nil {
		versionStr = *version
	}
	runtime := RuntimeByVersion[firstTwo(versionStr)]
	if runtime == "" {
		return nil, fmt.Sprintf("unsupported Spine version %s, supported: 3.7.x, 3.8.x, 4.0.x, 4.1.x, 4.2.x", pyRepr(version))
	}
	// The 3.7 web player has no binary reader, so only a JSON export can play.
	if runtime == "spine37" && strings.HasSuffix(skeleton, ".skel") {
		return nil, "Spine 3.7 needs a JSON export, because the 3.7 player cannot read .skel files"
	}

	modelID := filepath.Base(dir)
	kind := ""
	if k, ok := overrides["kind"].(string); ok && Kinds[k] {
		kind = k
	}
	if kind == "" {
		var names []string
		if strings.HasSuffix(skeleton, ".json") {
			names, _ = ReadJSONSlots(skelPath)
		}
		if len(names) == 0 {
			names, _ = ReadAtlasRegions(atlasPath)
		}
		kind = ClassifyKind(names)
	}
	var hideSlots []string
	if raw, ok := overrides["hideSlots"].([]any); ok {
		for _, s := range raw {
			if str, ok := s.(string); ok && str != "" {
				hideSlots = append(hideSlots, str)
			}
		}
	}
	if hideSlots == nil {
		hideSlots = []string{}
	}

	return &Model{
		ID:               modelID,
		Name:             textOr(overrides["name"], modelID),
		Group:            textOrNil(overrides["group"]),
		Character:        textOrNil(overrides["character"]),
		Skeleton:         skeleton,
		Atlas:            atlas,
		Version:          version,
		Runtime:          runtime,
		Skin:             textOr(overrides["skin"], "default"),
		Kind:             kind,
		HideSlots:        hideSlots,
		DefaultAnimation: textOr(overrides["defaultAnimation"], "idle"),
	}, ""
}

func contains(list []string, value string) bool {
	for _, s := range list {
		if s == value {
			return true
		}
	}
	return false
}

// firstWithSuffix returns the first sorted name ending in suffix, skipping one exact name.
func firstWithSuffix(files []string, suffix, skip string) string {
	for _, name := range files {
		if name == skip {
			continue
		}
		if strings.HasSuffix(name, suffix) {
			return name
		}
	}
	return ""
}

func firstTwo(version string) string {
	parts := strings.Split(version, ".")
	if len(parts) > 2 {
		parts = parts[:2]
	}
	return strings.Join(parts, ".")
}

func pyRepr(version *string) string {
	if version == nil {
		return "None"
	}
	return "'" + *version + "'"
}

// AssignCharacters fills each model's character grouping.
//
// A stem counts only when it names a shared character, so a lone "x_01" is not renamed.
func AssignCharacters(models []Model) {
	ids := map[string]bool{}
	stems := map[string][]int{}
	for i := range models {
		ids[models[i].ID] = true
		if m := variantSuffix.FindStringSubmatch(models[i].ID); m != nil {
			stems[m[1]] = append(stems[m[1]], i)
		}
	}
	for i := range models {
		if models[i].Character != nil {
			continue
		}
		m := variantSuffix.FindStringSubmatch(models[i].ID)
		stem := ""
		if m != nil {
			stem = m[1]
		}
		shared := stem != "" && (len(stems[stem]) >= 2 || ids[stem])
		value := models[i].ID
		if shared {
			value = stem
		}
		v := value
		models[i].Character = &v
	}
}

// FirstCharacterID returns the first character model's id, or the first model's id, or nil.
func FirstCharacterID(models []Model) *string {
	for i := range models {
		if models[i].Kind == "character" {
			id := models[i].ID
			return &id
		}
	}
	if len(models) > 0 {
		id := models[0].ID
		return &id
	}
	return nil
}

// LoadExtraDirs reads additional_model_dirs from the config file.
//
// Only the user's own config file can add a directory, never a request.
func LoadExtraDirs(modelsDir, configFile string, logf func(string)) []string {
	data, err := os.ReadFile(configFile)
	if err != nil {
		return nil
	}
	root, err := decode(data)
	if err != nil {
		logf(fmt.Sprintf("spinewall: %s is not valid JSON, so additional_model_dirs is ignored", configFile))
		return nil
	}
	obj, ok := root.(map[string]any)
	if !ok {
		return nil
	}
	raw, ok := obj["additional_model_dirs"]
	if !ok || raw == nil {
		return nil
	}
	list, ok := raw.([]any)
	if !ok {
		logf("spinewall: additional_model_dirs must be a list of paths, so it is ignored")
		return nil
	}
	var dirs []string
	for _, entry := range list {
		path := ""
		if s, ok := entry.(string); ok {
			path = ExpandUser(s)
		}
		if !filepath.IsAbs(path) || !isDir(path) {
			logf(fmt.Sprintf("spinewall: additional_model_dirs entry %s is not an existing absolute directory, skipped", pyReprAny(entry)))
		} else if realpath(path) != realpath(modelsDir) && !contains(dirs, path) {
			dirs = append(dirs, path)
		}
	}
	return dirs
}

// ExpandUser expands a leading ~ to the home directory, as Python's expanduser does.
func ExpandUser(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[1:])
		}
	}
	return path
}

func realpath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			return resolved
		}
		return abs
	}
	return path
}

// Scan walks the main model directory and any extra directories.
func Scan(modelsDir, configFile string, logf func(string)) Result {
	models := []Model{}
	skipped := map[string]string{}
	dirs := map[string]string{}

	roots := append([]string{modelsDir}, LoadExtraDirs(modelsDir, configFile, logf)...)
	for _, root := range roots {
		if !isDir(root) {
			continue
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		sort.Strings(names)
		// The first root wins on a duplicate id, so the main directory keeps its own models.
		for _, name := range names {
			directory := filepath.Join(root, name)
			if !isDir(directory) {
				continue
			}
			label := name
			if root != modelsDir {
				label = directory
			}
			if prev, ok := dirs[name]; ok {
				reason := "duplicate model ID, " + prev + " already has it"
				skipped[label] = reason
				logf("skipped model " + label + ": " + reason)
				continue
			}
			model, reason := ScanModel(directory)
			if model != nil {
				models = append(models, *model)
				dirs[name] = directory
			} else {
				skipped[label] = reason
				logf("skipped model " + label + ": " + reason)
			}
		}
	}

	AssignCharacters(models)
	ids := make([]string, len(models))
	for i := range models {
		ids[i] = models[i].ID
	}
	return Result{Models: models, Skipped: skipped, Dirs: dirs, IDs: ids}
}
