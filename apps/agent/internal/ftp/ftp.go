// Package ftp manages Pure-FTPd virtual users. Each one maps to the owner's
// Linux uid/gid and is chrooted into a directory inside the owner's home, so
// files keep the right owner and count towards the disk quota.
package ftp

import (
	"context"
	"os/user"
	"strconv"

	"xpanel/apps/agent/internal/accounts"
	"xpanel/apps/agent/internal/fsutil"
	"xpanel/apps/agent/internal/run"
	"xpanel/pkg/agentproto"
	"xpanel/pkg/validate"
)

const (
	passwdFile = "/etc/pure-ftpd/pureftpd.passwd"
	pdbFile    = "/etc/pure-ftpd/pureftpd.pdb"
)

// purePw runs pure-pw against X-Panel's database and rebuilds the .pdb (-m).
func purePw(ctx context.Context, stdin string, args ...string) error {
	args = append(args, "-f", passwdFile, "-F", pdbFile, "-m")
	return run.CmdStdin(ctx, stdin, "pure-pw", args...)
}

func owner(p agentproto.FTPParams) (*user.User, error) {
	u, err := accounts.Managed(p.Owner)
	if err != nil {
		return nil, err
	}
	if err := validate.FTPLogin(p.Owner, p.Login); err != nil {
		return nil, err
	}
	return u, nil
}

func checkPassword(pw string) error {
	// Same charset as database passwords: nothing that can break the stdin protocol.
	return validate.DBPassword(pw)
}

func Create(ctx context.Context, p agentproto.FTPParams) error {
	u, err := owner(p)
	if err != nil {
		return err
	}
	if err := validate.DocRoot(p.Dir); err != nil {
		return err
	}
	if err := checkPassword(p.Password); err != nil {
		return err
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	dir, err := fsutil.SafeMkdirAll(u.HomeDir, p.Dir, uid, gid, 0o750)
	if err != nil {
		return err
	}
	pw := p.Password + "\n" + p.Password + "\n"
	return purePw(ctx, pw, "useradd", p.Login, "-u", u.Uid, "-g", u.Gid, "-d", dir)
}

func SetPassword(ctx context.Context, p agentproto.FTPParams) error {
	if _, err := owner(p); err != nil {
		return err
	}
	if err := checkPassword(p.Password); err != nil {
		return err
	}
	return purePw(ctx, p.Password+"\n"+p.Password+"\n", "passwd", p.Login)
}

// Delete removes the login; an already missing login is not an error.
func Delete(ctx context.Context, p agentproto.FTPParams) error {
	if err := validate.FTPLogin(p.Owner, p.Login); err != nil {
		return err
	}
	err := purePw(ctx, "", "userdel", p.Login)
	if err != nil && !userMissing(ctx, p.Login) {
		return err
	}
	return nil
}

// Lock denies (or re-allows) logins from every client address.
func Lock(ctx context.Context, p agentproto.FTPLockParams) error {
	deny := ""
	if p.Locked {
		deny = "0.0.0.0/0,::/0"
	}
	for _, login := range p.Logins {
		if err := validate.FTPLogin(p.Owner, login); err != nil {
			return err
		}
		if err := purePw(ctx, "", "usermod", login, "-R", deny); err != nil {
			return err
		}
	}
	return nil
}

func userMissing(ctx context.Context, login string) bool {
	_, err := run.Output(ctx, "pure-pw", "show", login, "-f", passwdFile)
	return err != nil
}
