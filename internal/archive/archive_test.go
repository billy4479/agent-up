package archive

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteAndExtractDirectory(t *testing.T) {
	source := t.TempDir()
	if err := os.Mkdir(filepath.Join(source, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "nested", "hello.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	if err := WriteDirectory(&data, source); err != nil {
		t.Fatal(err)
	}
	destination := t.TempDir()
	if err := ExtractDirectory(&data, destination); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(destination, "nested", "hello.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("got %q", got)
	}
}

func TestWriteDirectoryRejectsSymlink(t *testing.T) {
	directory := t.TempDir()
	if err := os.Symlink("target", filepath.Join(directory, "link")); err != nil {
		t.Fatal(err)
	}
	if err := WriteDirectory(&bytes.Buffer{}, directory); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlink error, got %v", err)
	}
}

func TestExtractDirectoryRejectsUnsafeEntries(t *testing.T) {
	tests := []struct {
		name    string
		headers []*tar.Header
	}{
		{name: "traversal", headers: []*tar.Header{{Name: "../outside", Mode: 0o644, Typeflag: tar.TypeReg}}},
		{name: "absolute", headers: []*tar.Header{{Name: "/outside", Mode: 0o644, Typeflag: tar.TypeReg}}},
		{name: "backslash", headers: []*tar.Header{{Name: `..\outside`, Mode: 0o644, Typeflag: tar.TypeReg}}},
		{name: "symlink", headers: []*tar.Header{{Name: "link", Linkname: "target", Typeflag: tar.TypeSymlink}}},
		{name: "hardlink", headers: []*tar.Header{{Name: "link", Linkname: "target", Typeflag: tar.TypeLink}}},
		{name: "special", headers: []*tar.Header{{Name: "device", Typeflag: tar.TypeChar}}},
		{name: "duplicate", headers: []*tar.Header{
			{Name: "same", Mode: 0o644, Typeflag: tar.TypeReg},
			{Name: "same", Mode: 0o644, Typeflag: tar.TypeReg},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var data bytes.Buffer
			writer := tar.NewWriter(&data)
			for _, header := range test.headers {
				if err := writer.WriteHeader(header); err != nil {
					t.Fatal(err)
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := ExtractDirectory(&data, t.TempDir()); err == nil {
				t.Fatal("expected extraction to fail")
			}
		})
	}
}
