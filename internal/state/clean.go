package state

import (
	"regexp"
	"strings"
)

const (
	animationMaxChars  = 200
	presetMax          = 200
	presetNameMaxChars = 40
	screenKeyMaxChars  = 60
	recentMax          = 20
	positionMax        = 2.0
	scaleMin           = 0.3
	scaleMax           = 8.0
)

var uidPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

// prefChoices are the allowed values per prefs key.
var prefChoices = map[string][]string{
	"layout": {"grid", "list"},
	"sort":   {"name", "recent", "source"},
	"kind":   {"character", "scene", "all"},
}

// prefKeyOrder fixes the order the choices are applied in, as the Python dict does.
var prefKeyOrder = []string{"layout", "sort", "kind"}

func choiceAllowed(key, value string) bool {
	for _, allowed := range prefChoices[key] {
		if allowed == value {
			return true
		}
	}
	return false
}

// animationOrNil returns a valid animation name or nil.
func animationOrNil(v any) *string {
	s, ok := v.(string)
	if !ok || s == "" || truncateRunes(s, animationMaxChars) != s {
		return nil
	}
	return &s
}

// cleanCharacters keeps the valid characters of raw, in z order.
//
// An entry with a uid outside allowedUIDs is dropped, so a stale page cannot
// bring back a removed character. An entry without a uid is new and gets one.
// knownOnly=false keeps a saved preset's model that is missing now.
func cleanCharacters(raw any, ids map[string]bool, allowedUIDs map[string]bool, knownOnly bool) []Character {
	list, ok := asList(raw)
	if !ok {
		return []Character{}
	}
	cleaned := []Character{}
	seen := map[string]bool{}
	for _, entry := range list {
		obj, ok := asObject(entry)
		if !ok {
			continue
		}
		model, ok := obj["model"].(string)
		if !ok || model == "" || (knownOnly && !ids[model]) {
			continue
		}
		var uid string
		if rawUID, present := obj["uid"]; present && rawUID != nil {
			uidStr, ok := rawUID.(string)
			if !ok || !uidPattern.MatchString(uidStr) || seen[uidStr] {
				continue
			}
			if allowedUIDs != nil && !allowedUIDs[uidStr] {
				continue
			}
			uid = uidStr
		} else {
			taken := map[string]bool{}
			for k := range seen {
				taken[k] = true
			}
			for k := range allowedUIDs {
				taken[k] = true
			}
			uid = newUID(taken)
		}
		seen[uid] = true
		cleaned = append(cleaned, Character{
			UID:       uid,
			Model:     model,
			Animation: animationOrNil(obj["animation"]),
			X:         clamp(obj["x"], -positionMax, positionMax, 0.0),
			Y:         clamp(obj["y"], -positionMax, positionMax, 0.0),
			Scale:     clamp(obj["scale"], scaleMin, scaleMax, 1.0),
			Auto:      isTrue(obj["auto"]),
		})
	}
	return cleaned
}

// cleanScreenKey returns a screen key, or nil when the value is unusable.
// The empty key is the default stage, which is printable, so it passes.
func cleanScreenKey(value any) (string, bool) {
	s, ok := value.(string)
	if !ok || !isPrintable(s) {
		return "", false
	}
	if len([]rune(s)) > screenKeyMaxChars {
		return "", false
	}
	return s, true
}

// cleanPresetName returns a trimmed printable name up to the cap, or nil.
func cleanPresetName(value any) *string {
	s, ok := value.(string)
	if !ok {
		return nil
	}
	name := strings.TrimSpace(s)
	if name == "" || !isPrintable(name) {
		return nil
	}
	if len([]rune(name)) > presetNameMaxChars {
		return nil
	}
	return &name
}

