package main

import (
	"os"
	"os/exec"
	"regexp"
	"runtime"
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

func readReleaseWorkflow(t *testing.T) string {
	t.Helper()

	data, err := os.ReadFile(".github/workflows/release.yml")
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}
	return string(data)
}

func TestReleaseWorkflowDispatchesVersionedPackages(t *testing.T) {
	workflow := readReleaseWorkflow(t)

	required := []string{
		"dispatch-release-package:",
		"needs: [release-please]",
		"if: needs.release-please.outputs.release_created == 'true'",
		"actions: write",
		"contents: read",
		"GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}",
		"RELEASE_TAG: ${{ needs.release-please.outputs.tag_name }}",
		"gh api \"repos/${GITHUB_REPOSITORY}/git/ref/tags/${RELEASE_TAG}\"",
		"gh workflow run packages.yml",
		"--ref \"${RELEASE_TAG}\"",
		"--field \"release_tag=${RELEASE_TAG}\"",
	}
	for _, snippet := range required {
		if !strings.Contains(workflow, snippet) {
			t.Errorf("release workflow is missing %q", snippet)
		}
	}

	if strings.Contains(workflow, "PERSONAL_ACCESS_TOKEN") {
		t.Error("release workflow must dispatch with the scoped GITHUB_TOKEN, not a PAT")
	}
}

func TestPackagesWorkflowSelectsValidatedReleaseTags(t *testing.T) {
	workflow := readPackagesWorkflow(t)

	required := []string{
		"release_tag:",
		"description: 'Canonical release tag",
		"required: true",
		"group: packages-${{ (inputs.release_tag || github.event.release.tag_name) && 'release' || github.ref }}",
		"cancel-in-progress: false",
		"ref: ${{ steps.vars.outputs.source_ref || github.sha }}",
		"scripts/package-image-vars.sh \"$RELEASE_TAG\" \"$RELEASE_DATE\" \"$EVENT_NAME\"",
		"type=raw,value=main,enable=${{ steps.vars.outputs.mode == 'snapshot' }}",
		"type=sha,format=short,prefix=sha-,enable=${{ steps.vars.outputs.mode == 'snapshot' }}",
		"type=raw,value=${{ steps.vars.outputs.version }},enable=${{ steps.vars.outputs.mode == 'release' }}",
		"type=raw,value=${{ steps.vars.outputs.major_minor }},enable=${{ steps.vars.outputs.mode == 'release' && steps.vars.outputs.stable == 'true' }}",
		"type=raw,value=${{ steps.vars.outputs.major }},enable=${{ steps.vars.outputs.mode == 'release' && steps.vars.outputs.stable == 'true' }}",
		"type=raw,value=latest,enable=${{ steps.vars.outputs.mode == 'release' && steps.vars.outputs.stable == 'true' }}",
	}
	for _, snippet := range required {
		if !strings.Contains(workflow, snippet) {
			t.Errorf("packages workflow is missing %q", snippet)
		}
	}

	if strings.Contains(workflow, "value=${{ github.event.release.tag_name }}") {
		t.Error("packages workflow passes the prefixed release tag directly to image metadata")
	}
}

func TestPackagesWorkflowReplaysExactHistoricalTag(t *testing.T) {
	workflow := readPackagesWorkflow(t)

	required := []string{
		"- name: Checkout workflow tooling",
		"ref: ${{ github.workflow_sha }}",
		"- name: Verify exact release tag",
		"MODE: ${{ steps.vars.outputs.mode }}",
		"SOURCE_REF: ${{ steps.vars.outputs.source_ref }}",
		"git ls-remote --exit-code --refs origin \"$SOURCE_REF\"",
		"- name: Checkout selected source",
		"ref: ${{ steps.vars.outputs.source_ref || github.sha }}",
		"- name: Resolve source revision",
		"id: revision",
		"git rev-parse HEAD",
		"COMMIT=${{ steps.revision.outputs.commit }}",
		"org.opencontainers.image.revision=${{ steps.revision.outputs.commit }}",
	}
	for _, snippet := range required {
		if !strings.Contains(workflow, snippet) {
			t.Errorf("packages workflow is missing historical replay contract %q", snippet)
		}
	}

	ordered := []string{
		"- name: Checkout workflow tooling",
		"- name: Prepare image variables",
		"- name: Verify exact release tag",
		"- name: Checkout selected source",
		"- name: Resolve source revision",
		"- name: Generate Docker metadata",
	}
	previous := -1
	for _, step := range ordered {
		position := strings.Index(workflow, step)
		if position == -1 {
			t.Fatalf("packages workflow is missing ordered replay step %q", step)
		}
		if position <= previous {
			t.Fatalf("replay step %q is out of order", step)
		}
		previous = position
	}

	if got := strings.Count(workflow, "uses: actions/checkout@"); got != 2 {
		t.Errorf("checkout invocation count = %d, want workflow tooling plus selected source", got)
	}
	if strings.Contains(workflow, "COMMIT=${{ github.sha }}") {
		t.Error("packages workflow labels a replay with the dispatch ref revision")
	}
}

