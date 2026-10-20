package accounts

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"xpanel/apps/api/internal/apperr"
	"xpanel/apps/api/internal/secretbox"
	"xpanel/apps/api/internal/users"
	"xpanel/pkg/agentproto"
	"xpanel/pkg/gdrive"
)

// SetSecrets gives the service the box that seals stored secrets.
func (s *Service) SetSecrets(b *secretbox.Box) { s.secrets = b }

// backupConfig is one hosting account's own backup setup: its Google OAuth
// client, Drive connection, passphrase and schedule. Nothing is shared
// between accounts or with the administrator.
type backupConfig struct {
	ClientID     string
	ClientSecret string // sealed
	Refresh      string // sealed
	Email        string
	Folder       string
	Passphrase   string // sealed
	Schedule     string
	Hour         int
	Keep         int
	LastRun      *time.Time
}

func (s *Service) backupConfig(ctx context.Context, userID int64) (*backupConfig, error) {
	c := &backupConfig{Schedule: "off", Hour: 2, Keep: 7}
	err := s.pool.QueryRow(ctx, `SELECT client_id, client_secret, refresh_token, email, folder, passphrase,
		schedule, hour, keep, last_run FROM backup_configs WHERE user_id=$1`, userID).
		Scan(&c.ClientID, &c.ClientSecret, &c.Refresh, &c.Email, &c.Folder, &c.Passphrase, &c.Schedule, &c.Hour, &c.Keep, &c.LastRun)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, nil
	}
	return c, err
}