// cleanStage builds a full stage from stored JSON. A key that is missing or
// wrong falls back to its default.
func cleanStage(saved any, ids map[string]bool, firstCharacter *string) *Stage {
	stage := &Stage{
		Model:      cloneStr(firstCharacter),
		Animation:  nil,
		Rev:        0,
		Scale:      1.0,
		X:          0.0,
		Y:          0.0,
		Vrev:       0,
		Auto:       false,
		Preset:     nil,
		Characters: []Character{},
		Crev:       0,
	}
	obj, ok := asObject(saved)
	if !ok {
		return stage
	}
	// An empty list is single-character mode. Only a non-empty list switches the page to the shared stage.
	stage.Characters = cleanCharacters(obj["characters"], ids, nil, true)
	stage.Crev = max0(plainInt(obj["crev"], 0))
	stage.Preset = cleanPresetName(obj["preset"])

	model, ok := obj["model"].(string)
	if !ok || !ids[model] {
		return stage
	}
	stage.Model = strPtr(model)
	stage.Animation = animationOrNil(obj["animation"])
	stage.Rev = max0(plainInt(obj["rev"], 0))
	stage.Auto = isTrue(obj["auto"])
	stage.Vrev = max0(plainInt(obj["vrev"], 0))
	stage.Scale = clamp(obj["scale"], scaleMin, scaleMax, 1.0)
	stage.X = clamp(obj["x"], -positionMax, positionMax, 0.0)
	stage.Y = clamp(obj["y"], -positionMax, positionMax, 0.0)
	return stage
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

// cleanPreset returns a stored preset, or nil. Uids are dropped.
func cleanPreset(raw any, ids map[string]bool) *Preset {
	obj, ok := asObject(raw)
	if !ok {
		return nil
	}
	name := cleanPresetName(obj["name"])
	chars := cleanCharacters(obj["characters"], ids, nil, false)
	presetChars := make([]PresetChar, 0, len(chars))
	for _, c := range chars {
		presetChars = append(presetChars, PresetChar{
			Model:     c.Model,
			Animation: c.Animation,
			X:         c.X,
			Y:         c.Y,
			Scale:     c.Scale,
			Auto:      c.Auto,
		})
	}
	if name == nil || len(presetChars) == 0 {
		return nil
	}
	return &Preset{Name: *name, Characters: presetChars}
}

// cleanPresets keeps valid presets, drops duplicate names and caps the list.
func cleanPresets(raw any, ids map[string]bool) []Preset {
	list, ok := asList(raw)
	if !ok {
		return []Preset{}
	}
	presets := []Preset{}
	names := map[string]bool{}
	for _, entry := range list {
		preset := cleanPreset(entry, ids)
		if preset != nil && !names[preset.Name] && len(presets) < presetMax {
			names[preset.Name] = true
			presets = append(presets, *preset)
		}
	}
	return presets
}

// DefaultPrefs returns the prefs a fresh install starts with.
func DefaultPrefs() Prefs {
	return Prefs{
		Favorites: []string{},
		Recent:    []string{},
		Layout:    "grid",
		Sort:      "name",
		Kind:      "character",
		Variants:  true,
		Presets:   []Preset{},
	}
}

// cleanPrefs builds prefs from stored JSON.
func cleanPrefs(saved any, ids map[string]bool) Prefs {
	prefs := DefaultPrefs()
	obj, ok := asObject(saved)
	if !ok {
		return prefs
	}
	for _, key := range []string{"favorites", "recent"} {
		if list, ok := asList(obj[key]); ok {
			values := []string{}
			for _, item := range list {
				if s, ok := item.(string); ok {
					values = append(values, s)
				}
			}
			if key == "favorites" {
				prefs.Favorites = values
			} else {
				prefs.Recent = values
			}
		}
	}
	for _, key := range prefKeyOrder {
		if s, ok := obj[key].(string); ok && choiceAllowed(key, s) {
			switch key {
			case "layout":
				prefs.Layout = s
			case "sort":
				prefs.Sort = s
			case "kind":
				prefs.Kind = s
			}
		}
	}
	if b, ok := obj["variants"].(bool); ok {
		prefs.Variants = b
	}
	prefs.Presets = cleanPresets(obj["presets"], ids)
	return prefs
}

// ParseScreen validates a screen key from a request. It mirrors clean_screen_key:
// the value must be at most 60 characters and printable. The empty key is the
// default stage and passes; the caller tells absence apart from emptiness.
func ParseScreen(value string) (string, bool) {
	return cleanScreenKey(value)
}
