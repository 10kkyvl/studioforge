//go:build windows

package platform

import (
	"context"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	credTypeGeneric         = 1
	credPersistLocalMachine = 2
	errorNotFoundWin32      = 1168
)

var (
	modAdvapi32     = windows.NewLazySystemDLL("advapi32.dll")
	procCredWriteW  = modAdvapi32.NewProc("CredWriteW")
	procCredReadW   = modAdvapi32.NewProc("CredReadW")
	procCredDeleteW = modAdvapi32.NewProc("CredDeleteW")
	procCredFree    = modAdvapi32.NewProc("CredFree")
)

// credential mirrors the Win32 CREDENTIALW struct layout needed to round-trip
// a generic credential through CredWriteW/CredReadW.
type credential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

type windowsCredentialStore struct {
	service string
}

func openSystemSecretStore(service string) (SecretStore, error) {
	if err := modAdvapi32.Load(); err != nil {
		return nil, ErrSecretStoreUnavailable
	}
	if err := procCredWriteW.Find(); err != nil {
		return nil, ErrSecretStoreUnavailable
	}
	if err := procCredReadW.Find(); err != nil {
		return nil, ErrSecretStoreUnavailable
	}
	if err := procCredDeleteW.Find(); err != nil {
		return nil, ErrSecretStoreUnavailable
	}
	if err := procCredFree.Find(); err != nil {
		return nil, ErrSecretStoreUnavailable
	}
	return &windowsCredentialStore{service: service}, nil
}

func (w *windowsCredentialStore) targetName(key string) string {
	return w.service + ":" + key
}

func (w *windowsCredentialStore) Set(_ context.Context, key string, value []byte) error {
	targetPtr, err := windows.UTF16PtrFromString(w.targetName(key))
	if err != nil {
		return err
	}
	var blobPtr *byte
	if len(value) > 0 {
		blobPtr = &value[0]
	}
	cred := credential{
		Type:               credTypeGeneric,
		TargetName:         targetPtr,
		CredentialBlobSize: uint32(len(value)),
		CredentialBlob:     blobPtr,
		Persist:            credPersistLocalMachine,
	}
	r1, _, callErr := procCredWriteW.Call(uintptr(unsafe.Pointer(&cred)), 0)
	if r1 == 0 {
		return fmt.Errorf("CredWriteW: %w", callErr)
	}
	return nil
}

func (w *windowsCredentialStore) Get(_ context.Context, key string) ([]byte, error) {
	targetPtr, err := windows.UTF16PtrFromString(w.targetName(key))
	if err != nil {
		return nil, err
	}
	var credPtr *credential
	r1, _, callErr := procCredReadW.Call(uintptr(unsafe.Pointer(targetPtr)), uintptr(credTypeGeneric), 0, uintptr(unsafe.Pointer(&credPtr)))
	if r1 == 0 {
		if errno, ok := callErr.(windows.Errno); ok && uint32(errno) == errorNotFoundWin32 {
			return nil, ErrSecretNotFound
		}
		return nil, fmt.Errorf("CredReadW: %w", callErr)
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(credPtr)))
	if credPtr.CredentialBlobSize == 0 || credPtr.CredentialBlob == nil {
		return []byte{}, nil
	}
	blob := unsafe.Slice(credPtr.CredentialBlob, credPtr.CredentialBlobSize)
	out := make([]byte, len(blob))
	copy(out, blob)
	return out, nil
}

func (w *windowsCredentialStore) Delete(_ context.Context, key string) error {
	targetPtr, err := windows.UTF16PtrFromString(w.targetName(key))
	if err != nil {
		return err
	}
	r1, _, callErr := procCredDeleteW.Call(uintptr(unsafe.Pointer(targetPtr)), uintptr(credTypeGeneric), 0)
	if r1 == 0 {
		if errno, ok := callErr.(windows.Errno); ok && uint32(errno) == errorNotFoundWin32 {
			return nil
		}
		return fmt.Errorf("CredDeleteW: %w", callErr)
	}
	return nil
}
