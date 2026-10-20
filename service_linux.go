//go:build linux

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
)

// On Linux the service is a systemd user unit: no root needed. "Linger"
// keeps it running without a login session (after a reboot of the VM).

const unitName = "proof-of-fault.service"

func installedExe() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "bin", "proof-of-fault")
}

func unitPath() string {
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "systemd", "user", unitName)
}

func serviceInstalled() bool {
	_, err := os.Stat(unitPath())
	if err != nil {
		return false
	}
	self, _ := os.Executable()
	return sameFile(self, installedExe())
}

func installService() error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	dst := installedExe()
	if !sameFile(self, dst) {
		if err := copyFile(self, dst, 0o755); err != nil {
			return fmt.Errorf("Programm nach %s kopieren: %w", dst, err)
		}
	}
	unit := `[Unit]
Description=proof-of-fault – Nachweis von Internet-Störungen
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=` + dst + ` run
Restart=always
RestartSec=5

[Install]
WantedBy=default.target
`
	if err := os.MkdirAll(filepath.Dir(unitPath()), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(unitPath(), []byte(unit), 0o644); err != nil {
		return err
	}
	if out, err := systemctl("daemon-reload"); err != nil {
		return fmt.Errorf("systemctl --user nicht verfügbar (%s). Als normaler Benutzer per SSH oder an der Konsole anmelden, nicht per su/sudo: %w", out, err)
	}
	if out, err := systemctl("enable", unitName); err != nil {
		return fmt.Errorf("%s: %w", out, err)
	}
	// Keep the service running without login (allowed for the own user on
	// Ubuntu; otherwise a hint is printed).
	if u, err := user.Current(); err == nil {
		if out, err := exec.Command("loginctl", "enable-linger", u.Username).CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "Hinweis: Ohne Anmeldung läuft der Dienst erst nach einmaligem\n  sudo loginctl enable-linger %s\n(%s)\n", u.Username, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func startService() error {
	if out, err := systemctl("restart", unitName); err != nil {
		return fmt.Errorf("%s: %w", out, err)
	}
	return nil
}

func stopServiceManager() { systemctl("stop", unitName) }

func uninstallService() error {
	systemctl("disable", "--now", unitName)
	os.Remove(unitPath())
	systemctl("daemon-reload")
	return nil
}

func systemctl(args ...string) (string, error) {
	out, err := exec.Command("systemctl", append([]string{"--user"}, args...)...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// openBrowser opens url on a desktop; on a server it returns false.
func openBrowser(url string) bool {
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return false
	}
	return exec.Command("xdg-open", url).Start() == nil
}

func hasConsole() bool { return true }

func say(msg string) { fmt.Println(msg) }

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "Fehler: "+msg)
	os.Exit(1)
}

func sameFile(a, b string) bool {
	sa, err1 := os.Stat(a)
	sb, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(sa, sb)
}

func copyFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".new"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst) // atomic, works while the old binary runs
}
