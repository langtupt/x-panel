package backup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"xpanel/pkg/agentproto"
	"xpanel/pkg/gdrive"
)

// store is where encrypted backups live: the account's Google Drive or an
// FTP/FTPS server. A file is named by its id (Drive file id, FTP file name).
type store interface {
	Upload(ctx context.Context, name string, r io.Reader, progress func(int64)) (id string, size int64, err error)
	Download(ctx context.Context, id string) (io.ReadCloser, error)
	Delete(ctx context.Context, id string) error
	Name() string
}

func storeFor(drive gdrive.Creds, folder string, ftp *agentproto.FTPDest) (store, error) {
	if ftp != nil {
		if err := ValidateFTP(*ftp); err != nil {
			return nil, err
		}
		return &ftpStore{d: *ftp}, nil
	}
	return &driveStore{c: gdrive.New(drive), folder: folder}, nil
}

// ---------------------------------------------------------------- Drive

type driveStore struct {
	c      *gdrive.Client
	folder string
}

func (s *driveStore) Name() string { return "Google Drive" }

func (s *driveStore) Upload(ctx context.Context, name string, r io.Reader, progress func(int64)) (string, int64, error) {
	if s.folder == "" {
		return "", 0, errors.New("chưa kết nối Google Drive")
	}
	f, err := s.c.Upload(ctx, name, s.folder, r, progress)
	if err != nil {
		if f != nil {
			return f.ID, 0, err
		}
		return "", 0, err
	}
	return f.ID, f.SizeBytes(), nil
}

func (s *driveStore) Download(ctx context.Context, id string) (io.ReadCloser, error) {
	return s.c.Download(ctx, id)
}

func (s *driveStore) Delete(ctx context.Context, id string) error { return s.c.Delete(ctx, id) }

// ---------------------------------------------------------------- FTP

var (
	ftpHostRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$|^\[[0-9a-fA-F:]+\]$`)
	ftpSegRe  = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
)

// ValidateFTP checks an FTP destination; nothing reaches curl unchecked.
func ValidateFTP(d agentproto.FTPDest) error {
	if !ftpHostRe.MatchString(d.Host) {
		return errors.New("máy chủ FTP không hợp lệ")
	}
	if d.Port < 1 || d.Port > 65535 {
		return errors.New("cổng FTP 1–65535")
	}
	if d.User == "" || len(d.User) > 200 || len(d.Password) > 500 ||
		strings.ContainsAny(d.User, ":\r\n\x00") || strings.ContainsAny(d.Password, "\r\n\x00") {
		return errors.New("tên đăng nhập hoặc mật khẩu FTP không hợp lệ")
	}
	if d.Dir != "" {
		for _, seg := range strings.Split(d.Dir, "/") {
			if !ftpSegRe.MatchString(seg) || seg == "." || seg == ".." {
				return errors.New("thư mục FTP không hợp lệ (ví dụ backups/xpanel)")
			}
		}
	}
	return nil
}

type ftpStore struct{ d agentproto.FTPDest }

func (s *ftpStore) Name() string {
	if s.d.TLS {
		return "FTPS " + s.d.Host
	}
	return "FTP " + s.d.Host
}

// url of the backup folder (with a trailing slash) or of a file in it.
func (s *ftpStore) url(name string) string {
	u := "ftp://" + s.d.Host + ":" + strconv.Itoa(s.d.Port) + "/"
	if s.d.Dir != "" {
		u += s.d.Dir + "/"
	}
	return u + name
}

func quote(v string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v) + `"`
}

// curl runs curl with the credentials in a private config file (never on
// the command line, where other users could read them).
func (s *ftpStore) curl(ctx context.Context, stdin io.Reader, stdout io.Writer, url string, args ...string) (*exec.Cmd, func(), error) {
	f, err := os.CreateTemp("", "xpanel-ftp-*.conf")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { os.Remove(f.Name()) }
	cfg := fmt.Sprintf("user = %s\nurl = %s\n", quote(s.d.User+":"+s.d.Password), quote(url))
	if _, err := f.WriteString(cfg); err != nil {
		f.Close()
		cleanup()
		return nil, nil, err
	}
	f.Close()
	base := []string{"-sS", "--fail", "-K", f.Name(), "--connect-timeout", "20", "--ftp-pasv", "--ftp-create-dirs"}
	if s.d.TLS {
		base = append(base, "--ssl-reqd")
		if s.d.Insecure {
			base = append(base, "--insecure")
		}
	}
	cmd := exec.CommandContext(ctx, "curl", append(base, args...)...)
	cmd.Stdin, cmd.Stdout = stdin, stdout
	return cmd, cleanup, nil
}

