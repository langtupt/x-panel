package validate

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	localPartRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9._-]{0,62}[a-z0-9])?$`)
	// Destinations of forwarders may be any reasonable external address.
	externalLocalRe = regexp.MustCompile(`^[A-Za-z0-9._%+-]{1,64}$`)
)

// LocalPart checks the part before @ of a mailbox hosted here.
func LocalPart(s string) error {
	if !localPartRe.MatchString(s) || strings.Contains(s, "..") {
		return fmt.Errorf("tên hộp thư %q không hợp lệ (chữ thường, số, dấu . _ -)", s)
	}
	return nil
}

// MailAddress checks an address hosted on this server (lower case).
func MailAddress(addr string) error {
	local, domain, ok := strings.Cut(addr, "@")
	if !ok {
		return fmt.Errorf("địa chỉ %q không hợp lệ", addr)
	}
	if err := LocalPart(local); err != nil {
		return err
	}
	return Domain(domain)
}

// ForwardAddress checks a forwarding destination (may be external).
func ForwardAddress(addr string) error {
	local, domain, ok := strings.Cut(addr, "@")
	if !ok || !externalLocalRe.MatchString(local) || strings.Contains(local, "..") {
		return fmt.Errorf("địa chỉ chuyển tiếp %q không hợp lệ", addr)
	}
	return Domain(strings.ToLower(domain))
}

// BcryptHash accepts only bcrypt hashes (the agent never sees plaintext).
func BcryptHash(h string) error {
	if len(h) != 60 || !(strings.HasPrefix(h, "$2a$") || strings.HasPrefix(h, "$2b$") || strings.HasPrefix(h, "$2y$")) ||
		strings.ContainsAny(h, ":\n\r ") {
		return errors.New("invalid password hash")
	}
	return nil
}
