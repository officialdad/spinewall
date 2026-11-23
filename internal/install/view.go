package install

import (
	"path/filepath"

	"github.com/officialdad/spinewall/internal/view"
)

// installView writes the native launcher source and QML into a cache directory
// and, when BuildView is set, builds the launcher. The files are recorded in the
// manifest so uninstall removes them.
//
// The binary is built on the user's machine because QtWebEngine cannot link into
// a CGO_ENABLED=0 Go binary. A build failure is not fatal: the source stays in
// place, and `spinewall serve --with-view` builds it on first start.
func installView(w *writer, paths Paths, opts Options, logf func(string)) (string, error) {
	dir := paths.ViewDir()
	if err := w.ensureDir(dir); err != nil {
		return "", err
	}
	src := filepath.Join(dir, "launcher.cpp")
	qml := filepath.Join(dir, "view.qml")
	qmlX11 := filepath.Join(dir, "view-x11.qml")
	if err := w.write(src, view.LauncherSource, 0o644); err != nil {
		return "", err
	}
	// Both surfaces ship in the one launcher. A host that boots into either
	// session type finds the QML it needs without a reinstall.
	if err := w.write(qml, view.SurfaceQML, 0o644); err != nil {
		return "", err
	}
	if err := w.write(qmlX11, view.SurfaceX11QML, 0o644); err != nil {
		return "", err
	}

	if !opts.BuildView {
		logf("view launcher source written; it builds on first start")
		return dir, nil
	}

	bin, err := view.Build(dir, logf)
	if err != nil {
		// A machine without the Qt development files still gets a working install
		// if Qt WebEngine and a compiler arrive later; say so and keep going.
		logf("could not build the view launcher now: " + err.Error())
		return dir, nil
	}
	hash, err := sha256File(bin)
	if err != nil {
		return "", err
	}
	w.files[bin] = Entry{Path: bin, SHA256: hash, Mode: "0755", Created: true}
	logf("built view launcher: " + bin)
	return dir, nil
}

// ViewDir is where the native launcher source and binary live. It is the one
// definition: setup installs here and serve builds and runs from here.
func (p Paths) ViewDir() string {
	return filepath.Join(p.CacheDir, "view")
}
