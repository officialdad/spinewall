// Package tui is the spinewall setup wizard: a terminal form that detects the
// machine, shows the Spine license notice, fetches the runtimes, records the
// models folder, chooses a backend, installs, then starts and verifies.
//
// Every step is skippable and idempotent. --yes runs the same steps with
// defaults and no prompts, so scripts and CI can call it.
package tui

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"charm.land/huh/v2"
	"github.com/officialdad/spinewall/internal/doctor"
	"github.com/officialdad/spinewall/internal/install"
	"github.com/officialdad/spinewall/internal/runtime"
)

// Options configure one setup run.
type Options struct {
	Paths install.Paths
	// Yes runs every step with defaults and no prompts. Used by scripts and CI.
	Yes bool
	// LicenseAccepted skips the consent prompt when the user already accepted.
	LicenseAccepted bool
	// Backend is the backend to install. Empty uses the doctor's recommendation.
	Backend string
	// ModelDir overrides the models folder. Empty keeps the default.
	ModelDir string
	// SystemdUnit writes the opt-in systemd unit without asking.
	SystemdUnit bool
	// AllowLegacy installs autostart next to the old Python install without
	// asking. Both then start a server on one port.
	AllowLegacy bool
	// Offline refuses network fetches.
	Offline bool
	// Executable is the binary to install. Empty uses the running binary.
	Executable string
	// Doctor options let tests replace detection.
	Doctor doctor.Options
	// Runtime manager used for the fetch step. Nil builds one from Options.
	Runtime *runtime.Manager
	// Stdin and Stdout let a test drive the form through a pipe or a pty.
	Stdin  *os.File
	Stdout *os.File
	// Logf receives step progress. Nil writes to Stdout.
	Logf func(string)
}

// Result reports what setup did.
type Result struct {
	Report   doctor.Report
	Install  install.Result
	Models   int
	Skipped  int
	Verified bool
	// AddModelFolder is true when the server started with zero models, so the
	// wizard offered to add a folder.
	AddModelFolder bool
}

// Run runs the wizard and returns its result.
func Run(opts Options) (Result, error) {
	if opts.Paths.Home == "" {
		opts.Paths = install.DefaultPaths()
	}
	stdout := opts.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	logf := opts.Logf
	if logf == nil {
		logf = func(line string) { fmt.Fprintln(stdout, line) }
	}

	var res Result

	// 1. Doctor.
	report := doctor.Detect(opts.Doctor)
	res.Report = report
	// The table is the same whether the wizard is interactive or --yes, so a bug
	// report can quote it.
	doctor.Print(stdout, report)
	logf("checked this machine: " + report.Summary())
	legacy := install.DetectLegacy(opts.Paths)
	if len(legacy) > 0 {
		PrintLegacyWarning(stdout, legacy)
	}

	// 2. Spine licence notice and consent.
	if err := consent(opts, report, stdout); err != nil {
		return res, err
	}

	// 3. Fetch runtimes with progress.
	if err := fetchRuntimes(opts, logf); err != nil {
		// A failed fetch is not fatal: the server still starts and the page shows
		// its "runtime not installed" notice. setup says so and continues.
		logf("runtime fetch: " + err.Error())
	}

	// 4. Models folder choice.
	modelDir := chooseModelDir(opts, report, stdout)

	// 5. Backend choice. Only the KDE legacy backend is installable now.
	backend := chooseBackend(opts, report, stdout)

	// The systemd unit is an opt-in. The XDG autostart entry is the default.
	systemdUnit := chooseSystemdUnit(opts, report, stdout)
	allowLegacy := len(legacy) > 0 && chooseAllowLegacy(opts, stdout)

	// 6. Install.
	inst, err := install.Install(install.Options{
		Paths:       opts.Paths,
		Backend:     install.Backend(backend),
		SystemdUnit: systemdUnit,
		ModelDir:    modelDir,
		AllowLegacy: allowLegacy,
		Executable:  opts.Executable,
		Logf:        logf,
	})
	if err != nil {
		return res, err
	}
	res.Install = inst
	if inst.Autostart != "" {
		printCompositorLine(stdout, report.Desktop, inst.BinPath)
	}

	// The install linked the fetched players into the theme folder. Check that
	// before starting: a page that cannot load them shows "runtime not installed".
	if err := verifyThemeRuntime(opts.Paths); err != nil {
		logf("theme check: " + err.Error())
	}

	// 7. Start and verify via /info. Next to the old Python install a second
	// server would race it for the port, so setup stops before starting one.
	if len(legacy) > 0 && !allowLegacy {
		logf("setup did not start or verify the server. Run 'spinewall uninstall --legacy', then 'spinewall setup' again.")
		return res, nil
	}
	models, skipped, verified, err := startAndVerify(opts, logf)
	if err != nil {
		logf("start and verify: " + err.Error())
	}
	res.Models = models
	res.Skipped = skipped
	res.Verified = verified
	if verified && models == 0 {
		res.AddModelFolder = true
		if opts.Yes {
			fmt.Fprintln(stdout, "No models were found. Put a model folder in "+inst.ModelDir+" (see docs/assets.md).")
		} else {
			offerAddFolder(stdout, inst.ModelDir)
		}
	}

	logf("setup complete")
	return res, nil
}

