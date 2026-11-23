package secret

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// scheme names how the text was sealed: by DPAPI, for the user's account.
const scheme = "dpapi"

func blob(data []byte) *windows.DataBlob {
	if len(data) == 0 {
		return &windows.DataBlob{}
	}
	return &windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
}

// bytes copies what the system returned, and frees the system's copy.
func bytes(out *windows.DataBlob) []byte {
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...)
}

func (b *Box) seal(plain []byte) ([]byte, error) {
	var out windows.DataBlob
	// No prompt: the app has no place for the system's dialog.
	if err := windows.CryptProtectData(blob(plain), nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	return bytes(&out), nil
}

func (b *Box) open(sealed []byte) ([]byte, error) {
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(blob(sealed), nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	return bytes(&out), nil
}
