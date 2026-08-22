package cli

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestExecuteVersionNormalizesBuildDateAcrossHumanAndJSON(t *testing.T) {
	restoreCLIState(t)

	for _, test := range []struct {
		name string
		date string
		want string
	}{
		{name: "empty", date: "", want: "unknown"},
		{name: "whitespace", date: " \t\n", want: "unknown"},
		{name: "stamped", date: "2026-08-22T10:20:30Z", want: "2026-08-22T10:20:30Z"},
	} {
		t.Run(test.name, func(t *testing.T) {
			os.Args = []string{"impartus", "version"}
			human, stderr, err := captureOutputStreams(t, func() error { return Execute("v-test", test.date) })
			if err != nil || stderr != "" {
				t.Fatalf("human version error/stderr = %v/%q", err, stderr)
			}
			humanDate := strings.TrimSpace(strings.TrimPrefix(versionLine(t, human, "Build Date:"), "Build Date:"))
			if humanDate != test.want {
				t.Fatalf("human build date = %q, want %q; output=%q", humanDate, test.want, human)
			}

			os.Args = []string{"impartus", "version", "--json"}
			jsonOutput, stderr, err := captureOutputStreams(t, func() error { return Execute("v-test", test.date) })
			if err != nil || stderr != "" {
				t.Fatalf("JSON version error/stderr = %v/%q", err, stderr)
			}
			var envelope struct {
				Data struct {
					Version   string `json:"version"`
					BuildDate string `json:"buildDate"`
				} `json:"data"`
			}
			if err := json.Unmarshal([]byte(jsonOutput), &envelope); err != nil {
				t.Fatalf("decode JSON version: %v; output=%q", err, jsonOutput)
			}
			if envelope.Data.Version != "v-test" || envelope.Data.BuildDate != test.want {
				t.Fatalf("JSON version data = %+v, want version v-test and date %q", envelope.Data, test.want)
			}
			if envelope.Data.BuildDate != humanDate {
				t.Fatalf("human/JSON dates differ: %q vs %q", humanDate, envelope.Data.BuildDate)
			}
		})
	}
}

func versionLine(t *testing.T, output, prefix string) string {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	t.Fatalf("output %q has no %q line", output, prefix)
	return ""
}
