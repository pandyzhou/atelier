// Package secret keeps what must not be read from the data files, as an API
// key: Seal turns it into text to save, and Open turns that back.
//
// On Windows the system's data protection (DPAPI) encrypts it for the
// user's account: the file copied to another computer, or read by another
// user of this one, gives nothing. A program running as the same user can
// ask the system to open it, as it can read anything the user can.
//
// Elsewhere the text is encrypted with a key kept in a file beside the
// data. That keeps the secret out of the settings file and of what is
// shared or backed up of it alone, no more: who has both files has it.
package secret

import (
	"encoding/base64"
	"errors"
	"strings"
)

// Box seals and opens secrets; dir is where a key file goes, on the
// systems that need one.
type Box struct {
	dir string
}

// New returns a Box whose key file, if it needs one, is in dir.
func New(dir string) *Box { return &Box{dir: dir} }

// Seal encrypts plain into text that can be saved; "" stays "".
func (b *Box) Seal(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	data, err := b.seal([]byte(plain))
	if err != nil {
		return "", err
	}
	return scheme + ":" + base64.StdEncoding.EncodeToString(data), nil
}

// Open decrypts what Seal returned; "" stays "".
func (b *Box) Open(sealed string) (string, error) {
	if sealed == "" {
		return "", nil
	}
	name, text, ok := strings.Cut(sealed, ":")
	if !ok || name != scheme {
		return "", errors.New("secret: sealed by another system, or not sealed")
	}
	data, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		return "", err
	}
	plain, err := b.open(data)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
