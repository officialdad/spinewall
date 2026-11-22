package state

import (
	"math/rand/v2"
	"strings"
)

// Arg is a command argument: either one string or a pair of names.
type Arg struct {
	str  string
	pair [2]string
	kind argKind
}

type argKind int

const (
	argNone argKind = iota
	argString
	argPair
)

// StringArg is a single string argument, or empty for none.
func StringArg(s string) Arg {
	if s == "" {
		return Arg{}
	}
	return Arg{str: s, kind: argString}
}

// PairArg is the two names a rename-preset takes.
func PairArg(a, b string) Arg {
	return Arg{pair: [2]string{a, b}, kind: argPair}
}

func (a Arg) hasString() bool { return a.kind == argString }
func (a Arg) isPair() bool    { return a.kind == argPair }

// randomID picks a random scanned model id.
func (s *Store) randomID() string {
	return s.ids[rand.IntN(len(s.ids))]
}

func (s *Store) randomInt(n int) int {
	return rand.IntN(n)
}

// savePreset stores the current layout under a name. The same name replaces its
// preset in place. The caller holds the lock.
func (s *Store) savePreset(stage *Stage, rawName string) bool {
	name := cleanPresetName(rawName)
	if name == nil {
		return false
	}
	preset := buildPreset(*name, currentCharacters(stage), s.idsSet)
	if preset == nil {
		return false
	}
	index := -1
	for i, p := range s.prefs.Presets {
		if p.Name == *name {
			index = i
			break
		}
	}
	if index < 0 && len(s.prefs.Presets) >= presetMax {
		return false
	}
	if index < 0 {
		s.prefs.Presets = append(s.prefs.Presets, *preset)
	} else {
		s.prefs.Presets[index] = *preset
	}
	s.savePrefs()
	// The stage now matches this preset, so a cycle continues from it.
	stage.Preset = strPtr(*name)
	s.save(stage)
	return true
}

// buildPreset cleans a preset from characters that have no uids yet.
func buildPreset(name string, chars []PresetChar, ids map[string]bool) *Preset {
	raw := make([]any, 0, len(chars))
	for _, c := range chars {
		raw = append(raw, map[string]any{
			"model":     c.Model,
			"animation": ptrAny(c.Animation),
			"x":         c.X,
			"y":         c.Y,
			"scale":     c.Scale,
			"auto":      c.Auto,
		})
	}
	cleaned := cleanCharacters(raw, ids, nil, false)
	presetChars := make([]PresetChar, 0, len(cleaned))
	for _, c := range cleaned {
		presetChars = append(presetChars, PresetChar{
			Model:     c.Model,
			Animation: c.Animation,
			X:         c.X,
			Y:         c.Y,
			Scale:     c.Scale,
			Auto:      c.Auto,
		})
	}
	if len(presetChars) == 0 {
		return nil
	}
	return &Preset{Name: name, Characters: presetChars}
}

// updatePreset overwrites an existing preset. An unknown name creates nothing.
func (s *Store) updatePreset(stage *Stage, name string) bool {
	for _, p := range s.prefs.Presets {
		if p.Name == name {
			return s.savePreset(stage, name)
		}
	}
	return false
}

// renamePreset renames in place, so the cycle order stays.
func (s *Store) renamePreset(old, rawNew string) bool {
	newName := cleanPresetName(rawNew)
	index := -1
	for i, p := range s.prefs.Presets {
		if p.Name == old {
			index = i
			break
		}
	}
	if index < 0 || newName == nil {
		return false
	}
	if *newName == old {
		return true
	}
	for _, p := range s.prefs.Presets {
		if p.Name == *newName {
			return false
		}
	}
	s.prefs.Presets[index].Name = *newName
	s.savePrefs()
	for _, stage := range s.allStages() {
		if stage.Preset != nil && *stage.Preset == old {
			stage.Preset = strPtr(*newName)
			s.save(stage)
		}
	}
	return true
}

// deletePreset removes a preset and clears its pointer on every stage.
func (s *Store) deletePreset(name string) bool {
	kept := make([]Preset, 0, len(s.prefs.Presets))
	for _, p := range s.prefs.Presets {
		if p.Name != name {
			kept = append(kept, p)
		}
	}
	if len(kept) == len(s.prefs.Presets) {
		return false
	}
	s.prefs.Presets = kept
	s.savePrefs()
	for _, stage := range s.allStages() {
		if stage.Preset != nil && *stage.Preset == name {
			stage.Preset = nil
			s.save(stage)
		}
	}
	return true
}

// loadPreset replaces the layout with a preset. A model that is gone is skipped,
// and no model left changes nothing.
func (s *Store) loadPreset(stage *Stage, name string) bool {
	var preset *Preset
	for i := range s.prefs.Presets {
		if s.prefs.Presets[i].Name == name {
			preset = &s.prefs.Presets[i]
			break
		}
	}
	chars := []Character{}
	if preset != nil {
		chars = presetCharacters(preset, s.idsSet)
	}
	if len(chars) == 0 {
		return false
	}
	stage.Preset = strPtr(name)
	if len(chars) == 1 {
		// One character is the single-character layout, so the old keys carry it.
		only := chars[0]
		stage.Model = strPtr(only.Model)
		stage.Animation = cloneStr(only.Animation)
		stage.Auto = only.Auto
		stage.Rev++
		stage.Scale = only.Scale
		stage.X = only.X
		stage.Y = only.Y
		stage.Vrev++
		s.save(stage)
		s.recordRecent(only.Model)
		chars = []Character{}
	}
	s.setCharacters(stage, chars)
	// The multi path writes only the layout file, so the preset name needs its own save.
	s.save(stage)
	return true
}

