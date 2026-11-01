package auth

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsCredentialDirectoryRestartAndRejectPublicACL(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "keys")
	if err := EnsureCredentialKeyDirectory(dir); err != nil {
		t.Fatal(err)
	}
	identity, err := NewIdentityService(&identityTestStore{})
	if err != nil {
		t.Fatal(err)
	}
	gate := func(context.Context, CredentialOperation) error { return nil }
	first, err := NewPersistentCredentialService(context.Background(), identity, dir, gate, nil)
	if err != nil {
		t.Fatal(err)
	}
	setup, _ := identity.PrepareSetup(context.Background())
	if err = identity.Bootstrap(context.Background(), setup, "admin", identityTestPassword); err != nil {
		t.Fatal(err)
	}
	session := identityLogin(t, identity)
	meta, err := first.Create(context.Background(), session.Secret, session.CSRF, credentialInput("synthetic-windows-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if err = RotateCredentialMasterKey(dir); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewPersistentCredentialService(context.Background(), identity, dir, gate, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = restarted.UseForService(context.Background(), credentialOp(session, meta, "windows-restart-use", "use"), func(_ context.Context, b []byte) error {
		if string(b) != "synthetic-windows-secret" {
			t.Fatal("lost secret")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(dir, "credential-keys.json")
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, _ := sd.DACL()
	if err = windows.SetNamedSecurityInfo(key, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = NewPersistentCredentialService(context.Background(), identity, dir, gate, nil); err == nil {
		t.Fatal("Everyone ACL allowed")
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	private, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		t.Fatal(err)
	}
	privateDACL, _, err := private.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err = windows.SetNamedSecurityInfo(key, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, privateDACL, nil); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(key); err != nil {
		t.Fatal(err)
	}
	if _, err = NewPersistentCredentialService(context.Background(), identity, dir, gate, nil); err == nil {
		t.Fatal("regenerated lost key")
	}
}
