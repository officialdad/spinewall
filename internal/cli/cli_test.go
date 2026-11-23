package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQuoteMatchesPython(t *testing.T) {
	cases := map[string]string{
		"walk fast #1":  "walk%20fast%20%231",
		"idle/loop":     "idle%2Floop",
		"100%":          "100%25",
		"über ünï":      "%C3%BCber%20%C3%BCn%C3%AF",
		"a?b=c&d":       "a%3Fb%3Dc%26d",
		"plain":         "plain",
		"a_b.c-d~e":     "a_b.c-d~e",
		"1920x1080+0+0": "1920x1080%2B0%2B0",
	}
	for in, want := range cases {
		if got := quote(in); got != want {
			t.Errorf("quote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRoutePath(t *testing.T) {
	cases := []struct {
		command string
		args    []string
		path    string
		screen  bool
	}{
		{"info", []string{"info"}, "/info", false},
		{"rescan", []string{"rescan"}, "/rescan", false},
		{"presets", []string{"presets"}, "/presets", false},
		{"screens", []string{"screens"}, "/screens", false},
		{"next-preset", []string{"next-preset"}, "/cmd/next-preset", true},
		{"set-anim", []string{"set-anim", "walk fast #1"}, "/cmd/set-anim/walk%20fast%20%231", true},
		{"set-char-model", []string{"set-char-model", "uid", "delta"}, "/cmd/set-char-model/uid/delta", true},
	}
	for _, c := range cases {
		path, screen := routePath(c.command, c.args)
		if path != c.path || screen != c.screen {
			t.Errorf("routePath(%q) = (%q, %v), want (%q, %v)", c.command, path, screen, c.path, c.screen)
		}
	}
}

// collectServer records the EscapedPath and RawQuery of each request.
func collectServer(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.EscapedPath()+"?"+r.URL.RawQuery)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func newRunnable(base string, out, errOut *strings.Builder) *run {
	return &run{
		base:     base,
		stdout:   out,
		stderr:   errOut,
		client:   http.DefaultClient,
		resolver: func(*run) (string, error) { return "", nil },
	}
}

func TestEncodedPathReachesServerAsOneSegment(t *testing.T) {
	srv, seen := collectServer(t)
	var out, errOut strings.Builder
	r := newRunnable(srv.URL, &out, &errOut)
	if code := r.exec([]string{"set-anim", "idle/loop"}); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errOut.String())
	}
	if len(*seen) != 1 || (*seen)[0] != "/cmd/set-anim/idle%2Floop?" {
		t.Fatalf("server saw %v", *seen)
	}
	if !strings.HasSuffix(out.String(), "\n") {
		t.Errorf("output does not end with a newline: %q", out.String())
	}
}

func TestScreenQueryIsEncoded(t *testing.T) {
	srv, seen := collectServer(t)
	var out, errOut strings.Builder
	r := newRunnable(srv.URL, &out, &errOut)
	r.exec([]string{"--screen", "1920x1080+3440+236", "next-preset"})
	if len(*seen) != 1 || (*seen)[0] != "/cmd/next-preset?screen=1920x1080%2B3440%2B236" {
		t.Fatalf("server saw %v", *seen)
	}
}

func TestNoScreenSendsNoQuery(t *testing.T) {
	srv, seen := collectServer(t)
	var out, errOut strings.Builder
	r := newRunnable(srv.URL, &out, &errOut)
	r.exec([]string{"next-preset"})
	if len(*seen) != 1 || (*seen)[0] != "/cmd/next-preset?" {
		t.Fatalf("server saw %v", *seen)
	}
}

func TestThreeArgumentsAllowedFourRejected(t *testing.T) {
	srv, seen := collectServer(t)
	var out, errOut strings.Builder
	r := newRunnable(srv.URL, &out, &errOut)
	if code := r.exec([]string{"set-char-anim", "uid", "walk"}); code != 0 {
		t.Fatalf("three args exited %d", code)
	}
	errOut.Reset()
	*seen = nil
	if code := r.exec([]string{"set-anim", "a", "b", "c"}); code != 2 {
		t.Fatalf("four args exited %d, want 2", code)
	}
	if len(*seen) != 0 {
		t.Errorf("four args sent a request: %v", *seen)
	}
	if !strings.Contains(errOut.String(), "at most 3 arguments") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestNoArgumentRunsInfo(t *testing.T) {
	srv, seen := collectServer(t)
	var out, errOut strings.Builder
	r := newRunnable(srv.URL, &out, &errOut)
	if code := r.exec(nil); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if len(*seen) != 1 || (*seen)[0] != "/info?" {
		t.Fatalf("server saw %v", *seen)
	}
}

func TestScreenWithoutKeyIsUsageError(t *testing.T) {
	srv, seen := collectServer(t)
	var out, errOut strings.Builder
	r := newRunnable(srv.URL, &out, &errOut)
	if code := r.exec([]string{"--screen"}); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "--screen needs a screen key") {
		t.Errorf("stderr = %q", errOut.String())
	}
	if len(*seen) != 0 {
		t.Errorf("sent a request: %v", *seen)
	}
}

func TestUnsupportedScreenFallsBackToDefault(t *testing.T) {
	srv, seen := collectServer(t)
	var out, errOut strings.Builder
	r := newRunnable(srv.URL, &out, &errOut)
	r.resolver = func(*run) (string, error) { return "", errScreenUnsupported }
	if code := r.exec([]string{"--screen", "current", "next-preset"}); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errOut.String())
	}
	if len(*seen) != 1 || (*seen)[0] != "/cmd/next-preset?screen=" {
		t.Fatalf("server saw %v, want a default-stage call", *seen)
	}
	if !strings.Contains(errOut.String(), "not supported on this desktop") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestResolvedScreenIsUsed(t *testing.T) {
	srv, seen := collectServer(t)
	var out, errOut strings.Builder
	r := newRunnable(srv.URL, &out, &errOut)
	r.resolver = func(*run) (string, error) { return "3440x1440+0+0", nil }
	r.exec([]string{"--screen", "current", "next-preset"})
	if len(*seen) != 1 || (*seen)[0] != "/cmd/next-preset?screen=3440x1440%2B0%2B0" {
		t.Fatalf("server saw %v", *seen)
	}
}

