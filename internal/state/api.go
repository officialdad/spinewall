package state

import (
	"os"
	"path/filepath"

	"github.com/officialdad/spinewall/internal/scan"
)

// Everything here locks the store, does its work, and returns copies of the
// state it touched. No *Stage escapes the lock, so a concurrent request never
// sees a stage being mutated mid-reply.

// StateFor resolves the stage a request names and builds the /state reply.
//
// A page may create a stage (create=true). A CLI with a mistyped name gets a
// 404 instead. With no screen named, the default stage answers.
func (s *Store) StateFor(screen string, hasScreen, create bool) (reply StateReply, status int, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stage, status, msg := s.resolveStage(screen, hasScreen, create)
	if stage == nil {
		return StateReply{}, status, msg
	}
	return StateReply{Stage: *stage.clone(), Edit: s.edit}, 0, ""
}

// resolveStage returns the stage a request names, assuming the lock is held.
func (s *Store) resolveStage(screen string, hasScreen, create bool) (*Stage, int, string) {
	if !hasScreen {
		return s.defaultStage, 0, ""
	}
	stage := s.screenStage(screen, create)
	if stage == nil {
		if create {
			return nil, 400, "at most 16 screens"
		}
		return nil, 404, "unknown screen"
	}
	return stage, 0, ""
}

// CommandReply runs a command for a request that may name a screen and returns
// the stage it acted on. The reply is the bare stage, with no edit flag; only
// /state adds that.
//
// With no screen it reaches every stage and reports the default stage. With one
// it reaches that stage alone and answers 404 for a name it does not know.
func (s *Store) CommandReply(screen string, hasScreen bool, cmd string, arg Arg) (reply Stage, status int, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !hasScreen {
		targets := s.allStages()
		if onceCommands[cmd] && len(targets) > 0 {
			targets = targets[:1]
		}
		for _, stage := range targets {
			s.runCommand(stage, cmd, arg)
		}
		return *s.defaultStage.clone(), 0, ""
	}
	stage := s.screenStage(screen, false)
	if stage == nil {
		return Stage{}, 404, "unknown screen"
	}
	s.runCommand(stage, cmd, arg)
	s.noteChosenStage(stage)
	return *stage.clone(), 0, ""
}

// ViewPost sets the view for a stage. With no screen it acts on the default stage.
func (s *Store) ViewPost(screen string, hasScreen bool, scale, x, y any) (status int, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stage, status, msg := s.resolveStage(screen, hasScreen, true)
	if stage == nil {
		return status, msg
	}
	s.setView(stage, scale, x, y)
	s.noteChosenStage(stage)
	return 0, ""
}

// LayoutPost merges or replaces a stage's layout and returns the new list.
func (s *Store) LayoutPost(screen string, hasScreen bool, body map[string]any) (characters []Character, crev int, status int, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stage, status, msg := s.resolveStage(screen, hasScreen, true)
	if stage == nil {
		return nil, 0, status, msg
	}
	if _, ok := asList(body["characters"]); !ok {
		return nil, 0, 400, "bad layout"
	}
	s.setLayout(stage, body)
	s.noteChosenStage(stage)
	out := make([]Character, len(stage.Characters))
	copy(out, stage.Characters)
	return out, stage.Crev, 0, ""
}

// Report records a model's animation list. When the named stage shows that model
// with no animation yet, a playing name fills it in.
func (s *Store) Report(screen string, hasScreen bool, body map[string]any) (status int, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stage, status, msg := s.resolveStage(screen, hasScreen, true)
	if stage == nil {
		return status, msg
	}
	model, _ := body["model"].(string)
	animations, ok := asList(body["animations"])
	if !s.idsSet[model] || !ok || len(animations) > reportAnimsMax {
		return 400, "bad report"
	}
	names := make([]string, 0, len(animations))
	for _, a := range animations {
		str, ok := a.(string)
		if !ok || str == "" || truncateRunes(str, animationMaxChars) != str {
			return 400, "bad report"
		}
		names = append(names, str)
	}
	s.anims[model] = names
	playing, hasPlaying := body["playing"].(string)
	if stage.Model != nil && *stage.Model == model && stage.Animation == nil && hasPlaying && playing != "" {
		stage.Animation = strPtr(truncateRunes(playing, animationMaxChars))
	}
	return 0, ""
}

