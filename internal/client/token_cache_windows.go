//go:build windows

package client

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

func readTokenCacheFile(path string) ([]byte, error) {
	handle, err := openTokenCacheHandle(path, windows.GENERIC_READ|windows.READ_CONTROL)
	if err != nil {
		return nil, err
	}
	if err := validatePrivateTokenCacheACL(handle); err != nil {
		return nil, errors.Join(err, windows.CloseHandle(handle))
	}
	file := os.NewFile(uintptr(handle), path)
	if file == nil {
		return nil, errors.Join(os.ErrInvalid, windows.CloseHandle(handle))
	}
	defer func() { _ = file.Close() }() //nolint:errcheck
	return io.ReadAll(file)
}

func createTokenCacheTemp(parent, path string) (*os.File, error) {
	descriptor, err := privateTokenCacheSecurityDescriptor()
	if err != nil {
		return nil, err
	}
	attributes := windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: descriptor,
	}
	for attempt := 0; attempt < 128; attempt++ {
		var random [12]byte
		if _, err := rand.Read(random[:]); err != nil {
			return nil, fmt.Errorf("generate token cache temporary name: %w", err)
		}
		candidate := filepath.Join(parent, "."+filepath.Base(path)+".tmp-"+hex.EncodeToString(random[:]))
		candidatePointer, err := windows.UTF16PtrFromString(candidate)
		if err != nil {
			return nil, err
		}
		handle, err := windows.CreateFile(
			candidatePointer,
			windows.GENERIC_READ|windows.GENERIC_WRITE|windows.READ_CONTROL|windows.WRITE_DAC,
			windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
			&attributes,
			windows.CREATE_NEW,
			windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_WRITE_THROUGH,
			0,
		)
		runtime.KeepAlive(descriptor)
		if errors.Is(err, windows.ERROR_FILE_EXISTS) || errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if err := validateTokenCacheHandle(handle, true); err != nil {
			return nil, errors.Join(err, windows.CloseHandle(handle), os.Remove(candidate))
		}
		file := os.NewFile(uintptr(handle), candidate)
		if file == nil {
			return nil, errors.Join(os.ErrInvalid, windows.CloseHandle(handle), os.Remove(candidate))
		}
		return file, nil
	}
	return nil, errors.New("could not allocate a unique token cache temporary file")
}

func replaceTokenCacheFile(from, to string) error {
	fromPointer, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	toPointer, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(
		fromPointer,
		toPointer,
		windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH,
	)
}

func validatePublishedTokenCache(path string) error {
	handle, err := openTokenCacheHandle(path, windows.READ_CONTROL)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	return validateTokenCacheHandle(handle, true)
}

func openTokenCacheHandle(path string, access uint32) (windows.Handle, error) {
	pathPointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return windows.InvalidHandle, err
	}
	handle, err := windows.CreateFile(
		pathPointer,
		access,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return windows.InvalidHandle, err
	}
	if err := validateTokenCacheHandle(handle, false); err != nil {
		return windows.InvalidHandle, errors.Join(err, windows.CloseHandle(handle))
	}
	return handle, nil
}

func validateTokenCacheHandle(handle windows.Handle, requirePrivate bool) error {
	var information windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &information); err != nil {
		return fmt.Errorf("inspect opened token cache: %w", err)
	}
	if information.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 {
		return errors.New("token cache path must be a regular non-reparse file")
	}
	if requirePrivate {
		return validatePrivateTokenCacheACL(handle)
	}
	return nil
}

func privateTokenCacheSecurityDescriptor() (*windows.SECURITY_DESCRIPTOR, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("read current Windows user: %w", err)
	}
	userSID := user.User.Sid.String()
	descriptor, err := windows.SecurityDescriptorFromString(fmt.Sprintf(
		"O:%sD:P(A;;GA;;;%s)(A;;GA;;;SY)(A;;GA;;;BA)",
		userSID,
		userSID,
	))
	if err != nil {
		return nil, fmt.Errorf("build private token cache security descriptor: %w", err)
	}
	return descriptor, nil
}

func validatePrivateTokenCacheACL(handle windows.Handle) error {
	trusted, err := privateTokenCacheSIDs()
	if err != nil {
		return err
	}
	descriptor, err := windows.GetSecurityInfo(
		handle,
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		return fmt.Errorf("inspect token cache ACL: %w", err)
	}
	control, _, err := descriptor.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		return errors.New("token cache DACL is not protected")
	}
	owner, _, err := descriptor.Owner()
	if err != nil || !trustedTokenCacheSID(owner, trusted) {
		return errors.New("token cache owner is not trusted")
	}
	dacl, _, err := descriptor.DACL()
	if err != nil || dacl == nil {
		return errors.New("token cache has no private DACL")
	}
	for index := uint16(0); index < dacl.AceCount; index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, uint32(index), &ace); err != nil {
			return fmt.Errorf("inspect token cache ACL entry: %w", err)
		}
		if ace.Header.AceType == windows.ACCESS_DENIED_ACE_TYPE {
			continue
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return errors.New("token cache contains an unsupported ACL entry")
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart)) // #nosec G103 -- GetAce returns the documented inline SID
		if !trustedTokenCacheSID(sid, trusted) {
			return errors.New("token cache grants access to an untrusted identity")
		}
	}
	return nil
}

func privateTokenCacheSIDs() ([]*windows.SID, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("read current Windows user: %w", err)
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return nil, fmt.Errorf("resolve SYSTEM SID: %w", err)
	}
	administrators, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return nil, fmt.Errorf("resolve Administrators SID: %w", err)
	}
	return []*windows.SID{user.User.Sid, system, administrators}, nil
}

func trustedTokenCacheSID(candidate *windows.SID, trusted []*windows.SID) bool {
	if candidate == nil || !candidate.IsValid() {
		return false
	}
	for _, sid := range trusted {
		if sid != nil && candidate.Equals(sid) {
			return true
		}
	}
	return false
}
