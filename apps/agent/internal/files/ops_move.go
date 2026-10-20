package files

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// inside reports whether p is dir itself or below it.
func inside(p, dir string) bool {
	return p == dir || strings.HasPrefix(p, strings.TrimSuffix(dir, "/")+"/")
}

func checkDestDir(destDir string) error {
	info, err := os.Stat(destDir)
	if err != nil {
		return fmt.Errorf("thư mục đích %s không tồn tại", destDir)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s không phải thư mục", destDir)
	}
	return nil
}

// move puts each path into destDir, keeping its name.
func move(paths []string, destDir string) error {
	if err := checkDestDir(destDir); err != nil {
		return err
	}
	for _, raw := range paths {
		src := clean(raw)
		if src == "/" {
			return errors.New("không thể di chuyển thư mục gốc")
		}
		if inside(destDir, src) {
			return fmt.Errorf("không thể di chuyển %s vào chính nó", src)
		}
		dst := path.Join(destDir, path.Base(src))
		if dst == src {
			continue
		}
		if err := rename(src, dst); err != nil {
			return err
		}
	}
	return nil
}

// copyAll copies each path (files, directories, symlinks as links) into
// destDir; an existing name gets a " (copy)" suffix.
func copyAll(paths []string, destDir string) error {
	if err := checkDestDir(destDir); err != nil {
		return err
	}
	for _, raw := range paths {
		src := clean(raw)
		if src == "/" {
			return errors.New("không thể sao chép thư mục gốc")
		}
		if inside(destDir, src) {
			return fmt.Errorf("không thể sao chép %s vào chính nó", src)
		}
		dst := freeName(path.Join(destDir, path.Base(src)))
		if err := copyTree(src, dst); err != nil {
			return err
		}
	}
	return nil
}

func freeName(p string) string {
	if _, err := os.Lstat(p); errors.Is(err, fs.ErrNotExist) {
		return p
	}
	ext := path.Ext(p)
	base := strings.TrimSuffix(p, ext)
	for i := 1; ; i++ {
		name := fmt.Sprintf("%s (copy)%s", base, ext)
		if i > 1 {
			name = fmt.Sprintf("%s (copy %d)%s", base, i, ext)
		}
		if _, err := os.Lstat(name); errors.Is(err, fs.ErrNotExist) {
			return name
		}
	}
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := path.Join(dst, strings.TrimPrefix(p, src))
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case d.IsDir():
			return os.Mkdir(target, info.Mode().Perm()|0o700)
		case d.Type().IsRegular():
			return copyFile(p, target, info.Mode().Perm())
		}
		return nil // sockets, fifos, devices
	})
}

func copyFile(src, dst string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// compressTarGz packs paths into a .tar.gz (symlinks kept as links).
func compressTarGz(paths []string, dest string) error {
	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	fail := func(err error) error {
		tw.Close()
		gz.Close()
		f.Close()
		os.Remove(dest)
		return err
	}
	for _, raw := range paths {
		src := clean(raw)
		base := path.Dir(src)
		err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if p == dest {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			link := ""
			if d.Type()&fs.ModeSymlink != 0 {
				if link, err = os.Readlink(p); err != nil {
					return err
				}
			} else if !d.IsDir() && !d.Type().IsRegular() {
				return nil
			}
			hdr, err := tar.FileInfoHeader(info, link)
			if err != nil {
				return err
			}
			hdr.Name = strings.TrimPrefix(strings.TrimPrefix(p, base), "/")
			if d.IsDir() {
				hdr.Name += "/"
			}
			hdr.Uname, hdr.Gname = "", ""
			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}
			if d.Type().IsRegular() {
				in, err := os.Open(p)
				if err != nil {
					return err
				}
				defer in.Close()
				_, err = io.Copy(tw, in)
				return err
			}
			return nil
		})
		if err != nil {
			return fail(err)
		}
	}
	if err := tw.Close(); err != nil {
		return fail(err)
	}
	if err := gz.Close(); err != nil {
		return fail(err)
	}
	return f.Close()
}
