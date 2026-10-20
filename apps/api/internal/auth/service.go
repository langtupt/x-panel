// Package auth implements login with short-lived JWT access tokens and
// rotating opaque refresh tokens stored (hashed) in the sessions table.
package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"xpanel/apps/api/internal/users"
)

var (
	ErrInvalidCredentials = errors.New("sai tên đăng nhập hoặc mật khẩu")
	ErrSuspended          = errors.New("tài khoản đã bị khóa")
	ErrTooManyAttempts    = errors.New("đăng nhập sai quá nhiều lần, thử lại sau 15 phút")
	ErrInvalidSession     = errors.New("phiên đăng nhập không hợp lệ")
	ErrTOTPRequired       = errors.New("cần mã xác thực 2 lớp")
	ErrTOTPEnabled        = errors.New("two-factor authentication already enabled")
	ErrTOTPInvalid        = errors.New("mã xác thực 2 lớp không đúng")
)

type Tokens struct {
	Access  string
	Refresh string
}

type Service struct {
	pool    *pgxpool.Pool
	users   *users.Repo
	secret  []byte
	box     secretBox
	limiter *failLimiter
	// Per account, so guessing from many addresses is limited too. Higher
	// than the per-IP limit: the owner keeps a margin to log in meanwhile.
	accountLimiter *failLimiter

	usedMu    sync.Mutex
	usedCodes map[string]time.Time // "userID:code" -> when it stops being valid
}

func NewService(pool *pgxpool.Pool, repo *users.Repo, secret []byte) *Service {
	return &Service{pool: pool, users: repo, secret: secret, box: newSecretBox(secret),
		limiter: newFailLimiter(10, 15*time.Minute), accountLimiter: newFailLimiter(30, 15*time.Minute), usedCodes: map[string]time.Time{}}
}

func (s *Service) Secret() []byte { return s.secret }

// checkTOTP verifies the second factor of an account that has it enabled.
func (s *Service) checkTOTP(u *users.User, code string) error {
	if u.TOTPSecret == nil {
		return nil
	}
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if code == "" {
		return ErrTOTPRequired
	}
	secret, err := s.box.open(*u.TOTPSecret)
	if err != nil {
		return err
	}
	if !VerifyTOTP(secret, code, time.Now()) {
		return ErrTOTPInvalid
	}
	// A code works once: one seen over the user's shoulder or replayed from
	// a captured request is refused for the rest of its validity window.
	s.usedMu.Lock()
	defer s.usedMu.Unlock()
	now := time.Now()
	for k, until := range s.usedCodes {
		if now.After(until) {
			delete(s.usedCodes, k)
		}
	}
	key := fmt.Sprintf("%d:%s", u.ID, code)
	if _, used := s.usedCodes[key]; used {
		return ErrTOTPInvalid
	}
	s.usedCodes[key] = now.Add(3 * time.Minute)
	return nil
}

// EnableTOTP stores secret after the user proved their app produces valid codes.
func (s *Service) EnableTOTP(ctx context.Context, u *users.User, secret, code string) error {
	if u.TOTPSecret != nil {
		return ErrTOTPEnabled
	}
	if !VerifyTOTP(secret, code, time.Now()) {
		return ErrTOTPInvalid
	}
	sealed, err := s.box.seal(secret)
	if err != nil {
		return err
	}
	ok, err := s.users.EnableTOTP(ctx, u.ID, sealed)
	if err != nil {
		return err
	}
	if !ok {
		return ErrTOTPEnabled
	}
	return nil
}

// DisableTOTP requires the password and a current code.
func (s *Service) DisableTOTP(ctx context.Context, u *users.User, password, code string) error {
	if !CheckPassword(u.PasswordHash, password) {
		return ErrInvalidCredentials
	}
	if err := s.checkTOTP(u, code); err != nil {
		if errors.Is(err, ErrTOTPRequired) {
			return ErrTOTPInvalid
		}
		return err
	}
	return s.users.SetTOTP(ctx, u.ID, nil)
}

// Impersonate starts a session as target on behalf of actor (permission is
// checked by the caller). The session remembers who started it.
func (s *Service) Impersonate(ctx context.Context, actor, target *users.User, ip, ua string) (Tokens, error) {
	if target.Status != users.StatusActive {
		return Tokens{}, ErrSuspended
	}
	return s.issue(ctx, s.pool, target, &actor.ID, ip, ua)
}