// presetCharacters cleans a preset's characters into fresh stage characters.
func presetCharacters(preset *Preset, ids map[string]bool) []Character {
	raw := make([]any, 0, len(preset.Characters))
	for _, c := range preset.Characters {
		raw = append(raw, map[string]any{
			"model":     c.Model,
			"animation": ptrAny(c.Animation),
			"x":         c.X,
			"y":         c.Y,
			"scale":     c.Scale,
			"auto":      c.Auto,
		})
	}
	return cleanCharacters(raw, ids, nil, true)
}

// stepPreset loads the next or previous preset after the one this stage loaded
// last, skipping a preset whose models are all gone.
func (s *Store) stepPreset(stage *Stage, step int) {
	names := make([]string, len(s.prefs.Presets))
	for i, p := range s.prefs.Presets {
		names[i] = p.Name
	}
	if len(names) == 0 {
		return
	}
	start := -1
	if stage.Preset != nil {
		for i, n := range names {
			if n == *stage.Preset {
				start = i
				break
			}
		}
	}
	if start < 0 {
		if step > 0 {
			start = -1
		} else {
			start = 0
		}
	}
	for offset := 1; offset <= len(names); offset++ {
		if s.loadPreset(stage, names[mod(start+step*offset, len(names))]) {
			return
		}
	}
}

// runCommand runs one command on one stage. It assumes the caller holds the lock
// and has already checked that there is at least one model.
func (s *Store) runCommand(stage *Stage, cmd string, arg Arg) {
	if len(s.ids) == 0 {
		return
	}
	current := deref(stage.Model)
	switch cmd {
	case "zoom-in":
		s.setView(stage, stage.Scale*1.1, nil, nil)
	case "zoom-out":
		s.setView(stage, stage.Scale/1.1, nil, nil)
	case "move-left":
		s.setView(stage, nil, stage.X-0.05, stage.Y)
	case "move-right":
		s.setView(stage, nil, stage.X+0.05, stage.Y)
	case "move-up":
		s.setView(stage, nil, stage.X, stage.Y-0.05)
	case "move-down":
		s.setView(stage, nil, stage.X, stage.Y+0.05)
	case "reset-view":
		s.setView(stage, 1.0, 0.0, 0.0)
	case "next-model":
		s.changeModel(stage, s.ids[mod(indexOf(s.ids, current)+1, len(s.ids))])
	case "prev-model":
		s.changeModel(stage, s.ids[mod(indexOf(s.ids, current)-1, len(s.ids))])
	case "random-model":
		s.changeModel(stage, s.randomID())
	case "set-model":
		if arg.hasString() && s.idsSet[arg.str] {
			s.changeModel(stage, arg.str)
		}
	case "next-anim", "prev-anim", "random-anim":
		names := s.anims[current]
		if len(names) == 0 {
			return
		}
		i := -1
		if stage.Animation != nil {
			for idx, n := range names {
				if n == *stage.Animation {
					i = idx
					break
				}
			}
		}
		switch cmd {
		case "next-anim":
			s.changeAnimation(stage, names[mod(i+1, len(names))])
		case "prev-anim":
			s.changeAnimation(stage, names[mod(i-1, len(names))])
		default:
			s.changeAnimation(stage, names[s.randomInt(len(names))])
		}
	case "set-anim":
		if arg.hasString() {
			s.changeAnimation(stage, truncateRunes(arg.str, animationMaxChars))
		}
	case "add-character":
		s.addCharacter(stage, arg.str)
	case "save-preset":
		if arg.hasString() {
			s.savePreset(stage, arg.str)
		}
	case "update-preset":
		if arg.hasString() {
			s.updatePreset(stage, arg.str)
		}
	case "rename-preset":
		if arg.isPair() {
			s.renamePreset(arg.pair[0], arg.pair[1])
		}
	case "load-preset":
		if arg.hasString() {
			s.loadPreset(stage, arg.str)
		}
	case "next-preset":
		s.stepPreset(stage, 1)
	case "prev-preset":
		s.stepPreset(stage, -1)
	case "delete-preset":
		if arg.hasString() {
			s.deletePreset(arg.str)
		}
	case "clear-characters":
		if len(stage.Characters) > 0 {
			s.setCharacters(stage, []Character{})
		}
	case "remove-character":
		if arg.hasString() {
			s.removeCharacter(stage, arg.str)
		}
	case "next-char-anim":
		if arg.hasString() {
			s.cycleCharacterAnimation(stage, arg.str, 1)
		}
	case "prev-char-anim":
		if arg.hasString() {
			s.cycleCharacterAnimation(stage, arg.str, -1)
		}
	case "auto-cycle":
		if arg.hasString() {
			s.setAutoCycle(stage, arg.str)
		}
	case "set-char-anim", "set-char-model":
		if arg.hasString() {
			uid, value := splitOnce(arg.str, "/")
			if cmd == "set-char-anim" && value != "" {
				v := truncateRunes(value, animationMaxChars)
				s.updateCharacter(stage, uid, func(c *Character) { c.Animation = &v })
			} else if cmd == "set-char-model" && s.idsSet[value] {
				s.updateCharacter(stage, uid, func(c *Character) { c.Model = value; c.Animation = nil })
			}
		}
	}
}

func indexOf(list []string, value string) int {
	for i, v := range list {
		if v == value {
			return i
		}
	}
	return -1
}

// splitOnce splits on the first occurrence of sep, as Python's partition does.
func splitOnce(s, sep string) (string, string) {
	i := strings.Index(s, sep)
	if i < 0 {
		return s, ""
	}
	return s[:i], s[i+len(sep):]
}

// onceCommands act on the shared preset list, so one run is enough.
var onceCommands = map[string]bool{
	"save-preset":   true,
	"update-preset": true,
	"rename-preset": true,
	"delete-preset": true,
}
