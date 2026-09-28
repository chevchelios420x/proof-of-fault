//go:build windows

package secret

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

func blob(b []byte) *windows.DataBlob {
	if len(b) == 0 {
		return &windows.DataBlob{}
	}
	return &windows.DataBlob{Size: uint32(len(b)), Data: &b[0]}
}

func out(d *windows.DataBlob) []byte {
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(d.Data)))
	return append([]byte(nil), unsafe.Slice(d.Data, d.Size)...)
}

func protect(b []byte) ([]byte, bool) {
	var o windows.DataBlob
	if err := windows.CryptProtectData(blob(b), nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &o); err != nil {
		return nil, false
	}
	return out(&o), true
}

func unprotect(b []byte) ([]byte, bool) {
	var o windows.DataBlob
	if err := windows.CryptUnprotectData(blob(b), nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &o); err != nil {
		return nil, false
	}
	return out(&o), true
}
