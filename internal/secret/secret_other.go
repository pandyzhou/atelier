//go:build !windows

package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
)

// scheme names how the text was sealed: AES-256-GCM, with the key of the
// file beside the data.
const scheme = "aesgcm"

// key reads the key file, making it on the first use.
func (b *Box) key() ([]byte, error) {
	path := filepath.Join(b.dir, "secret.key")
	if key, err := os.ReadFile(path); err == nil && len(key) == 32 {
		return key, nil
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if b.dir == "" {
		return nil, errors.New("secret: no directory for the key")
	}
	if err := os.MkdirAll(b.dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

func (b *Box) gcm() (cipher.AEAD, error) {
	key, err := b.key()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func (b *Box) seal(plain []byte) ([]byte, error) {
	gcm, err := b.gcm()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plain, nil), nil
}

func (b *Box) open(sealed []byte) ([]byte, error) {
	gcm, err := b.gcm()
	if err != nil {
		return nil, err
	}
	if len(sealed) < gcm.NonceSize() {
		return nil, errors.New("secret: too short")
	}
	return gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], nil)
}
