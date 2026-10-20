//go:build !windows

package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"os"
	"path/filepath"
	"sync"
)

// Without DPAPI the value is encrypted with AES-256-GCM. The key is created
// once and stored readable only by the user (0600) next to the database, so
// the database alone (e.g. a copy or backup) does not reveal the password.

var (
	keyOnce sync.Once
	gcm     cipher.AEAD
)

func aead() cipher.AEAD {
	keyOnce.Do(func() {
		dir, err := os.UserConfigDir()
		if err != nil {
			return
		}
		dir = filepath.Join(dir, "proof-of-fault")
		os.MkdirAll(dir, 0o700)
		p := filepath.Join(dir, "secret.key")
		key, err := os.ReadFile(p)
		if err != nil || len(key) != 32 {
			key = make([]byte, 32)
			if _, err := rand.Read(key); err != nil {
				return
			}
			if err := os.WriteFile(p, key, 0o600); err != nil {
				return
			}
		}
		b, err := aes.NewCipher(key)
		if err != nil {
			return
		}
		gcm, _ = cipher.NewGCM(b)
	})
	return gcm
}

func protect(b []byte) ([]byte, bool) {
	g := aead()
	if g == nil {
		return nil, false
	}
	nonce := make([]byte, g.NonceSize())
	rand.Read(nonce)
	return g.Seal(nonce, nonce, b, nil), true
}

func unprotect(b []byte) ([]byte, bool) {
	g := aead()
	if g == nil || len(b) < g.NonceSize() {
		return nil, false
	}
	out, err := g.Open(nil, b[:g.NonceSize()], b[g.NonceSize():], nil)
	return out, err == nil
}
