package accounts

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"xpanel/apps/api/internal/apperr"
	"xpanel/apps/api/internal/mailrepo"
	"xpanel/apps/api/internal/users"
	"xpanel/pkg/agentproto"
	"xpanel/pkg/gdrive"
)

// Moving an account to another server: create the account with the same
// username there, connect the same Google OAuth client, import the backup
// list from Drive and restore with "recreate", which rebuilds the sites,
// databases, mailboxes, cron jobs and FTP accounts from the backup's
// manifest before restoring their data.

var backupNameRe = regexp.MustCompile(`^([a-z][a-z0-9_]{1,31})-(\d{8}-\d{6})\.xpb$`)

// ImportBackups adds the account's backups found in its Drive folder that
// this server does not know yet (made on another server, or before a
// reinstall). It returns how many were added.
func (s *Service) ImportBackups(ctx context.Context, actor *users.User) (int, error) {
	if err := backupOwner(actor); err != nil {
		return 0, err
	}
	creds, cfg, err := s.driveCreds(ctx, actor.ID)
	if err != nil {
		return 0, err
	}
	if cfg.Folder == "" {
		return 0, apperr.Conflict("chưa kết nối Google Drive")
	}
	files, err := gdrive.New(creds).List(ctx, cfg.Folder)
	if err != nil {
		return 0, apperr.Agent(err)
	}
	added := 0
	for _, f := range files {
		m := backupNameRe.FindStringSubmatch(f.Name)
		if m == nil || m[1] != actor.Username {
			continue
		}
		at, err := time.ParseInLocation("20060102-150405", m[2], time.Local)
		if err != nil {
			continue
		}
		tag, err := s.pool.Exec(ctx, `INSERT INTO backups (user_id, username, file_id, file_name, size_bytes, kind, contents, created_by, created_at)
			SELECT $1, $2, $3, $4, $5, 'imported', '{}', $1, $6
			WHERE NOT EXISTS (SELECT 1 FROM backups WHERE file_id = $3)`,
			actor.ID, actor.Username, f.ID, f.Name, f.SizeBytes(), at)
		if err != nil {
			return added, err
		}
		added += int(tag.RowsAffected())
	}
	return added, nil
}

// backupManifest is what accountManifest writes (format 1).
type backupManifest struct {
	Username string `json:"username"`
	Sites    []struct {
		Domain     string            `json:"domain"`
		Aliases    []string          `json:"aliases"`
		AppType    string            `json:"app_type"`
		Runtime    string            `json:"runtime"`
		Env        map[string]string `json:"env"`
		PHPVersion string            `json:"php_version"`
		DocRoot    string            `json:"doc_root"`
	} `json:"sites"`
	Databases []struct {
		Name   string `json:"name"`
		Engine string `json:"engine"`
	} `json:"databases"`
	Mail []struct {
		Domain    string `json:"domain"`
		Mailboxes []struct {
			LocalPart string `json:"local_part"`
			QuotaMB   int    `json:"quota_mb"`
		} `json:"mailboxes"`
		Aliases []struct {
			LocalPart    string   `json:"local_part"`
			Destinations []string `json:"destinations"`
		} `json:"aliases"`
		PasswordHashes map[string]string `json:"password_hashes"`
	} `json:"mail"`
	Cron []struct {
		Schedule string `json:"schedule"`
		Command  string `json:"command"`
		Enabled  bool   `json:"enabled"`
	} `json:"cron"`
	FTP []struct {
		Login string `json:"login"`
		Dir   string `json:"dir"`
	} `json:"ftp"`
}

// Only bcrypt hashes as hashMailPassword makes them reach the Dovecot
// passwd-file; the manifest is data the account itself could have crafted.
var mailHashRe = regexp.MustCompile(`^\$2[aby]\$[0-9]{2}\$[./A-Za-z0-9]{53}$`)