func TestInfoIgnoresScreenCurrent(t *testing.T) {
	// Info has no stage, so --screen current must not trigger a D-Bus lookup.
	srv, seen := collectServer(t)
	var out, errOut strings.Builder
	r := newRunnable(srv.URL, &out, &errOut)
	r.resolver = func(*run) (string, error) { t.Fatal("resolver called for info"); return "", nil }
	if code := r.exec([]string{"info", "--screen", "current"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if len(*seen) != 1 || (*seen)[0] != "/info?" {
		t.Fatalf("server saw %v", *seen)
	}
}

func TestServerErrorIsNonzero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unknown screen", http.StatusNotFound)
	}))
	defer srv.Close()
	var out, errOut strings.Builder
	r := newRunnable(srv.URL, &out, &errOut)
	if code := r.exec([]string{"next-preset", "--screen", "nope"}); code != 22 {
		t.Fatalf("exit %d, want 22", code)
	}
	if !strings.Contains(errOut.String(), "404") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestDeadServerIsNonzero(t *testing.T) {
	var out, errOut strings.Builder
	r := &run{
		base:     "http://127.0.0.1:1",
		stdout:   &out,
		stderr:   &errOut,
		client:   http.DefaultClient,
		resolver: func(*run) (string, error) { return "", nil },
	}
	if code := r.exec([]string{"info"}); code != 7 {
		t.Fatalf("exit %d against a dead server, want 7", code)
	}
}

func TestEmptyScreensFallsBackToDefaultStage(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		seen = append(seen, req.URL.EscapedPath()+"?"+req.URL.RawQuery)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	var out, errOut strings.Builder
	r := newRunnable(srv.URL, &out, &errOut)
	r.resolver = func(*run) (string, error) {
		return pickCurrentScreen("DP-2", nil, nil)
	}
	if code := r.exec([]string{"--screen", "current", "next-preset"}); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errOut.String())
	}
	if len(seen) != 1 || seen[0] != "/cmd/next-preset?screen=" {
		t.Fatalf("server saw %v", seen)
	}
}

func TestEditWithoutArgumentToggles(t *testing.T) {
	srv, seen := collectServer(t)
	var out, errOut strings.Builder
	r := newRunnable(srv.URL, &out, &errOut)
	if code := r.exec([]string{"edit"}); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errOut.String())
	}
	if len(*seen) != 1 || (*seen)[0] != "/edit/toggle?" {
		t.Fatalf("server saw %v, want the toggle path", *seen)
	}
}

func TestEditOnAndOffSetTheFlag(t *testing.T) {
	srv, seen := collectServer(t)
	var out, errOut strings.Builder
	r := newRunnable(srv.URL, &out, &errOut)
	cases := map[string]string{"on": "/edit/1?", "off": "/edit/0?"}
	for arg, want := range cases {
		*seen = nil
		if code := r.exec([]string{"edit", arg}); code != 0 {
			t.Fatalf("edit %s exit %d: %s", arg, code, errOut.String())
		}
		if len(*seen) != 1 || (*seen)[0] != want {
			t.Errorf("edit %s: server saw %v, want %s", arg, *seen, want)
		}
	}
}

func TestEditWithTwoArgumentsIsUsageError(t *testing.T) {
	srv, seen := collectServer(t)
	var out, errOut strings.Builder
	r := newRunnable(srv.URL, &out, &errOut)
	if code := r.exec([]string{"edit", "on", "extra"}); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if len(*seen) != 0 {
		t.Errorf("sent a request: %v", *seen)
	}
	if !strings.Contains(errOut.String(), "at most one argument") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestEditRejectsAnythingButOnAndOff(t *testing.T) {
	for _, arg := range []string{"0", "1", "true", "false", "of"} {
		srv, seen := collectServer(t)
		var out, errOut strings.Builder
		r := newRunnable(srv.URL, &out, &errOut)
		if code := r.exec([]string{"edit", arg}); code != 2 {
			t.Errorf("edit %s: exit %d, want 2", arg, code)
		}
		if len(*seen) != 0 {
			t.Errorf("edit %s sent a request: %v", arg, *seen)
		}
	}
}

func TestMaskCarriesTheScreen(t *testing.T) {
	srv, seen := collectServer(t)
	var out, errOut strings.Builder
	r := newRunnable(srv.URL, &out, &errOut)
	if code := r.exec([]string{"mask", "--screen", "2560x1440+0+0"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if len(*seen) != 1 || (*seen)[0] != "/mask?screen=2560x1440%2B0%2B0" {
		t.Fatalf("server saw %v", *seen)
	}
}

func TestMaskReadsTheMaskPath(t *testing.T) {
	srv, seen := collectServer(t)
	var out, errOut strings.Builder
	r := newRunnable(srv.URL, &out, &errOut)
	if code := r.exec([]string{"mask"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if len(*seen) != 1 || (*seen)[0] != "/mask?" {
		t.Fatalf("server saw %v", *seen)
	}
}
