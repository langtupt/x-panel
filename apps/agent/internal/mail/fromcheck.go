package mail

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/mail"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"xpanel/apps/agent/internal/run"
)

// The header From check is a small milter on the submission ports.
// reject_sender_login_mismatch only looks at the envelope sender, so a
// mailbox could log in as itself and write "From: someone-else@hosted" in
// the message, which OpenDKIM would then sign. The milter rejects a message
// whose From: or Sender: addresses are not owned by the SASL login, using
// the same owners as smtpd_sender_login_maps.
const (
	FromCheckAddr = "127.0.0.1:8892"
	// Postfix fails closed on the submission ports if the agent is down.
	// No spaces around "=": smtpd refuses "default_action = tempfail".
	submissionMilters = "{ inet:" + FromCheckAddr + ", default_action=tempfail }, inet:127.0.0.1:8891"
)

// senderOwners caches the senders map (address -> logins allowed to use it).
var senderOwners struct {
	sync.Mutex
	mod    time.Time
	owners map[string][]string
}

// sendersFile is the smtpd_sender_login_maps source (a variable for tests).
var sendersFile = filepath.Join(mapDir, "senders")

func ownersOf(addr string) []string {
	path := sendersFile
	senderOwners.Lock()
	defer senderOwners.Unlock()
	if fi, err := os.Stat(path); err == nil && !fi.ModTime().Equal(senderOwners.mod) {
		if b, err := os.ReadFile(path); err == nil {
			m := map[string][]string{}
			for _, line := range strings.Split(string(b), "\n") {
				f := strings.Fields(line)
				if len(f) == 2 {
					m[strings.ToLower(f[0])] = strings.Split(strings.ToLower(f[1]), ",")
				}
			}
			senderOwners.owners, senderOwners.mod = m, fi.ModTime()
		}
	}
	return senderOwners.owners[strings.ToLower(addr)]
}

var addrRE = regexp.MustCompile(`[^\s<>"(),;:]+@[^\s<>"(),;:]+`)

// checkFrom returns "" when login may send a message with these From:/Sender:
// header values, else the reason to reject it.
func checkFrom(login string, values []string) string {
	login = strings.ToLower(strings.TrimSpace(login))
	for _, v := range values {
		var addrs []string
		if list, err := (&mail.AddressParser{}).ParseList(v); err == nil {
			for _, a := range list {
				addrs = append(addrs, a.Address)
			}
		} else {
			// Display names in charsets net/mail cannot decode: the addresses
			// themselves are plain ASCII.
			addrs = addrRE.FindAllString(v, -1)
		}
		if len(addrs) == 0 {
			return "invalid From/Sender header"
		}
		for _, a := range addrs {
			owned := false
			for _, o := range ownersOf(a) {
				if o == login {
					owned = true
				}
			}
			if !owned {
				return fmt.Sprintf("header address <%s> not owned by user %s", a, login)
			}
		}
	}
	return ""
}

// ServeFromCheck runs the milter until ctx ends.
func ServeFromCheck(ctx context.Context, log *slog.Logger) {
	ln, err := net.Listen("tcp", FromCheckAddr)
	if err != nil {
		log.Error("from-check milter", "err", err)
		return
	}
	go func() { <-ctx.Done(); ln.Close() }()
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() == nil {
				log.Error("from-check milter", "err", err)
			}
			return
		}
		go func() {
			defer c.Close()
			if err := serveMilter(c, c); err != nil && !errors.Is(err, io.EOF) {
				log.Warn("from-check milter session", "err", err)
			}
		}()
	}
}

// Milter protocol (version 6), the subset this check needs.
const (
	smfiOptneg  = 'O'
	smfiMacro   = 'D'
	smfiHeader  = 'L'
	smfiEOM     = 'E'
	smfiAbort   = 'A'
	smfiQuit    = 'Q'
	smfiQuitNC  = 'K'
	smfrCont    = 'c'
	smfrAccept  = 'a'
	smfrReplyCd = 'y'
	smfiMail    = 'M'
	// Skip the events the check does not use.
	smfipSkip = 0x1 | 0x2 | 0x8 | 0x10 | 0x100 | 0x200 // connect, helo, rcpt, body, unknown, data
)

