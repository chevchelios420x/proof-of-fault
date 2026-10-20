//go:build windows

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// On Windows the service is started at logon through the user's Run key
// (HKCU) – a real Windows service would need administrator rights. The
// program is copied to %LOCALAPPDATA%\Programs\proof-of-fault so that the
// downloaded file can be deleted or replaced by a newer version.

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`
const runValue = "proof-of-fault"

func installedExe() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base, _ = os.UserConfigDir()
	}
	return filepath.Join(base, "Programs", "proof-of-fault", "proof-of-fault.exe")
}

func serviceInstalled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetStringValue(runValue)
	return err == nil && v == runCommand()
}

func runCommand() string { return `"` + installedExe() + `" run` }

func installService() error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	dst := installedExe()
	if !sameFile(self, dst) {
		if err := copyFile(self, dst); err != nil {
			return fmt.Errorf("Programm nach %s kopieren: %w", dst, err)
		}
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(runValue, runCommand())
}

func startService() error {
	cmd := exec.Command(installedExe(), "run")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func stopServiceManager() {}

func uninstallService() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return nil
	}
	defer k.Close()
	k.DeleteValue(runValue)
	return nil
}

func openBrowser(url string) bool {
	verb, _ := windows.UTF16PtrFromString("open")
	u, _ := windows.UTF16PtrFromString(url)
	return windows.ShellExecute(0, verb, u, nil, nil, windows.SW_SHOWNORMAL) == nil
}

// hasConsole: the program is built as GUI application; started from a
// console its output is not visible, so messages go to a dialog.
func hasConsole() bool { return false }

var (
	user32      = windows.NewLazySystemDLL("user32.dll")
	pMessageBox = user32.NewProc("MessageBoxW")
)

func messageBox(msg string, flags uintptr) {
	t, _ := windows.UTF16PtrFromString("proof-of-fault v" + Version)
	m, _ := windows.UTF16PtrFromString(msg)
	pMessageBox.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), flags)
}

func say(msg string) {
	if os.Getenv("POF_CONSOLE") != "" {
		fmt.Println(msg)
		return
	}
	messageBox(msg, 0x40) // MB_ICONINFORMATION
}

func fail(msg string) {
	if os.Getenv("POF_CONSOLE") != "" {
		fmt.Fprintln(os.Stderr, "Fehler: "+msg)
	} else {
		messageBox(msg, 0x10) // MB_ICONERROR
	}
	os.Exit(1)
}

func sameFile(a, b string) bool {
	sa, err1 := os.Stat(a)
	sb, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(sa, sb)
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
