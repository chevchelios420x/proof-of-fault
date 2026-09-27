//go:build !windows

package netinfo

import "os/exec"

func hideWindow(*exec.Cmd) {}

func decode(b []byte) string { return string(b) }