func (s *Service) saveBackupConfig(ctx context.Context, userID int64, c *backupConfig) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO backup_configs (user_id, client_id, client_secret, refresh_token, email,
		folder, passphrase, schedule, hour, keep, last_run) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (user_id) DO UPDATE SET client_id=$2, client_secret=$3, refresh_token=$4, email=$5, folder=$6,
		passphrase=$7, schedule=$8, hour=$9, keep=$10, last_run=$11, updated_at=now()`,
		userID, c.ClientID, c.ClientSecret, c.Refresh, c.Email, c.Folder, c.Passphrase, c.Schedule, c.Hour, c.Keep, c.LastRun)
	return err
}

func (s *Service) seal(v string) (string, error) {
	if v == "" {
		return "", nil
	}
	return s.secrets.Seal(v)
}

// driveCreds returns the account's Google credentials.
func (s *Service) driveCreds(ctx context.Context, userID int64) (gdrive.Creds, *backupConfig, error) {
	c, err := s.backupConfig(ctx, userID)
	if err != nil {
		return gdrive.Creds{}, nil, err
	}
	if c.Refresh == "" || c.Folder == "" {
		return gdrive.Creds{}, c, apperr.Conflict("tài khoản chưa kết nối Google Drive (trang Sao lưu)")
	}
	creds := gdrive.Creds{ClientID: c.ClientID}
	if creds.ClientSecret, err = s.secrets.Open(c.ClientSecret); err != nil {
		return creds, c, err
	}
	if creds.RefreshToken, err = s.secrets.Open(c.Refresh); err != nil {
		return creds, c, err
	}
	return creds, c, nil
}

func (s *Service) backupPassphrase(c *backupConfig) (string, error) {
	p, err := s.secrets.Open(c.Passphrase)
	if err == nil && p == "" {
		err = apperr.Conflict("chưa đặt passphrase mã hóa backup (trang Sao lưu)")
	}
	return p, err
}

// backupOwner is the hosting account whose backup settings the actor edits:
// only the account itself (administrators and resellers use "log in as").
func backupOwner(actor *users.User) error {
	if actor.Role != users.RoleUser || actor.LinuxUID == nil {
		return apperr.Forbidden("cấu hình sao lưu do từng tài khoản hosting tự thiết lập (dùng \"Đăng nhập thay\" để cấu hình hộ)")
	}
	return nil
}

type BackupSettings struct {
	Connected     bool          `json:"connected"`
	Email         string        `json:"email"`
	ClientID      string        `json:"client_id"`
	Schedule      string        `json:"schedule"`
	Hour          int           `json:"hour"`
	Keep          int           `json:"keep"`
	PassphraseSet bool          `json:"passphrase_set"`
	Pending       *DriveConnect `json:"pending,omitempty"`
}

// DriveConnect is an OAuth device authorization in progress.
type DriveConnect struct {
	UserCode        string    `json:"user_code"`
	VerificationURL string    `json:"verification_url"`
	Status          string    `json:"status"` // waiting | done | failed
	Error           string    `json:"error,omitempty"`
	ExpiresAt       time.Time `json:"expires_at"`
}

var (
	driveMu      sync.Mutex
	drivePending = map[int64]*DriveConnect{} // per account
)

func (s *Service) BackupSettings(ctx context.Context, actor *users.User) (*BackupSettings, error) {
	if err := backupOwner(actor); err != nil {
		return nil, err
	}
	c, err := s.backupConfig(ctx, actor.ID)
	if err != nil {
		return nil, err
	}
	st := &BackupSettings{ClientID: c.ClientID, Email: c.Email, Schedule: c.Schedule, Hour: c.Hour, Keep: c.Keep,
		Connected: c.Refresh != "" && c.Folder != "", PassphraseSet: c.Passphrase != ""}
	driveMu.Lock()
	if p := drivePending[actor.ID]; p != nil {
		cp := *p
		st.Pending = &cp
	}
	driveMu.Unlock()
	return st, nil
}

type BackupSettingsInput struct {
	Schedule   string  `json:"schedule"`
	Hour       int     `json:"hour"`
	Keep       int     `json:"keep"`
	Passphrase *string `json:"passphrase"`
}

func (s *Service) SaveBackupSettings(ctx context.Context, actor *users.User, in BackupSettingsInput) (*BackupSettings, error) {
	if err := backupOwner(actor); err != nil {
		return nil, err
	}
	if in.Schedule != "off" && in.Schedule != "daily" && in.Schedule != "weekly" {
		return nil, apperr.Invalid("lịch phải là off, daily hoặc weekly")
	}
	if in.Hour < 0 || in.Hour > 23 || in.Keep < 1 || in.Keep > 365 {
		return nil, apperr.Invalid("giờ chạy 0–23, số bản giữ lại 1–365")
	}
	c, err := s.backupConfig(ctx, actor.ID)
	if err != nil {
		return nil, err
	}
	if in.Passphrase != nil {
		if len(*in.Passphrase) < 12 || len(*in.Passphrase) > 200 {
			return nil, apperr.Invalid("passphrase mã hóa backup phải dài 12–200 ký tự")
		}
		if c.Passphrase, err = s.seal(*in.Passphrase); err != nil {
			return nil, err
		}
	}
	if in.Schedule != "off" && c.Passphrase == "" {
		return nil, apperr.Invalid("đặt passphrase mã hóa trước khi bật lịch backup")
	}
	c.Schedule, c.Hour, c.Keep = in.Schedule, in.Hour, in.Keep
	if err := s.saveBackupConfig(ctx, actor.ID, c); err != nil {
		return nil, err
	}
	return s.BackupSettings(ctx, actor)
}

// ConnectDrive starts the OAuth device flow with the account's own OAuth
// client; the account owner approves it on google.com/device.
func (s *Service) ConnectDrive(ctx context.Context, actor *users.User, clientID, clientSecret string) (*DriveConnect, error) {
	if err := backupOwner(actor); err != nil {
		return nil, err
	}
	clientID, clientSecret = strings.TrimSpace(clientID), strings.TrimSpace(clientSecret)
	if !strings.HasSuffix(clientID, ".apps.googleusercontent.com") || len(clientID) > 200 || clientSecret == "" || len(clientSecret) > 200 {
		return nil, apperr.Invalid("Client ID / Client Secret không hợp lệ (OAuth client loại \"TVs and Limited Input devices\")")
	}
	auth, err := gdrive.StartDevice(ctx, clientID)
	if err != nil {
		return nil, apperr.Invalid("Google từ chối: %s", err.Error())
	}
	p := &DriveConnect{UserCode: auth.UserCode, VerificationURL: auth.VerificationURL, Status: "waiting",
		ExpiresAt: time.Now().Add(time.Duration(auth.ExpiresIn) * time.Second)}
	driveMu.Lock()
	drivePending[actor.ID] = p
	driveMu.Unlock()
	go s.pollDrive(actor.ID, actor.Username, clientID, clientSecret, auth, p)
	out := *p
	return &out, nil
}

func (s *Service) pollDrive(userID int64, username, clientID, clientSecret string, auth *gdrive.DeviceAuth, p *DriveConnect) {
	ctx, cancel := context.WithDeadline(context.Background(), p.ExpiresAt)
	defer cancel()
	fail := func(err error) {
		driveMu.Lock()
		p.Status, p.Error = "failed", err.Error()
		driveMu.Unlock()
	}
	interval := time.Duration(auth.Interval) * time.Second
	var refresh string
	for refresh == "" {
		select {
		case <-ctx.Done():
			fail(errors.New("hết thời gian chờ xác nhận"))
			return
		case <-time.After(interval):
		}
		driveMu.Lock()
		stale := drivePending[userID] != p
		driveMu.Unlock()
		if stale {
			return
		}
		tok, err := gdrive.PollDevice(ctx, clientID, clientSecret, auth.DeviceCode)
		switch {
		case errors.Is(err, gdrive.ErrPending):
		case errors.Is(err, gdrive.ErrSlowDown):
			interval += 5 * time.Second
		case err != nil:
			fail(err)
			return
		default:
			refresh = tok
		}
	}
	c := gdrive.New(gdrive.Creds{ClientID: clientID, ClientSecret: clientSecret, RefreshToken: refresh})
	email, err := c.Email(ctx)
	if err != nil {
		fail(err)
		return
	}
	var info agentproto.SystemInfo
	_ = s.agent.Call(ctx, agentproto.ActionSystemInfo, nil, &info)
	host := info.Hostname
	if host == "" {
		host = "server"
	}
	root, err := c.EnsureFolder(ctx, "X-Panel Backups", "")
	if err == nil {
		root, err = c.EnsureFolder(ctx, host+" - "+username, root)
	}
	if err != nil {
		fail(err)
		return
	}
	cfg, err := s.backupConfig(ctx, userID)
	if err == nil {
		cfg.ClientID, cfg.Email, cfg.Folder = clientID, email, root
		if cfg.ClientSecret, err = s.seal(clientSecret); err == nil {
			if cfg.Refresh, err = s.seal(refresh); err == nil {
				err = s.saveBackupConfig(ctx, userID, cfg)
			}
		}
	}
	if err != nil {
		fail(err)
		return
	}
	driveMu.Lock()
	p.Status = "done"
	driveMu.Unlock()
	s.log.Info("google drive connected", "user", username, "email", email)
}

func (s *Service) DisconnectDrive(ctx context.Context, actor *users.User) error {
	if err := backupOwner(actor); err != nil {
		return err
	}
	c, err := s.backupConfig(ctx, actor.ID)
	if err != nil {
		return err
	}
	if tok, _ := s.secrets.Open(c.Refresh); tok != "" {
		_ = gdrive.Revoke(ctx, tok)
	}
	driveMu.Lock()
	delete(drivePending, actor.ID)
	driveMu.Unlock()
	c.Refresh, c.Email, c.Folder, c.ClientSecret, c.Schedule = "", "", "", "", "off"
	return s.saveBackupConfig(ctx, actor.ID, c)
}

// RunBackups starts each account's scheduled backup at its own hour (daily,
// or Sundays for weekly).
func (s *Service) RunBackups(ctx context.Context) {
	tick := time.NewTicker(10 * time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		if err := s.scheduledBackups(ctx); err != nil && ctx.Err() == nil {
			s.log.Warn("scheduled backups", "err", err)
		}
	}
}

func (s *Service) scheduledBackups(ctx context.Context) error {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	rows, err := s.pool.Query(ctx, `SELECT user_id FROM backup_configs
		WHERE refresh_token<>'' AND passphrase<>'' AND hour=$1
		  AND (schedule='daily' OR (schedule='weekly' AND $2))
		  AND (last_run IS NULL OR last_run < $3)`, now.Hour(), now.Weekday() == time.Sunday, today)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if _, err := s.pool.Exec(ctx, `UPDATE backup_configs SET last_run=$2 WHERE user_id=$1`, id, today); err != nil {
			return err
		}
		u, err := s.users.ByID(ctx, id)
		if err != nil || u.LinuxUID == nil {
			continue
		}
		if _, err := s.enqueueBackup(ctx, nil, u, "scheduled"); err != nil {
			s.log.Warn("scheduled backup", "user", u.Username, "err", err)
		}
	}
	return nil
}
