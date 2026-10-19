package users

import (
	"errors"
	"net/mail"
	"regexp"
)

// Usernames double as Linux account names, so keep them strict.
var usernameRe = regexp.MustCompile(`^[a-z][a-z0-9_]{2,31}$`)

// Reserved names that already exist on Debian/Ubuntu or are used by the panel.
var reserved = map[string]bool{
	"root": true, "xpanel": true, "nginx": true, "www": true, "www_data": true,
	"mysql": true, "postgres": true, "daemon": true, "bin": true, "sys": true, "sync": true,
	"nobody": true, "apache": true, "ftp": true, "mail": true, "sshd": true, "systemd": true,
}

const MinPasswordLen = 10

func ValidateNew(username, password, email string) error {
	if !usernameRe.MatchString(username) {
		return errors.New("username: 3-32 ký tự, bắt đầu bằng chữ thường, chỉ gồm a-z, 0-9, _")
	}
	if reserved[username] {
		return errors.New("username đã được hệ thống dùng")
	}
	if len(password) < MinPasswordLen {
		return errors.New("mật khẩu tối thiểu 10 ký tự")
	}
	if email != "" {
		if _, err := mail.ParseAddress(email); err != nil {
			return errors.New("email không hợp lệ")
		}
	}
	return nil
}

// CanCreate reports whether actor may create an account with the given role.
// Admin creates resellers and users; resellers create users only.
func CanCreate(actor *User, role Role) bool {
	switch actor.Role {
	case RoleAdmin:
		return role == RoleReseller || role == RoleUser
	case RoleReseller:
		return role == RoleUser
	}
	return false
}

// CanManage reports whether actor may view/modify target.
func CanManage(actor, target *User) bool {
	if actor.ID == target.ID {
		return false // nobody suspends or edits their own account through management endpoints
	}
	switch actor.Role {
	case RoleAdmin:
		return target.Role != RoleAdmin
	case RoleReseller:
		return target.ParentID != nil && *target.ParentID == actor.ID
	}
	return false
}