// SetEdit sets the edit-mode flag and returns it.
func (s *Store) SetEdit(on bool) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if on {
		s.edit = 1
	} else {
		s.edit = 0
	}
	return s.edit
}

// ToggleEdit flips the edit-mode flag and returns the new value, so a bare
// `spinewall edit` on a shortcut works whether the flag is on or off.
func (s *Store) ToggleEdit() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.edit == 0 {
		s.edit = 1
	} else {
		s.edit = 0
	}
	return s.edit
}

// AddInput adds the input counters from a body, capped at the per-step and total limits.
func (s *Store) AddInput(body map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, key := range inputKeys {
		step := plainInt(body[key], 0)
		if step < 0 {
			step = 0
		}
		if step > inputStepMax {
			step = inputStepMax
		}
		total := int64(s.input[key]) + int64(step)
		if total > inputTotalMax {
			total = inputTotalMax
		}
		s.input[key] = int(total)
	}
}

// ModelsCopy returns the scanned models for /models.
func (s *Store) ModelsCopy() []scan.Model {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]scan.Model, len(s.models))
	copy(out, s.models)
	return out
}

// Rescan rescans and returns the models, for /rescan.
func (s *Store) Rescan() []scan.Model {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rescan()
	out := make([]scan.Model, len(s.models))
	copy(out, s.models)
	return out
}

// ModelsDir returns the main model directory path.
func (s *Store) ModelsDir() string { return s.paths.ModelsDir }

// Input returns a copy of the input counters.
func (s *Store) Input() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return copyInput(s.input)
}

// ToggleFavorite toggles a favorite and returns false when the model is unknown.
func (s *Store) ToggleFavorite(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.toggleFavorite(id)
}

// PrefsPost applies pref changes from a body and reports whether anything changed.
func (s *Store) PrefsPost(body map[string]any) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.setPrefs(body)
}

// PrefsCopy returns a copy of the current prefs.
func (s *Store) PrefsCopy() Prefs {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.prefs
	out.Favorites = append([]string{}, s.prefs.Favorites...)
	out.Recent = append([]string{}, s.prefs.Recent...)
	out.Presets = append([]Preset{}, s.prefs.Presets...)
	return out
}

// ThumbPath returns a model's thumbnail path, or false when the id is unknown.
func (s *Store) ThumbPath(id string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.idsSet[id] {
		return "", false
	}
	return filepath.Join(s.paths.ThumbDir, id+".png"), true
}

// ScreensReply builds the /screens reply rows.
func (s *Store) ScreensReply() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]map[string]any, 0, len(s.screenOrder))
	for _, key := range s.screenOrder {
		stage := s.screens[key]
		out = append(out, map[string]any{
			"screen":     key,
			"preset":     stage.Preset,
			"characters": len(stage.Characters),
		})
	}
	return out
}

// PresetsReply builds the /presets reply rows.
func (s *Store) PresetsReply() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]map[string]any, 0, len(s.prefs.Presets))
	for _, p := range s.prefs.Presets {
		out = append(out, map[string]any{"name": p.Name, "characters": len(p.Characters)})
	}
	return out
}

// InfoReply builds the /info reply.
func (s *Store) InfoReply() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	current := deref(s.defaultStage.Model)
	animations := s.animationsLocked(current)
	screens := orderedObject{}
	for _, key := range s.screenOrder {
		screens = append(screens, orderedEntry{Key: key, Value: s.screens[key].clone()})
	}
	return map[string]any{
		"state":               s.defaultStage.clone(),
		"screens":             screens,
		"animations":          animations,
		"models":              len(s.models),
		"skipped":             s.skipped,
		"modelsDir":           s.paths.ModelsDir,
		"additionalModelDirs": nonNil(scan.LoadExtraDirs(s.paths.ModelsDir, s.paths.ConfigFile, s.logf)),
		"input":               copyInput(s.input),
	}
}

func (s *Store) animationsLocked(model string) []string {
	names := s.anims[model]
	if names == nil {
		return []string{}
	}
	out := make([]string, len(names))
	copy(out, names)
	return out
}

func nonNil(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}

func copyInput(in map[string]int) map[string]int {
	out := map[string]int{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

// WriteThumb stores a thumbnail atomically.
func WriteThumb(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