// consent shows the Spine licence notice and asks for consent. In --yes mode it
// prints the notice and accepts for the user.
func consent(opts Options, report doctor.Report, stdout *os.File) error {
	notice := LicenseNotice()
	if opts.LicenseAccepted {
		fmt.Fprintln(stdout, notice)
		return nil
	}
	if opts.Yes {
		fmt.Fprintln(stdout, notice)
		return nil
	}
	if !interactive(opts) {
		// No terminal and no --yes: print the notice and stop, so a pipe never
		// silently accepts a licence.
		fmt.Fprintln(stdout, notice)
		return fmt.Errorf("run with --yes to accept the Spine licence notice non-interactively")
	}
	value := false
	fmt.Fprintln(stdout, notice)
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Do you hold a Spine licence, or agree to obtain one as the license requires?").
				Value(&value),
		),
	)
	form.WithInput(stdin(opts))
	form.WithOutput(stdout)
	if err := form.Run(); err != nil {
		return err
	}
	if !value {
		return fmt.Errorf("the Spine licence must be accepted to continue. Run again when you can accept it.")
	}
	return nil
}

// fetchRuntimes downloads every pinned player, or uses the cache when offline.
func fetchRuntimes(opts Options, logf func(string)) error {
	manager := opts.Runtime
	if manager == nil {
		manager = runtime.New(runtime.Options{
			Dir:     opts.Paths.Runtime,
			Offline: opts.Offline,
			Logf:    logf,
		})
	}
	return manager.FetchAll()
}

// chooseModelDir asks for the models folder. The default is used in --yes mode
// and when the terminal is absent.
func chooseModelDir(opts Options, report doctor.Report, stdout *os.File) string {
	if opts.ModelDir != "" {
		return opts.ModelDir
	}
	def := opts.Paths.ModelsDir()
	if opts.Yes || !interactive(opts) {
		return def
	}
	value := def
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Models folder").
				Description("Where Spinewall looks for models. You can add more folders later.").
				Value(&value),
		),
	)
	form.WithInput(stdin(opts))
	form.WithOutput(stdout)
	if err := form.Run(); err != nil {
		return def
	}
	if strings.TrimSpace(value) == "" {
		return def
	}
	return value
}

// chooseBackend asks which backend to install. Only the KDE legacy backend is
// installable now; the others are listed as not available yet.
func chooseBackend(opts Options, report doctor.Report, stdout *os.File) string {
	if opts.Backend != "" {
		return opts.Backend
	}
	if opts.Yes || !interactive(opts) {
		return string(install.BackendKDEBackend)
	}
	// A select with the installable backend plus disabled-looking rows would
	// mislead; the note names what is not ready and the select holds the one
	// installable choice.
	choice := string(install.BackendKDEBackend)
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Backend").
				Description("Layer-shell (Wayland), X11, and GNOME backends are not available yet.\nThe KDE Plasma legacy backend installs today."),
			huh.NewSelect[string]().
				Title("Choose a backend").
				Options(huh.NewOption("KDE Plasma legacy (webwallpaper + containment patch)", string(install.BackendKDEBackend))).
				Value(&choice),
		),
	)
	form.WithInput(stdin(opts))
	form.WithOutput(stdout)
	if err := form.Run(); err != nil {
		return string(install.BackendKDEBackend)
	}
	return choice
}

