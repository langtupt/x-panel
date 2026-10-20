package backup

import (
	"archive/tar"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMailRestoreContainment(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "message")
	if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	h := &tar.Header{Typeflag: tar.TypeReg}
	if err := writeMailAt(base, "../message", h, strings.NewReader("overwrite")); err == nil {
		t.Fatal("traversal accepted")
	}
	if err := os.Symlink(outside, filepath.Join(base, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := writeMailAt(base, "escape/message", h, strings.NewReader("overwrite")); err == nil {
		t.Fatal("symlink escape accepted")
	}
	got, _ := os.ReadFile(target)
	if string(got) != "original" {
		t.Fatal("outside file modified")
	}
	if err := writeMailAt(base, "box/new/message", h, strings.NewReader("valid")); err != nil {
		t.Fatal(err)
	}
}
