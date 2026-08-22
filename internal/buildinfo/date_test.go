package buildinfo

import (
	"testing"
	"time"
)

func TestResolveDate(t *testing.T) {
	fixedNow := time.Date(2026, time.August, 22, 10, 30, 45, 987654321, time.FixedZone("test", 5*60*60+30*60))

	for _, test := range []struct {
		name     string
		explicit string
		epoch    string
		want     string
		wantErr  bool
	}{
		{name: "explicit wins", explicit: "2026-08-22T10:20:30Z", epoch: "invalid-but-ignored", want: "2026-08-22T10:20:30Z"},
		{name: "explicit offset normalizes UTC", explicit: "2026-08-22T15:50:30+05:30", want: "2026-08-22T10:20:30Z"},
		{name: "epoch zero", explicit: " \t", epoch: "0", want: "1970-01-01T00:00:00Z"},
		{name: "representative epoch", epoch: "1700000000", want: "2023-11-14T22:13:20Z"},
		{name: "current UTC fallback", want: "2026-08-22T05:00:45Z"},
		{name: "invalid explicit", explicit: "22 August 2026", wantErr: true},
		{name: "explicit normalization outside RFC3339 range", explicit: "9999-12-31T23:59:59-14:00", wantErr: true},
		{name: "explicit normalization before RFC3339 range", explicit: "0000-01-01T00:00:00+14:00", wantErr: true},
		{name: "negative epoch", epoch: "-1", wantErr: true},
		{name: "fractional epoch", epoch: "1.5", wantErr: true},
		{name: "epoch outside RFC3339 range", epoch: "253402300800", wantErr: true},
		{name: "overflow epoch", epoch: "999999999999999999999999", wantErr: true},
		{name: "malformed epoch", epoch: "tomorrow", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := ResolveDate(test.explicit, test.epoch, fixedNow)
			if test.wantErr {
				if err == nil {
					t.Fatalf("ResolveDate(%q, %q) = %q, want error", test.explicit, test.epoch, got)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("ResolveDate(%q, %q) = %q, %v; want %q", test.explicit, test.epoch, got, err, test.want)
			}
		})
	}
}

func TestNormalizeDate(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
		want  string
	}{
		{name: "preserves timestamp", value: "2026-08-22T10:20:30Z", want: "2026-08-22T10:20:30Z"},
		{name: "trims padded timestamp", value: " 2026-08-22T10:20:30Z\t", want: "2026-08-22T10:20:30Z"},
		{name: "normalizes whitespace", value: " \t\n", want: UnknownDate},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := NormalizeDate(test.value); got != test.want {
				t.Fatalf("NormalizeDate(%q) = %q, want %q", test.value, got, test.want)
			}
		})
	}
}
