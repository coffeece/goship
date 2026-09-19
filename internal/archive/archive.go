// Package archive turns a project directory into the gzipped tarball a deploy
// uploads.
package archive

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	ignore "github.com/sabhiram/go-gitignore"
)

// IgnoreFiles are read from the project root, first match wins. They use
// gitignore syntax. .tsuruignore is honoured for projects that predate GoShip.
var IgnoreFiles = []string{".goshipignore", ".tsuruignore", ".dockerignore"}

// alwaysIgnored never belongs in a build: version control, and the CLI's own
// per-machine state.
var alwaysIgnored = []string{".git"}

// Write streams dir as a gzipped tarball to w and returns how many files it
// packed and which ignore file it applied ("" for none).
func Write(dir string, w io.Writer) (files int, ignoreFile string, err error) {
	matcher, ignoreFile, err := loadIgnore(dir)
	if err != nil {
		return 0, "", err
	}

	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)

	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil || rel == "." {
			return err
		}
		rel = filepath.ToSlash(rel)

		if skip(rel, d.IsDir(), matcher) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		var link string
		if info.Mode()&os.ModeSymlink != 0 {
			if link, err = os.Readlink(path); err != nil {
				return err
			}
		} else if !info.Mode().IsRegular() && !info.IsDir() {
			return nil // sockets, devices, pipes
		}

		hdr, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		hdr.Name = rel
		if info.IsDir() {
			hdr.Name += "/"
		}
		// Who owns the files on the developer's machine means nothing in the
		// build, and leaks their username.
		hdr.Uid, hdr.Gid, hdr.Uname, hdr.Gname = 0, 0, "", ""
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close() //nolint:errcheck
		if _, err := io.Copy(tw, f); err != nil {
			return fmt.Errorf("packing %s: %w", rel, err)
		}
		files++
		return nil
	})
	if err != nil {
		return files, ignoreFile, err
	}
	if err := tw.Close(); err != nil {
		return files, ignoreFile, err
	}
	return files, ignoreFile, gz.Close()
}

func skip(rel string, isDir bool, matcher *ignore.GitIgnore) bool {
	for _, name := range alwaysIgnored {
		if rel == name || strings.HasPrefix(rel, name+"/") {
			return true
		}
	}
	if matcher == nil {
		return false
	}
	if isDir {
		return matcher.MatchesPath(rel + "/")
	}
	return matcher.MatchesPath(rel)
}

func loadIgnore(dir string) (*ignore.GitIgnore, string, error) {
	for _, name := range IgnoreFiles {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		m, err := ignore.CompileIgnoreFile(path)
		if err != nil {
			return nil, "", fmt.Errorf("reading %s: %w", name, err)
		}
		return m, name, nil
	}
	return nil, "", nil
}
