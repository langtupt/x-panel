package validate

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	dbNameRe     = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	dbPasswordRe = regexp.MustCompile(`^[A-Za-z0-9!@#$%^&*()_+=.,:;?~\-]{10,64}$`)
)

// DBName checks a MariaDB database or user name owned by owner: "<owner>_<suffix>".
// The strict charset makes the names safe to use as quoted identifiers.
func DBName(owner, name string) error {
	if !dbNameRe.MatchString(name) || !strings.HasPrefix(name, owner+"_") || len(name) <= len(owner)+1 {
		return fmt.Errorf("tên database %q không hợp lệ (phải có dạng %s_tên, chỉ a-z, 0-9, _)", name, owner)
	}
	return nil
}

// DBPassword allows 10–64 characters without quotes, backslashes or spaces,
// so it can never break out of an SQL string literal.
func DBPassword(p string) error {
	if !dbPasswordRe.MatchString(p) {
		return errors.New("mật khẩu database: 10–64 ký tự, gồm chữ, số và !@#$%^&*()_+=.,:;?~-")
	}
	return nil
}

// FTPLogin checks a Pure-FTPd login "<owner>_<suffix>" (at most 32 characters).
func FTPLogin(owner, login string) error {
	if !dbNameRe.MatchString(login) || !strings.HasPrefix(login, owner+"_") || len(login) <= len(owner)+1 || len(login) > 32 {
		return fmt.Errorf("tên FTP %q không hợp lệ (dạng %s_tên, chỉ a-z, 0-9, _, tối đa 32 ký tự)", login, owner)
	}
	return nil
}
