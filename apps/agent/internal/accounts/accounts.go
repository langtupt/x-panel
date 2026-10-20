// Package accounts manages the Linux users that back hosting accounts.
// Every user it creates carries the GECOS marker "xpanel"; it refuses to
// modify or delete any account without that marker.
package accounts

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"regexp"
	"strconv"
	"strings"

	"xpanel/apps/agent/internal/fsutil"
	"xpanel/apps/agent/internal/run"
)

const (
	Marker    = "xpanel"
	HomeBase  = "/home"
	SFTPGroup = "xpanel-sftp"
	minUID    = 1000
)

var usernameRe = regexp.MustCompile(`^[a-z][a-z0-9_]{2,31}$`)

func validate(username string) error {
	if !usernameRe.MatchString(username) {
		return fmt.Errorf("invalid username %q", username)
	}
	return nil
}

// Managed returns the Linux user if it exists and was created by X-Panel.
func Managed(username string) (*user.User, error) {
	if err := validate(username); err != nil {
		return nil, err
	}
	u, err := user.Lookup(username)
	if err != nil {
		return nil, err
	}
	uid, _ := strconv.Atoi(u.Uid)
	if u.Name != Marker || uid < minUID {
		return nil, fmt.Errorf("user %q is not managed by xpanel", username)
	}
	return u, nil
}

func Create(ctx context.Context, username string) (uid int, home string, err error) {
	if err := validate(username); err != nil {
		return 0, "", err
	}
	if _, err := user.Lookup(username); err == nil {
		return 0, "", fmt.Errorf("linux user %q already exists", username)
	}
	home = HomeBase + "/" + username
	groupArgs, err := groupFor(username)
	if err != nil {
		return 0, "", err
	}
	args := append([]string{"--create-home", "--home-dir", home, "--shell", "/usr/sbin/nologin"}, groupArgs...)
	if err := run.Cmd(ctx, "useradd", append(args, "--comment", Marker, username)...); err != nil {
		return 0, "", err
	}
	u, err := user.Lookup(username)
	if err != nil {
		return 0, "", err
	}
	if err := prepareSFTP(ctx, u); err != nil {
		return 0, "", err
	}
	uid, _ = strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	// The chrooted home is root-owned, so SFTP users need these to exist from the start.
	for _, d := range []struct {
		name string
		mode os.FileMode
	}{{"www", 0o750}, {"tmp", 0o700}, {"logs", 0o750}} {
		if _, err := fsutil.SafeMkdirAll(home, d.name, uid, gid, d.mode); err != nil {
			return 0, "", err
		}
	}
	return uid, home, nil
}

// prepareSFTP makes the home usable as an OpenSSH chroot (owned by root,
// group = the user, 0750; the user writes only inside www/tmp/logs) and puts
// the user in the group matched by the installer's sshd "Match" block.
func prepareSFTP(ctx context.Context, u *user.User) error {
	gid, _ := strconv.Atoi(u.Gid)
	if err := os.Chown(u.HomeDir, 0, gid); err != nil {
		return err
	}
	if err := os.Chmod(u.HomeDir, 0o750); err != nil {
		return err
	}
	if _, err := user.LookupGroup(SFTPGroup); err != nil {
		return nil // installer has not set up SFTP (older install)
	}
	return run.Cmd(ctx, "gpasswd", "-a", u.Username, SFTPGroup)
}

// SetPassword sets the Linux password used for SFTP logins.
func SetPassword(ctx context.Context, username, password string) error {
	u, err := Managed(username)
	if err != nil {
		return err
	}
	if len(password) < 8 || strings.ContainsAny(password, "\r\n") {
		return errors.New("invalid password")
	}
	if err := run.CmdStdin(ctx, username+":"+password+"\n", "chpasswd"); err != nil {
		return err
	}
	return prepareSFTP(ctx, u)
}

// Delete kills the user's processes and removes the account and its home.
// A user that no longer exists is not an error.
func Delete(ctx context.Context, username string) error {
	u, err := Managed(username)
	if err != nil {
		var unknown user.UnknownUserError
		if errors.As(err, &unknown) {
			return nil
		}
		return err
	}
	_ = run.Cmd(ctx, "pkill", "-KILL", "-u", username) // exit 1 = no processes
	// userdel refuses (exit 12) to remove a home it does not own (ours is
	// root-owned for the SFTP chroot) or a group with other members (nginx).
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	if err := os.Lchown(u.HomeDir, uid, gid); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if g, err := user.LookupGroupId(u.Gid); err == nil {
		for _, member := range groupMembers(g.Name) {
			_ = run.Cmd(ctx, "gpasswd", "-d", member, g.Name)
		}
	}
	return run.Cmd(ctx, "userdel", "--remove", username)
}

// groupMembers lists the supplementary members of a group from /etc/group.
func groupMembers(group string) []string {
	b, err := os.ReadFile("/etc/group")
	if err != nil {
		return nil
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Split(line, ":")
		if len(f) == 4 && f[0] == group && f[3] != "" {
			return strings.Split(f[3], ",")
		}
	}
	return nil
}

// Lock expires the account so neither SSH/SFTP nor cron can use it.
// (usermod -L/-U is avoided: unlocking a password-less account fails.)
func Lock(ctx context.Context, username string, locked bool) error {
	if _, err := Managed(username); err != nil {
		return err
	}
	expire := ""
	if locked {
		expire = "1"
	}
	if err := run.Cmd(ctx, "usermod", "--expiredate", expire, username); err != nil {
		return err
	}
	if locked {
		// Kill everything the account runs (PHP, apps, an in-progress build).
		_ = run.Cmd(ctx, "systemctl", "stop", "xpanel-"+username+".slice")
	}
	return nil
}

// Locked reports whether the account is suspended (expired in /etc/shadow).
func Locked(username string) bool {
	b, err := os.ReadFile("/etc/shadow")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Split(line, ":")
		if len(f) >= 8 && f[0] == username {
			return f[7] == "1"
		}
	}
	return false
}

// groupFor picks useradd's group option. A group left behind with the same
// name (an older uninstall removed the user but not its group, which nginx
// is a member of) is reused when no account has it as primary group, so the
// name can be created again and kept files keep their group.
func groupFor(username string) ([]string, error) {
	g, err := user.LookupGroup(username)
	if err != nil {
		return []string{"--user-group"}, nil
	}
	gid, _ := strconv.Atoi(g.Gid)
	if gid < minUID {
		return nil, fmt.Errorf("group %q is a system group", username)
	}
	if primaryUser(g.Gid) {
		return nil, fmt.Errorf("group %q belongs to another user", username)
	}
	return []string{"--gid", username}, nil
}

// primaryUser reports whether some user has gid as its primary group.
func primaryUser(gid string) bool {
	b, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return true // be safe: do not reuse
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Split(line, ":")
		if len(f) > 3 && f[3] == gid {
			return true
		}
	}
	return false
}
