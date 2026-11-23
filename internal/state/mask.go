package state

// Mask is the input region a native-view window accepts pointer events in,
// in window pixels. Input-mask support is what lets the layer-shell backend
// drop the KDE containment patch: a click inside the mask reaches the page, a
// click outside falls through to the desktop.
//
// It is a region of rectangles, not one box, so a click between two separated
// characters still reaches the desktop. It belongs to one window, which is one
// screen's stage, and it is never written to disk. It starts Full, so a page
// that has not reported yet keeps every click.
type Mask struct {
	// Full is true when the whole window takes clicks.
	Full bool `json:"full"`
	// Rects are the clickable rectangles when Full is false. An empty list
	// lets every click through.
	Rects []MaskRect `json:"rects"`
	// Rev bumps on each change, so the launcher can skip an unchanged mask.
	Rev int `json:"rev"`
}

// MaskRect is a window-pixel rectangle.
type MaskRect struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

// maskRectsMax bounds how many rectangles one report may carry, so a hostile
// page cannot grow the reply without limit.
const maskRectsMax = 64

// fullMask is the startup mask. Rects is an empty slice, never nil, so the JSON
// reply carries [] like the Python server. mask is a pointer field on Stage so
// a nil stage or an unmasked one falls back here.
func fullMask() Mask { return Mask{Full: true, Rects: []MaskRect{}} }

func (m Mask) clone() Mask {
	out := Mask{Full: m.Full, Rev: m.Rev}
	out.Rects = make([]MaskRect, len(m.Rects))
	copy(out.Rects, m.Rects)
	return out
}

// maskLocked returns a stage's mask, creating the full one on first use. The
// caller holds the lock.
func (s *Store) maskLocked(stage *Stage) Mask {
	if stage.mask == nil {
		full := fullMask()
		stage.mask = &full
	}
	return stage.mask.clone()
}

// MaskFor resolves a request's stage and returns its mask for /mask. A page may
// create a stage; a mistyped name is a 404, as on every other stage route.
func (s *Store) MaskFor(screen string, hasScreen, create bool) (Mask, int, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stage, status, msg := s.resolveStage(screen, hasScreen, create)
	if stage == nil {
		return Mask{}, status, msg
	}
	return s.maskLocked(stage), 0, ""
}

// MaskPost applies a page's mask report to a stage. A body with "full" true
// makes the whole window take clicks; otherwise "rects" is a list of
// window-pixel rectangles. An unreadable body is a 400, so a broken page is
// noticed rather than silently keeping every click.
func (s *Store) MaskPost(screen string, hasScreen bool, body map[string]any) (Mask, int, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stage, status, msg := s.resolveStage(screen, hasScreen, true)
	if stage == nil {
		return Mask{}, status, msg
	}
	current := s.maskLocked(stage)

	if full, ok := body["full"].(bool); ok && full {
		return s.applyMaskLocked(stage, Mask{Full: true, Rects: []MaskRect{}}), 0, ""
	}
	raw, ok := asList(body["rects"])
	if !ok {
		if _, hasFull := body["full"]; hasFull {
			// full:false with no rects means every click passes through.
			return s.applyMaskLocked(stage, Mask{Full: false, Rects: []MaskRect{}}), 0, ""
		}
		return current, 400, "bad mask"
	}
	rects := make([]MaskRect, 0, len(raw))
	for _, item := range raw {
		obj, ok := asObject(item)
		if !ok {
			return current, 400, "bad mask"
		}
		r := MaskRect{
			X: plainInt(obj["x"], 0),
			Y: plainInt(obj["y"], 0),
			W: plainInt(obj["w"], 0),
			H: plainInt(obj["h"], 0),
		}
		if r.W <= 0 || r.H <= 0 {
			continue
		}
		if len(rects) >= maskRectsMax {
			break
		}
		rects = append(rects, r)
	}
	return s.applyMaskLocked(stage, Mask{Full: false, Rects: rects}), 0, ""
}

// applyMaskLocked stores m on the stage, bumping Rev only when it changed. The
// caller holds the lock.
func (s *Store) applyMaskLocked(stage *Stage, m Mask) Mask {
	cur := s.maskLocked(stage)
	if m.Full == cur.Full && sameRects(m.Rects, cur.Rects) {
		return cur
	}
	next := Mask{Full: m.Full, Rects: m.Rects, Rev: cur.Rev + 1}
	stage.mask = &next
	return next.clone()
}

func sameRects(a, b []MaskRect) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
