// Package validate holds input rules shared by the API and the agent; the
// agent re-checks everything it receives.
package validate

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	labelRe   = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	tldRe     = regexp.MustCompile(`^([a-z]{2,63}|xn--[a-z0-9-]{1,59})$`)
	segmentRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

// Domain accepts lower-case ASCII host names (IDNs in punycode), at least two labels.
func Domain(d string) error {
	if len(d) == 0 || len(d) > 253 {
		return fmt.Errorf("tên miền %q không hợp lệ", d)
	}
	labels := strings.Split(d, ".")
	if len(labels) < 2 || !tldRe.MatchString(labels[len(labels)-1]) {
		return fmt.Errorf("tên miền %q không hợp lệ", d)
	}
	for _, l := range labels {
		if !labelRe.MatchString(l) {
			return fmt.Errorf("tên miền %q không hợp lệ", d)
		}
	}
	return nil
}

// DocRoot is a relative path inside the site directory; "" means the site directory itself.
func DocRoot(p string) error {
	if p == "" {
		return nil
	}
	if len(p) > 200 {
		return errors.New("thư mục gốc quá dài")
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "." || seg == ".." || !segmentRe.MatchString(seg) {
			return fmt.Errorf("thư mục gốc %q không hợp lệ (đường dẫn tương đối, ví dụ: public)", p)
		}
	}
	return nil
}
