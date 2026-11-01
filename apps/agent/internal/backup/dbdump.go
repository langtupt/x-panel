package backup

// Per-database dumps: one database is exported to, or imported from, a file
// in the account's own ~/backups/databases. The dump tool matches the engine
// (mysqldump for MariaDB, pg_dump for PostgreSQL). Files are written and read
// by the account itself (runuser), never by root, so a symlink planted in the
// home cannot redirect them; the import goes through importDB, which uses a
// temporary login limited to that one database.

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"xpanel/pkg/agentproto"
	"xpanel/pkg/validate"
)

// DBDumpDir is relative to the account's home.
const DBDumpDir = "backups/databases"

// Database names never contain "-", so "<db>-…" belongs to exactly one database.
var dumpFileRe = regexp.MustCompile(`^[a-z0-9_]+-[A-Za-z0-9._-]{1,120}\.sql(\.gz)?$`)

func dumpTarget(p agentproto.DBDumpParams) (*user.User, error) {
	u, err := account(p.Username)
	if err != nil {
		return nil, err
	}
	if p.DB.Engine != "mariadb" && p.DB.Engine != "postgresql" {
		return nil, fmt.Errorf("unknown database engine %q", p.DB.Engine)
	}
	if err := validate.DBName(p.Username, p.DB.Name); err != nil {
		return nil, err
	}
	if err := validate.DBName(p.Username, p.DB.User); err != nil {
		return nil, err
	}
	return u, nil
}

func dumpFile(p agentproto.DBDumpParams) error {
	if !dumpFileRe.MatchString(p.File) || !strings.HasPrefix(p.File, p.DB.Name+"-") {
		return fmt.Errorf("tên file sao lưu không hợp lệ: %q", p.File)
	}
	return nil
}

// asUser runs a small sh script as the account; extra args become $1, $2…
func asUser(ctx context.Context, u *user.User, script string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "runuser", append([]string{"-u", u.Username, "--", "/bin/sh", "-c", script, "sh"}, args...)...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + u.HomeDir, "LANG=C.UTF-8"}
	cmd.Dir = "/"
	return cmd
}

