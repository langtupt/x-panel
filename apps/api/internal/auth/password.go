package auth

import "golang.org/x/crypto/bcrypt"

const bcryptCost = 12

func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcryptCost)
	return string(b), err
}

func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// dummyHash is compared against when the username does not exist, so a
// failed login takes the same time whether or not the account exists.
var dummyHash, _ = HashPassword("xpanel-timing-equalizer")
