package library

import "testing"

func TestPrivateWindowsDACLPolicyRequiresInheritanceProtection(t *testing.T) {
	t.Parallel()

	if privateWindowsDACLIsProtected(0) {
		t.Fatal("unprotected Windows DACL was accepted")
	}
	if !privateWindowsDACLIsProtected(privateWindowsDACLProtected) {
		t.Fatal("protected Windows DACL was rejected")
	}
}
