package extractor

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func tarWithHeaders(t *testing.T, headers ...tar.Header) string {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, header := range headers {
		header.Mode = 0755
		if err := tw.WriteHeader(&header); err != nil {
			t.Fatal(err)
		}
		if header.Size > 0 {
			if _, err := tw.Write(bytes.Repeat([]byte("x"), int(header.Size))); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return writeFile(t, buf.Bytes())
}

func TestTarRejectsWritesThroughSymlinks(t *testing.T) {
	for _, target := range []string{"../outside", "inside"} {
		t.Run(target, func(t *testing.T) {
			root := t.TempDir()
			dest := filepath.Join(root, "dest")
			if err := os.Mkdir(dest, 0755); err != nil {
				t.Fatal(err)
			}
			archive := tarWithHeaders(t,
				tar.Header{Name: "link", Typeflag: tar.TypeSymlink, Linkname: target},
				tar.Header{Name: "link/payload", Typeflag: tar.TypeReg, Size: 1})
			err := extractTar(context.Background(), archive, "tar", dest, nil)
			if !errors.Is(err, ErrPathTraversal) {
				t.Fatalf("got %v, want ErrPathTraversal", err)
			}
			if _, err := os.Stat(filepath.Join(root, "outside", "payload")); !os.IsNotExist(err) {
				t.Fatalf("outside payload exists: %v", err)
			}
		})
	}
}

func TestTarRejectsExistingSymlinkAndHardlinkEscapes(t *testing.T) {
	for _, header := range []tar.Header{
		{Name: "link/payload", Typeflag: tar.TypeReg, Size: 1},
		{Name: "hard", Typeflag: tar.TypeLink, Linkname: "../outside/payload"},
		{Name: "hard", Typeflag: tar.TypeLink, Linkname: "link/payload"},
	} {
		t.Run(header.Name+header.Linkname, func(t *testing.T) {
			dest, outside := t.TempDir(), t.TempDir()
			if err := os.Symlink(outside, filepath.Join(dest, "link")); err != nil {
				t.Skip(err)
			}
			err := extractTar(context.Background(), tarWithHeaders(t, header), "tar", dest, nil)
			if !errors.Is(err, ErrPathTraversal) {
				t.Fatalf("got %v, want ErrPathTraversal", err)
			}
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 0 {
				t.Fatalf("outside changed: %v, %v", entries, err)
			}
		})
	}
}

func TestTarPreservesInternalSymlinks(t *testing.T) {
	dest := t.TempDir()
	archive := tarWithHeaders(t,
		tar.Header{Name: "alias", Typeflag: tar.TypeSymlink, Linkname: "game/file"},
		tar.Header{Name: "chain", Typeflag: tar.TypeSymlink, Linkname: "alias"},
		tar.Header{Name: "game/file", Typeflag: tar.TypeReg, Size: 1})
	if err := extractTar(context.Background(), archive, "tar", dest, nil); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alias", "chain"} {
		body, err := os.ReadFile(filepath.Join(dest, name))
		if err != nil || string(body) != "x" {
			t.Fatalf("%s: %q, %v", name, body, err)
		}
	}
}

func TestTarRejectsDeferredSymlinkChainEscape(t *testing.T) {
	dest, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dest, "existing")); err != nil {
		t.Skip(err)
	}
	archive := tarWithHeaders(t,
		tar.Header{Name: "alias", Typeflag: tar.TypeSymlink, Linkname: "chain"},
		tar.Header{Name: "chain", Typeflag: tar.TypeSymlink, Linkname: "existing/../payload"})
	err := extractTar(context.Background(), archive, "tar", dest, nil)
	if !errors.Is(err, ErrPathTraversal) {
		t.Fatalf("got %v, want ErrPathTraversal", err)
	}
	if _, err := os.Lstat(filepath.Join(dest, "alias")); !os.IsNotExist(err) {
		t.Fatalf("unsafe deferred link created: %v", err)
	}
}
