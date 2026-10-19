// Package files implements the web file manager. Operations run in a helper
// process ("xpanel-agent fshelper") that chroots into the customer's home and
// drops to their uid/gid before reading the request, so every path is
// confined to the home and checked with the customer's own permissions.
package files

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"xpanel/pkg/agentproto"
)

const (
	MaxEditSize    = 2 << 20 // text editor limit
	maxListEntries = 5000
	maxExtractSize = 20 << 30
)

// clean turns a request path into an absolute path inside the chroot.
func clean(p string) string { return path.Clean("/" + p) }

func dispatch(req agentproto.FSRequest, in io.Reader, out io.Writer) (*agentproto.FSResult, error) {
	switch req.Op {
	case "list":
		return list(clean(req.Path))
	case "read":
		return read(clean(req.Path))
	case "write":
		return nil, write(clean(req.Path), req.Content)
	case "mkdir":
		return nil, os.Mkdir(clean(req.Path), 0o755)
	case "rename":
		return nil, rename(clean(req.Path), clean(req.To))
	case "delete":
		return nil, remove(req.Paths)
	case "chmod":
		return nil, chmod(clean(req.Path), req.Mode)
	case "compress":
		return nil, compress(req.Paths, clean(req.To))
	case "extract":
		return nil, extract(clean(req.Path), clean(req.To))
	case "upload":
		return nil, upload(clean(req.Path), in)
	case "download":
		return nil, download(clean(req.Path), out)
	}
	return nil, fmt.Errorf("unknown op %q", req.Op)
}

