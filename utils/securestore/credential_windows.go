//go:build windows

package securestore

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

const (
	credentialTypeGeneric   = 1
	credentialPersistLocal  = 2
	errorNotFound           = syscall.Errno(1168)
	maxCredentialBlobLength = 5 * 512
)

var (
	advapi32    = syscall.NewLazyDLL("advapi32.dll")
	credWriteW  = advapi32.NewProc("CredWriteW")
	credReadW   = advapi32.NewProc("CredReadW")
	credDeleteW = advapi32.NewProc("CredDeleteW")
	credFree    = advapi32.NewProc("CredFree")
)

// credential 对应 Windows 的 CREDENTIALW。InkFlow 通用凭据不使用属性，故特意
// 保持为 nil。
type credential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        syscall.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

// WindowsCredentialStore 将每个值作为当前 Windows 用户的“凭据管理器通用凭据”
// 保存，操作系统会在静态存储时加密。
type WindowsCredentialStore struct{}

func New() Store { return WindowsCredentialStore{} }

func (WindowsCredentialStore) Load(key string) ([]byte, error) {
	target, err := syscall.UTF16PtrFromString(key)
	if err != nil {
		return nil, fmt.Errorf("credential target: %w", err)
	}
	var result *credential
	r1, _, callErr := credReadW.Call(
		uintptr(unsafe.Pointer(target)),
		uintptr(credentialTypeGeneric),
		0,
		uintptr(unsafe.Pointer(&result)),
	)
	if r1 == 0 {
		if errors.Is(callErr, errorNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("read Windows credential: %w", callErr)
	}
	defer credFree.Call(uintptr(unsafe.Pointer(result)))
	if result == nil || result.CredentialBlobSize == 0 {
		return nil, ErrNotFound
	}
	return append([]byte(nil), unsafe.Slice(result.CredentialBlob, int(result.CredentialBlobSize))...), nil
}

func (WindowsCredentialStore) Save(key string, value []byte) error {
	if len(value) == 0 {
		return fmt.Errorf("credential value is empty")
	}
	if len(value) > maxCredentialBlobLength {
		return fmt.Errorf("credential value exceeds Windows Credential Manager limit")
	}
	target, err := syscall.UTF16PtrFromString(key)
	if err != nil {
		return fmt.Errorf("credential target: %w", err)
	}
	username, err := syscall.UTF16PtrFromString("InkFlow")
	if err != nil {
		return fmt.Errorf("credential user name: %w", err)
	}
	entry := credential{
		Type:               credentialTypeGeneric,
		TargetName:         target,
		CredentialBlobSize: uint32(len(value)),
		CredentialBlob:     &value[0],
		Persist:            credentialPersistLocal,
		UserName:           username,
	}
	if r1, _, callErr := credWriteW.Call(uintptr(unsafe.Pointer(&entry)), 0); r1 == 0 {
		return fmt.Errorf("write Windows credential: %w", callErr)
	}
	return nil
}

func (WindowsCredentialStore) Delete(key string) error {
	target, err := syscall.UTF16PtrFromString(key)
	if err != nil {
		return fmt.Errorf("credential target: %w", err)
	}
	if r1, _, callErr := credDeleteW.Call(uintptr(unsafe.Pointer(target)), uintptr(credentialTypeGeneric), 0); r1 == 0 {
		if errors.Is(callErr, errorNotFound) {
			return nil
		}
		return fmt.Errorf("delete Windows credential: %w", callErr)
	}
	return nil
}
