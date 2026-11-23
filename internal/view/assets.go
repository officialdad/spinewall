// Package view assets are embedded so the binary can write them out and build
// the launcher on the user's machine. They are never embedded as a runnable
// engine: the build needs the system Qt WebEngine, which a CGO_ENABLED=0 Go
// binary cannot link.
package view

import _ "embed"

// LauncherSource is the C++ launcher the user's machine compiles.
//
//go:embed assets/launcher.cpp
var LauncherSource []byte

// SurfaceQML is the layer-shell surface the launcher loads.
//
//go:embed assets/view.qml
var SurfaceQML []byte
