package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/buildinfo"
)

func TestMakeBuildGoMetadata(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Make build smoke test in short mode")
	}
	makePath, err := exec.LookPath("make")
	if err != nil {
		t.Skip("make is not available")
	}
	repositoryRoot := filepath.Dir(mustWorkingDirectory(t))

	for _, test := range []struct {
		name                string
		buildDate           string
		epoch               string
		buildVersion        string
		buildVersionDefault string
		target              string
		makeArguments       bool
		wantDate            string
		wantError           string
		wantVersion         string
		wantReleaseVersion  bool
	}{
		{name: "default UTC timestamp"},
		{name: "ambient release default is ignored", buildVersionDefault: "ambient-version", wantVersion: buildinfo.Version},
		{name: "explicit timestamp and version", buildDate: "2026-08-22T10:20:30Z", buildVersion: "v-test", wantDate: "2026-08-22T10:20:30Z"},
		{name: "command-line timestamp and version", buildDate: "2026-08-22T10:20:31Z", buildVersion: "v-command-line", makeArguments: true, wantDate: "2026-08-22T10:20:31Z"},
		{name: "source date epoch", epoch: "0", wantDate: "1970-01-01T00:00:00Z"},
		{name: "release version default", target: "build-go-release", wantReleaseVersion: true},
		{name: "invalid timestamp fails", buildDate: "not-a-timestamp", wantError: "BUILD_DATE must be RFC3339"},
		{name: "invalid version fails", buildVersion: "bad version", wantError: "resolved build version contains unsupported characters"},
	} {
		t.Run(test.name, func(t *testing.T) {
			binary := filepath.Join(t.TempDir(), "impartus")
			wantReleaseVersion := ""
			if test.wantReleaseVersion {
				var ok bool
				wantReleaseVersion, ok = gitDescribe(t, repositoryRoot)
				if !ok {
					t.Skip("Git metadata is unavailable for the release-version smoke test")
				}
			}
			target := test.target
			if target == "" {
				target = "build-go"
			}
			arguments := []string{"-C", repositoryRoot, target, "GO_BINARY=" + binary}
			overrides := []string{
				"BUILD_DATE=",
				"SOURCE_DATE_EPOCH=",
				"BUILD_VERSION=",
			}
			metadata := []string{
				"BUILD_DATE=" + test.buildDate,
				"SOURCE_DATE_EPOCH=" + test.epoch,
				"BUILD_VERSION=" + test.buildVersion,
				"BUILD_VERSION_DEFAULT=" + test.buildVersionDefault,
			}
			if test.makeArguments {
				arguments = append(arguments, metadata...)
			} else {
				overrides = metadata
			}
			if goFlags := smokeGOFLAGS(repositoryRoot, os.Getenv("GOFLAGS")); goFlags != "" {
				overrides = append(overrides, "GOFLAGS="+goFlags)
			}
			command := exec.CommandContext(t.Context(), makePath, arguments...)
			command.Env = metadataTestEnvironment(overrides...)
			output, buildErr := command.CombinedOutput()
			if test.wantError != "" {
				if buildErr == nil {
					t.Fatalf("make build-go succeeded for invalid metadata; output=%s", output)
				}
				if !strings.Contains(string(output), test.wantError) {
					t.Fatalf("make build-go error output = %q, want %q", output, test.wantError)
				}
				return
			}
			if buildErr != nil {
				t.Fatalf("make build-go failed: %v; output=%s", buildErr, output)
			}

			humanVersion, humanDate := runHumanVersion(t, binary)
			jsonVersion, jsonDate := runJSONVersion(t, binary)
			if humanDate != jsonDate || humanVersion != jsonVersion {
				t.Fatalf("human/JSON metadata differ: (%q, %q) vs (%q, %q)", humanVersion, humanDate, jsonVersion, jsonDate)
			}
			if humanVersion == "" {
				t.Fatal("make build-go produced an empty version")
			}
			if test.buildVersion != "" && humanVersion != test.buildVersion {
				t.Fatalf("version = %q, want override %q", humanVersion, test.buildVersion)
			}
			if test.wantVersion != "" && humanVersion != test.wantVersion {
				t.Fatalf("version = %q, want compiled default %q", humanVersion, test.wantVersion)
			}
			if wantReleaseVersion != "" && humanVersion != wantReleaseVersion {
				t.Fatalf("release version = %q, want git describe %q", humanVersion, wantReleaseVersion)
			}
			if test.wantDate != "" {
				if humanDate != test.wantDate {
					t.Fatalf("date = %q, want %q", humanDate, test.wantDate)
				}
			} else {
				parsed, err := time.Parse(time.RFC3339, humanDate)
				if err != nil {
					t.Fatalf("default build date %q is not RFC3339: %v", humanDate, err)
				}
				if parsed.Location() != time.UTC || !strings.HasSuffix(humanDate, "Z") {
					t.Fatalf("default build date %q is not UTC", humanDate)
				}
			}
		})
	}
}

