//go:build windows

package store

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// Historic value from before the app got its name. Never change it: stored
// passwords would no longer decrypt.
var dpapiEntropy = []byte("shellbird-secret-v1")

// DPAPI encrypts secrets bound to the current Windows user account.
type DPAPI struct{}

// DefaultProtector returns the platform secret protector.
func DefaultProtector() Protector { return DPAPI{} }

func blob(b []byte) *windows.DataBlob {
	return &windows.DataBlob{Size: uint32(len(b)), Data: &b[0]}
}

func takeBlob(out windows.DataBlob) []byte {
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...)
}

// Protect encrypts plain with CryptProtectData.
func (DPAPI) Protect(plain []byte) ([]byte, error) {
	if len(plain) == 0 {
		return []byte{}, nil // DPAPI rejects empty input
	}
	var out windows.DataBlob
	err := windows.CryptProtectData(blob(plain), nil, blob(dpapiEntropy), 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	if err != nil {
		return nil, err
	}
	return takeBlob(out), nil
}

// Unprotect decrypts data produced by Protect.
func (DPAPI) Unprotect(cipher []byte) ([]byte, error) {
	if len(cipher) == 0 {
		return []byte{}, nil
	}
	var out windows.DataBlob
	err := windows.CryptUnprotectData(blob(cipher), nil, blob(dpapiEntropy), 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	if err != nil {
		return nil, err
	}
	return takeBlob(out), nil
}
