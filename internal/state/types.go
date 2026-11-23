// Package state holds the server's mutable state: one stage per monitor, the
// character list, presets and prefs, plus the pure functions that change them.
//
// It mirrors server/spinewall-server.py. The Python server is the reference and
// the two must stay in step, including key order in the JSON replies so diffs
// read cleanly. No HTTP lives here.
package state

// Character is one character on a stage, in z order (last draws on top).
type Character struct {
	UID       string  `json:"uid"`
	Model     string  `json:"model"`
	Animation *string `json:"animation"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Scale     float64 `json:"scale"`
	Auto      bool    `json:"auto"`
}

// PresetChar is a character inside a stored preset. It has no uid, because a
// load makes fresh uids so two loads never share one.
type PresetChar struct {
	Model     string  `json:"model"`
	Animation *string `json:"animation"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Scale     float64 `json:"scale"`
	Auto      bool    `json:"auto"`
}

// Preset is a named layout in prefs.json.
type Preset struct {
	Name       string       `json:"name"`
	Characters []PresetChar `json:"characters"`
}

// Prefs are kept apart from the stage so a favorite never bumps a revision.
type Prefs struct {
	Favorites []string `json:"favorites"`
	Recent    []string `json:"recent"`
	Layout    string   `json:"layout"`
	Sort      string   `json:"sort"`
	Kind      string   `json:"kind"`
	Variants  bool     `json:"variants"`
	Presets   []Preset `json:"presets"`
}

// Stage is one monitor's state. Field order matches the Python dict so the JSON
// replies keep the same key order.
type Stage struct {
	Model      *string     `json:"model"`
	Animation  *string     `json:"animation"`
	Rev        int         `json:"rev"`
	Scale      float64     `json:"scale"`
	X          float64     `json:"x"`
	Y          float64     `json:"y"`
	Vrev       int         `json:"vrev"`
	Auto       bool        `json:"auto"`
	Preset     *string     `json:"preset"`
	Characters []Character `json:"characters"`
	Crev       int         `json:"crev"`
	// mask holds the native-view input region for this stage's window. It is
	// never written to disk; see mask.go. Nil reads as the full mask.
	mask *Mask
}

// StateReply is /state: a stage plus the current edit-mode flag.
type StateReply struct {
	Stage
	Edit int `json:"edit"`
}

// clone returns a deep copy, used when a new screen copies the last chosen stage.
func (s *Stage) clone() *Stage {
	out := *s
	out.Model = cloneStr(s.Model)
	out.Animation = cloneStr(s.Animation)
	out.Preset = cloneStr(s.Preset)
	out.Characters = make([]Character, len(s.Characters))
	for i, c := range s.Characters {
		out.Characters[i] = c
		out.Characters[i].Animation = cloneStr(c.Animation)
	}
	if s.mask != nil {
		m := s.mask.clone()
		out.mask = &m
	}
	return &out
}

func cloneStr(s *string) *string {
	if s == nil {
		return nil
	}
	v := *s
	return &v
}

func strPtr(s string) *string { return &s }