func (s *ftpStore) run(ctx context.Context, stdin io.Reader, url string, args ...string) error {
	var stderr bytes.Buffer
	cmd, cleanup, err := s.curl(ctx, stdin, io.Discard, url, args...)
	if err != nil {
		return err
	}
	defer cleanup()
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("FTP: %s", firstLine(stderr.String(), err))
	}
	return nil
}

func firstLine(s string, err error) string {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "curl: "))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return err.Error()
	}
	return s
}

type countingReader struct {
	r        io.Reader
	n        int64
	progress func(int64)
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	if c.progress != nil && n > 0 {
		c.progress(c.n)
	}
	return n, err
}

func (s *ftpStore) Upload(ctx context.Context, name string, r io.Reader, progress func(int64)) (string, int64, error) {
	if !fileNameRe.MatchString(name) {
		return "", 0, errors.New("tên file backup không hợp lệ")
	}
	cr := &countingReader{r: r, progress: progress}
	if err := s.run(ctx, cr, s.url(name), "-T", "-"); err != nil {
		return name, cr.n, err
	}
	return name, cr.n, nil
}

type cmdReader struct {
	io.ReadCloser
	cmd     *exec.Cmd
	cleanup func()
	stderr  *bytes.Buffer
}

func (c *cmdReader) Close() error {
	c.ReadCloser.Close()
	err := c.cmd.Wait()
	c.cleanup()
	if err != nil && c.cmd.ProcessState != nil && !c.cmd.ProcessState.Success() && c.stderr.Len() > 0 {
		return fmt.Errorf("FTP: %s", firstLine(c.stderr.String(), err))
	}
	return nil
}

func (s *ftpStore) Download(ctx context.Context, id string) (io.ReadCloser, error) {
	if !fileNameRe.MatchString(id) {
		return nil, errors.New("tên file backup không hợp lệ")
	}
	cmd, cleanup, err := s.curl(ctx, nil, nil, s.url(id))
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		cleanup()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cleanup()
		return nil, err
	}
	// Fail early (wrong password, missing file) instead of a decrypt error.
	br := &peekReader{r: out}
	if _, err := br.peek(); err != nil {
		_ = cmd.Wait()
		cleanup()
		return nil, fmt.Errorf("FTP: %s", firstLine(stderr.String(), err))
	}
	return &cmdReader{ReadCloser: struct {
		io.Reader
		io.Closer
	}{br, out}, cmd: cmd, cleanup: cleanup, stderr: &stderr}, nil
}

// peekReader reads the first byte ahead so a failed download shows up before
// the caller starts decrypting.
type peekReader struct {
	r    io.Reader
	buf  []byte
	done bool
}

func (p *peekReader) peek() (int, error) {
	b := make([]byte, 1)
	n, err := io.ReadFull(p.r, b)
	if n == 0 {
		if err == io.ErrUnexpectedEOF || err == io.EOF {
			err = errors.New("file trống hoặc không tải được")
		}
		return 0, err
	}
	p.buf = b[:n]
	return n, nil
}

func (p *peekReader) Read(b []byte) (int, error) {
	if len(p.buf) > 0 {
		n := copy(b, p.buf)
		p.buf = p.buf[n:]
		return n, nil
	}
	return p.r.Read(b)
}

func (s *ftpStore) Delete(ctx context.Context, id string) error {
	if !fileNameRe.MatchString(id) {
		return errors.New("tên file backup không hợp lệ")
	}
	// List the folder (cheap) and delete the file after changing into it.
	return s.run(ctx, nil, s.url(""), "-l", "-Q", "-DELE "+id)
}

// TestFTP logs in, creates the folder if needed and writes then deletes a
// small file, so a wrong password or a read-only account shows at once.
func TestFTP(ctx context.Context, d agentproto.FTPDest) error {
	if err := ValidateFTP(d); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	s := &ftpStore{d: d}
	name := fmt.Sprintf("xpanel-test-%d.xpb", time.Now().UnixNano())
	if err := s.run(ctx, strings.NewReader("X-Panel FTP test\n"), s.url(name), "-T", "-"); err != nil {
		return err
	}
	return s.Delete(ctx, name)
}

// FTPOp runs a maintenance operation the panel needs on an FTP destination.
func FTPOp(ctx context.Context, p agentproto.BackupFTPParams) error {
	switch p.Op {
	case "test":
		return TestFTP(ctx, p.FTP)
	case "delete":
		if err := ValidateFTP(p.FTP); err != nil {
			return err
		}
		return (&ftpStore{d: p.FTP}).Delete(ctx, p.Name)
	}
	return fmt.Errorf("unknown op %q", p.Op)
}