func list(dir string) (*agentproto.FSResult, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	if len(entries) > maxListEntries {
		entries = entries[:maxListEntries]
	}
	res := &agentproto.FSResult{Entries: []agentproto.FSEntry{}}
	for _, e := range entries {
		info, err := e.Info() // lstat
		if err != nil {
			continue
		}
		fe := agentproto.FSEntry{Name: e.Name(), Size: info.Size(), ModTime: info.ModTime(),
			Mode: fmt.Sprintf("%03o", info.Mode().Perm()), Type: "file"}
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			fe.Type = "link"
			fe.Target, _ = os.Readlink(path.Join(dir, e.Name()))
		case info.IsDir():
			fe.Type = "dir"
		}
		res.Entries = append(res.Entries, fe)
	}
	sort.SliceStable(res.Entries, func(i, j int) bool {
		a, b := res.Entries[i], res.Entries[j]
		if (a.Type == "dir") != (b.Type == "dir") {
			return a.Type == "dir"
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	return res, nil
}

func regular(p string) (fs.FileInfo, error) {
	info, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("không phải file thường")
	}
	return info, nil
}

func read(p string) (*agentproto.FSResult, error) {
	info, err := regular(p)
	if err != nil {
		return nil, err
	}
	if info.Size() > MaxEditSize {
		return nil, errors.New("file quá lớn để mở trong trình soạn thảo (tối đa 2 MB)")
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(b) {
		return nil, errors.New("file nhị phân, không mở được bằng trình soạn thảo")
	}
	return &agentproto.FSResult{Content: string(b)}, nil
}

// write replaces the file atomically, keeping its permissions.
func write(p, content string) error {
	if len(content) > MaxEditSize {
		return errors.New("nội dung quá lớn (tối đa 2 MB)")
	}
	mode := fs.FileMode(0o644)
	if info, err := os.Stat(p); err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("không phải file thường")
		}
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(path.Dir(p), "."+path.Base(p)+".xpanel-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

func rename(from, to string) error {
	if from == "/" {
		return errors.New("không thể đổi tên thư mục gốc")
	}
	if _, err := os.Lstat(to); err == nil {
		return fmt.Errorf("%s đã tồn tại", to)
	}
	return os.Rename(from, to)
}

func remove(paths []string) error {
	for _, raw := range paths {
		p := clean(raw)
		if p == "/" {
			return errors.New("không thể xóa thư mục gốc")
		}
		if err := os.RemoveAll(p); err != nil {
			return err
		}
	}
	return nil
}

func chmod(p, mode string) error {
	m, err := strconv.ParseUint(mode, 8, 32)
	if err != nil || m > 0o777 {
		return errors.New("quyền không hợp lệ (ví dụ 644, 755)")
	}
	return os.Chmod(p, fs.FileMode(m))
}

func upload(p string, in io.Reader) error {
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, in); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// download writes "OK <size>\n" followed by the file, so the agent can
// report errors before it starts streaming.
func download(p string, out io.Writer) error {
	info, err := regular(p)
	if err != nil {
		return err
	}
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := fmt.Fprintf(out, "OK %d\n", info.Size()); err != nil {
		return err
	}
	_, err = io.Copy(out, f)
	return err
}

// compress zips files and directories (symlinks are skipped) into dest.
func compress(paths []string, dest string) error {
	if !strings.HasSuffix(dest, ".zip") {
		return errors.New("file nén phải có đuôi .zip")
	}
	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	for _, raw := range paths {
		src := clean(raw)
		base := path.Dir(src)
		err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if p == dest || d.Type()&fs.ModeSymlink != 0 {
				return nil
			}
			name := strings.TrimPrefix(strings.TrimPrefix(p, base), "/")
			info, err := d.Info()
			if err != nil {
				return err
			}
			hdr, err := zip.FileInfoHeader(info)
			if err != nil {
				return err
			}
			hdr.Name = name
			if d.IsDir() {
				hdr.Name += "/"
				_, err = zw.CreateHeader(hdr)
				return err
			}
			hdr.Method = zip.Deflate
			w, err := zw.CreateHeader(hdr)
			if err != nil {
				return err
			}
			src, err := os.Open(p)
			if err != nil {
				return err
			}
			defer src.Close()
			_, err = io.Copy(w, src)
			return err
		})
		if err != nil {
			zw.Close()
			f.Close()
			os.Remove(dest)
			return err
		}
	}
	if err := zw.Close(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// extract unpacks .zip, .tar, .tar.gz/.tgz into destDir. Entry names are
// cleaned under destDir; links and special files are skipped.
func extract(archive, destDir string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	lower := strings.ToLower(archive)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return extractZip(archive, destDir)
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		f, err := os.Open(archive)
		if err != nil {
			return err
		}
		defer f.Close()
		gz, err := gzip.NewReader(bufio.NewReader(f))
		if err != nil {
			return err
		}
		return extractTar(tar.NewReader(gz), destDir)
	case strings.HasSuffix(lower, ".tar"):
		f, err := os.Open(archive)
		if err != nil {
			return err
		}
		defer f.Close()
		return extractTar(tar.NewReader(f), destDir)
	}
	return errors.New("chỉ hỗ trợ .zip, .tar, .tar.gz, .tgz")
}

func target(destDir, name string) string { return path.Join(destDir, clean(name)) }

func writeEntry(p string, mode fs.FileMode, r io.Reader, budget *int64) error {
	if err := os.MkdirAll(path.Dir(p), 0o755); err != nil {
		return err
	}
	mode = mode.Perm() & 0o777
	if mode == 0 {
		mode = 0o644
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(r, *budget+1))
	*budget -= n
	if err == nil && *budget < 0 {
		err = errors.New("dữ liệu giải nén quá lớn")
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

func extractZip(archive, destDir string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer zr.Close()
	budget := int64(maxExtractSize)
	for _, zf := range zr.File {
		p := target(destDir, zf.Name)
		mode := zf.Mode()
		switch {
		case mode.IsDir():
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
		case mode.IsRegular():
			rc, err := zf.Open()
			if err != nil {
				return err
			}
			err = writeEntry(p, mode, rc, &budget)
			rc.Close()
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func extractTar(tr *tar.Reader, destDir string) error {
	budget := int64(maxExtractSize)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		p := target(destDir, hdr.Name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := writeEntry(p, fs.FileMode(hdr.Mode), tr, &budget); err != nil {
				return err
			}
		}
	}
}