func (s *Service) Login(ctx context.Context, username, password, code, ip, ua string) (*users.User, Tokens, error) {
	if s.limiter.Blocked(ip) {
		return nil, Tokens{}, ErrTooManyAttempts
	}
	u, err := s.users.ByUsername(ctx, username)
	if errors.Is(err, users.ErrNotFound) {
		CheckPassword(dummyHash, password)
		s.limiter.Fail(ip)
		return nil, Tokens{}, ErrInvalidCredentials
	}
	if err != nil {
		return nil, Tokens{}, err
	}
	if s.accountLimiter.Blocked(u.Username) {
		s.limiter.Fail(ip)
		return nil, Tokens{}, ErrTooManyAttempts
	}
	if !CheckPassword(u.PasswordHash, password) {
		s.limiter.Fail(ip)
		s.accountLimiter.Fail(u.Username)
		return nil, Tokens{}, ErrInvalidCredentials
	}
	if u.Status != users.StatusActive {
		return nil, Tokens{}, ErrSuspended
	}
	if err := s.checkTOTP(u, code); err != nil {
		if errors.Is(err, ErrTOTPInvalid) {
			s.limiter.Fail(ip)
			s.accountLimiter.Fail(u.Username)
		}
		return nil, Tokens{}, err
	}
	s.limiter.Reset(ip)
	t, err := s.issue(ctx, s.pool, u, nil, ip, ua)
	return u, t, err
}

// Refresh rotates a refresh token. Presenting an already-revoked token is
// treated as theft: every session of that user is revoked.
func (s *Service) Refresh(ctx context.Context, refresh, ip, ua string) (*users.User, Tokens, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, Tokens{}, err
	}
	defer tx.Rollback(ctx)

	var (
		sessID, userID int64
		expires        time.Time
		revoked        *time.Time
		impID          *int64
	)
	err = tx.QueryRow(ctx,
		`SELECT id, user_id, expires_at, revoked_at, impersonator_id FROM sessions WHERE token_hash=$1 FOR UPDATE`,
		hashRefresh(refresh)).Scan(&sessID, &userID, &expires, &revoked, &impID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, Tokens{}, ErrInvalidSession
	}
	if err != nil {
		return nil, Tokens{}, err
	}
	if revoked != nil {
		if _, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, userID); err != nil {
			return nil, Tokens{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, Tokens{}, err
		}
		return nil, Tokens{}, ErrInvalidSession
	}
	if time.Now().After(expires) {
		return nil, Tokens{}, ErrInvalidSession
	}

	u, err := s.users.ByID(ctx, userID)
	if err != nil {
		return nil, Tokens{}, err
	}
	if u.Status != users.StatusActive {
		return nil, Tokens{}, ErrSuspended
	}
	if _, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE id=$1`, sessID); err != nil {
		return nil, Tokens{}, err
	}
	t, err := s.issue(ctx, tx, u, impID, ip, ua)
	if err != nil {
		return nil, Tokens{}, err
	}
	return u, t, tx.Commit(ctx)
}

func (s *Service) Logout(ctx context.Context, refresh string) error {
	_, err := s.pool.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE token_hash=$1 AND revoked_at IS NULL`, hashRefresh(refresh))
	return err
}

// RevokeAll ends every session of a user, e.g. after suspension.
func (s *Service) RevokeAll(ctx context.Context, userID int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, userID)
	return err
}

// execer is satisfied by both *pgxpool.Pool and pgx.Tx.
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func (s *Service) issue(ctx context.Context, db execer, u *users.User, impID *int64, ip, ua string) (Tokens, error) {
	now := time.Now()
	access, err := signAccess(s.secret, u, impID, now)
	if err != nil {
		return Tokens{}, err
	}
	refresh, hash, err := newRefresh()
	if err != nil {
		return Tokens{}, err
	}
	if len(ua) > 255 {
		ua = ua[:255]
	}
	_, err = db.Exec(ctx,
		`INSERT INTO sessions (user_id, token_hash, user_agent, ip, expires_at, impersonator_id) VALUES ($1,$2,$3,$4,$5,$6)`,
		u.ID, hash, ua, ip, now.Add(RefreshTTL), impID)
	return Tokens{Access: access, Refresh: refresh}, err
}

// Reissue starts a fresh session for u (e.g. after a password change revoked all).
func (s *Service) Reissue(ctx context.Context, u *users.User, ip, ua string) (Tokens, error) {
	return s.issue(ctx, s.pool, u, nil, ip, ua)
}
