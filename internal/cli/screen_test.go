package cli

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// screensServer answers /screens with the given keys, as the real server does.
func screensServer(t *testing.T, keys ...string) *httptest.Server {
	t.Helper()
	var body strings.Builder
	body.WriteString("[")
	for i, k := range keys {
		if i > 0 {
			body.WriteString(",")
		}
		body.WriteString(`{"screen":"` + k + `"}`)
	}
	body.WriteString("]")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(body.String()))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// providerRun is a run wired for a compositor provider test: no KDE, a fake
// environment and a fake command runner, against the given screens server.
func providerRun(t *testing.T, srv *httptest.Server, env map[string]string, commands map[string]string) *run {
	t.Helper()
	var out, errOut strings.Builder
	return &run{
		base:      srv.URL,
		stdout:    &out,
		stderr:    &errOut,
		client:    http.DefaultClient,
		getenv:    func(k string) string { return env[k] },
		kdeScreen: func(*run) (string, error) { return "", errScreenUnsupported },
		execCmd: func(name string, args ...string) ([]byte, error) {
			key := name + " " + strings.Join(args, " ")
			body, ok := commands[key]
			if !ok {
				return nil, errors.New("unexpected command " + key)
			}
			return []byte(body), nil
		},
	}
}

func TestResolveHyprlandUsesTheActiveWorkspaceMonitor(t *testing.T) {
	srv := screensServer(t, "3440x1440+0+0", "1920x1080+3440+236")
	r := providerRun(t, srv, map[string]string{"HYPRLAND_INSTANCE_SIGNATURE": "abc"}, map[string]string{
		"hyprctl -j activeworkspace": `{"monitor":"DP-2","id":1}`,
		"hyprctl -j monitors": `[{"name":"DP-2","x":0,"y":0,"width":3440,"height":1440,"focused":true},` +
			`{"name":"HDMI-A-1","x":3440,"y":236,"width":1920,"height":1080,"focused":false}]`,
	})
	got, err := r.resolveCurrentScreen()
	if err != nil {
		t.Fatal(err)
	}
	if got != "3440x1440+0+0" {
		t.Errorf("resolved %q", got)
	}
}

func TestResolveSwayUsesTheFocusedOutput(t *testing.T) {
	srv := screensServer(t, "3440x1440+0+0", "1920x1080+3440+236")
	r := providerRun(t, srv, map[string]string{"SWAYSOCK": "/run/user/1000/sway"}, map[string]string{
		"swaymsg -t get_outputs": `[{"name":"DP-2","active":true,"focused":false,"rect":{"x":0,"y":0,"width":3440,"height":1440}},` +
			`{"name":"HDMI-A-1","active":true,"focused":true,"rect":{"x":3440,"y":236,"width":1920,"height":1080}}]`,
	})
	got, err := r.resolveCurrentScreen()
	if err != nil {
		t.Fatal(err)
	}
	if got != "1920x1080+3440+236" {
		t.Errorf("resolved %q", got)
	}
}

func TestResolveWithoutAProviderIsUnsupported(t *testing.T) {
	srv := screensServer(t)
	r := providerRun(t, srv, map[string]string{}, map[string]string{})
	if _, err := r.resolveCurrentScreen(); !errors.Is(err, errScreenUnsupported) {
		t.Fatalf("err = %v, want errScreenUnsupported", err)
	}
}

func TestParseSwayOutputsWithoutAFocusedOutputIsAnError(t *testing.T) {
	if _, _, err := parseSwayOutputs([]byte(`[{"name":"DP-2","active":true,"focused":false,"rect":{"x":0,"y":0,"width":1,"height":1}}]`)); err == nil {
		t.Fatal("expected an error for no focused output")
	}
}

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
