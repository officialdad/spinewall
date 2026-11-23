package view

import (
	"fmt"
	"os/exec"
)

// browserCandidates are the kiosk browsers to look for, in order. Firefox takes
// its profile in a different form from the Chromium family.
var browserCandidates = []struct {
	name    string
	firefox bool
}{
	{"chromium", false},
	{"chromium-browser", false},
	{"google-chrome-stable", false},
	{"google-chrome", false},
	{"firefox", true},
}

// Browser is a kiosk-browser fallback: a fullscreen window with the page, for a
// desktop where no real wallpaper layer is available. It is not a wallpaper:
// the window sits above everything and takes input like any app. It is best
// effort and unverified; see docs/desktops.md.
type Browser struct {
	Name string
	Args []string
}

// FindBrowser returns the first installed kiosk browser, or "" when none is.
// Doctor and BrowserCommand both use it, so they always name the same browser.
func FindBrowser(lookPath func(string) (string, error)) string {
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	for _, c := range browserCandidates {
		if _, err := lookPath(c.name); err == nil {
			return c.name
		}
	}
	return ""
}

// BrowserCommand returns the kiosk browser command for url, or an error naming
// the supported browsers. lookPath is injected so a test never touches the box.
//
// The browser runs on its own profile in profileDir. With the user's default
// profile, a browser that is already open takes the URL as a normal tab and the
// launched process exits at once, so the kiosk window would never be ours to
// stop, and the page would share the user's cookies and storage.
func BrowserCommand(url, profileDir string, lookPath func(string) (string, error)) (Browser, error) {
	name := FindBrowser(lookPath)
	for _, c := range browserCandidates {
		if c.name != name {
			continue
		}
		if c.firefox {
			return Browser{Name: name, Args: []string{"--new-instance", "--profile", profileDir, "--kiosk", url}}, nil
		}
		return Browser{Name: name, Args: []string{"--user-data-dir=" + profileDir, "--no-first-run", "--no-default-browser-check", "--kiosk", url}}, nil
	}
	return Browser{}, fmt.Errorf("no kiosk browser found: install chromium or firefox, or use the native view")
}

// BrowserURL is the page URL the kiosk fallback loads. It carries the native
// flag so the Edit button is available with no containment patch, the same as
// the native view.
func BrowserURL(apiBase string) string {
	return PageURL(apiBase, "")
}
