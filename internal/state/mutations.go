package state

import (
	"strings"
)

// setCharacters replaces the layout and bumps crev. The caller holds the lock.
func (s *Store) setCharacters(stage *Stage, characters []Character) {
	if characters == nil {
		characters = []Character{}
	}
	stage.Characters = characters
	stage.Crev++
	s.saveLayout(stage)
}

func charactersEqual(a, b []Character) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !characterEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

func characterEqual(a, b Character) bool {
	if a.UID != b.UID || a.Model != b.Model || a.X != b.X || a.Y != b.Y || a.Scale != b.Scale || a.Auto != b.Auto {
		return false
	}
	return strEqual(a.Animation, b.Animation)
}

func strEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func findCharacter(stage *Stage, uid string) *Character {
	for i := range stage.Characters {
		if stage.Characters[i].UID == uid {
			return &stage.Characters[i]
		}
	}
	return nil
}

// defaultAnimation picks the model's default animation if the report lists it,
// else "idle" if listed, else the first listed name, else nil.
func (s *Store) defaultAnimation(modelID string) *string {
	names := s.anims[modelID]
	defaultName := ""
	found := false
	for _, m := range s.models {
		if m.ID == modelID {
			defaultName = m.DefaultAnimation
			found = true
			break
		}
	}
	// Python looks at (model["defaultAnimation"], "idle"), with None first when
	// the model is unknown, so "idle" wins over the first name in that case.
	candidates := []string{}
	if found {
		candidates = append(candidates, defaultName)
	}
	candidates = append(candidates, "idle")
	for _, name := range candidates {
		for _, have := range names {
			if have == name {
				n := name
				return &n
			}
		}
	}
	if len(names) > 0 {
		n := names[0]
		return &n
	}
	return nil
}

func (s *Store) addCharacter(stage *Stage, modelID string) {
	chars := stage.Characters
	if len(chars) == 0 {
		// The visible character keeps its place when the stage takes over, so adding one never makes it jump.
		chars = []Character{{
			UID:       newUID(map[string]bool{}),
			Model:     deref(stage.Model),
			Animation: cloneStr(stage.Animation),
			X:         stage.X,
			Y:         stage.Y,
			Scale:     stage.Scale,
			Auto:      stage.Auto,
		}}
	}
	n := len(chars)
	side := -1.0
	if n%2 == 1 {
		side = 1.0
	}
	step := float64(((n+1)/2-1)%4 + 1)
	taken := map[string]bool{}
	for _, c := range chars {
		taken[c.UID] = true
	}
	addedModel := deref(stage.Model)
	if s.idsSet[modelID] {
		addedModel = modelID
	}
	chars = append(chars, Character{
		UID:       newUID(taken),
		Model:     addedModel,
		Animation: nil,
		X:         side * step * 0.2,
		Y:         0.0,
		Scale:     0.7,
		Auto:      false,
	})
	s.setCharacters(stage, chars)
}

func (s *Store) removeCharacter(stage *Stage, uid string) {
	chars := make([]Character, 0, len(stage.Characters))
	for _, c := range stage.Characters {
		if c.UID != uid {
			chars = append(chars, c)
		}
	}
	if len(chars) == len(stage.Characters) {
		return
	}
	if len(chars) == 1 {
		// One character left is the single-character layout, so the old keys carry it and the player returns.
		last := chars[0]
		chars = []Character{}
		if last.Model != deref(stage.Model) || last.Animation != nil || last.Auto != stage.Auto {
			stage.Model = strPtr(last.Model)
			stage.Animation = cloneStr(last.Animation)
			stage.Auto = last.Auto
			stage.Rev++
		}
		stage.Scale = last.Scale
		stage.X = last.X
		stage.Y = last.Y
		stage.Vrev++
		s.save(stage)
	}
	s.setCharacters(stage, chars)
}

