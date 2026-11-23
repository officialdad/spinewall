package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/godbus/dbus/v5"
)

// errScreenUnsupported means this desktop offers no way to read the active
// monitor, so the caller falls back to the default stage instead of failing.
var errScreenUnsupported = errors.New("screen current is not supported on this desktop")

// kwinOutput is one monitor row from KWin's support information.
type kwinOutput struct {
	name    string
	enabled bool
	x, y    int
	w, h    int
	hasGeom bool
}

// resolveCurrentScreen finds the stage key of the monitor KWin calls active, so a
// shortcut acts on the monitor in use. It reads the active output from D-Bus,
// the output layout from KWin's support information, and the known stage keys
// from the server, then applies the same position match the Python CLI used.
//
// A desktop with no KWin answers errScreenUnsupported. A KWin that answers but
// cannot resolve the monitor is a real error, so the caller names a key instead.
func (r *run) resolveCurrentScreen() (string, error) {
	conn, err := dbus.SessionBus()
	if err != nil {
		// No session bus means no KDE desktop to ask, so fall back cleanly.
		return "", errScreenUnsupported
	}
	obj := conn.Object("org.kde.KWin", dbus.ObjectPath("/KWin"))
	name, err := callString(obj, "org.kde.KWin.activeOutputName")
	if err != nil {
		if isServiceUnknown(err) {
			return "", errScreenUnsupported
		}
		return "", fmt.Errorf("cannot read the active monitor from KWin: %v. Name it with --screen KEY", err)
	}
	if name == "" {
		return "", errScreenUnsupported
	}
	info, err := callString(obj, "org.kde.KWin.supportInformation")
	if err != nil {
		return "", fmt.Errorf("cannot read the monitor layout from KWin: %v", err)
	}
	outputs := parseSupportInformation(info)
	known, err := r.knownScreens()
	if err != nil {
		return "", err
	}
	return pickCurrentScreen(name, outputs, known)
}

// pickCurrentScreen turns the active output name, the output layout and the
// known stage keys into one stage key. No known key means the default stage
// answers: a lone monitor, or a page that has not connected yet.
func pickCurrentScreen(name string, outputs []kwinOutput, known []string) (string, error) {
	if len(known) == 0 {
		return "", errScreenUnsupported
	}
	return matchScreen(name, outputs, known)
}

// callString calls one D-Bus method that returns a single string.
func callString(obj dbus.BusObject, method string) (string, error) {
	call := obj.Call(method, 0)
	if call.Err != nil {
		return "", call.Err
	}
	var s string
	if err := call.Store(&s); err != nil {
		return "", err
	}
	return s, nil
}

// isServiceUnknown reports whether the error means the D-Bus name has no owner,
// which is how a non-KDE desktop answers.
func isServiceUnknown(err error) bool {
	var dbusErr dbus.Error
	return errors.As(err, &dbusErr) &&
		(dbusErr.Name == "org.freedesktop.DBus.Error.ServiceUnknown" ||
			dbusErr.Name == "org.freedesktop.DBus.Error.NameHasNoOwner")
}

// knownScreens lists the stage keys the server knows, in order.
func (r *run) knownScreens() ([]string, error) {
	status, body, err := r.get("/screens", "")
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("cannot list screens: the server answered %d", status)
	}
	var rows []struct {
		Screen string `json:"screen"`
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("cannot read the screen list: %v", err)
	}
	keys := make([]string, len(rows))
	for i, row := range rows {
		keys[i] = row.Screen
	}
	return keys, nil
}

// matchScreen picks the one stage key for a monitor. The exact size and position
// wins, because a monitor that once ran in another mode leaves an old key at the
// same position. A scaled monitor reports a size the page does not use, so the
// position alone is the fallback.
func matchScreen(name string, outputs []kwinOutput, known []string) (string, error) {
	var found *kwinOutput
	for i := range outputs {
		if outputs[i].name == name {
			found = &outputs[i]
			break
		}
	}
	if found == nil || !found.enabled || !found.hasGeom {
		return "", fmt.Errorf("KWin reports monitor %s, which the output list does not include. Name it with --screen KEY", name)
	}
	suffix := fmt.Sprintf("+%d+%d", found.x, found.y)
	exact := fmt.Sprintf("%dx%d%s", found.w, found.h, suffix)
	var match []string
	if containsStr(known, exact) {
		match = []string{exact}
	} else {
		for _, key := range known {
			if strings.HasSuffix(key, suffix) {
				match = append(match, key)
			}
		}
	}
	if len(match) != 1 {
		return "", fmt.Errorf("cannot pick the wallpaper screen of monitor %s at %s from %v. Name it with --screen KEY", name, suffix, known)
	}
	return match[0], nil
}

// parseSupportInformation reads the Screens block of KWin's support information.
// The block lists each monitor as a "Screen N:" group of Name, Enabled and
// Geometry fields. The labels are developer text, never translated.
func parseSupportInformation(text string) []kwinOutput {
	var out []kwinOutput
	var cur *kwinOutput
	started, done := false, false
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimRight(raw, "\r")
		if line == "Screens" {
			started = true
			continue
		}
		if !started || done {
			continue
		}
		if isScreenHeader(line) {
			if cur != nil {
				out = append(out, *cur)
			}
			cur = &kwinOutput{enabled: true}
			continue
		}
		if cur == nil {
			continue
		}
		switch {
		case strings.HasPrefix(line, "Name: "):
			cur.name = strings.TrimSpace(strings.TrimPrefix(line, "Name: "))
		case strings.HasPrefix(line, "Enabled: "):
			cur.enabled = strings.TrimSpace(strings.TrimPrefix(line, "Enabled: ")) != "0"
		case strings.HasPrefix(line, "Geometry: "):
			if x, y, w, h, ok := parseGeometry(strings.TrimSpace(strings.TrimPrefix(line, "Geometry: "))); ok {
				cur.x, cur.y, cur.w, cur.h, cur.hasGeom = x, y, w, h, true
			}
		case isSectionEnd(line):
			out = append(out, *cur)
			cur, done = nil, true
		}
	}
	if cur != nil {
		out = append(out, *cur)
	}
	return out
}

// isScreenHeader matches a line like "Screen 0:".
func isScreenHeader(line string) bool {
	rest, ok := strings.CutPrefix(line, "Screen ")
	if !ok || !strings.HasSuffix(rest, ":") {
		return false
	}
	digits := rest[:len(rest)-1]
	if digits == "" {
		return false
	}
	for _, c := range digits {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// isSectionEnd matches the next top-level heading, a line with no colon and no
// indent, which closes the Screens block.
func isSectionEnd(line string) bool {
	if line == "" || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "---") {
		return false
	}
	return !strings.Contains(line, ":")
}

// parseGeometry reads a KWin geometry string like "3440,236,1920x1080".
func parseGeometry(s string) (x, y, w, h int, ok bool) {
	parts := strings.Split(s, ",")
	if len(parts) != 3 {
		return 0, 0, 0, 0, false
	}
	x, errX := strconv.Atoi(strings.TrimSpace(parts[0]))
	y, errY := strconv.Atoi(strings.TrimSpace(parts[1]))
	size := strings.SplitN(strings.TrimSpace(parts[2]), "x", 2)
	if errX != nil || errY != nil || len(size) != 2 {
		return 0, 0, 0, 0, false
	}
	w, errW := strconv.Atoi(size[0])
	h, errH := strconv.Atoi(size[1])
	if errW != nil || errH != nil {
		return 0, 0, 0, 0, false
	}
	return x, y, w, h, true
}

func containsStr(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
