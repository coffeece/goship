package archive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func unpack(t *testing.T, r io.Reader) map[string]string {
	t.Helper()
	gz, err := gzip.NewReader(r)
	if err != nil {
		t.Fatalf("not gzip: %v", err)
	}
	tr := tar.NewReader(gz)
	out := map[string]string{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Uid != 0 || h.Uname != "" {
			t.Errorf("%s carries the local owner (uid=%d uname=%q)", h.Name, h.Uid, h.Uname)
		}
		b, _ := io.ReadAll(tr)
		out[h.Name] = string(b)
	}
}

func names(m map[string]string) string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return strings.Join(ks, " ")
}

func TestWritePacksTheProjectWithoutVersionControl(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "main.go", "package main")
	write(t, dir, "web/index.html", "<h1>hi</h1>")
	write(t, dir, ".git/config", "[core]")
	write(t, dir, ".env", "SECRET=1")

	var buf bytes.Buffer
	files, ignoreFile, err := Write(dir, &buf)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	got := unpack(t, &buf)
	if names(got) != ".env main.go web/ web/index.html" {
		t.Errorf("entries = %q", names(got))
	}
	if got["web/index.html"] != "<h1>hi</h1>" || files != 3 || ignoreFile != "" {
		t.Errorf("content=%q files=%d ignoreFile=%q", got["web/index.html"], files, ignoreFile)
	}
}

func TestWriteHonoursTheFirstIgnoreFilePresent(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "main.go", "package main")
	write(t, dir, ".env", "SECRET=1")
	write(t, dir, "node_modules/x/index.js", "x")
	write(t, dir, "notes.txt", "draft")
	write(t, dir, ".goshipignore", ".env\nnode_modules/\n")
	// Present but not consulted: .goshipignore comes first.
	write(t, dir, ".dockerignore", "main.go\n")

	var buf bytes.Buffer
	_, ignoreFile, err := Write(dir, &buf)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	got := names(unpack(t, &buf))
	if ignoreFile != ".goshipignore" {
		t.Errorf("ignoreFile = %q", ignoreFile)
	}
	for _, leaked := range []string{".env", "node_modules"} {
		if strings.Contains(" "+got+" ", " "+leaked) {
			t.Errorf("%s was packed: %q", leaked, got)
		}
	}
	if !strings.Contains(got, "main.go") || !strings.Contains(got, "notes.txt") {
		t.Errorf("entries = %q", got)
	}
}
