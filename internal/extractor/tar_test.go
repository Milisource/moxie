package extractor

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ulikunitz/xz"
)

// tarBytes builds an uncompressed tar holding a single top-level game dir.
func tarBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeFile(t *testing.T, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "aQiB1niF") // pixeldrain-style: no extension
	if err := os.WriteFile(p, data, 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTarFormats(t *testing.T) {
	files := map[string]string{
		"Game-0.18.1-linux/Game.sh":         "#!/bin/sh\n",
		"Game-0.18.1-linux/game/script.rpy": "label start:\n",
	}
	raw := tarBytes(t, files)

	compress := map[string]func() ([]byte, error){
		"tar": func() ([]byte, error) { return raw, nil },
		"tar.gz": func() ([]byte, error) {
			var b bytes.Buffer
			w := gzip.NewWriter(&b)
			w.Write(raw)
			w.Close()
			return b.Bytes(), nil
		},
		"tar.xz": func() ([]byte, error) {
			var b bytes.Buffer
			w, err := xz.NewWriter(&b)
			if err != nil {
				return nil, err
			}
			w.Write(raw)
			w.Close()
			return b.Bytes(), nil
		},
		"tar.bz2": func() ([]byte, error) {
			// The stdlib has no bzip2 writer; use the system tool if present.
			if _, err := exec.LookPath("bzip2"); err != nil {
				return nil, nil
			}
			cmd := exec.Command("bzip2", "-c")
			cmd.Stdin = bytes.NewReader(raw)
			return cmd.Output()
		},
	}

	for typ, mk := range compress {
		t.Run(typ, func(t *testing.T) {
			data, err := mk()
			if err != nil {
				t.Fatal(err)
			}
			if data == nil {
				t.Skip("bzip2 binary not installed")
			}
			p := writeFile(t, data)
			got, err := DetectArchiveType(p)
			if err != nil || got != typ {
				t.Fatalf("DetectArchiveType = %q, %v; want %q", got, err, typ)
			}
			dest := t.TempDir()
			root, err := Extract(context.Background(), p, dest, nil)
			if err != nil {
				t.Fatalf("Extract: %v", err)
			}
			if filepath.Base(root) != "Game-0.18.1-linux" {
				t.Errorf("root = %s, want single-folder unwrap", root)
			}
			b, err := os.ReadFile(filepath.Join(root, "game", "script.rpy"))
			if err != nil || string(b) != "label start:\n" {
				t.Errorf("script.rpy = %q, %v", b, err)
			}
		})
	}
}

func TestTarRejectsTraversal(t *testing.T) {
	p := writeFile(t, tarBytes(t, map[string]string{"../evil.sh": "x"}))
	_, err := Extract(context.Background(), p, t.TempDir(), nil)
	if !errors.Is(err, ErrPathTraversal) {
		t.Fatalf("err = %v, want ErrPathTraversal", err)
	}
}

func TestDetectDescribesNonArchives(t *testing.T) {
	cases := map[string]string{
		"<!DOCTYPE html><html>":  "HTML page",
		"MZ\x90\x00\x03\x00\x00": "Windows executable",
		"\x1f\x8bnot-really":     "gzip file that is not a tar",
	}
	for body, want := range cases {
		_, err := DetectArchiveType(writeFile(t, []byte(body)))
		if !errors.Is(err, ErrUnknownFormat) || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err = %v, want mention of %q", body, err, want)
		}
	}
}
