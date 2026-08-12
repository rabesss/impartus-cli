//go:build windows

package cli

import "os"

func assessDoctorProbePrivacy(probe *os.File) doctorPrivacyAssessment {
	assessment := inspectDoctorWindowsACL(probe.Name())
	if assessment.Status == doctorStatusPass {
		assessment.Detail = "new files inherit a private Windows ACL"
	}
	return assessment
}
