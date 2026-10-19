// Package fetch downloads release archives over HTTPS, verifies their
// SHA-256 and unpacks them. Only used for root-owned install locations
// with archives from official sources (nodejs.org, go.dev, getcomposer.org).
package fetch

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var client = &http.Client{Timeout: 15 * time.Minute}

func get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "xpanel-agent")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return resp, nil
}

// JSON decodes a small JSON document.
func JSON(ctx context.Context, url string, out any) error {
	resp, err := get(ctx, url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(out)
}

// Text returns a small text document.
func Text(ctx context.Context, url string) (string, error) {
	resp, err := get(ctx, url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return string(b), err
}

// File downloads url to a temp file and checks its SHA-256 (hex).
func File(ctx context.Context, url, wantSHA256 string) (string, error) {
	resp, err := get(ctx, url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	f, err := os.CreateTemp("", "xpanel-dl-*")
	if err != nil {
		return "", err
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, 1<<30)); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, strings.TrimSpace(wantSHA256)) {
		os.Remove(f.Name())
		return "", fmt.Errorf("SHA-256 mismatch for %s", url)
	}
	return f.Name(), nil
}

// UntarGz unpacks a .tar.gz into dest, dropping the first `strip` path
// components. Regular files, directories and symlinks are supported;
// entries escaping dest are rejected.
func UntarGz(archive, dest string, strip int) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		parts := strings.Split(strings.TrimPrefix(hdr.Name, "./"), "/")
		if len(parts) <= strip {
			continue
		}
		rel := filepath.Clean(strings.Join(parts[strip:], "/"))
		if rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			continue
		}
		p := filepath.Join(dest, rel)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.FileMode(hdr.Mode).Perm()|0o444)
			if err != nil {
				return err
			}
			_, err = io.Copy(out, tr)
			if cerr := out.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
		case tar.TypeSymlink:
			target := hdr.Linkname
			resolved := filepath.Clean(filepath.Join(filepath.Dir(p), target))
			if filepath.IsAbs(target) || !strings.HasPrefix(resolved, filepath.Clean(dest)+string(os.PathSeparator)) {
				return errors.New("archive symlink escapes destination: " + hdr.Name)
			}
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			_ = os.Remove(p)
			if err := os.Symlink(target, p); err != nil {
				return err
			}
		}
	}
}

// Replace installs a freshly unpacked directory at dest atomically-ish:
// unpack into dest.new, then swap.
func Replace(dest string, fill func(tmp string) error) error {
	tmp := dest + ".new"
	_ = os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return err
	}
	if err := fill(tmp); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	old := dest + ".old"
	_ = os.RemoveAll(old)
	if _, err := os.Stat(dest); err == nil {
		if err := os.Rename(dest, old); err != nil {
			return err
		}
	}
	if err := os.Rename(tmp, dest); err != nil {
		return err
	}
	return os.RemoveAll(old)
}