func runAsUser(ctx context.Context, u *user.User, script string, args ...string) (string, error) {
	cmd := asUser(ctx, u, script, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

type countWriter struct {
	w io.Writer
	n int64
}

func (c *countWriter) Write(b []byte) (int, error) {
	n, err := c.w.Write(b)
	c.n += int64(n)
	return n, err
}

// CreateDBDump writes "<db>-<time><suffix>.sql.gz" into ~/backups/databases.
func CreateDBDump(ctx context.Context, p agentproto.DBDumpParams) (agentproto.DBDumpFile, error) {
	u, err := dumpTarget(p)
	if err != nil {
		return agentproto.DBDumpFile{}, err
	}
	return createDump(ctx, u, p.DB, "")
}

func createDump(ctx context.Context, u *user.User, d agentproto.BackupDB, suffix string) (agentproto.DBDumpFile, error) {
	dir := filepath.Join(u.HomeDir, DBDumpDir)
	now := time.Now()
	name := d.Name + "-" + now.Format("20060102-150405") + suffix + ".sql.gz"
	part := "." + name + ".part"

	dump := dumpCommand(ctx, d)
	var dumpErr bytes.Buffer
	dump.Stderr = &dumpErr
	out, err := dump.StdoutPipe()
	if err != nil {
		return agentproto.DBDumpFile{}, err
	}
	writer := asUser(ctx, u, `umask 077 && mkdir -p -- "$1" && cat > "$1/$2"`, dir, part)
	pr, pw := io.Pipe()
	var writeErr bytes.Buffer
	writer.Stdin, writer.Stderr = pr, &writeErr
	cleanCtx := context.WithoutCancel(ctx)
	if err := writer.Start(); err != nil {
		return agentproto.DBDumpFile{}, err
	}
	// If the writer dies (e.g. the account's disk quota is full) the pipe is
	// closed so the copy below fails instead of blocking forever.
	writeDone := make(chan error, 1)
	go func() {
		err := writer.Wait()
		pr.CloseWithError(errors.Join(errors.New("ghi file dừng"), err))
		writeDone <- err
	}()
	var copyErr error
	counter := &countWriter{w: pw}
	if err := dump.Start(); err != nil {
		copyErr = err
	} else {
		gz := gzip.NewWriter(counter)
		_, copyErr = io.Copy(gz, out)
		copyErr = errors.Join(copyErr, gz.Close())
		if copyErr != nil {
			_ = dump.Process.Kill() // it may be blocked writing to us
		}
		if err := dump.Wait(); err != nil {
			copyErr = errors.Join(copyErr, fmt.Errorf("%v: %s", err, strings.TrimSpace(dumpErr.String())))
		}
	}
	pw.Close()
	if err := <-writeDone; err != nil {
		copyErr = errors.Join(copyErr, fmt.Errorf("ghi file: %v: %s", err, strings.TrimSpace(writeErr.String())))
	}
	if copyErr != nil {
		_, _ = runAsUser(cleanCtx, u, `rm -f -- "$1/$2"`, dir, part)
		return agentproto.DBDumpFile{}, copyErr
	}
	if _, err := runAsUser(cleanCtx, u, `mv -f -- "$1/$2" "$1/$3"`, dir, part, name); err != nil {
		return agentproto.DBDumpFile{}, err
	}
	return agentproto.DBDumpFile{Name: name, Path: "/" + DBDumpDir + "/" + name, Size: counter.n,
		Modified: now.UTC().Format(time.RFC3339)}, nil
}

// ListDBDumps lists the database's dump files, newest first.
func ListDBDumps(ctx context.Context, p agentproto.DBDumpParams) (agentproto.DBDumpList, error) {
	res := agentproto.DBDumpList{Dir: "/" + DBDumpDir, Files: []agentproto.DBDumpFile{}}
	u, err := dumpTarget(p)
	if err != nil {
		return res, err
	}
	out, err := runAsUser(ctx, u, `umask 077; mkdir -p -- "$1" && cd -- "$1" && find . -maxdepth 1 -type f -printf '%f\t%s\t%T@\n'`,
		filepath.Join(u.HomeDir, DBDumpDir))
	if err != nil {
		return res, err
	}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 3 || dumpFile(agentproto.DBDumpParams{DB: p.DB, File: f[0]}) != nil {
			continue
		}
		size, _ := strconv.ParseInt(f[1], 10, 64)
		sec, _ := strconv.ParseFloat(f[2], 64)
		res.Files = append(res.Files, agentproto.DBDumpFile{Name: f[0], Path: res.Dir + "/" + f[0], Size: size,
			Modified: time.Unix(int64(sec), 0).UTC().Format(time.RFC3339)})
	}
	sort.Slice(res.Files, func(i, j int) bool { return res.Files[i].Modified > res.Files[j].Modified })
	return res, nil
}

// RestoreDBDump imports a dump file into its database. The current content
// is dumped first ("-pre-restore"), so a wrong file can be undone.
func RestoreDBDump(ctx context.Context, p agentproto.DBDumpParams) (agentproto.TaskLog, error) {
	var log strings.Builder
	u, err := dumpTarget(p)
	if err != nil {
		return agentproto.TaskLog{}, err
	}
	if err := dumpFile(p); err != nil {
		return agentproto.TaskLog{}, err
	}
	before, err := createDump(ctx, u, p.DB, "-pre-restore")
	if err != nil {
		return agentproto.TaskLog{}, fmt.Errorf("không sao lưu được dữ liệu hiện tại trước khi khôi phục: %w", err)
	}
	fmt.Fprintf(&log, "Đã sao lưu dữ liệu hiện tại vào ~%s (%d byte)\n", before.Path, before.Size)

	reader := asUser(ctx, u, `cat -- "$1/$2"`, filepath.Join(u.HomeDir, DBDumpDir), p.File)
	var readErr bytes.Buffer
	reader.Stderr = &readErr
	out, err := reader.StdoutPipe()
	if err != nil {
		return agentproto.TaskLog{Log: log.String()}, err
	}
	if err := reader.Start(); err != nil {
		return agentproto.TaskLog{Log: log.String()}, err
	}
	var src io.Reader = bufio.NewReaderSize(out, 1<<16)
	if strings.HasSuffix(p.File, ".gz") {
		gz, err := gzip.NewReader(src)
		if err != nil {
			_ = reader.Process.Kill()
			_ = reader.Wait()
			return agentproto.TaskLog{Log: log.String()}, fmt.Errorf("file .gz không hợp lệ: %w", err)
		}
		src = gz
	}
	fmt.Fprintf(&log, "Nhập %s vào %s (%s)…\n", p.File, p.DB.Name, p.DB.Engine)
	importErr := importDB(ctx, u, p.DB, src)
	if importErr != nil {
		_ = reader.Process.Kill()
	} else {
		_, _ = io.Copy(io.Discard, out)
	}
	if err := reader.Wait(); err != nil && importErr == nil {
		importErr = fmt.Errorf("đọc file: %v: %s", err, strings.TrimSpace(readErr.String()))
	}
	if importErr != nil {
		fmt.Fprintf(&log, "Khôi phục thất bại. Dữ liệu trước đó vẫn còn trong %s\n", before.Name)
		return agentproto.TaskLog{Log: log.String()}, importErr
	}
	log.WriteString("Đã khôi phục xong.\n")
	return agentproto.TaskLog{Log: log.String()}, nil
}

// DeleteDBDump removes one dump file.
func DeleteDBDump(ctx context.Context, p agentproto.DBDumpParams) error {
	u, err := dumpTarget(p)
	if err != nil {
		return err
	}
	if err := dumpFile(p); err != nil {
		return err
	}
	_, err = runAsUser(ctx, u, `rm -f -- "$1/$2"`, filepath.Join(u.HomeDir, DBDumpDir), p.File)
	return err
}