func serveMilter(r io.Reader, w io.Writer) error {
	br := bufio.NewReader(r)
	var login string
	var from []string
	send := func(cmd byte, data []byte) error {
		var hdr [5]byte
		binary.BigEndian.PutUint32(hdr[:4], uint32(len(data)+1))
		hdr[4] = cmd
		if _, err := w.Write(hdr[:]); err != nil {
			return err
		}
		_, err := w.Write(data)
		return err
	}
	for {
		var hdr [4]byte
		if _, err := io.ReadFull(br, hdr[:]); err != nil {
			return err
		}
		n := binary.BigEndian.Uint32(hdr[:])
		if n == 0 || n > 1<<20 {
			return errors.New("bad milter packet")
		}
		pkt := make([]byte, n)
		if _, err := io.ReadFull(br, pkt); err != nil {
			return err
		}
		cmd, data := pkt[0], pkt[1:]
		var err error
		switch cmd {
		case smfiOptneg:
			if len(data) < 12 {
				return errors.New("bad option negotiation")
			}
			out := make([]byte, 12)
			binary.BigEndian.PutUint32(out[0:], 6)
			binary.BigEndian.PutUint32(out[4:], 0) // never modifies messages
			binary.BigEndian.PutUint32(out[8:], binary.BigEndian.Uint32(data[8:])&smfipSkip)
			err = send(smfiOptneg, out)
		case smfiMacro:
			if len(data) > 0 && data[0] == smfiMail {
				kv := bytes.Split(data[1:], []byte{0})
				for i := 0; i+1 < len(kv); i += 2 {
					if string(kv[i]) == "{auth_authen}" {
						login = string(kv[i+1])
					}
				}
			}
		case smfiHeader:
			if f := bytes.SplitN(data, []byte{0}, 3); len(f) >= 2 {
				if name := strings.ToLower(string(f[0])); name == "from" || name == "sender" {
					from = append(from, string(f[1]))
				}
			}
			err = send(smfrCont, nil)
		case smfiEOM:
			reason := ""
			if login != "" {
				reason = checkFrom(login, from)
			}
			if reason == "" {
				err = send(smfrAccept, nil)
			} else {
				err = send(smfrReplyCd, []byte("550 5.7.1 Sender address rejected: "+reason+"\x00"))
			}
			login, from = "", nil
		case smfiAbort:
			login, from = "", nil
		case smfiQuitNC:
			login, from = "", nil
		case smfiQuit:
			return nil
		default:
			// MAIL, end of headers and any event not skipped: carry on.
			err = send(smfrCont, nil)
		}
		if err != nil {
			return err
		}
	}
}

// MigrateFromCheck puts the milter on the submission ports of servers set up
// by older versions. Run at agent start.
func MigrateFromCheck(ctx context.Context) error {
	if !Installed() {
		return nil
	}
	cur, err := run.Output(ctx, "postconf", "-h", "xpanel_submission_milters")
	if err == nil && strings.TrimSpace(cur) == submissionMilters {
		return nil
	}
	mu.Lock()
	defer mu.Unlock()
	if err := configureFromCheck(ctx); err != nil {
		return err
	}
	return run.Cmd(ctx, "postfix", "reload")
}

func configureFromCheck(ctx context.Context) error {
	if err := postconf(ctx, "-e", "xpanel_submission_milters = "+submissionMilters); err != nil {
		return err
	}
	for _, name := range []string{"submission", "submissions"} {
		if err := postconf(ctx, "-P", name+"/inet/smtpd_milters=$xpanel_submission_milters"); err != nil {
			return err
		}
	}
	return nil
}
