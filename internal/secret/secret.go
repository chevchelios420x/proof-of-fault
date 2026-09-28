// Package secret protects stored credentials. On Windows it uses DPAPI
// (bound to the Windows user account); elsewhere values are only encoded.
package secret

import (
	"encoding/base64"
	"strings"
)

// Protect encrypts s for storage ("" stays "").
func Protect(s string) string {
	if s == "" {
		return ""
	}
	if b, ok := protect([]byte(s)); ok {
		return "dpapi:" + base64.StdEncoding.EncodeToString(b)
	}
	return "b64:" + base64.StdEncoding.EncodeToString([]byte(s))
}

// Unprotect reverses Protect; unknown formats are returned unchanged.
func Unprotect(s string) string {
	switch {
	case strings.HasPrefix(s, "dpapi:"):
		b, err := base64.StdEncoding.DecodeString(s[6:])
		if err != nil {
			return ""
		}
		if out, ok := unprotect(b); ok {
			return string(out)
		}
		return ""
	case strings.HasPrefix(s, "b64:"):
		b, _ := base64.StdEncoding.DecodeString(s[4:])
		return string(b)
	}
	return s
}
