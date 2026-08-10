//go:build !windows

package cli

import (
	"fmt"
	"os"
)

func assessDoctorProbePrivacy(probe *os.File) doctorPrivacyAssessment {
	if err := probe.Chmod(0o600); err != nil {
		return doctorPrivacyAssessment{
			Status: doctorStatusFail,
			Detail: fmt.Sprintf("cannot secure files in the state directory: %v", err),
		}
	}
	return doctorPrivacyAssessment{Status: doctorStatusPass, Detail: "new files can be secured with mode 0600"}
}
