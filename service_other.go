//go:build !linux && !windows

package main

import (
	"errors"
	"fmt"
	"os"
)

var errNoService = errors.New("Autostart ist auf diesem System noch nicht umgesetzt – bitte `proof-of-fault run` selbst starten")

func serviceInstalled() bool  { return false }
func installService() error   { return errNoService }
func startService() error     { return errNoService }
func stopServiceManager()     {}
func uninstallService() error { return nil }
func openBrowser(string) bool { return false }
func hasConsole() bool        { return true }
func say(msg string)          { fmt.Println(msg) }
func fail(msg string) {
	fmt.Fprintln(os.Stderr, "Fehler: "+msg)
	os.Exit(1)
}
