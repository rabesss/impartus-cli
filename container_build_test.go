package main

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// These tests pin the container build contract from issue #304: one Go
// version everywhere, a cross-compiled build stage, a Dependabot-trackable
// runtime base and an ffmpeg smoke test of the built image.

func readRepoFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	// Line-anchored checks must also hold for core.autocrlf=true checkouts.
	return strings.ReplaceAll(string(data), "\r\n", "\n")
}

var (
	goDirectivePattern        = regexp.MustCompile(`(?m)^go (\d+\.\d+(?:\.\d+)?)\s*$`)
	toolchainDirectivePattern = regexp.MustCompile(`(?m)^toolchain go(\d+\.\d+\.\d+)\s*$`)
	buildStagePattern         = regexp.MustCompile(`(?m)^FROM (--platform=\$BUILDPLATFORM )?golang:(\d+\.\d+\.\d+)-bookworm@sha256:[0-9a-f]{64} AS build$`)
	runtimeStagePattern       = regexp.MustCompile(`(?m)^FROM (debian:\S+)$`)
	datedDebianPattern        = regexp.MustCompile(`^debian:bookworm-\d{8}-slim@sha256:[0-9a-f]{64}$`)
)

// goModVersion returns the single Go version CI, release and the Dockerfile
// build with: the go.mod toolchain directive if present, else the go directive
// (setup-go's go-version-file resolves it the same way).
func goModVersion(t *testing.T) string {
	t.Helper()

	goMod := readRepoFile(t, "go.mod")
	toolchains := toolchainDirectivePattern.FindAllStringSubmatch(goMod, -1)
	switch len(toolchains) {
	case 0:
	case 1:
		return toolchains[0][1]
	default:
		t.Fatalf("go.mod has %d toolchain directives, want at most one", len(toolchains))
	}

	match := goDirectivePattern.FindStringSubmatch(goMod)
	if match == nil {
		t.Fatal("go.mod is missing its go directive")
	}
	if strings.Count(match[1], ".") != 2 {
		t.Fatalf("go directive %q must name an exact patch release (goX.Y.Z) so every build uses the same Go", match[1])
	}
	return match[1]
}

func parseGoVersion(t *testing.T, version string) [3]int {
	t.Helper()

	var parsed [3]int
	for i, part := range strings.Split(version, ".") {
		if i >= len(parsed) {
			t.Fatalf("malformed Go version %q", version)
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				t.Fatalf("malformed Go version %q", version)
			}
			parsed[i] = parsed[i]*10 + int(r-'0')
		}
	}
	return parsed
}

func goVersionLessOrEqual(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return true
}

func TestGoModIsTheSingleGoVersion(t *testing.T) {
	version := goModVersion(t)

	match := goDirectivePattern.FindStringSubmatch(readRepoFile(t, "go.mod"))
	if match == nil {
		t.Fatal("go.mod is missing its go directive")
	}
	if !goVersionLessOrEqual(parseGoVersion(t, match[1]), parseGoVersion(t, version)) {
		t.Errorf("go directive %s is newer than toolchain go%s", match[1], version)
	}

	dockerfile := readRepoFile(t, "Dockerfile")
	build := buildStagePattern.FindStringSubmatch(dockerfile)
	if build == nil {
		t.Fatal("Dockerfile build stage must be `FROM --platform=$BUILDPLATFORM golang:<go.mod version>-bookworm@sha256:<digest> AS build`")
	}
	if build[2] != version {
		t.Errorf("Dockerfile builds with golang:%s, but go.mod selects go%s; bump both in one change", build[2], version)
	}
}

func TestWorkflowsTakeGoVersionFromGoMod(t *testing.T) {
	paths, err := filepath.Glob(".github/workflows/*.yml")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no workflows found")
	}

	versionFile := regexp.MustCompile(`^\s*go-version-file: '?go\.mod'?\s*$`)
	setupGoUses := 0
	for _, path := range paths {
		lines := strings.Split(readRepoFile(t, path), "\n")
		for i, line := range lines {
			if strings.Contains(line, "go-version:") {
				t.Errorf("%s:%d pins a Go version; use go-version-file: go.mod", path, i+1)
			}
			if strings.Contains(line, "GOTOOLCHAIN") {
				t.Errorf("%s:%d overrides GOTOOLCHAIN; go.mod is the only Go version", path, i+1)
			}
			if !strings.Contains(line, "uses: actions/setup-go@") {
				continue
			}
			setupGoUses++
			found := false
			for _, next := range lines[i+1 : min(i+6, len(lines))] {
				if versionFile.MatchString(next) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s:%d sets up Go without go-version-file: go.mod", path, i+1)
			}
		}
	}
	if setupGoUses == 0 {
		t.Fatal("no actions/setup-go uses found")
	}
}

