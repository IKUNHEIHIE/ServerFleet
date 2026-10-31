package localpki

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureCreatesAndReloadsStableLocalCA(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pki")
	first, err := Ensure(dir)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if first.Issuer == nil || first.Roots == nil || len(first.RootPEM) == 0 || len(first.RootSPKI) != 64 {
		t.Fatalf("incomplete material: %+v", first)
	}
	if first.Directory != dir {
		t.Fatalf("directory = %q", first.Directory)
	}
	for _, name := range []string{CertificateName, KeyName} {
		if info, err := os.Stat(filepath.Join(dir, name)); err != nil || !info.Mode().IsRegular() {
			t.Fatalf("%s not written: %v", name, err)
		}
	}
	second, err := Ensure(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if second.RootSPKI != first.RootSPKI || string(second.RootPEM) != string(first.RootPEM) || second.Issuer == nil {
		t.Fatal("local CA was not stable across reloads")
	}
}

func TestEnsureRejectsPartialAndCorruptState(t *testing.T) {
	partial := filepath.Join(t.TempDir(), "partial")
	if err := os.MkdirAll(partial, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(partial, KeyName), []byte("junk"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Ensure(partial); !errors.Is(err, ErrLocalPKI) {
		t.Fatalf("partial state = %v", err)
	}

	corrupt := filepath.Join(t.TempDir(), "corrupt")
	if _, err := Ensure(corrupt); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(corrupt, KeyName), []byte("-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Ensure(corrupt); !errors.Is(err, ErrLocalPKI) {
		t.Fatalf("corrupt key = %v", err)
	}

	if _, err := Ensure(""); !errors.Is(err, ErrLocalPKI) {
		t.Fatalf("empty directory = %v", err)
	}
}