func (s *Store) updateCharacter(stage *Stage, uid string, change func(*Character)) {
	chars := make([]Character, len(stage.Characters))
	copy(chars, stage.Characters)
	for i := range chars {
		if chars[i].UID == uid {
			change(&chars[i])
		}
	}
	if !charactersEqual(chars, stage.Characters) {
		s.setCharacters(stage, chars)
	}
}

func (s *Store) cycleCharacterAnimation(stage *Stage, uid string, step int) {
	char := findCharacter(stage, uid)
	if char == nil {
		return
	}
	names := s.anims[char.Model]
	if len(names) == 0 {
		return
	}
	current := ""
	if char.Animation != nil {
		current = *char.Animation
	} else if def := s.defaultAnimation(char.Model); def != nil {
		current = *def
	}
	i := -1
	for idx, n := range names {
		if n == current {
			i = idx
			break
		}
	}
	next := names[mod(i+step, len(names))]
	s.updateCharacter(stage, uid, func(c *Character) {
		c.Animation = strPtr(next)
	})
}

func (s *Store) setAutoCycle(stage *Stage, arg string) {
	slash := strings.LastIndex(arg, "/")
	uid, value := "", arg
	if slash >= 0 {
		uid, value = arg[:slash], arg[slash+1:]
	}
	if value != "on" && value != "off" {
		return
	}
	on := value == "on"
	switch {
	case slash >= 0:
		s.updateCharacter(stage, uid, func(c *Character) { c.Auto = on })
	case len(stage.Characters) > 0:
		chars := make([]Character, len(stage.Characters))
		copy(chars, stage.Characters)
		for i := range chars {
			chars[i].Auto = on
		}
		if !charactersEqual(chars, stage.Characters) {
			s.setCharacters(stage, chars)
		}
	case stage.Auto != on:
		stage.Auto = on
		stage.Rev++
		s.save(stage)
	}
}

func (s *Store) setLayout(stage *Stage, body map[string]any) {
	existing := map[string]Character{}
	order := []string{}
	for _, c := range stage.Characters {
		existing[c.UID] = c
		order = append(order, c.UID)
	}
	replace := isTrue(body["replace"])

	var pushed []map[string]any
	if list, ok := asList(body["characters"]); ok {
		for _, e := range list {
			if obj, ok := asObject(e); ok {
				pushed = append(pushed, obj)
			}
		}
	}
	// A bad flag keeps the stored one. Only a real bool changes it.
	for i, e := range pushed {
		if _, ok := e["auto"].(bool); !ok {
			without := map[string]any{}
			for k, v := range e {
				if k != "auto" {
					without[k] = v
				}
			}
			pushed[i] = without
		}
	}
	if !replace {
		filtered := pushed[:0]
		for _, e := range pushed {
			uid, _ := e["uid"].(string)
			if _, ok := existing[uid]; ok {
				filtered = append(filtered, e)
			}
		}
		pushed = filtered
		// Pushed entries carry position, scale and order. The stored model and animation fill the rest.
		for i, e := range pushed {
			without := map[string]any{}
			for k, v := range e {
				if k != "model" && k != "animation" {
					without[k] = v
				}
			}
			pushed[i] = without
		}
	}
	merged := make([]any, 0, len(pushed))
	for _, e := range pushed {
		base := map[string]any{}
		if uid, ok := e["uid"].(string); ok {
			if ex, ok := existing[uid]; ok {
				base = characterMap(ex)
			}
		}
		for k, v := range e {
			base[k] = v
		}
		merged = append(merged, base)
	}
	var allowed map[string]bool
	if !replace {
		allowed = map[string]bool{}
		for uid := range existing {
			allowed[uid] = true
		}
	}
	cleaned := cleanCharacters(merged, s.idsSet, allowed, true)
	if !replace {
		mentioned := map[string]bool{}
		for _, c := range cleaned {
			mentioned[c.UID] = true
		}
		for _, uid := range order {
			if !mentioned[uid] {
				cleaned = append(cleaned, existing[uid])
			}
		}
	}
	if !charactersEqual(cleaned, stage.Characters) {
		s.setCharacters(stage, cleaned)
	}
}

