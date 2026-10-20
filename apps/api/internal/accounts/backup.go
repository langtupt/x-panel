package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"xpanel/apps/api/internal/apperr"
	"xpanel/apps/api/internal/mailrepo"
	"xpanel/apps/api/internal/tasks"
	"xpanel/apps/api/internal/users"
	"xpanel/pkg/agentproto"
	"xpanel/pkg/gdrive"
)

const (
	TaskBackupCreate  = "backup.create"
	TaskBackupRestore = "backup.restore"
)

// Backup is a stored backup visible to the actor.
type Backup struct {
	ID        int64          `json:"id"`
	UserID    *int64         `json:"user_id"`
	Username  string         `json:"username"`
	FileName  string         `json:"file_name"`
	SizeBytes int64          `json:"size_bytes"`
	Kind      string         `json:"kind"`
	Contents  BackupContents `json:"contents"`
	CreatedAt time.Time      `json:"created_at"`
	fileID    string
}

type BackupContents struct {
	Databases   []agentproto.BackupDB `json:"databases"`
	MailDomains []string              `json:"mail_domains"`
}

const backupCols = `SELECT b.id, b.user_id, b.username, b.file_name, b.size_bytes, b.kind, b.contents, b.created_at, b.file_id
	FROM backups b LEFT JOIN users u ON u.id=b.user_id`

func scanBackups(rows pgx.Rows, err error) ([]Backup, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Backup{}
	for rows.Next() {
		var b Backup
		if err := rows.Scan(&b.ID, &b.UserID, &b.Username, &b.FileName, &b.SizeBytes, &b.Kind, &b.Contents, &b.CreatedAt, &b.fileID); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Service) ListBackups(ctx context.Context, actor *users.User) ([]Backup, error) {
	switch actor.Role {
	case users.RoleAdmin:
		return scanBackups(s.pool.Query(ctx, backupCols+` ORDER BY b.created_at DESC`))
	case users.RoleReseller:
		return scanBackups(s.pool.Query(ctx, backupCols+` WHERE u.parent_id=$1 ORDER BY b.created_at DESC`, actor.ID))
	}
	return scanBackups(s.pool.Query(ctx, backupCols+` WHERE b.user_id=$1 ORDER BY b.created_at DESC`, actor.ID))
}

// backupTarget returns a hosting account the actor may back up or restore.
func (s *Service) backupTarget(ctx context.Context, actor *users.User, id int64) (*users.User, error) {
	u, err := s.users.ByID(ctx, id)
	if err != nil || u.Role != users.RoleUser || (u.ID != actor.ID && !users.CanManage(actor, u)) {
		return nil, apperr.NotFound("không tìm thấy tài khoản")
	}
	if u.LinuxUID == nil {
		return nil, apperr.Conflict("tài khoản chưa có user Linux")
	}
	return u, nil
}

func (s *Service) accessibleBackup(ctx context.Context, actor *users.User, id int64) (*Backup, error) {
	list, err := scanBackups(s.pool.Query(ctx, backupCols+` WHERE b.id=$1`, id))
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, apperr.NotFound("không tìm thấy bản backup")
	}
	b := &list[0]
	if actor.Role == users.RoleAdmin {
		return b, nil
	}
	if b.UserID == nil {
		return nil, apperr.NotFound("không tìm thấy bản backup")
	}
	if _, err := s.backupTarget(ctx, actor, *b.UserID); err != nil {
		return nil, apperr.NotFound("không tìm thấy bản backup")
	}
	return b, nil
}

type backupJob struct {
	UserID int64  `json:"user_id"`
	Kind   string `json:"kind"`
}

