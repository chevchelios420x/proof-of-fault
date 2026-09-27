//go:build windows

package netinfo

import (
	"os/exec"
	"syscall"

	"golang.org/x/text/encoding/charmap"
)

// hideWindow keeps console windows of the helper commands from flashing up.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}

// decode converts console output (OEM code page 850 on German/Western
// Windows) to UTF-8.
func decode(b []byte) string {
	if s, err := charmap.CodePage850.NewDecoder().Bytes(b); err == nil {
		return string(s)
	}
	return string(b)
}