func TestCIRunsTheGoModVersion(t *testing.T) {
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		t.Skip("only meaningful under actions/setup-go")
	}
	if want := "go" + goModVersion(t); runtime.Version() != want {
		t.Errorf("CI runs %s, want %s from go.mod", runtime.Version(), want)
	}
}

func TestDockerfileCrossCompilesBuildStage(t *testing.T) {
	dockerfile := readRepoFile(t, "Dockerfile")

	build := buildStagePattern.FindStringSubmatch(dockerfile)
	if build == nil || build[1] == "" {
		t.Fatal("Dockerfile build stage must run on $BUILDPLATFORM so arm64 cross-compiles instead of building under QEMU")
	}

	download := strings.Index(dockerfile, "RUN go mod download")
	targetArgs := strings.Index(dockerfile, "ARG TARGETOS TARGETARCH")
	goBuild := strings.Index(dockerfile, "go build")
	if download == -1 || targetArgs == -1 || goBuild == -1 {
		t.Fatalf("Dockerfile is missing go mod download (%d), ARG TARGETOS TARGETARCH (%d) or go build (%d)", download, targetArgs, goBuild)
	}
	if download > targetArgs || targetArgs > goBuild {
		t.Error("declare ARG TARGETOS TARGETARCH after go mod download so the module layer is shared across platforms")
	}
	if !strings.Contains(dockerfile, "CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH}") {
		t.Error("go build must cross-compile with CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH}")
	}
	if strings.Contains(dockerfile, "TARGETARCH=amd64") || strings.Contains(dockerfile, "TARGETOS=linux") {
		t.Error("TARGETOS/TARGETARCH defaults would mask a missing BuildKit platform")
	}
}

func TestDockerfileRuntimeBaseIsDependabotTrackable(t *testing.T) {
	match := runtimeStagePattern.FindStringSubmatch(readRepoFile(t, "Dockerfile"))
	if match == nil {
		t.Fatal("Dockerfile is missing the debian runtime stage")
	}
	// A rolling tag such as bookworm-slim gives Dependabot a version that never
	// changes, so one closed digest PR (#75) suppressed every later refresh (#304).
	if !datedDebianPattern.MatchString(match[1]) {
		t.Errorf("runtime base %q must be a dated debian:bookworm-YYYYMMDD-slim@sha256 pin so Dependabot sees new versions", match[1])
	}
}

func TestContainerSmokeScriptCoversCLIAndFFmpegPaths(t *testing.T) {
	const path = "scripts/container-smoke.sh"
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	// Windows reports no Unix execute bits; git still records the mode there.
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		t.Errorf("%s is not executable", path)
	}

	script := readRepoFile(t, path)
	required := []string{
		"set -euo pipefail",
		`--platform "$platform"`,
		"--help",
		// Old release images run as a non-root user without a writable /work.
		"--tmpfs /smoke",
		"--workdir /smoke",
		// Encode an HLS source, then exercise each internal/downloader/ffmpeg.go path.
		"-f hls",
		"-c:v libx264",
		"-c:a aac",
		"-map 0 -map 1 -c copy",
		"-f matroska",
		"-acodec libmp3lame",
		"-f ipod",
		"-acodec libopus",
		"-f opus",
		"-xerror",
		"-f null",
		"ffprobe",
	}
	for _, snippet := range required {
		if !strings.Contains(script, snippet) {
			t.Errorf("%s is missing %q", path, snippet)
		}
	}
	if strings.Contains(script, "/work") {
		t.Errorf("%s must not depend on /work, which older release images do not make writable", path)
	}
}

func TestCIContainerJobRunsSmokeScript(t *testing.T) {
	ci := readRepoFile(t, ".github/workflows/ci.yml")

	start := strings.Index(ci, "  container-permissions:")
	if start == -1 {
		t.Fatal("ci.yml is missing the container-permissions job")
	}
	job := ci[start:]
	if end := strings.Index(job[1:], "\n  build:"); end != -1 {
		job = job[:end+1]
	}

	required := []string{
		"docker build -t impartus:permission-test .",
		"test -w /work -a -w /work/temp -a -w /work/downloads",
		"docker run --rm impartus:permission-test --help",
		"scripts/container-smoke.sh impartus:permission-test linux/amd64",
	}
	for _, snippet := range required {
		if !strings.Contains(job, snippet) {
			t.Errorf("container-permissions job is missing %q", snippet)
		}
	}
}

func TestReadRepoFileNormalisesCRLF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Dockerfile")
	if err := os.WriteFile(path, []byte("FROM a AS build\r\nFROM b\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// core.autocrlf=true checkouts must not break the line-anchored contract checks.
	if got, want := readRepoFile(t, path), "FROM a AS build\nFROM b\n"; got != want {
		t.Errorf("readRepoFile = %q, want %q", got, want)
	}
}