func (s *Service) CreateBackup(ctx context.Context, actor *users.User, userID int64) (*tasks.Task, error) {
	u, err := s.backupTarget(ctx, actor, userID)
	if err != nil {
		return nil, err
	}
	_, cfg, err := s.driveCreds(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	if cfg.Passphrase == "" {
		return nil, apperr.Conflict("tài khoản chưa đặt passphrase mã hóa backup")
	}
	return s.enqueueBackup(ctx, &actor.ID, u, "manual")
}

func (s *Service) enqueueBackup(ctx context.Context, by *int64, u *users.User, kind string) (*tasks.Task, error) {
	if open, err := s.tasks.HasOpen(ctx, TaskBackupCreate, u.Username); err != nil {
		return nil, err
	} else if open {
		return nil, apperr.Conflict("đang backup tài khoản %s", u.Username)
	}
	t, err := s.tasks.Enqueue(ctx, s.pool, by, TaskBackupCreate, u.Username, backupJob{UserID: u.ID, Kind: kind})
	if err != nil {
		return nil, err
	}
	s.tasks.Notify()
	return t, nil
}

// accountManifest records the account's panel objects inside the backup.
func (s *Service) accountManifest(ctx context.Context, u *users.User) (json.RawMessage, BackupContents, error) {
	var c BackupContents
	sitesList, err := s.sites.ByUser(ctx, u.ID)
	if err != nil {
		return nil, c, err
	}
	dbList, err := s.dbs.ByUser(ctx, u.ID)
	if err != nil {
		return nil, c, err
	}
	for _, d := range dbList {
		c.Databases = append(c.Databases, agentproto.BackupDB{Name: d.Name, Engine: d.Engine, User: d.DBUser})
	}
	mr := mailrepo.NewRepo(s.pool)
	domains, err := mr.DomainsByUser(ctx, u.ID)
	if err != nil {
		return nil, c, err
	}
	type mailDomain struct {
		Domain    string             `json:"domain"`
		Mailboxes []mailrepo.Mailbox `json:"mailboxes"`
		Aliases   []mailrepo.Alias   `json:"aliases"`
	}
	var mail []mailDomain
	for _, d := range domains {
		c.MailDomains = append(c.MailDomains, d.Domain)
		boxes, err := mr.Mailboxes(ctx, d.ID)
		if err != nil {
			return nil, c, err
		}
		aliases, err := mr.Aliases(ctx, d.ID)
		if err != nil {
			return nil, c, err
		}
		mail = append(mail, mailDomain{Domain: d.Domain, Mailboxes: boxes, Aliases: aliases})
	}
	cron, err := s.cron.ByUser(ctx, u.ID)
	if err != nil {
		return nil, c, err
	}
	ftp, err := s.ftp.ByUser(ctx, u.ID)
	if err != nil {
		return nil, c, err
	}
	b, err := json.Marshal(map[string]any{
		"format": 1, "username": u.Username, "created_at": time.Now().UTC(),
		"sites": sitesList, "databases": dbList, "mail": mail, "cron": cron, "ftp": ftp,
	})
	return b, c, err
}

func (s *Service) runBackupCreate(ctx context.Context, t *tasks.Task, logf func(string, ...any)) error {
	var job backupJob
	if err := json.Unmarshal(t.Payload, &job); err != nil {
		return err
	}
	u, err := s.users.ByID(ctx, job.UserID)
	if err != nil {
		return err
	}
	creds, cfg, err := s.driveCreds(ctx, u.ID)
	if err != nil {
		return err
	}
	pass, err := s.backupPassphrase(cfg)
	if err != nil {
		return err
	}
	manifest, contents, err := s.accountManifest(ctx, u)
	if err != nil {
		return err
	}
	name := fmt.Sprintf("%s-%s.xpb", u.Username, time.Now().Format("20060102-150405"))
	logf("Backup tài khoản %s: home, %d database, %d tên miền email", u.Username, len(contents.Databases), len(contents.MailDomains))
	callCtx, cancel := context.WithTimeout(ctx, 12*time.Hour)
	defer cancel()
	var res agentproto.BackupCreateResult
	err = s.agent.Call(callCtx, agentproto.ActionBackupCreate, agentproto.BackupCreateParams{
		Username: u.Username, Databases: contents.Databases, MailDomains: contents.MailDomains,
		Drive: creds, FolderID: cfg.Folder, FileName: name, Passphrase: pass, Manifest: manifest,
	}, &res)
	if res.Log != "" {
		logf("%s", strings.TrimSpace(res.Log))
	}
	if err != nil {
		return err
	}
	cj, _ := json.Marshal(contents)
	if _, err := s.pool.Exec(ctx, `INSERT INTO backups (user_id, username, file_id, file_name, size_bytes, kind, contents, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, u.ID, u.Username, res.FileID, name, res.Size, job.Kind, cj, t.UserID); err != nil {
		return err
	}
	return s.pruneBackups(ctx, u, logf)
}

// pruneBackups keeps the newest N backups of an account.
func (s *Service) pruneBackups(ctx context.Context, u *users.User, logf func(string, ...any)) error {
	creds, cfg, err := s.driveCreds(ctx, u.ID)
	if err != nil {
		return err
	}
	keep := cfg.Keep
	old, err := scanBackups(s.pool.Query(ctx, backupCols+` WHERE b.user_id=$1 ORDER BY b.created_at DESC OFFSET $2`, u.ID, keep))
	if err != nil || len(old) == 0 {
		return err
	}
	c := gdrive.New(creds)
	for _, b := range old {
		if err := c.Delete(ctx, b.fileID); err != nil {
			logf("Không xóa được bản cũ %s: %v", b.FileName, err)
			continue
		}
		if _, err := s.pool.Exec(ctx, `DELETE FROM backups WHERE id=$1`, b.ID); err != nil {
			return err
		}
		logf("Đã xóa bản cũ %s (giữ %d bản)", b.FileName, keep)
	}
	return nil
}

func (s *Service) DeleteBackup(ctx context.Context, actor *users.User, id int64) (*Backup, error) {
	b, err := s.accessibleBackup(ctx, actor, id)
	if err != nil {
		return nil, err
	}
	// The file lives in the account's own Drive; a deleted account's Drive
	// is no longer reachable, so only the record goes.
	if b.UserID != nil {
		creds, _, err := s.driveCreds(ctx, *b.UserID)
		if err != nil {
			return nil, err
		}
		if err := gdrive.New(creds).Delete(ctx, b.fileID); err != nil {
			return nil, apperr.Agent(err)
		}
	}
	_, err = s.pool.Exec(ctx, `DELETE FROM backups WHERE id=$1`, id)
	return b, err
}

type RestoreInput struct {
	Files      bool   `json:"files"`
	Databases  bool   `json:"databases"`
	Mail       bool   `json:"mail"`
	Passphrase string `json:"passphrase"` // for backups made before the passphrase was changed
}

type restoreJob struct {
	BackupID   int64  `json:"backup_id"`
	Files      bool   `json:"files"`
	Databases  bool   `json:"databases"`
	Mail       bool   `json:"mail"`
	Passphrase string `json:"passphrase,omitempty"` // sealed
}

func (s *Service) RestoreBackup(ctx context.Context, actor *users.User, id int64, in RestoreInput) (*tasks.Task, error) {
	b, err := s.accessibleBackup(ctx, actor, id)
	if err != nil {
		return nil, err
	}
	if !in.Files && !in.Databases && !in.Mail {
		return nil, apperr.Invalid("chọn ít nhất một phần để khôi phục")
	}
	// The account (same username) must exist now; for a deleted account the
	// administrator recreates it first.
	u, err := s.users.ByUsername(ctx, b.Username)
	if err != nil || u.Role != users.RoleUser || u.LinuxUID == nil {
		return nil, apperr.Conflict("tài khoản %s không còn tồn tại: tạo lại tài khoản cùng tên rồi khôi phục", b.Username)
	}
	if actor.Role != users.RoleAdmin {
		if _, err := s.backupTarget(ctx, actor, u.ID); err != nil {
			return nil, err
		}
	}
	job := restoreJob{BackupID: b.ID, Files: in.Files, Databases: in.Databases, Mail: in.Mail}
	if in.Passphrase != "" {
		if job.Passphrase, err = s.secrets.Seal(in.Passphrase); err != nil {
			return nil, err
		}
	}
	if open, err := s.tasks.HasOpen(ctx, TaskBackupRestore, b.Username); err != nil {
		return nil, err
	} else if open {
		return nil, apperr.Conflict("đang khôi phục tài khoản %s", b.Username)
	}
	t, err := s.tasks.Enqueue(ctx, s.pool, &actor.ID, TaskBackupRestore, b.Username, job)
	if err != nil {
		return nil, err
	}
	s.tasks.Notify()
	return t, nil
}

func (s *Service) runBackupRestore(ctx context.Context, t *tasks.Task, logf func(string, ...any)) error {
	var job restoreJob
	if err := json.Unmarshal(t.Payload, &job); err != nil {
		return err
	}
	list, err := scanBackups(s.pool.Query(ctx, backupCols+` WHERE b.id=$1`, job.BackupID))
	if err != nil || len(list) == 0 {
		return errors.New("bản backup không còn tồn tại")
	}
	b := list[0]
	u, err := s.users.ByUsername(ctx, b.Username)
	if err != nil || u.Role != users.RoleUser {
		return errors.New("tài khoản không còn tồn tại")
	}
	creds, cfg, err := s.driveCreds(ctx, u.ID)
	if err != nil {
		return err
	}
	var pass string
	if job.Passphrase != "" {
		pass, err = s.secrets.Open(job.Passphrase)
	} else {
		pass, err = s.backupPassphrase(cfg)
	}
	if err != nil {
		return err
	}
	p := agentproto.BackupRestoreParams{Username: u.Username, Drive: creds, FileID: b.fileID, Passphrase: pass, Files: job.Files}
	// Only databases and mail domains the account owns now are written.
	if job.Databases {
		current, err := s.dbs.ByUser(ctx, u.ID)
		if err != nil {
			return err
		}
		for _, d := range current {
			p.Databases = append(p.Databases, agentproto.BackupDB{Name: d.Name, Engine: d.Engine, User: d.DBUser})
		}
		if len(p.Databases) == 0 {
			logf("Tài khoản chưa có database nào: tạo database cùng tên trước để khôi phục dữ liệu")
		}
	}
	if job.Mail {
		domains, err := mailrepo.NewRepo(s.pool).DomainsByUser(ctx, u.ID)
		if err != nil {
			return err
		}
		for _, d := range domains {
			p.MailDomains = append(p.MailDomains, d.Domain)
		}
	}
	logf("Khôi phục %s từ %s", u.Username, b.FileName)
	callCtx, cancel := context.WithTimeout(ctx, 12*time.Hour)
	defer cancel()
	var res agentproto.TaskLog
	err = s.agent.Call(callCtx, agentproto.ActionBackupRestore, p, &res)
	if res.Log != "" {
		logf("%s", strings.TrimSpace(res.Log))
	}
	return err
}
