package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"

	"xpanel/apps/api/internal/users"
)

// Recovery codes let an account with two-factor authentication log in when
// its authenticator is lost. Each works once; only an HMAC of it is stored.
const recoveryCount = 10

var recoveryRe = regexp.MustCompile(`^[a-z2-7]{5}-?[a-z2-7]{5}$`)

const recoveryAlphabet = "abcdefghijklmnopqrstuvwxyz234567"

func newRecoveryCode() (string, error) {
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, 0, 11)
	for i, v := range b {
		if i == 5 {
			out = append(out, '-')
		}
		out = append(out, recoveryAlphabet[int(v)%len(recoveryAlphabet)])
	}
	return string(out), nil
}

func normalizeRecovery(code string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(code)), " ", "")
}

// IsRecoveryCode reports whether input has the shape of a recovery code.
func IsRecoveryCode(code string) bool { return recoveryRe.MatchString(normalizeRecovery(code)) }

func (s *Service) recoveryHash(code string) string {
	m := hmac.New(sha256.New, append([]byte("xpanel-recovery:"), s.secret...))
	m.Write([]byte(strings.ReplaceAll(normalizeRecovery(code), "-", "")))
	return hex.EncodeToString(m.Sum(nil))
}

// replaceRecoveryCodes issues a new set, invalidating the previous one.
func (s *Service) replaceRecoveryCodes(ctx context.Context, userID int64) ([]string, error) {
	codes := make([]string, 0, recoveryCount)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM totp_recovery_codes WHERE user_id=$1`, userID); err != nil {
		return nil, err
	}
	for len(codes) < recoveryCount {
		c, err := newRecoveryCode()
		if err != nil {
			return nil, err
		}
		tag, err := tx.Exec(ctx, `INSERT INTO totp_recovery_codes (user_id, code_hash) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
			userID, s.recoveryHash(c))
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() == 1 {
			codes = append(codes, c)
		}
	}
	return codes, tx.Commit(ctx)
}

// useRecoveryCode consumes an unused recovery code of the account.
func (s *Service) useRecoveryCode(ctx context.Context, u *users.User, code string) (bool, error) {
	if !IsRecoveryCode(code) {
		return false, nil
	}
	tag, err := s.pool.Exec(ctx, `UPDATE totp_recovery_codes SET used_at=now()
		WHERE user_id=$1 AND code_hash=$2 AND used_at IS NULL`, u.ID, s.recoveryHash(code))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// RecoveryCodesLeft counts unused recovery codes.
func (s *Service) RecoveryCodesLeft(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM totp_recovery_codes WHERE user_id=$1 AND used_at IS NULL`, userID).Scan(&n)
	return n, err
}

// RegenerateRecoveryCodes needs the password and a current authenticator
// code (not a recovery code), so a stolen session cannot mint new ones.
func (s *Service) RegenerateRecoveryCodes(ctx context.Context, u *users.User, password, code string) ([]string, error) {
	if u.TOTPSecret == nil {
		return nil, errors.New("two-factor authentication is not enabled")
	}
	if !CheckPassword(u.PasswordHash, password) {
		return nil, ErrInvalidCredentials
	}
	if err := s.checkTOTP(u, code); err != nil {
		if errors.Is(err, ErrTOTPRequired) {
			return nil, ErrTOTPInvalid
		}
		return nil, err
	}
	return s.replaceRecoveryCodes(ctx, u.ID)
}
