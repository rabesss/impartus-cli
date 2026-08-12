//go:build !windows

package cli

import "fmt"

func inspectDoctorWindowsACL(path string) doctorPrivacyAssessment {
	return doctorPrivacyAssessment{
		Status: doctorStatusWarn,
		Detail: fmt.Sprintf(
			"%s Windows ACL privacy could not be verified on this platform",
			path,
		),
	}
}
