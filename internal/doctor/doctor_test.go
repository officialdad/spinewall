package doctor

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// fakeEnv builds Options from a map so a test controls every detection source.
func fakeEnv(env map[string]string, present map[string]bool, network bool) Options {
	return Options{
		Getenv: func(k string) string { return env[k] },
		LookPath: func(name string) (string, error) {
			if present[name] {
				return "/usr/bin/" + name, nil
			}
			return "", errNotFound
		},
		Exists: func(path string) bool { return present[path] },
		ReadOSRelease: func(string) (map[string]string, error) {
			m := map[string]string{"ID": env["DISTRO_ID"]}
			if idLike, ok := env["DISTRO_ID_LIKE"]; ok {
				m["ID_LIKE"] = idLike
			}
			return m, nil
		},
		Network: func() bool { return network },
	}
}

type notFoundError struct{}

func (notFoundError) Error() string { return "not found" }

var errNotFound = notFoundError{}

func TestDetectNamesAgnosticSessionAndDesktop(t *testing.T) {
	opts := fakeEnv(map[string]string{
		"XDG_SESSION_TYPE":    "wayland",
		"XDG_CURRENT_DESKTOP": "KDE",
		"DISTRO_ID":           "arch",
	}, map[string]bool{"qml": true, "plasmashell": true, "systemctl": true, fmt.Sprintf("/run/user/%d/systemd", os.Getuid()): true}, true)
	r := Detect(opts)
	if r.SessionType != "wayland" {
		t.Errorf("session = %q", r.SessionType)
	}
	if r.Desktop != "KDE" {
		t.Errorf("desktop = %q", r.Desktop)
	}
	if r.Distro != "arch" {
		t.Errorf("distro = %q", r.Distro)
	}
	if !r.SystemdUser {
		t.Error("systemd user session not detected")
	}
	if !r.Network {
		t.Error("network not detected")
	}
	if r.PackageCmd != "pacman -S" {
		t.Errorf("package cmd = %q", r.PackageCmd)
	}
	// qml + plasmashell is the Qt WebEngine route.
	if r.WebEngineName == "" || !strings.Contains(r.WebEngineName, "WebEngine") {
		t.Errorf("web engine = %q", r.WebEngineName)
	}
}

func TestMissingWebEngineNamesThePackage(t *testing.T) {
	opts := fakeEnv(map[string]string{
		"XDG_SESSION_TYPE":    "x11",
		"XDG_CURRENT_DESKTOP": "XFCE",
		"DISTRO_ID":           "debian",
	}, map[string]bool{}, false)
	r := Detect(opts)
	var webCheck *Check
	for i := range r.Checks {
		if r.Checks[i].Name == "web engine" {
			webCheck = &r.Checks[i]
		}
	}
	if webCheck == nil {
		t.Fatal("no web engine check")
	}
	if webCheck.Status != Missing {
		t.Errorf("web engine status = %q, want missing", webCheck.Status)
	}
	if !strings.Contains(webCheck.Package, "apt install") {
		t.Errorf("package hint %q lacks the distro command", webCheck.Package)
	}
	if r.PackageCmd != "apt install" {
		t.Errorf("package cmd = %q", r.PackageCmd)
	}
}

func TestPrintNeverRunsAPackageManager(t *testing.T) {
	// Print only formats. It takes a writer and returns; the test asserts it
	// wrote a package hint rather than executing one.
	opts := fakeEnv(map[string]string{"DISTRO_ID": "fedora"}, map[string]bool{}, false)
	r := Detect(opts)
	var sb strings.Builder
	Print(&sb, r)
	out := sb.String()
	if !strings.Contains(out, "check") || !strings.Contains(out, "status") {
		t.Errorf("no table header:\n%s", out)
	}
	if !strings.Contains(out, "spinewall never runs a package manager") {
		t.Errorf("missing the never-run note:\n%s", out)
	}
}

func TestOfflineIsReported(t *testing.T) {
	opts := fakeEnv(map[string]string{"DISTRO_ID": "arch"}, map[string]bool{}, false)
	r := Detect(opts)
	if r.Network {
		t.Error("network true when dial fails")
	}
	var sb strings.Builder
	Print(&sb, r)
	if !strings.Contains(sb.String(), "offline") {
		t.Errorf("offline not reported:\n%s", sb.String())
	}
}

func TestArchFamilyResolvesToPacman(t *testing.T) {
	// A derivative whose own id is unknown resolves through ID_LIKE, which is how
	// an Arch-based distro gets the pacman command without naming it.
	opts := fakeEnv(map[string]string{
		"DISTRO_ID":      "somearchclone",
		"DISTRO_ID_LIKE": "arch",
	}, map[string]bool{}, true)
	r := Detect(opts)
	if r.PackageCmd != "pacman -S" {
		t.Errorf("package cmd = %q, want pacman -S", r.PackageCmd)
	}
}

func TestRecommendBackendIsKdeLegacy(t *testing.T) {
	opts := fakeEnv(map[string]string{"XDG_CURRENT_DESKTOP": "GNOME"}, map[string]bool{}, true)
	r := Detect(opts)
	if r.Backend != "kde-legacy" {
		t.Errorf("backend = %q", r.Backend)
	}
}