func packageImageVars(t *testing.T, releaseTag, releaseDate string) map[string]string {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("package image helper is executed by the Linux Packages workflow")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is unavailable")
	}
	command := exec.CommandContext(t.Context(), bash, "scripts/package-image-vars.sh", releaseTag, releaseDate)
	output, err := command.Output()
	if err != nil {
		t.Fatalf("package image helper failed: %v", err)
	}

	values := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("malformed helper output %q", line)
		}
		values[key] = value
	}
	return values
}

func TestPackageImageVars(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("package image helper is executed by the Linux Packages workflow")
	}

	info, err := os.Stat("scripts/package-image-vars.sh")
	if err != nil {
		t.Fatalf("stat package image helper: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Error("package image helper is not executable")
	}

	tests := []struct {
		name       string
		releaseTag string
		want       map[string]string
	}{
		{
			name: "main snapshot",
			want: map[string]string{
				"mode": "snapshot", "version": "main", "source_ref": "", "stable": "false",
			},
		},
		{
			name:       "stable release",
			releaseTag: "impartus-cli-v0.1.27",
			want: map[string]string{
				"mode": "release", "release_tag": "impartus-cli-v0.1.27",
				"version": "0.1.27", "source_ref": "refs/tags/impartus-cli-v0.1.27",
				"major_minor": "0.1", "major": "0", "stable": "true",
			},
		},
		{
			name:       "prerelease",
			releaseTag: "impartus-cli-v1.2.0-rc.1",
			want: map[string]string{
				"mode": "release", "release_tag": "impartus-cli-v1.2.0-rc.1",
				"version": "1.2.0-rc.1", "source_ref": "refs/tags/impartus-cli-v1.2.0-rc.1",
				"major_minor": "1.2", "major": "1", "stable": "false",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := packageImageVars(t, test.releaseTag, "2026-08-24T00:00:00Z")
			for key, want := range test.want {
				if got[key] != want {
					t.Errorf("%s = %q, want %q", key, got[key], want)
				}
			}
			if got["build_date"] != "2026-08-24T00:00:00Z" {
				t.Errorf("build_date = %q", got["build_date"])
			}
		})
	}
}

func TestPackageImageVarsRejectsMalformedTags(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("package image helper is executed by the Linux Packages workflow")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is unavailable")
	}

	for _, tag := range []string{
		"v0.1.27",
		"impartus-cli-v0.1",
		"impartus-cli-v01.1.27",
		"impartus-cli-v1.2.3-01",
		"impartus-cli-v1.2.3+build.1",
		"impartus-cli-v0.1.27\nmalicious=true",
	} {
		t.Run(strings.ReplaceAll(tag, "\n", "_newline_"), func(t *testing.T) {
			command := exec.CommandContext(t.Context(), bash, "scripts/package-image-vars.sh", tag, "")
			if err := command.Run(); err == nil {
				t.Fatalf("malformed tag %q was accepted", tag)
			}
		})
	}
}

func TestPackageImageVarsRejectsEmptyManualReleaseTag(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("package image helper is executed by the Linux Packages workflow")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is unavailable")
	}

	command := exec.CommandContext(t.Context(), bash, "scripts/package-image-vars.sh", "", "", "workflow_dispatch")
	if err := command.Run(); err == nil {
		t.Fatal("manual package replay accepted an empty release tag")
	}
}

func TestPackagesWorkflowPublishesScannedMultiPlatformArtifact(t *testing.T) {
	workflow := readPackagesWorkflow(t)

	required := []string{
		"platforms: linux/amd64,linux/arm64",
		"push: false",
		"outputs: type=oci,dest=/tmp/impartus-image,tar=false",
		"id: scan_amd64",
		"TRIVY_PLATFORM: linux/amd64",
		"id: scan_arm64",
		"TRIVY_PLATFORM: linux/arm64",
		"input: '/tmp/impartus-image'",
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
	if got := strings.Count(workflow, "input: '/tmp/impartus-image'"); got != 2 {
		t.Errorf("OCI layout scan input count = %d, want both scans to use the same untagged directory", got)
	}
	if strings.Contains(workflow, "input: '/tmp/impartus-image:prepublish-scan'") {
		t.Error("Trivy action input must be the untagged OCI layout directory")
	}
	if strings.Contains(workflow, "input: '/tmp/impartus-image.tar'") {
		t.Error("Trivy 0.70 cannot scan the Buildx OCI tar through the action input")
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
	if got := strings.Count(workflow, scanGate); got != 3 {
		t.Errorf("explicit two-platform scan gate count = %d, want ORAS setup, login, and publish gates", got)
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