func randomSecret() string {
	b := make([]byte, 15)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// recreateFromManifest creates whatever the backup lists that the account
// does not have yet. It acts as the account itself, so package limits and
// every usual check apply; failures are logged and skipped.
func (s *Service) recreateFromManifest(ctx context.Context, u *users.User, raw json.RawMessage, logf func(string, ...any)) error {
	var m backupManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return errors.New("manifest của backup không đọc được")
	}
	if m.Username != u.Username {
		return fmt.Errorf("backup này thuộc tài khoản %q", m.Username)
	}
	skip := func(what string, err error) { logf("  bỏ qua %s: %v", what, err) }
	logf("Tạo lại cấu hình từ backup")

	for _, st := range m.Sites {
		var exists bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM sites WHERE domain = $1)`, st.Domain).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue // here already, or another account's: never taken over
		}
		in := SiteInput{Domain: st.Domain, Aliases: st.Aliases, AppType: st.AppType, Runtime: st.Runtime,
			Env: st.Env, PHPVersion: st.PHPVersion, DocRoot: st.DocRoot}
		if _, err := s.CreateSite(ctx, u, in); err != nil {
			skip("website "+st.Domain, err)
			continue
		}
		logf("  website %s", st.Domain)
	}

	current, err := s.dbs.ByUser(ctx, u.ID)
	if err != nil {
		return err
	}
	haveDB := map[string]bool{}
	for _, d := range current {
		haveDB[d.Name] = true
	}
	for _, d := range m.Databases {
		suffix, ok := strings.CutPrefix(d.Name, u.Username+"_")
		if !ok || haveDB[d.Name] {
			continue
		}
		// The old password comes back with the data (db-auth.json).
		if _, err := s.CreateDatabase(ctx, u, DatabaseInput{Engine: d.Engine, Name: suffix, Password: randomSecret() + "Aa1"}); err != nil {
			skip("database "+d.Name, err)
			continue
		}
		logf("  database %s", d.Name)
	}

	mr := mailrepo.NewRepo(s.pool)
	resetPw := 0
	for _, md := range m.Mail {
		var domainID int64
		domains, err := mr.DomainsByUser(ctx, u.ID)
		if err != nil {
			return err
		}
		for _, d := range domains {
			if d.Domain == md.Domain {
				domainID = d.ID
			}
		}
		if domainID == 0 {
			d, err := s.CreateMailDomain(ctx, u, MailDomainInput{Domain: md.Domain})
			if err != nil {
				skip("tên miền email "+md.Domain, err)
				continue
			}
			domainID = d.ID
			logf("  tên miền email %s (DKIM mới: cập nhật lại bản ghi DNS)", md.Domain)
		}
		for _, mb := range md.Mailboxes {
			box, err := s.CreateMailbox(ctx, u, domainID, MailboxInput{LocalPart: mb.LocalPart, Password: randomSecret(), QuotaMB: mb.QuotaMB})
			if err != nil {
				if !strings.Contains(err.Error(), "đã tồn tại") {
					skip("hộp thư "+mb.LocalPart+"@"+md.Domain, err)
				}
				continue
			}
			if h := md.PasswordHashes[mb.LocalPart]; mailHashRe.MatchString(h) {
				if _, err := s.pool.Exec(ctx, `UPDATE mailboxes SET password_hash = $2 WHERE id = $1`, box.ID, h); err != nil {
					return err
				}
			} else {
				resetPw++
			}
			logf("  hộp thư %s", box.Address)
		}
		for _, a := range md.Aliases {
			if _, err := s.CreateAlias(ctx, u, domainID, AliasInput{LocalPart: a.LocalPart, Destinations: a.Destinations}); err != nil {
				if !strings.Contains(err.Error(), "đã tồn tại") {
					skip("chuyển tiếp "+a.LocalPart+"@"+md.Domain, err)
				}
			}
		}
	}
	if len(m.Mail) > 0 {
		// Push the restored password hashes to Dovecot.
		if _, err := s.syncMail(ctx, s.pool); err != nil {
			return err
		}
	}
	if resetPw > 0 {
		logf("  %d hộp thư từ backup cũ chưa lưu mật khẩu: đặt mật khẩu mới trong trang Email", resetPw)
	}

	jobs, err := s.cron.ByUser(ctx, u.ID)
	if err != nil {
		return err
	}
	haveCron := map[string]bool{}
	for _, j := range jobs {
		haveCron[j.Schedule+"\x00"+j.Command] = true
	}
	for _, c := range m.Cron {
		if haveCron[c.Schedule+"\x00"+c.Command] {
			continue
		}
		if _, err := s.CreateCron(ctx, u, CronInput{Schedule: c.Schedule, Command: c.Command, Enabled: c.Enabled}); err != nil {
			skip("cron "+c.Schedule, err)
			continue
		}
		logf("  cron %s", c.Schedule)
	}

	ftpNew := 0
	for _, f := range m.FTP {
		suffix, ok := strings.CutPrefix(f.Login, u.Username+"_")
		if !ok {
			continue
		}
		if _, err := s.CreateFTP(ctx, u, FTPInput{Name: suffix, Dir: f.Dir, Password: randomSecret() + "Aa1"}); err != nil {
			if !strings.Contains(err.Error(), "tồn tại") {
				skip("FTP "+f.Login, err)
			}
			continue
		}
		ftpNew++
		logf("  FTP %s", f.Login)
	}
	if ftpNew > 0 {
		logf("  tài khoản FTP được tạo với mật khẩu ngẫu nhiên: đặt lại mật khẩu trong trang FTP")
	}
	if len(m.Sites) > 0 {
		logf("  SSL không được chuyển theo: trỏ tên miền về máy chủ này rồi cấp lại chứng chỉ")
	}
	return nil
}

// readBackupManifest fetches the manifest of a backup through the agent.
func (s *Service) readBackupManifest(ctx context.Context, u *users.User, creds gdrive.Creds, fileID, pass string) (json.RawMessage, error) {
	var res agentproto.BackupManifest
	if err := s.agent.Call(ctx, agentproto.ActionBackupManifest, agentproto.BackupManifestParams{
		Username: u.Username, Drive: creds, FileID: fileID, Passphrase: pass}, &res); err != nil {
		return nil, err
	}
	return res.Manifest, nil
}
