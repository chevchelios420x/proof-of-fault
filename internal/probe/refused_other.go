//go:build !windows

package probe

import (
	"errors"
	"syscall"
)

// isRefused reports a connection actively refused by the host (RST).
func isRefused(err error) bool { return errors.Is(err, syscall.ECONNREFUSED) }
