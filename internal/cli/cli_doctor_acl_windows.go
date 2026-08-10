//go:build windows

package cli

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

func inspectDoctorWindowsACL(path string) doctorPrivacyAssessment {
	sd, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil || sd == nil {
		return unverifiedDoctorWindowsACL(path, err)
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil {
		return unverifiedDoctorWindowsACL(path, err)
	}
	current, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil || current == nil || current.User.Sid == nil {
		return unverifiedDoctorWindowsACL(path, err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return unverifiedDoctorWindowsACL(path, err)
	}
	if dacl == nil {
		return doctorPrivacyAssessment{
			Status: doctorStatusFail,
			Detail: fmt.Sprintf("%s Windows ACL allows unrestricted access", path),
		}
	}
	control, _, err := sd.Control()
	if err != nil {
		return unverifiedDoctorWindowsACL(path, err)
	}

	entries := make([]doctorACLEntry, 0, dacl.AceCount)
	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, index, &ace); err != nil || ace == nil {
			return unverifiedDoctorWindowsACL(path, err)
		}
		if ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 ||
			ace.Header.AceType == windows.ACCESS_DENIED_ACE_TYPE {
			continue
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return doctorPrivacyAssessment{
				Status: doctorStatusFail,
				Detail: fmt.Sprintf("%s Windows ACL contains unsupported entry type %d", path, ace.Header.AceType),
			}
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if sid == nil || !sid.IsValid() {
			return unverifiedDoctorWindowsACL(path, windows.ERROR_INVALID_SID)
		}
		trusted := sid.Equals(owner) ||
			sid.IsWellKnown(windows.WinLocalSystemSid) ||
			sid.IsWellKnown(windows.WinBuiltinAdministratorsSid)
		entries = append(entries, doctorACLEntry{
			Allowed: true,
			Trusted: trusted,
			Mask:    uint32(ace.Mask),
		})
	}

	assessment := assessDoctorACLEntries(owner.Equals(current.User.Sid), entries)
	if assessment.Status == doctorStatusPass && control&windows.SE_DACL_PROTECTED == 0 {
		assessment.Status = doctorStatusWarn
		assessment.Detail = "Windows ACL is currently private but inherits future permission changes from its parent"
	}
	assessment.Detail = fmt.Sprintf("%s %s", path, assessment.Detail)
	return assessment
}

func unverifiedDoctorWindowsACL(path string, err error) doctorPrivacyAssessment {
	detail := fmt.Sprintf("%s Windows ACL privacy could not be verified", path)
	if err != nil {
		detail += fmt.Sprintf(": %v", err)
	}
	return doctorPrivacyAssessment{Status: doctorStatusWarn, Detail: detail}
}
