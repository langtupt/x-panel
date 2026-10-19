// Package fsutil has filesystem helpers that never follow customer-controlled symlinks.
package fsutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SafeMkdirAll creates base/rel one segment at a time without following
// symlinks. base must be a trusted directory; everything below it is
// customer-controlled, so an existing symlink or non-directory is refused
// instead of being followed (which could let a customer redirect a
// root-owned chown or write elsewhere).
func SafeMkdirAll(base, rel string, uid, gid int, mode os.FileMode) (string, error) {
	p := base
	if rel == "" {
		return p, nil
	}
	for _, seg := range strings.Split(rel, "/") {
		p = filepath.Join(p, seg)
		st, err := os.Lstat(p)
		switch {
		case errors.Is(err, os.ErrNotExist):
			if err := os.Mkdir(p, mode); err != nil {
				return "", err
			}
			if err := os.Lchown(p, uid, gid); err != nil {
				return "", err
			}
		case err != nil:
			return "", err
		case st.Mode()&os.ModeSymlink != 0 || !st.IsDir():
			return "", fmt.Errorf("unsafe path %s: not a real directory", p)
		}
	}
	return p, nil
}

// CreateIfEmpty writes a placeholder file into dir when dir has no entries.
// O_EXCL refuses to follow a pre-planted symlink.
func CreateIfEmpty(dir, name string, content []byte, uid, gid int) error {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) > 0 {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.Chown(uid, gid); err != nil {
		return err
	}
	_, err = f.Write(content)
	return err
}
