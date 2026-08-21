//go:build windows

package client

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestReadTokenCacheFileRejectsFinalReparsePoint(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target-token")
	if err := os.WriteFile(target, []byte("must-not-be-read"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "cache-token")
	if err := os.Symlink(target, link); err != nil {
		if os.Getenv("GITHUB_ACTIONS") == "true" {
			t.Fatalf("Windows CI must support the decisive reparse-point test: %v", err)
		}
		t.Skipf("Windows symlink creation unavailable: %v", err)
	}
	if content, err := readTokenCacheFile(link); err == nil {
		t.Fatalf("readTokenCacheFile() followed reparse point and returned %q", content)
	}
}

func TestWriteTokenCachePublishesProtectedPrivateDACL(t *testing.T) {
	parent := t.TempDir()
	grantWorldReadableParent(t, parent)
	path := filepath.Join(parent, "cache-token")
	if err := writeTokenCache(path, []byte("private-token")); err != nil {
		t.Fatalf("writeTokenCache() error = %v", err)
	}

	descriptor, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		t.Fatalf("inspect token cache security descriptor: %v", err)
	}
	control, _, err := descriptor.Control()
	if err != nil {
		t.Fatal(err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatal("token cache DACL inherits from its permissive parent")
	}
	dacl, _, err := descriptor.DACL()
	if err != nil || dacl == nil {
		t.Fatalf("token cache has no explicit DACL: %v", err)
	}
	world, err := windows.CreateWellKnownSid(windows.WinWorldSid)
	if err != nil {
		t.Fatal(err)
	}
	for index := uint16(0); index < dacl.AceCount; index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, uint32(index), &ace); err != nil {
			t.Fatal(err)
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			continue
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart)) // #nosec G103 -- GetAce returns the documented inline SID
		if sid.Equals(world) {
			t.Fatalf("token cache retained a World allow ACE with mask %#x", ace.Mask)
		}
	}
}

func TestReadTokenCacheFileRejectsInheritedWorldReadableDACL(t *testing.T) {
	parent := t.TempDir()
	grantWorldReadableParent(t, parent)
	path := filepath.Join(parent, "legacy-token")
	if err := os.WriteFile(path, []byte("legacy-token"), 0o600); err != nil {
		t.Fatal(err)
	}

	if content, err := readTokenCacheFile(path); err == nil {
		t.Fatalf("readTokenCacheFile() accepted unsafe inherited ACL and returned %q", content)
	}
	if err := writeTokenCache(path, []byte("replacement-token")); err != nil {
		t.Fatalf("rewrite unsafe legacy cache: %v", err)
	}
	content, err := readTokenCacheFile(path)
	if err != nil || string(content) != "replacement-token" {
		t.Fatalf("read rewritten private cache = %q, error = %v", content, err)
	}
}

func grantWorldReadableParent(t *testing.T, path string) {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	world, err := windows.CreateWellKnownSid(windows.WinWorldSid)
	if err != nil {
		t.Fatal(err)
	}
	var pinner runtime.Pinner
	defer pinner.Unpin()
	pinner.Pin(user.User.Sid)
	pinner.Pin(world)
	inheritance := uint32(windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE)
	entries := []windows.EXPLICIT_ACCESS{
		{
			AccessPermissions: windows.GENERIC_ALL,
			AccessMode:        windows.GRANT_ACCESS,
			Inheritance:       inheritance,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_USER,
				TrusteeValue: windows.TrusteeValueFromSID(user.User.Sid),
			},
		},
		{
			AccessPermissions: windows.GENERIC_READ,
			AccessMode:        windows.GRANT_ACCESS,
			Inheritance:       inheritance,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_WELL_KNOWN_GROUP,
				TrusteeValue: windows.TrusteeValueFromSID(world),
			},
		},
	}
	acl, err := windows.ACLFromEntries(entries, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		acl,
		nil,
	); err != nil {
		t.Fatal(err)
	}
}
