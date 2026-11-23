package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/officialdad/spinewall/internal/doctor"
	"github.com/officialdad/spinewall/internal/install"
	"github.com/officialdad/spinewall/internal/runtime"
	"github.com/officialdad/spinewall/internal/tui"
	"github.com/officialdad/spinewall/theme"
)

// runDoctor prints the found/missing table. It runs no package manager.
func runDoctor(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("doctor takes no arguments")
	}
	report := doctor.Detect(doctor.Options{})
	doctor.Print(os.Stdout, report)
	if legacy := install.DetectLegacy(install.DefaultPaths()); len(legacy) > 0 {
		tui.PrintLegacyWarning(os.Stdout, legacy)
	}
	return nil
}

// runSetup runs the setup wizard. Every step is skippable and idempotent.
func runSetup(args []string) error {
	opts := tui.Options{Paths: install.DefaultPaths()}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--yes", "-y":
			opts.Yes = true
		case "--accept-spine-license":
			opts.LicenseAccepted = true
		case "--offline":
			opts.Offline = true
		case "--systemd-unit":
			opts.SystemdUnit = true
		case "--alongside-legacy":
			opts.AllowLegacy = true
		case "--backend":
			if i+1 >= len(args) {
				return fmt.Errorf("--backend needs a value")
			}
			i++
			opts.Backend = args[i]
		case "--model-dir":
			if i+1 >= len(args) {
				return fmt.Errorf("--model-dir needs a directory")
			}
			i++
			opts.ModelDir = args[i]
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}
	_, err := tui.Run(opts)
	return err
}

// runInstall installs without the wizard. It is the non-interactive path that
// mirrors the install step of setup.
func runInstall(args []string) error {
	opts := install.Options{Paths: install.DefaultPaths(), Backend: install.BackendKDEBackend}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--systemd-unit":
			opts.SystemdUnit = true
		case "--alongside-legacy":
			opts.AllowLegacy = true
		case "--model-dir":
			if i+1 >= len(args) {
				return fmt.Errorf("--model-dir needs a directory")
			}
			i++
			opts.ModelDir = args[i]
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}
	opts.Logf = func(line string) { fmt.Fprintln(os.Stdout, line) }
	res, err := install.Install(opts)
	if err != nil {
		return err
	}
	fmt.Printf("installed %d file(s). Run 'spinewall serve' or log in to start the server.\n", len(res.Manifest.Files))
	return nil
}

// runUninstall removes the files the manifest records. It asks before touching
// state and cache, and never touches the models directory. In --yes mode it
// removes state and cache too, so a throwaway HOME returns to its earlier shape.
func runUninstall(args []string) error {
	paths := install.DefaultPaths()
	opts := install.UninstallOptions{Paths: paths, StopProcess: true}
	ask := true
	yes := false
	legacy := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--legacy":
			legacy = true
		case "--yes", "-y":
			yes = true
		case "--keep-state":
			opts.RemoveState = false
			opts.RemoveCache = false
			ask = false
		case "--purge":
			opts.RemoveState = true
			opts.RemoveCache = true
			ask = false
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}

	if legacy {
		if err := install.LegacyUninstall(paths, func(line string) { fmt.Fprintln(os.Stdout, line) }); err != nil {
			return err
		}
	}

	switch {
	case ask && isTTY(os.Stdin) && !yes:
		if confirm("Also remove saved prefs and the runtime cache?") {
			opts.RemoveState = true
			opts.RemoveCache = true
		}
	case ask && yes:
		// A scripted uninstall cannot ask, so it removes everything setup wrote,
		// which is what restores the home.
		opts.RemoveState = true
		opts.RemoveCache = true
	}
	opts.Logf = func(line string) { fmt.Fprintln(os.Stdout, line) }
	res, err := install.Uninstall(opts)
	if err != nil {
		return err
	}
	fmt.Printf("removed %d file(s).\n", len(res.RemovedFiles))
	if len(res.KeptFiles) > 0 {
		fmt.Printf("kept %d file(s) you changed.\n", len(res.KeptFiles))
	}
	if !opts.RemoveState {
		fmt.Println("state and cache were kept. Run 'spinewall uninstall --purge' to remove them.")
	}
	if legacy {
		fmt.Println("The old Python install and this install are both removed. Run 'spinewall setup' to install spinewall again.")
	}
	return nil
}

// runBuildTheme writes the page, the local host, and the transformed Spine
// players into a directory. package-windows.py uses it so the runtime pins stay
// in the Go binary, not in a shell script.
func runBuildTheme(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: spinewall build-theme DIR")
	}
	out := args[0]
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	themeFiles := map[string][]byte{
		"index.html":      theme.IndexHTML,
		"local-host.js":   theme.LocalHostJS,
		"spinewall.theme": theme.ThemeManifest,
	}
	for name, data := range themeFiles {
		if err := writeFile(out, name, data); err != nil {
			return err
		}
	}

	manager := runtime.New(runtime.Options{
		Dir:  envOrDefault("SPINEWALL_RUNTIME_DIR", runtime.DefaultDir()),
		Logf: func(line string) { fmt.Fprintln(os.Stderr, line) },
	})
	if err := manager.FetchAll(); err != nil {
		return err
	}
	// Copy every fetched player and the stylesheet from the cache.
	man := manager.Manifest()
	for name := range man.Files {
		data, ok := manager.File(name)
		if !ok {
			return fmt.Errorf("runtime file %s missing from the cache", name)
		}
		if err := writeFile(out, name, data); err != nil {
			return err
		}
	}
	fmt.Printf("built theme into %s\n", out)
	return nil
}

// writeFile writes data to dir/name with a fixed mode.
func writeFile(dir, name string, data []byte) error {
	return os.WriteFile(filepath.Join(dir, name), data, 0o644)
}
func isTTY(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// confirm asks a yes/no question on the terminal.
func confirm(question string) bool {
	fmt.Fprintf(os.Stdout, "%s [y/N] ", question)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}
