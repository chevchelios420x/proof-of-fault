//go:build !windows

package secret

func protect([]byte) ([]byte, bool)   { return nil, false }
func unprotect([]byte) ([]byte, bool) { return nil, false }
