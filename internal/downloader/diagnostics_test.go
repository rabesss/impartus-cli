package downloader

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func TestDownloaderDiagnosticLoggerIsPerInstance(t *testing.T) {
	if got := New(nil, nil).diagnostics; got != log.Default() {
		t.Fatal("New should preserve the standard diagnostic logger")
	}

	var firstOutput bytes.Buffer
	var secondOutput bytes.Buffer
	first := NewWithDiagnosticWriter(nil, nil, &firstOutput)
	second := NewWithDiagnosticWriter(nil, nil, &secondOutput)

	first.logDiagnostic("first diagnostic")
	second.logDiagnostic("second diagnostic")

	if !strings.Contains(firstOutput.String(), "first diagnostic") || strings.Contains(firstOutput.String(), "second diagnostic") {
		t.Fatalf("first downloader diagnostics were cross-routed: %q", firstOutput.String())
	}
	if !strings.Contains(secondOutput.String(), "second diagnostic") || strings.Contains(secondOutput.String(), "first diagnostic") {
		t.Fatalf("second downloader diagnostics were cross-routed: %q", secondOutput.String())
	}
}
