package doctor

import (
	"fmt"
	"io"
	"strings"
)

// Print writes the found/missing table and any package hints to w. It never
// runs a package manager; it only names the command to run.
func Print(w io.Writer, r Report) {
	fmt.Fprintf(w, "spinewall doctor\n")
	fmt.Fprintf(w, "  os        %s (%s)\n", r.OS, r.Distro)
	fmt.Fprintf(w, "  session   %s\n", r.SessionType)
	fmt.Fprintf(w, "  desktop   %s\n", r.Desktop)
	fmt.Fprintln(w)

	width := len("check")
	for _, c := range r.Checks {
		if len(c.Name) > width {
			width = len(c.Name)
		}
	}
	fmt.Fprintf(w, "  %-*s  %-7s  %s\n", width, "check", "status", "detail")
	for _, c := range r.Checks {
		mark := "yes"
		switch c.Status {
		case Missing:
			mark = "no"
		case Unknown:
			mark = "?"
		}
		fmt.Fprintf(w, "  %-*s  %-7s  %s\n", width, c.Name, mark, c.Detail)
	}

	if hints := r.PackageHints(); len(hints) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "To install what is missing, run the matching line for your distro:")
		for _, hint := range hints {
			fmt.Fprintf(w, "  %s\n", hint)
		}
		fmt.Fprintln(w, "spinewall never runs a package manager for you.")
	}

	if missing := r.Missing(); len(missing) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Recommended backend: %s (some requirements are missing)\n", r.Backend)
	} else {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Recommended backend: %s (all requirements found)\n", r.Backend)
	}
	// Name every backend the user may pick, so a bug report shows the choice.
	fmt.Fprintf(w, "Backends available: %s\n", strings.Join(r.Backends(), ", "))
	// The phase 6 backends are best effort and unconfirmed, so say so the way
	// docs/windows.md does for Windows.
	fmt.Fprintf(w, "Unverified backends: %s (best effort, not confirmed on real hardware; see docs/desktops.md)\n", strings.Join(r.Unverified(), ", "))
	if r.GNOME() {
		fmt.Fprintln(w, "GNOME: unsupported, and no layer-shell backend is offered (see docs/desktops.md)")
	} else {
		fmt.Fprintln(w, "GNOME: unsupported (no layer-shell; see docs/desktops.md)")
	}
}

// Summary is a one-line form for logs.
func (r Report) Summary() string {
	var missing []string
	for _, c := range r.Missing() {
		missing = append(missing, c.Name)
	}
	if len(missing) == 0 {
		return fmt.Sprintf("%s on %s, all requirements found", r.Desktop, r.SessionType)
	}
	return fmt.Sprintf("%s on %s, missing: %s", r.Desktop, r.SessionType, strings.Join(missing, ", "))
}
