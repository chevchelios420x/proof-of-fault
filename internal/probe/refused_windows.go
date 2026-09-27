//go:build windows

package probe

import (
	"errors"
	"syscall"

	"golang.org/x/sys/windows"
)

// isRefused reports a connection actively refused by the host (RST). Windows
// reports it as WSAECONNREFUSED, which is not syscall.ECONNREFUSED.
func isRefused(err error) bool {
	return errors.Is(err, windows.WSAECONNREFUSED) || errors.Is(err, syscall.ECONNREFUSED)
}
