package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func readPackagesWorkflow(t *testing.T) string {
	t.Helper()

	data, err := os.ReadFile(".github/workflows/packages.yml")
	if err != nil {
		t.Fatalf("read packages workflow: %v", err)
	}
	return string(data)
}

func TestPackagesWorkflowPublishesScannedMultiPlatformArtifact(t *testing.T) {
	workflow := readPackagesWorkflow(t)

	required := []string{
		"platforms: linux/amd64,linux/arm64",
		"push: false",
		"outputs: type=oci,dest=/tmp/impartus-image.tar,tar=true",
		"id: scan_amd64",
		"TRIVY_PLATFORM: linux/amd64",
		"id: scan_arm64",
		"TRIVY_PLATFORM: linux/arm64",
		"input: '/tmp/impartus-image.tar'",
		"tar --extract --file /tmp/impartus-image.tar --directory /tmp/impartus-image",
		"oras cp --from-oci-layout /tmp/impartus-image:prepublish-scan",
		"oras resolve --oci-layout /tmp/impartus-image:prepublish-scan",
	}
	for _, snippet := range required {
		if !strings.Contains(workflow, snippet) {
			t.Errorf("packages workflow is missing %q", snippet)
		}
	}

	if got := strings.Count(workflow, "uses: docker/build-push-action@"); got != 1 {
		t.Errorf("build-push-action invocation count = %d, want exactly one build", got)
	}
	if got := strings.Count(workflow, "uses: aquasecurity/trivy-action@"); got != 2 {
		t.Errorf("Trivy action invocation count = %d, want one scan per platform", got)
	}
	if got := strings.Count(workflow, "input: '/tmp/impartus-image.tar'"); got != 2 {
		t.Errorf("OCI archive scan input count = %d, want both scans to use the same artifact", got)
	}
	if strings.Contains(workflow, "input: '/tmp/impartus-image:prepublish-scan'") {
		t.Error("Trivy action input must be the OCI archive, not a tagged layout directory")
	}
	if got := strings.Count(workflow, "tar --extract --file /tmp/impartus-image.tar"); got != 1 {
		t.Errorf("OCI archive extraction count = %d, want exactly one extraction after both scans", got)
	}
	if strings.Contains(workflow, "push: true") {
		t.Error("packages workflow rebuilds or pushes through Buildx instead of publishing the scanned OCI artifact")
	}
}

func TestPackagesWorkflowScanOutcomesGateCredentialsAndPublish(t *testing.T) {
	workflow := readPackagesWorkflow(t)

	ordered := []string{
		"- name: Build multi-platform OCI image for security gate",
		"- name: Scan linux/amd64 image before publish",
		"- name: Scan linux/arm64 image before publish",
		"- name: Upload Trivy image reports",
		"- name: Extract scanned OCI image",
		"- name: Set up ORAS",
		"- name: Log in to GHCR",
		"- name: Publish scanned multi-platform OCI image",
	}
	previous := -1
	for _, step := range ordered {
		position := strings.Index(workflow, step)
		if position == -1 {
			t.Fatalf("packages workflow is missing ordered step %q", step)
		}
		if position <= previous {
			t.Fatalf("step %q is out of order", step)
		}
		previous = position
	}

	scanGate := "steps.scan_amd64.outcome == 'success' && steps.scan_arm64.outcome == 'success'"
	if got := strings.Count(workflow, scanGate); got != 4 {
		t.Errorf("explicit two-platform scan gate count = %d, want extraction, ORAS setup, login, and publish gates", got)
	}
	if got := strings.Count(workflow, "steps.extract.outcome == 'success'"); got != 3 {
		t.Errorf("successful extraction gate count = %d, want ORAS setup, login, and publish gates", got)
	}
	if strings.Contains(workflow, "success()") {
		t.Error("generic success() gate makes report-upload outcomes affect publication")
	}
	if got := strings.Count(workflow, "if: ${{ always()"); got < 6 {
		t.Errorf("always-run condition count = %d, want scans/reports and explicit outcome gates", got)
	}
}

func TestPackagesWorkflowActionsUseImmutablePins(t *testing.T) {
	workflow := readPackagesWorkflow(t)
	pinnedUse := regexp.MustCompile(`^\s*uses: [^\s@]+@[0-9a-f]{40}(?:\s+#.*)?$`)

	for _, line := range strings.Split(workflow, "\n") {
		if !strings.Contains(line, "uses:") {
			continue
		}
		if !pinnedUse.MatchString(line) {
			t.Errorf("action is not pinned to an immutable commit: %q", strings.TrimSpace(line))
		}
	}
}
