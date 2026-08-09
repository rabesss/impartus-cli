//go:build windows

package library

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

func validatePrivateDirectoryPermissions(path string, _ os.FileInfo) error {
	return validatePrivateWindowsACL(path, "library state directory")
}

func validatePrivateDatabasePermissions(path string, _ os.FileInfo) error {
	return validatePrivateWindowsACL(path, "library database")
}

func secureNewStateDirectory(path string) error {
	return setPrivateWindowsACL(path, true)
}

func secureNewDatabaseFile(file *os.File) error {
	if file == nil {
		return errors.New("library database file is required")
	}
	return setPrivateWindowsACL(file.Name(), false)
}

func validatePrivateWindowsACL(path, label string) error {
	trusted, err := privateWindowsSIDs()
	if err != nil {
		return fmt.Errorf("resolve trusted Windows identities: %w", err)
	}
	descriptor, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		return fmt.Errorf("inspect %s ACL: %w", label, err)
	}
	if descriptor == nil {
		return fmt.Errorf("%s has no security descriptor", label)
	}
	owner, _, err := descriptor.Owner()
	if err != nil {
		return fmt.Errorf("inspect %s owner: %w", label, err)
	}
	if owner == nil {
		return fmt.Errorf("%s has no owner", label)
	}
	if !trustedWindowsSID(owner, trusted) {
		return fmt.Errorf("%s owner %s is not the current user, SYSTEM, or Administrators", label, owner)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return fmt.Errorf("inspect %s DACL: %w", label, err)
	}
	if dacl == nil {
		return fmt.Errorf("%s must have an explicit private DACL", label)
	}
	for index := uint16(0); index < dacl.AceCount; index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, uint32(index), &ace); err != nil {
			return fmt.Errorf("inspect %s ACL entry %d: %w", label, index, err)
		}
		if ace.Header.AceType == windows.ACCESS_DENIED_ACE_TYPE {
			continue
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return fmt.Errorf("%s contains unsupported ACL entry type %d", label, ace.Header.AceType)
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart)) // #nosec G103 -- GetAce returns the documented variable-length SID inline
		if !sid.IsValid() {
			return fmt.Errorf("%s contains an invalid ACL identity", label)
		}
		if !trustedWindowsSID(sid, trusted) {
			return fmt.Errorf("%s grants access to untrusted identity %s", label, sid)
		}
	}
	return nil
}

func setPrivateWindowsACL(path string, directory bool) error {
	trusted, err := privateWindowsSIDs()
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
		return fmt.Errorf("build private Windows ACL: %w", err)
	}
	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		acl,
		nil,
	); err != nil {
		return fmt.Errorf("apply private Windows ACL: %w", err)
	}
	return validatePrivateWindowsACL(path, "new private library path")
}

func privateWindowsSIDs() ([]*windows.SID, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("read current process user: %w", err)
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

func trustedWindowsSID(candidate *windows.SID, trusted []*windows.SID) bool {
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
