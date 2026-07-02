package downloader

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func reverseBytes(in []byte) []byte {
	out := make([]byte, len(in))
	for i := range in {
		out[i] = in[len(in)-1-i]
	}
	return out
}

func fakeKeyResponse(key []byte) []byte {
	return append([]byte{0, 0}, reverseBytes(key)...)
}

func writeFakeFFmpegScript(t *testing.T, logPath string, outputContent string) string {
	t.Helper()

	scriptPath := filepath.Join(t.TempDir(), "ffmpeg")
	script := fmt.Sprintf(`#!/usr/bin/env bash
set -eu
printf '%%s\n' "$@" > %q
last="${@: -1}"
printf '%%s' %q > "$last"
`, logPath, outputContent)
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(fake ffmpeg) failed: %v", err)
	}
	return scriptPath
}

// TestNewDownloaderWithConfigDefaults tests that ApplyDefaults is called