func TestMakeCombinedGoalsKeepReleaseMetadata(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Make build smoke test in short mode")
	}
	makePath, err := exec.LookPath("make")
	if err != nil {
		t.Skip("make is not available")
	}
	repositoryRoot := filepath.Dir(mustWorkingDirectory(t))
	wantVersion, ok := gitDescribe(t, repositoryRoot)
	if !ok {
		t.Skip("Git metadata is unavailable for the combined release-goal smoke test")
	}

	for _, goals := range [][]string{
		{"build-go", "build-go-release"},
		{"build-go-release", "build-go"},
	} {
		t.Run(strings.Join(goals, " then "), func(t *testing.T) {
			binary := filepath.Join(t.TempDir(), "impartus")
			arguments := append([]string{"-C", repositoryRoot}, goals...)
			arguments = append(arguments, "GO_BINARY="+binary)
			command := exec.CommandContext(t.Context(), makePath, arguments...)
			overrides := []string{"BUILD_DATE=2026-08-22T10:20:30Z"}
			if goFlags := smokeGOFLAGS(repositoryRoot, os.Getenv("GOFLAGS")); goFlags != "" {
				overrides = append(overrides, "GOFLAGS="+goFlags)
			}
			command.Env = metadataTestEnvironment(overrides...)
			output, buildErr := command.CombinedOutput()
			if buildErr != nil {
				t.Fatalf("make %s failed: %v; output=%s", strings.Join(goals, " "), buildErr, output)
			}

			version, _ := runHumanVersion(t, binary)
			if version != wantVersion {
				t.Fatalf("make %s stamped %q, want git describe %q; output=%s", strings.Join(goals, " "), version, wantVersion, output)
			}
		})
	}
}

func TestSmokeGOFLAGSPreservesCallerAndMatchesCheckout(t *testing.T) {
	repositoryRoot := filepath.Dir(mustWorkingDirectory(t))
	got := smokeGOFLAGS(repositoryRoot, "-mod=readonly")
	if !strings.Contains(got, "-mod=readonly") {
		t.Fatalf("smokeGOFLAGS dropped caller flags: %q", got)
	}
	gitEntry, err := os.Stat(filepath.Join(repositoryRoot, ".git"))
	if os.IsNotExist(err) {
		if strings.Contains(got, "-buildvcs=false") {
			t.Fatalf("smokeGOFLAGS added linked-worktree flag without Git metadata: %q", got)
		}
		return
	}
	if err != nil {
		t.Fatalf("stat checkout metadata: %v", err)
	}
	if linked := !gitEntry.IsDir(); linked != strings.Contains(got, "-buildvcs=false") {
		t.Fatalf("smokeGOFLAGS(%q) linked=%t, got %q", repositoryRoot, linked, got)
	}
}

func TestMetadataTestEnvironmentScrubsAmbientBuildState(t *testing.T) {
	for _, name := range []string{
		"BUILD_VERSION_DEFAULT",
		"MAKEFLAGS",
		"GOOS",
		"GOARCH",
		"CGO_ENABLED",
	} {
		t.Setenv(name, "ambient-value")
	}
	environment := metadataTestEnvironment()
	for _, entry := range environment {
		name, _, _ := strings.Cut(entry, "=")
		switch name {
		case "BUILD_VERSION_DEFAULT", "MAKEFLAGS", "GOOS", "GOARCH", "CGO_ENABLED":
			t.Fatalf("metadataTestEnvironment retained ambient %s", name)
		}
	}
}

func smokeGOFLAGS(repositoryRoot, inherited string) string {
	gitEntry, err := os.Stat(filepath.Join(repositoryRoot, ".git"))
	if err != nil || gitEntry.IsDir() {
		return strings.TrimSpace(inherited)
	}
	return strings.TrimSpace(inherited + " -buildvcs=false")
}

func gitDescribe(t *testing.T, repositoryRoot string) (string, bool) {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return "", false
	}
	output, err := exec.CommandContext(t.Context(), gitPath, "-C", repositoryRoot, "describe", "--tags", "--always", "--dirty").Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(output)), true
}

func metadataTestEnvironment(overrides ...string) []string {
	excluded := map[string]struct{}{
		"BUILD_DATE": {}, "SOURCE_DATE_EPOCH": {}, "BUILD_VERSION": {}, "BUILD_VERSION_DEFAULT": {},
		"GOFLAGS": {}, "MAKEFLAGS": {}, "MFLAGS": {}, "GNUMAKEFLAGS": {}, "MAKEOVERRIDES": {}, "MAKELEVEL": {},
		"GOOS": {}, "GOARCH": {}, "CGO_ENABLED": {},
	}
	environment := make([]string, 0, len(os.Environ())+len(overrides))
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if _, skip := excluded[name]; !skip {
			environment = append(environment, entry)
		}
	}
	return append(environment, overrides...)
}

func mustWorkingDirectory(t *testing.T) string {
	t.Helper()
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	return workingDirectory
}

func runHumanVersion(t *testing.T, binary string) (string, string) {
	t.Helper()
	output, err := exec.CommandContext(t.Context(), binary, "version").CombinedOutput()
	if err != nil {
		t.Fatalf("run human version: %v; output=%s", err, output)
	}
	return metadataLine(t, string(output), "Version:"), metadataLine(t, string(output), "Build Date:")
}

func runJSONVersion(t *testing.T, binary string) (string, string) {
	t.Helper()
	output, err := exec.CommandContext(t.Context(), binary, "version", "--json").CombinedOutput()
	if err != nil {
		t.Fatalf("run JSON version: %v; output=%s", err, output)
	}
	var envelope struct {
		Data struct {
			Version   string `json:"version"`
			BuildDate string `json:"buildDate"`
		} `json:"data"`
	}
	if err := json.Unmarshal(output, &envelope); err != nil {
		t.Fatalf("decode JSON version: %v; output=%s", err, output)
	}
	return envelope.Data.Version, envelope.Data.BuildDate
}

func metadataLine(t *testing.T, output, prefix string) string {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	t.Fatalf("output %q has no %q line", output, prefix)
	return ""
}
