package recovery

import (
	"archive/tar"
	"bytes"
	"testing"
)

// Exercise the real tar parser: dropping the deprecated Header.Xattrs projection
// must not allow hidden extended metadata to enter the restored private tree.
func TestRecoveryRejectsParsedExtendedMetadata(t *testing.T) {
	var archive bytes.Buffer
	w := tar.NewWriter(&archive)
	h := &tar.Header{Name: "controller.db", Mode: 0600, Typeflag: tar.TypeReg, Size: 1, Format: tar.FormatPAX, PAXRecords: map[string]string{"SCHILY.xattr.user.hidden": "value"}}
	if e := w.WriteHeader(h); e != nil {
		t.Fatal(e)
	}
	if _, e := w.Write([]byte{0}); e != nil {
		t.Fatal(e)
	}
	if e := w.Close(); e != nil {
		t.Fatal(e)
	}
	parsed, e := tar.NewReader(bytes.NewReader(archive.Bytes())).Next()
	if e != nil {
		t.Fatal(e)
	}
	if safeRecoveryHeader(parsed) {
		t.Fatal("parsed extended archive metadata admitted")
	}
}
