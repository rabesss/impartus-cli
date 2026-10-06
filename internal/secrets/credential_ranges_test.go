package secrets

import (
	"strings"
	"testing"
)

func TestReplaceRawCredentialRanges(t *testing.T) {
	t.Parallel()

	const value = "0123456789ABCDEF"
	for _, test := range []struct {
		name   string
		ranges []rawSpan
		want   string
	}{
		{name: "no ranges", want: value},
		{name: "single range", ranges: []rawSpan{{3, 5}}, want: "012REDACTED56789ABCDEF"},
		{name: "partial overlap", ranges: []rawSpan{{0, 5}, {3, 10}}, want: "REDACTEDABCDEF"},
		{name: "reversed overlap", ranges: []rawSpan{{3, 10}, {0, 5}}, want: "REDACTEDABCDEF"},
		{name: "contained range", ranges: []rawSpan{{0, 10}, {3, 5}}, want: "REDACTEDABCDEF"},
		{name: "same start", ranges: []rawSpan{{3, 5}, {3, 10}}, want: "012REDACTEDABCDEF"},
		{name: "same end", ranges: []rawSpan{{3, 10}, {0, 10}}, want: "REDACTEDABCDEF"},
		{name: "duplicate ranges", ranges: []rawSpan{{3, 10}, {3, 10}}, want: "012REDACTEDABCDEF"},
		{name: "overlap chain", ranges: []rawSpan{{7, 12}, {3, 8}, {0, 5}}, want: "REDACTEDCDEF"},
		{name: "disjoint ranges", ranges: []rawSpan{{8, 10}, {0, 3}}, want: "REDACTED34567REDACTEDABCDEF"},
		{name: "adjacent ranges", ranges: []rawSpan{{0, 5}, {5, 10}}, want: "REDACTEDREDACTEDABCDEF"},
		{name: "overlap then disjoint", ranges: []rawSpan{{2, 5}, {4, 8}, {10, 12}}, want: "01REDACTED89REDACTEDCDEF"},
		{name: "overlap then adjacent", ranges: []rawSpan{{0, 5}, {3, 10}, {10, 12}}, want: "REDACTEDREDACTEDCDEF"},
		{name: "overlap to end", ranges: []rawSpan{{2, 5}, {4, 16}}, want: "01REDACTED"},
		{name: "entire value", ranges: []rawSpan{{0, 16}}, want: "REDACTED"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := replaceRawCredentialRanges(value, test.ranges); got != test.want {
				t.Fatalf("replaceRawCredentialRanges() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestReplaceRawCredentialRangesPreservesOnlyUncoveredBytes(t *testing.T) {
	t.Parallel()

	// Exhaust every pair of nonempty, in-bounds spans, including both input
	// orders. A byte mask is an independent oracle for their union, regardless
	// of how many replacement markers the implementation emits.
	const value = "01234567"
	spans := make([]rawSpan, 0, len(value)*(len(value)+1)/2)
	for start := range len(value) {
		for end := start + 1; end <= len(value); end++ {
			spans = append(spans, rawSpan{start, end})
		}
	}
	for _, first := range spans {
		for _, second := range spans {
			var covered [len(value)]bool
			for _, span := range []rawSpan{first, second} {
				for index := span.start; index < span.end; index++ {
					covered[index] = true
				}
			}
			var want strings.Builder
			for index := range len(value) {
				if !covered[index] {
					want.WriteByte(value[index])
				}
			}
			got := replaceRawCredentialRanges(value, []rawSpan{first, second})
			if visible := strings.ReplaceAll(got, "REDACTED", ""); visible != want.String() {
				t.Fatalf("ranges %v and %v: visible bytes = %q, want %q (output %q)", first, second, visible, want.String(), got)
			}
		}
	}
}

func TestScrubRedactsOverlappingEncodedEscapedQuotedValue(t *testing.T) {
	t.Parallel()

	const input = "password%3D%5C%22secret-one%20secret-two%5C%22 tail"
	const want = "password%3DREDACTED%5C%22 tail"
	got := Scrub(input)
	for _, secret := range []string{"secret-one", "secret-two"} {
		if strings.Contains(got, secret) {
			t.Errorf("Scrub(%q) leaked %q: %q", input, secret, got)
		}
	}
	if got != want {
		t.Errorf("Scrub(%q) = %q, want %q", input, got, want)
	}
}
