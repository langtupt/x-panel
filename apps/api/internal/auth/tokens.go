package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"xpanel/apps/api/internal/users"
)

const (
	AccessTTL  = 15 * time.Minute
	RefreshTTL = 7 * 24 * time.Hour
)

type Claims struct {
	Role users.Role `json:"role"`
	Imp  int64      `json:"imp,omitempty"` // id of the admin/reseller acting as this user
	jwt.RegisteredClaims
}

func signAccess(secret []byte, u *users.User, impID *int64, now time.Time) (string, error) {
	c := Claims{
		Role: u.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatInt(u.ID, 10),
			Issuer:    "xpanel",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(AccessTTL)),
		},
	}
	if impID != nil {
		c.Imp = *impID
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(secret)
}

// ParseAccess validates an access token and returns the user id it was issued
// to and, for impersonated sessions, the impersonator id (0 otherwise).
func ParseAccess(secret []byte, token string) (userID, impID int64, err error) {
	var c Claims
	_, err = jwt.ParseWithClaims(token, &c, func(*jwt.Token) (any, error) { return secret, nil },
		jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer("xpanel"))
	if err != nil {
		return 0, 0, err
	}
	id, err := strconv.ParseInt(c.Subject, 10, 64)
	if err != nil {
		return 0, 0, errors.New("bad subject")
	}
	return id, c.Imp, nil
}

// newRefresh returns an opaque token for the cookie and its SHA-256 for the DB.
func newRefresh() (token string, hash []byte, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", nil, err
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, hashRefresh(token), nil
}

func hashRefresh(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}
