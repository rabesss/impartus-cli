//go:build windows

package tuihost

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

func createBootstrapFile(path string) (*os.File, error) {
	pathPointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, fmt.Errorf("encode private OpenTUI bootstrap path: %w", err)
	}
	handle, err := windows.CreateFile(
		pathPointer,
		windows.GENERIC_WRITE|windows.READ_CONTROL|windows.WRITE_DAC,
		0,
		nil,
		windows.CREATE_NEW,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(handle), path)
	if file == nil {
		_ = windows.CloseHandle(handle)
		return nil, errors.New("wrap private OpenTUI bootstrap handle")
	}
	return file, nil
}

func secureBootstrapDirectory(path string) error {
	pathPointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("encode private OpenTUI bootstrap directory: %w", err)
	}
	handle, err := windows.CreateFile(
		pathPointer,
		windows.READ_CONTROL|windows.WRITE_DAC,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return fmt.Errorf("open private OpenTUI bootstrap directory handle: %w", err)
	}
	defer windows.CloseHandle(handle)
	return setAndValidatePrivateBootstrapACL(handle, true, "directory")
}

func secureBootstrapFile(file *os.File) error {
	if file == nil {
		return errors.New("private OpenTUI bootstrap file is required")
	}
	return setAndValidatePrivateBootstrapACL(windows.Handle(file.Fd()), false, "file")
}

func validateBootstrapFile(file *os.File) error {
	if file == nil {
		return errors.New("private OpenTUI bootstrap file is required")
	}
	return validatePrivateBootstrapACL(windows.Handle(file.Fd()), "file")
}

func setAndValidatePrivateBootstrapACL(handle windows.Handle, directory bool, label string) error {
	trusted, err := bootstrapWindowsSIDs()
	if err != nil {
		return err
	}
	var pinner runtime.Pinner
	defer pinner.Unpin()
	entries := make([]windows.EXPLICIT_ACCESS, 0, len(trusted))
	for _, sid := range trusted {
		pinner.Pin(sid)
		inheritance := uint32(windows.NO_INHERITANCE)
		if directory {
			inheritance = windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE
		}
		entries = append(entries, windows.EXPLICIT_ACCESS{
			AccessPermissions: windows.GENERIC_ALL,
			AccessMode:        windows.GRANT_ACCESS,
			Inheritance:       inheritance,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_UNKNOWN,
				TrusteeValue: windows.TrusteeValueFromSID(sid),
			},
		})
	}
	acl, err := windows.ACLFromEntries(entries, nil)
	if err != nil {
		return fmt.Errorf("build private OpenTUI bootstrap ACL: %w", err)
	}
	if err := windows.SetSecurityInfo(
		handle,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		acl,
		nil,
	); err != nil {
		return fmt.Errorf("apply private OpenTUI bootstrap %s ACL: %w", label, err)
	}
	return validatePrivateBootstrapACL(handle, label)
}

func validatePrivateBootstrapACL(handle windows.Handle, label string) error {
	trusted, err := bootstrapWindowsSIDs()
	if err != nil {
		return err
	}
	descriptor, err := windows.GetSecurityInfo(
		handle,
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		return fmt.Errorf("inspect private OpenTUI bootstrap %s ACL: %w", label, err)
	}
	if descriptor == nil {
		return fmt.Errorf("private OpenTUI bootstrap %s has no security descriptor", label)
	}
	control, _, err := descriptor.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		return fmt.Errorf("private OpenTUI bootstrap %s DACL is not protected", label)
	}
	owner, _, err := descriptor.Owner()
	if err != nil || !trustedBootstrapWindowsSID(owner, trusted) {
		return fmt.Errorf("private OpenTUI bootstrap %s owner is not trusted", label)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil || dacl == nil {
		return fmt.Errorf("private OpenTUI bootstrap %s has no private DACL", label)
	}
	for index := uint16(0); index < dacl.AceCount; index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, uint32(index), &ace); err != nil {
			return fmt.Errorf("inspect private OpenTUI bootstrap %s ACL entry: %w", label, err)
		}
		if ace.Header.AceType == windows.ACCESS_DENIED_ACE_TYPE {
			continue
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return fmt.Errorf("private OpenTUI bootstrap %s contains an unsupported ACL entry", label)
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart)) // #nosec G103 -- GetAce returns the documented inline SID
		if !trustedBootstrapWindowsSID(sid, trusted) {
			return fmt.Errorf("private OpenTUI bootstrap %s grants access to an untrusted identity", label)
		}
	}
	return nil
}

func bootstrapWindowsSIDs() ([]*windows.SID, error) {
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

func trustedBootstrapWindowsSID(candidate *windows.SID, trusted []*windows.SID) bool {
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