// chooseSystemdUnit asks once whether to generate the opt-in systemd user unit.
// The XDG autostart entry is always written, so the answer only adds a unit.
func chooseSystemdUnit(opts Options, report doctor.Report, stdout *os.File) bool {
	if opts.SystemdUnit {
		return true
	}
	if !report.SystemdUser {
		return false
	}
	if opts.Yes || !interactive(opts) {
		return false
	}
	value := false
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Also install the opt-in systemd user unit?").
				Description("The XDG autostart entry already starts the server at login. The unit is for systemd users who want its hardening.").
				Value(&value),
		),
	)
	form.WithInput(stdin(opts))
	form.WithOutput(stdout)
	if err := form.Run(); err != nil {
		return false
	}
	return value
}

// chooseAllowLegacy asks whether to start the new server at login while the
// old Python install is still present. The default is no, and --yes never
// says yes on the user's behalf.
func chooseAllowLegacy(opts Options, stdout *os.File) bool {
	if opts.AllowLegacy {
		return true
	}
	if opts.Yes || !interactive(opts) {
		return false
	}
	value := false
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Start the new server at login anyway?").
				Description("The old Python server may also start at login. Both use one port, so only one of them runs.").
				Value(&value),
		),
	)
	form.WithInput(stdin(opts))
	form.WithOutput(stdout)
	if err := form.Run(); err != nil {
		return false
	}
	return value
}

// PrintLegacyWarning names the old Python install files and the command that
// removes them. setup and doctor both print it.
func PrintLegacyWarning(w io.Writer, legacy []string) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Warning: the old Python install is still here:")
	for _, path := range legacy {
		fmt.Fprintln(w, "  "+path)
	}
	fmt.Fprintln(w, "It starts its own server on the same port. Remove it with 'spinewall uninstall --legacy',")
	fmt.Fprintln(w, "then run 'spinewall setup' again.")
	fmt.Fprintln(w)
}

// compositorStartLine is the line a compositor that skips XDG autostart needs
// in its own config, and that config's usual path. Spinewall prints it and
// never edits the file.
func compositorStartLine(desktop, binPath string) (config, line string, ok bool) {
	command := binPath + " serve"
	if strings.ContainsAny(binPath, " \t'\"") {
		command = "'" + strings.ReplaceAll(binPath, "'", `'\''`) + "' serve"
	}
	for _, name := range strings.Split(strings.ToLower(desktop), ":") {
		switch name {
		case "sway":
			return "~/.config/sway/config", "exec " + command, true
		case "hyprland":
			return "~/.config/hypr/hyprland.conf", "exec-once = " + command, true
		case "river":
			return "~/.config/river/init", "riverctl spawn " + strconv.Quote(command), true
		}
	}
	return "", "", false
}

// printCompositorLine prints the startup line for sway, Hyprland, or river.
func printCompositorLine(w io.Writer, desktop, binPath string) {
	config, line, ok := compositorStartLine(desktop, binPath)
	if !ok {
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "This compositor does not run XDG autostart entries. Add this line to %s:\n", config)
	fmt.Fprintln(w, "  "+line)
	fmt.Fprintln(w, "Spinewall never edits your compositor config.")
}

// offerAddFolder prints the exact next step when the server found no models.
func offerAddFolder(stdout *os.File, modelDir string) {
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "No models were found, so the wallpaper will be black.")
	fmt.Fprintf(stdout, "Add a model folder to %s, then run `spinewall rescan`.\n", modelDir)
	fmt.Fprintln(stdout, "See docs/assets.md for the model layout.")
}

// interactive reports whether a real terminal can drive the form.
func interactive(opts Options) bool {
	in := opts.Stdin
	if in == nil {
		in = os.Stdin
	}
	out := opts.Stdout
	if out == nil {
		out = os.Stdout
	}
	return isTerminal(in) && isTerminal(out)
}

func stdin(opts Options) *os.File {
	if opts.Stdin != nil {
		return opts.Stdin
	}
	return os.Stdin
}

// isTerminal reports whether f is a character device.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
