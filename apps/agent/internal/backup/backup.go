// Package backup archives one hosting account (home directory, databases,
// mail), encrypts it and streams it to Google Drive, and restores it.
//
// Archive layout (tar, gzip, then encrypted, see crypt.go):
//
//	manifest.json             panel records of the account
//	home/...                  the home directory, read as the account's user
//	databases/<name>.sql      mysqldump / pg_dump output
//	mail/<domain>/...         maildirs
package backup

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"xpanel/apps/agent/internal/apps"
	"xpanel/apps/agent/internal/run"
	"xpanel/pkg/agentproto"
	"xpanel/pkg/gdrive"
	"xpanel/pkg/validate"
)

const (
	tmpDir   = "/var/lib/xpanel/backup-tmp"
	mailRoot = "/var/mail/vhosts"
)

// One backup or restore at a time: they are I/O heavy.
var running sync.Mutex

var fileNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,200}$`)

type logger struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (l *logger) printf(f string, a ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(&l.buf, time.Now().Format("15:04:05")+" "+f+"\n", a...)
}

func (l *logger) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

func account(username string) (*user.User, error) {
	if err := validate.Username(username); err != nil {
		return nil, err
	}
	u, err := user.Lookup(username)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(u.HomeDir, "/home/") || filepath.Clean(u.HomeDir) != u.HomeDir {
		return nil, fmt.Errorf("unexpected home directory %q", u.HomeDir)
	}
	return u, nil
}

func checkDBs(owner string, dbs []agentproto.BackupDB) error {
	for _, d := range dbs {
		if err := validate.DBName(owner, d.Name); err != nil {
			return err
		}
		if d.User != "" {
			if err := validate.DBName(owner, d.User); err != nil {
				return err
			}
		}
		if d.Engine != "mariadb" && d.Engine != "postgresql" {
			return fmt.Errorf("unknown engine %q", d.Engine)
		}
	}
	return nil
}

func checkDomains(ds []string) error {
	for _, d := range ds {
		if err := validate.Domain(d); err != nil {
			return err
		}
	}
	return nil
}

// Create builds the archive and uploads it while it is being produced.
func Create(ctx context.Context, p agentproto.BackupCreateParams) (agentproto.BackupCreateResult, error) {
	var res agentproto.BackupCreateResult
	log := &logger{}
	defer func() { res.Log = log.String() }()
	u, err := account(p.Username)
	if err != nil {
		return res, err
	}
	if err := checkDBs(p.Username, p.Databases); err != nil {
		return res, err
	}
	if err := checkDomains(p.MailDomains); err != nil {
		return res, err
	}
	if !fileNameRe.MatchString(p.FileName) || p.FolderID == "" || len(p.Passphrase) < 12 {
		return res, errors.New("invalid backup parameters")
	}
	if !running.TryLock() {
		return res, errors.New("đang có một backup/restore khác chạy, thử lại sau")
	}
	defer running.Unlock()
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		return res, err
	}

	pr, pw := io.Pipe()
	written := make(chan error, 1)
	go func() {
		err := writeArchive(ctx, pw, u, p, log)
		pw.CloseWithError(err)
		written <- err
	}()
	log.printf("Tải lên Google Drive: %s", p.FileName)
	var lastLog time.Time
	file, upErr := gdrive.New(p.Drive).Upload(ctx, p.FileName, p.FolderID, pr, func(n int64) {
		if time.Since(lastLog) > 30*time.Second {
			lastLog = time.Now()
			log.printf("  đã tải lên %d MB", n>>20)
		}
	})
	pr.CloseWithError(errors.New("upload stopped"))
	if werr := <-written; werr != nil {
		if file != nil {
			_ = gdrive.New(p.Drive).Delete(context.WithoutCancel(ctx), file.ID)
		}
		return res, werr
	}
	if upErr != nil {
		return res, upErr
	}
	res.FileID, res.Size = file.ID, file.SizeBytes()
	log.printf("Hoàn tất: %.1f MB (đã mã hóa)", float64(res.Size)/(1<<20))
	return res, nil
}

func writeArchive(ctx context.Context, w io.Writer, u *user.User, p agentproto.BackupCreateParams, log *logger) error {
	enc, err := NewEncrypter(w, p.Passphrase)
	if err != nil {
		return err
	}
	gz, _ := gzip.NewWriterLevel(enc, gzip.DefaultCompression)
	tw := tar.NewWriter(gz)
	now := time.Now()
	if err := tw.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0o600, Size: int64(len(p.Manifest)), ModTime: now}); err != nil {
		return err
	}
	if _, err := tw.Write(p.Manifest); err != nil {
		return err
	}
	if auth, _ := json.Marshal(dbAuth(ctx, p.Databases)); len(auth) > 2 {
		if err := tw.WriteHeader(&tar.Header{Name: dbAuthName, Mode: 0o600, Size: int64(len(auth)), ModTime: now}); err != nil {
			return err
		}
		if _, err := tw.Write(auth); err != nil {
			return err
		}
	}

	log.printf("Thư mục home %s", u.HomeDir)
	if err := addHome(ctx, tw, u, log); err != nil {
		return fmt.Errorf("home: %w", err)
	}
	for _, d := range p.Databases {
		log.printf("Database %s (%s)", d.Name, d.Engine)
		if err := addDatabase(ctx, tw, d, now); err != nil {
			return fmt.Errorf("database %s: %w", d.Name, err)
		}
	}
	for _, d := range p.MailDomains {
		log.printf("Email @%s", d)
		if err := addMail(tw, d); err != nil {
			return fmt.Errorf("mail %s: %w", d, err)
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return enc.Close()
}

// addHome copies the home directory into the archive. tar runs as the
// account's own user, so a symlink the user plants can never make it read
// a file the user could not read anyway.
func addHome(ctx context.Context, tw *tar.Writer, u *user.User, log *logger) error {
	cmd := exec.CommandContext(ctx, "runuser", "-u", u.Username, "--", "nice", "-n", "10",
		"tar", "--create", "--file=-", "--numeric-owner", "--one-file-system", "--warning=no-file-changed",
		"--exclude=./.cache", "--exclude=./tmp/*", "-C", u.HomeDir, ".")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	tr := tar.NewReader(out)
	count := 0
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return err
		}
		rel := strings.TrimPrefix(h.Name, "./")
		if rel == "" || rel == "." {
			continue
		}
		h.Name = "home/" + rel
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if _, err := io.Copy(tw, tr); err != nil {
			return err
		}
		count++
	}
	err = cmd.Wait()
	// GNU tar exits 1 when files changed while being read and 2 when some
	// could not be read: the archive is still usable, so only warn.
	var exit *exec.ExitError
	if err != nil && !(errors.As(err, &exit) && exit.ExitCode() <= 2) {
		return err
	}
	if msg := strings.TrimSpace(stderr.String()); msg != "" {
		lines := strings.Split(msg, "\n")
		log.printf("  cảnh báo (%d dòng): %s", len(lines), lines[0])
	}
	log.printf("  %d mục", count)
	return nil
}

func dumpCommand(ctx context.Context, d agentproto.BackupDB) *exec.Cmd {
	if d.Engine == "postgresql" {
		return exec.CommandContext(ctx, "runuser", "-u", "postgres", "--", "nice", "-n", "10",
			"pg_dump", "-p", strconv.Itoa(apps.PGPort), "--clean", "--if-exists", "--no-owner", "--no-privileges", d.Name)
	}
	return exec.CommandContext(ctx, "nice", "-n", "10", "mysqldump", "--single-transaction", "--quick",
		"--routines", "--triggers", "--events", "--no-tablespaces", "--skip-lock-tables", d.Name)
}

// addDatabase dumps to a private temporary file first: tar needs the size.
func addDatabase(ctx context.Context, tw *tar.Writer, d agentproto.BackupDB, now time.Time) error {
	f, err := os.CreateTemp(tmpDir, "dump-*.sql")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	cmd := dumpCommand(ctx, d)
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = f, &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
	}
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Name: "databases/" + d.Name + ".sql", Mode: 0o600, Size: info.Size(), ModTime: now}); err != nil {
		return err
	}
	_, err = io.Copy(tw, f)
	return err
}

// addMail archives a domain's maildirs (owned by vmail; customers cannot
// write there, so the tree holds no planted symlinks).
func addMail(tw *tar.Writer, domain string) error {
	root := filepath.Join(mailRoot, domain)
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		rel, _ := filepath.Rel(root, path)
		info, err := d.Info()
		if err != nil {
			return nil
		}
		name := "mail/" + domain + "/" + filepath.ToSlash(rel)
		switch {
		case d.IsDir():
			return tw.WriteHeader(&tar.Header{Name: name + "/", Typeflag: tar.TypeDir, Mode: 0o700, ModTime: info.ModTime()})
		case d.Type().IsRegular():
			f, err := os.Open(path)
			if err != nil {
				return nil // delivered mail can be moved by Dovecot meanwhile
			}
			defer f.Close()
			if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: info.Size(), ModTime: info.ModTime()}); err != nil {
				return err
			}
			_, err = io.CopyN(tw, f, info.Size())
			return err
		}
		return nil
	})
}

// Restore downloads, verifies and writes back the selected parts.
func Restore(ctx context.Context, p agentproto.BackupRestoreParams) (agentproto.TaskLog, error) {
	log := &logger{}
	res := func(err error) (agentproto.TaskLog, error) { return agentproto.TaskLog{Log: log.String()}, err }
	u, err := account(p.Username)
	if err != nil {
		return res(err)
	}
	if err := checkDBs(p.Username, p.Databases); err != nil {
		return res(err)
	}
	if err := checkDomains(p.MailDomains); err != nil {
		return res(err)
	}
	if !running.TryLock() {
		return res(errors.New("đang có một backup/restore khác chạy, thử lại sau"))
	}
	defer running.Unlock()

	log.printf("Tải backup từ Google Drive")
	body, err := gdrive.New(p.Drive).Download(ctx, p.FileID)
	if err != nil {
		return res(err)
	}
	defer body.Close()
	dec, err := NewDecrypter(bufio.NewReaderSize(body, 1<<20), p.Passphrase)
	if err != nil {
		return res(err)
	}
	gz, err := gzip.NewReader(dec)
	if err != nil {
		return res(ErrBadBackup)
	}
	tr := tar.NewReader(gz)

	dbs := map[string]agentproto.BackupDB{}
	for _, d := range p.Databases {
		dbs[d.Name] = d
	}
	mail := map[string]bool{}
	for _, d := range p.MailDomains {
		mail[d] = true
	}
	var home *homeWriter
	defer func() {
		if home != nil {
			home.abort()
		}
	}()
	restoredDBs := map[string]bool{}
	auth := map[string]string{}
	manifestSeen := false
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return res(err)
		}
		if !manifestSeen && h.Name != "manifest.json" {
			return res(errors.New("backup thiếu manifest đầu tiên"))
		}
		switch {
		case h.Name == "manifest.json":
			if manifestSeen || h.Typeflag != tar.TypeReg || h.Size > 16<<20 {
				return res(ErrBadBackup)
			}
			manifestSeen = true
			var m struct {
				Username string `json:"username"`
			}
			b, _ := io.ReadAll(io.LimitReader(tr, 16<<20))
			if json.Unmarshal(b, &m) != nil || m.Username != p.Username {
				return res(fmt.Errorf("backup này thuộc tài khoản %q, không phải %q", m.Username, p.Username))
			}
		case h.Name == dbAuthName:
			if h.Size > 1<<20 {
				return res(ErrBadBackup)
			}
			b, _ := io.ReadAll(io.LimitReader(tr, 1<<20))
			auth = parseDBAuth(b)
		case strings.HasPrefix(h.Name, "home/"):
			if !p.Files {
				continue
			}
			if home == nil {
				log.printf("Khôi phục file vào %s", u.HomeDir)
				if home, err = newHomeWriter(ctx, u); err != nil {
					return res(err)
				}
			}
			if err := home.add(h, tr); err != nil {
				return res(err)
			}
		case strings.HasPrefix(h.Name, "databases/"):
			name := strings.TrimSuffix(strings.TrimPrefix(h.Name, "databases/"), ".sql")
			d, ok := dbs[name]
			if !ok {
				continue
			}
			log.printf("Khôi phục database %s", name)
			if err := importDB(ctx, u, d, tr); err != nil {
				return res(fmt.Errorf("database %s: %w", name, err))
			}
			restoredDBs[name] = true
			if err := applyDBAuth(ctx, d, auth); err != nil {
				log.printf("  không khôi phục được mật khẩu user %s: %v", d.User, err)
			} else if auth[dbAuthKey(d.Engine, d.User)] != "" {
				log.printf("  user %s dùng lại mật khẩu cũ", d.User)
			}
		case strings.HasPrefix(h.Name, "mail/"):
			rest := strings.TrimPrefix(h.Name, "mail/")
			domain, rel, _ := strings.Cut(rest, "/")
			if !mail[domain] {
				continue
			}
			if err := writeMail(domain, rel, h, tr); err != nil {
				return res(err)
			}
		}
	}
	if !manifestSeen {
		return res(ErrBadBackup)
	}
	// tar may stop before gzip's checksum and the encrypted final frame.
	if _, err := io.Copy(io.Discard, gz); err != nil {
		return res(err)
	}
	if err := gz.Close(); err != nil {
		return res(err)
	}
	if _, err := io.Copy(io.Discard, dec); err != nil {
		return res(err)
	}
	if home != nil {
		err := home.finish()
		home = nil
		if err != nil {
			return res(err)
		}
	}
	for name := range dbs {
		if !restoredDBs[name] {
			log.printf("  backup không có database %s, bỏ qua", name)
		}
	}
	if len(mail) > 0 {
		for d := range mail {
			_ = exec.CommandContext(ctx, "chown", "-R", "vmail:vmail", filepath.Join(mailRoot, d)).Run()
		}
		log.printf("Đã khôi phục email")
	}
	log.printf("Khôi phục hoàn tất")
	return res(nil)
}

// homeWriter feeds home/ entries to tar running as the account's user, so
// the restore cannot write anywhere the user could not.
type homeWriter struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	tw     *tar.Writer
	stderr bytes.Buffer
}

func newHomeWriter(ctx context.Context, u *user.User) (*homeWriter, error) {
	hw := &homeWriter{}
	hw.cmd = exec.CommandContext(ctx, "runuser", "-u", u.Username, "--", "tar", "--extract", "--file=-",
		"--no-same-owner", "--preserve-permissions", "--overwrite", "-C", u.HomeDir)
	hw.cmd.Stderr = &hw.stderr
	var err error
	if hw.stdin, err = hw.cmd.StdinPipe(); err != nil {
		return nil, err
	}
	if err := hw.cmd.Start(); err != nil {
		return nil, err
	}
	hw.tw = tar.NewWriter(hw.stdin)
	return hw, nil
}

func (hw *homeWriter) add(h *tar.Header, r io.Reader) error {
	rel := filepath.ToSlash(filepath.Clean(strings.TrimPrefix(h.Name, "home/")))
	if rel == "." || strings.HasPrefix(rel, "../") || strings.HasPrefix(rel, "/") {
		return nil
	}
	switch h.Typeflag {
	case tar.TypeReg, tar.TypeDir, tar.TypeSymlink:
	default:
		return nil // no devices, fifos or hard links
	}
	h.Name = "./" + rel
	if h.Typeflag == tar.TypeDir {
		h.Name += "/"
	}
	h.Uname, h.Gname, h.Uid, h.Gid = "", "", 0, 0
	if err := hw.tw.WriteHeader(h); err != nil {
		return err
	}
	_, err := io.Copy(hw.tw, r)
	return err
}

func (hw *homeWriter) finish() error {
	writeErr := hw.tw.Close()
	closeErr := hw.stdin.Close()
	err := hw.cmd.Wait()
	if err != nil {
		return fmt.Errorf("tar: %v: %s", err, strings.TrimSpace(hw.stderr.String()))
	}
	return errors.Join(writeErr, closeErr)
}

func (hw *homeWriter) abort() {
	hw.stdin.Close()
	_ = hw.cmd.Process.Kill()
	_ = hw.cmd.Wait()
}

// definerRe matches DEFINER clauses of mysqldump output; they are dropped so
// objects belong to the importing session instead of naming another account.
var definerRe = regexp.MustCompile("DEFINER=`[^`]*`@`[^`]*`")

func importDB(ctx context.Context, u *user.User, d agentproto.BackupDB, r io.Reader) (result error) {
	login, password, err := restoreCredentials()
	if err != nil {
		return err
	}
	cleanup := func() {
		cleanCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		result = errors.Join(result, restoreGrant(cleanCtx, d, login, password, true))
	}
	defer cleanup()
	if err := restoreGrant(ctx, d, login, password, false); err != nil {
		return err
	}
	var cmd *exec.Cmd
	var stdin io.Reader = r
	if d.Engine == "postgresql" {
		if d.User == "" {
			return errors.New("missing database owner")
		}
		cmd = exec.CommandContext(ctx, "runuser", "-u", u.Username, "--", "psql", "-h", "127.0.0.1", "-p", strconv.Itoa(apps.PGPort),
			"-U", login, "-X", "-q", "-v", "ON_ERROR_STOP=1", "-d", d.Name)
		stdin = io.MultiReader(strings.NewReader(`SET ROLE "`+d.User+`";`+"\n"), r)
		cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "HOME=" + u.HomeDir, "PGPASSWORD=" + password, "LANG=C.UTF-8"}
	} else {
		cmd = exec.CommandContext(ctx, "runuser", "-u", u.Username, "--", "mariadb", "--no-defaults", "--protocol=TCP", "--host=127.0.0.1", "--user="+login, "--batch", "--binary-mode", "--local-infile=0", "--database="+d.Name)
		cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "HOME=" + u.HomeDir, "MYSQL_PWD=" + password, "LANG=C.UTF-8"}
		pr, pw := io.Pipe()
		go func() {
			sc := bufio.NewReaderSize(r, 1<<20)
			for {
				line, err := sc.ReadString('\n')
				if len(line) > 0 {
					if _, werr := io.WriteString(pw, definerRe.ReplaceAllString(line, "")); werr != nil {
						pw.CloseWithError(werr)
						return
					}
				}
				if err != nil {
					if err == io.EOF {
						err = nil
					}
					pw.CloseWithError(err)
					return
				}
			}
		}()
		stdin = pr
		defer pr.Close() // unblock the filtering goroutine if the client exits early
	}
	var stderr bytes.Buffer
	cmd.Stdin, cmd.Stderr = stdin, &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// restoreCredentials is intentionally unrelated to any persistent database password.
func restoreCredentials() (string, string, error) {
	var data [40]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", "", err
	}
	return "xp_restore_" + hex.EncodeToString(data[:8]), hex.EncodeToString(data[8:]), nil
}

func restoreGrant(ctx context.Context, d agentproto.BackupDB, login, password string, remove bool) error {
	if d.User == "" {
		return errors.New("missing database owner")
	}
	var sql string
	if d.Engine == "postgresql" {
		if remove {
			sql = `ALTER ROLE "` + login + `" NOLOGIN PASSWORD NULL; SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE usename='` + login + `' AND pid<>pg_backend_pid(); REASSIGN OWNED BY "` + login + `" TO "` + d.User + `"; DROP OWNED BY "` + login + `"; DROP ROLE "` + login + `";`
		} else {
			sql = `CREATE ROLE "` + login + `" LOGIN PASSWORD '` + password + `' VALID UNTIL '` + time.Now().UTC().Add(2*time.Hour).Format(time.RFC3339) + `' NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION; GRANT "` + d.User + `" TO "` + login + `";`
		}
		_, err := run.With(ctx, run.Opts{Stdin: sql}, "runuser", "-u", "postgres", "--", "psql", "-p", strconv.Itoa(apps.PGPort), "-X", "-q", "-v", "ON_ERROR_STOP=1", "-d", d.Name)
		return err
	}
	for _, host := range []string{"localhost", "127.0.0.1"} {
		account := "'" + login + "'@'" + host + "'"
		if remove {
			sql += "DROP USER IF EXISTS " + account + ";"
		} else {
			sql += "CREATE USER " + account + " IDENTIFIED BY '" + password + "'; GRANT ALL PRIVILEGES ON `" + strings.ReplaceAll(d.Name, "_", `\_`) + "`.* TO " + account + ";"
		}
	}
	_, err := run.With(ctx, run.Opts{Stdin: sql}, "mariadb", "--batch")
	return err
}

func writeMail(domain, rel string, h *tar.Header, r io.Reader) error {
	base := filepath.Join(mailRoot, domain)
	info, err := os.Lstat(base)
	if errors.Is(err, os.ErrNotExist) {
		// A domain that has received no mail yet (e.g. just recreated on
		// a new server); ownership is fixed to vmail after the restore.
		if err := os.Mkdir(base, 0o700); err != nil {
			return err
		}
		info, err = os.Lstat(base)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("invalid mail domain directory")
	}
	return writeMailAt(base, rel, h, r)
}

// os.Root confines writes even if a mailbox path is replaced with a symlink.
func writeMailAt(base, rel string, h *tar.Header, r io.Reader) error {
	root, err := os.OpenRoot(base)
	if err != nil {
		return err
	}
	defer root.Close()
	rel = filepath.FromSlash(rel)
	switch h.Typeflag {
	case tar.TypeDir:
		return root.MkdirAll(rel, 0o700)
	case tar.TypeReg:
		if err := root.MkdirAll(filepath.Dir(rel), 0o700); err != nil {
			return err
		}
		f, err := root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(f, r)
		return errors.Join(copyErr, f.Close())
	}
	return nil
}

// ReadManifest downloads a backup only as far as its manifest (the first
// entry) and returns it, so the panel can recreate the account's sites,
// databases and mailboxes before restoring their data.
func ReadManifest(ctx context.Context, p agentproto.BackupManifestParams) (agentproto.BackupManifest, error) {
	if _, err := account(p.Username); err != nil {
		return agentproto.BackupManifest{}, err
	}
	body, err := gdrive.New(p.Drive).Download(ctx, p.FileID)
	if err != nil {
		return agentproto.BackupManifest{}, err
	}
	defer body.Close()
	dec, err := NewDecrypter(bufio.NewReaderSize(body, 1<<16), p.Passphrase)
	if err != nil {
		return agentproto.BackupManifest{}, err
	}
	gz, err := gzip.NewReader(dec)
	if err != nil {
		return agentproto.BackupManifest{}, ErrBadBackup
	}
	tr := tar.NewReader(gz)
	h, err := tr.Next()
	if err != nil || h.Name != "manifest.json" || h.Typeflag != tar.TypeReg || h.Size > 16<<20 {
		return agentproto.BackupManifest{}, ErrBadBackup
	}
	b, err := io.ReadAll(io.LimitReader(tr, 16<<20))
	if err != nil {
		return agentproto.BackupManifest{}, err
	}
	var m struct {
		Username string `json:"username"`
	}
	if json.Unmarshal(b, &m) != nil {
		return agentproto.BackupManifest{}, ErrBadBackup
	}
	if m.Username != p.Username {
		return agentproto.BackupManifest{}, fmt.Errorf("backup này thuộc tài khoản %q, không phải %q", m.Username, p.Username)
	}
	return agentproto.BackupManifest{Manifest: b}, nil
}