// characterMap renders a Character as the Python dict with the same keys.
func characterMap(c Character) map[string]any {
	return map[string]any{
		"uid":       c.UID,
		"model":     c.Model,
		"animation": ptrAny(c.Animation),
		"x":         c.X,
		"y":         c.Y,
		"scale":     c.Scale,
		"auto":      c.Auto,
	}
}

func ptrAny(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

// currentCharacters returns the on-screen characters without uids.
func currentCharacters(stage *Stage) []PresetChar {
	if len(stage.Characters) > 0 {
		out := make([]PresetChar, 0, len(stage.Characters))
		for _, c := range stage.Characters {
			out = append(out, PresetChar{
				Model:     c.Model,
				Animation: cloneStr(c.Animation),
				X:         c.X,
				Y:         c.Y,
				Scale:     c.Scale,
				Auto:      c.Auto,
			})
		}
		return out
	}
	return []PresetChar{{
		Model:     deref(stage.Model),
		Animation: cloneStr(stage.Animation),
		X:         stage.X,
		Y:         stage.Y,
		Scale:     stage.Scale,
		Auto:      stage.Auto,
	}}
}

// setView keeps an unusable value, so a bad push never writes NaN into the state.
func (s *Store) setView(stage *Stage, scale, x, y any) {
	if scale != nil {
		stage.Scale = clamp(scale, scaleMin, scaleMax, stage.Scale)
	}
	if x != nil {
		stage.X = clamp(x, -positionMax, positionMax, stage.X)
	}
	if y != nil {
		stage.Y = clamp(y, -positionMax, positionMax, stage.Y)
	}
	stage.Vrev++
	s.save(stage)
}

func (s *Store) changeAnimation(stage *Stage, name string) {
	stage.Animation = strPtr(name)
	stage.Rev++
	s.save(stage)
}

func (s *Store) changeModel(stage *Stage, modelID string) {
	if stage.Model != nil && *stage.Model == modelID {
		return
	}
	stage.Model = strPtr(modelID)
	stage.Animation = nil
	stage.Rev++
	s.save(stage)
	s.recordRecent(modelID)
}

func (s *Store) recordRecent(modelID string) {
	recent := []string{modelID}
	for _, id := range s.prefs.Recent {
		if id != modelID && len(recent) < recentMax {
			recent = append(recent, id)
		}
	}
	s.prefs.Recent = recent
	s.savePrefs()
}

func (s *Store) toggleFavorite(modelID string) bool {
	if !s.idsSet[modelID] {
		return false
	}
	favorites := s.prefs.Favorites
	found := -1
	for i, id := range favorites {
		if id == modelID {
			found = i
			break
		}
	}
	if found >= 0 {
		s.prefs.Favorites = append(favorites[:found], favorites[found+1:]...)
	} else {
		s.prefs.Favorites = append(favorites, modelID)
	}
	s.savePrefs()
	return true
}

func (s *Store) setPrefs(body map[string]any) bool {
	changed := false
	for _, key := range prefKeyOrder {
		if v, ok := body[key].(string); ok && choiceAllowed(key, v) {
			switch key {
			case "layout":
				s.prefs.Layout = v
			case "sort":
				s.prefs.Sort = v
			case "kind":
				s.prefs.Kind = v
			}
			changed = true
		}
	}
	if v, ok := body["variants"].(bool); ok {
		s.prefs.Variants = v
		changed = true
	}
	if changed {
		s.savePrefs()
	}
	return changed
}

func mod(a, n int) int {
	m := a % n
	if m < 0 {
		m += n
	}
	return m
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// loadPrefs reads prefs.json with the model ids for preset model checks.
func loadPrefs(path string, ids map[string]bool) Prefs {
	return cleanPrefs(readJSONObject(path), ids)
}
