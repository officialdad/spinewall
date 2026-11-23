package view

import (
	"fmt"
	"os/exec"
)

// browserCandidates are the kiosk browsers to look for, in order, with the
// argument that gives a fullscreen window on each.
var browserCandidates = []struct {
	name string
	arg  string
}{
	{"chromium", "--kiosk"},
	{"chromium-browser", "--kiosk"},
	{"google-chrome-stable", "--kiosk"},
	{"google-chrome", "--kiosk"},
	{"firefox", "--kiosk"},
}

// Browser is a kiosk-browser fallback: a fullscreen window with the page, for a
// desktop where no real wallpaper layer is available. It is not a wallpaper:
// the window sits above everything and takes input like any app. It is best
// effort and unverified; see docs/desktops.md.
type Browser struct {
	Name string
	Args []string
}

// BrowserCommand returns the kiosk browser command for url, or an error naming
// the supported browsers. lookPath is injected so a test never touches the box.
func BrowserCommand(url string, lookPath func(string) (string, error)) (Browser, error) {
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	for _, c := range browserCandidates {
		if _, err := lookPath(c.name); err == nil {
			return Browser{Name: c.name, Args: []string{c.arg, url}}, nil
		}
	}
	return Browser{}, fmt.Errorf("no kiosk browser found: install chromium or firefox, or use the native view")
}

// BrowserURL is the page URL the kiosk fallback loads. It carries the native
// flag so the Edit button is available with no containment patch, the same as
// the native view.
func BrowserURL(apiBase string) string {
	return PageURL(apiBase, "")
}
