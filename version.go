package main

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var versionFile string

// Version is the application version (single source: the VERSION file).
var Version = strings.TrimSpace(versionFile)

// GetVersion returns the application version, e.g. "v0.2.0".
func (a *App) GetVersion() string { return "v" + Version }
