package cli

import (
	"errors"
	"testing"
)

const sampleSupport = `KWin Support Information:
Screens
=======
Number of Screens: 3

Screen 0:
---------
Name: Unknown-1
Enabled: 0
Screen 1:
---------
Name: HDMI-A-1
Enabled: 1
Geometry: 3440,236,1920x1080
Physical size: 345x194mm
Scale: 1
Screen 2:
---------
Name: DP-2
Enabled: 1
Geometry: 0,0,3440x1440
Scale: 1

Compositing
===========
Compositing is active
`

func TestParseSupportInformation(t *testing.T) {
	outputs := parseSupportInformation(sampleSupport)
	if len(outputs) != 3 {
		t.Fatalf("got %d outputs, want 3: %+v", len(outputs), outputs)
	}
	dp := outputs[2]
	if dp.name != "DP-2" || !dp.enabled || dp.x != 0 || dp.y != 0 || dp.w != 3440 || dp.h != 1440 || !dp.hasGeom {
		t.Errorf("DP-2 parsed wrong: %+v", dp)
	}
	unknown := outputs[0]
	if unknown.enabled || unknown.hasGeom {
		t.Errorf("Unknown-1 should be disabled with no geometry: %+v", unknown)
	}
}

func TestMatchScreenExactWins(t *testing.T) {
	outputs := parseSupportInformation(sampleSupport)
	// A stale key at the same position must lose to the exact key.
	known := []string{"1920x1080+3440+236", "3440x1440+0+0", "1280x720+0+0"}
	got, err := matchScreen("DP-2", outputs, known)
	if err != nil {
		t.Fatal(err)
	}
	if got != "3440x1440+0+0" {
		t.Errorf("matched %q", got)
	}
}

func TestMatchScreenPositionFallback(t *testing.T) {
	outputs := parseSupportInformation(sampleSupport)
	// A scaled monitor reports a size the page does not use, so only the
	// position matches. One key at the suffix is the answer.
	known := []string{"2560x1440+3440+236"}
	got, err := matchScreen("HDMI-A-1", outputs, known)
	if err != nil {
		t.Fatal(err)
	}
	if got != "2560x1440+3440+236" {
		t.Errorf("matched %q", got)
	}
}

func TestMatchScreenAmbiguousIsError(t *testing.T) {
	outputs := parseSupportInformation(sampleSupport)
	// Neither key is the exact size, so both match on position and the pick is
	// ambiguous.
	known := []string{"1280x720+3440+236", "2560x1440+3440+236"}
	if _, err := matchScreen("HDMI-A-1", outputs, known); err == nil {
		t.Fatal("expected an error for two keys at one position")
	}
}

func TestMatchScreenMissingMonitorIsError(t *testing.T) {
	outputs := parseSupportInformation(sampleSupport)
	if _, err := matchScreen("DP-9", outputs, []string{"3440x1440+0+0"}); err == nil {
		t.Fatal("expected an error for an unknown monitor")
	}
}

func TestPickCurrentScreenNoKeysFallsBack(t *testing.T) {
	// A lone monitor or a page that has not connected yet leaves /screens empty,
	// which is the default stage, not an error.
	outputs := parseSupportInformation(sampleSupport)
	if _, err := pickCurrentScreen("DP-2", outputs, nil); !errors.Is(err, errScreenUnsupported) {
		t.Fatalf("err = %v, want errScreenUnsupported", err)
	}
}

func TestPickCurrentScreenResolves(t *testing.T) {
	outputs := parseSupportInformation(sampleSupport)
	got, err := pickCurrentScreen("DP-2", outputs, []string{"3440x1440+0+0", "1920x1080+3440+236"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "3440x1440+0+0" {
		t.Errorf("got %q", got)
	}
}
