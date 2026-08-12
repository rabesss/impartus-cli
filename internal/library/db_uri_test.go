package library

import (
	"strings"
	"testing"
	"time"
)

func TestSQLiteDSNForWindowsDriveUsesAbsoluteFileURI(t *testing.T) {
	t.Parallel()

	dsn := sqliteDSNForOS(`C:\Users\alice\AppData\Local\impartus\library.db`, time.Second, false, "windows")
	if !strings.HasPrefix(dsn, "file:///C:/Users/alice/AppData/Local/impartus/library.db?") {
		t.Fatalf("sqliteDSNForOS() = %q, want an absolute Windows file URI", dsn)
	}
}
