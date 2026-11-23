package state

import (
	"encoding/json"
	"strconv"
	"testing"
)

// rectsBody is a mask report in the JSON shapes the decoder produces.
func rectsBody(rects ...map[string]any) map[string]any {
	list := make([]any, len(rects))
	for i, r := range rects {
		list[i] = r
	}
	return map[string]any{"rects": list}
}

func rect(x, y, w, h int) map[string]any {
	return map[string]any{
		"x": json.Number(strconv.Itoa(x)),
		"y": json.Number(strconv.Itoa(y)),
		"w": json.Number(strconv.Itoa(w)),
		"h": json.Number(strconv.Itoa(h)),
	}
}

func TestMaskStartsFull(t *testing.T) {
	store, _ := newTestStore(t)
	mask, status, msg := store.MaskFor("", false, true)
	if status != 0 {
		t.Fatalf("%d %s", status, msg)
	}
	if !mask.Full || len(mask.Rects) != 0 || mask.Rev != 0 {
		t.Fatalf("start mask = %+v, want full rev 0", mask)
	}
}

func TestMaskPostRectsBumpsRev(t *testing.T) {
	store, _ := newTestStore(t)
	first, status, msg := store.MaskPost("", false, rectsBody(rect(10, 20, 100, 200)))
	if status != 0 {
		t.Fatalf("%d %s", status, msg)
	}
	if first.Full {
		t.Fatal("rect mask reported full")
	}
	if len(first.Rects) != 1 || first.Rects[0] != (MaskRect{X: 10, Y: 20, W: 100, H: 200}) {
		t.Fatalf("rects = %+v", first.Rects)
	}
	if first.Rev != 1 {
		t.Fatalf("rev = %d, want 1", first.Rev)
	}
}

func TestMaskPostSameRegionKeepsRev(t *testing.T) {
	store, _ := newTestStore(t)
	body := rectsBody(rect(1, 2, 3, 4))
	store.MaskPost("", false, body)
	second, _, _ := store.MaskPost("", false, body)
	if second.Rev != 1 {
		t.Fatalf("rev bumped on an identical report: %d", second.Rev)
	}
}

func TestMaskPostFullTakesEveryClick(t *testing.T) {
	store, _ := newTestStore(t)
	store.MaskPost("", false, rectsBody(rect(0, 0, 10, 10)))
	full, status, msg := store.MaskPost("", false, map[string]any{"full": true})
	if status != 0 {
		t.Fatalf("%d %s", status, msg)
	}
	if !full.Full || len(full.Rects) != 0 {
		t.Fatalf("full mask = %+v", full)
	}
}

func TestMaskPostDropsEmptyRects(t *testing.T) {
	store, _ := newTestStore(t)
	mask, status, msg := store.MaskPost("", false, rectsBody(rect(0, 0, 0, 10), rect(5, 5, 10, 10), rect(0, 0, 4, -1)))
	if status != 0 {
		t.Fatalf("%d %s", status, msg)
	}
	if len(mask.Rects) != 1 || mask.Rects[0] != (MaskRect{X: 5, Y: 5, W: 10, H: 10}) {
		t.Fatalf("rects = %+v, want one positive rect", mask.Rects)
	}
}

func TestMaskPostEmptyRectsLetsEveryClickThrough(t *testing.T) {
	store, _ := newTestStore(t)
	mask, status, msg := store.MaskPost("", false, rectsBody())
	if status != 0 {
		t.Fatalf("%d %s", status, msg)
	}
	if mask.Full || len(mask.Rects) != 0 {
		t.Fatalf("mask = %+v, want not full and no rects", mask)
	}
}

func TestMaskPostBadBodyIs400(t *testing.T) {
	store, _ := newTestStore(t)
	for _, body := range []map[string]any{
		{},
		{"rects": "nope"},
		{"rects": []any{"not an object"}},
	} {
		if _, status, _ := store.MaskPost("", false, body); status != 400 {
			t.Errorf("%v: status %d, want 400", body, status)
		}
	}
}

func TestMaskIsPerScreen(t *testing.T) {
	store, _ := newTestStore(t)
	key := "1920x1080+0+0"
	if _, status, msg := store.MaskPost("", false, rectsBody(rect(0, 0, 100, 100))); status != 0 {
		t.Fatalf("%d %s", status, msg)
	}
	other, status, msg := store.MaskFor(key, true, true)
	if status != 0 {
		t.Fatalf("%d %s", status, msg)
	}
	if !other.Full {
		t.Fatal("a second screen shared the default stage's mask")
	}
}

func TestMaskUnknownScreenWithoutCreateIs404(t *testing.T) {
	store, _ := newTestStore(t)
	if _, status, _ := store.MaskFor("nope", true, false); status != 404 {
		t.Errorf("status = %d, want 404", status)
	}
}

func TestToggleEditFlipsTheFlag(t *testing.T) {
	store, _ := newTestStore(t)
	if got := store.ToggleEdit(); got != 1 {
		t.Fatalf("first toggle = %d, want 1", got)
	}
	if got := store.ToggleEdit(); got != 0 {
		t.Fatalf("second toggle = %d, want 0", got)
	}
}
