package validate

import (
	"errors"
	"regexp"
	"slices"
	"strings"
)

var (
	cronFieldRe = regexp.MustCompile(`^(\*|[0-9]+(-[0-9]+)?)(/[0-9]+)?(,(\*|[0-9]+(-[0-9]+)?)(/[0-9]+)?)*$`)
	cronMacros  = []string{"@hourly", "@daily", "@weekly", "@monthly", "@yearly", "@annually", "@midnight"}
)

// CronSchedule accepts five numeric crontab fields or a macro (not @reboot).
func CronSchedule(s string) error {
	s = strings.TrimSpace(s)
	if slices.Contains(cronMacros, s) {
		return nil
	}
	f := strings.Fields(s)
	if len(f) != 5 {
		return errors.New("lịch chạy phải có 5 trường (phút giờ ngày tháng thứ) hoặc @hourly/@daily/@weekly/@monthly")
	}
	for _, x := range f {
		if !cronFieldRe.MatchString(x) {
			return errors.New("lịch chạy chỉ dùng số, *, -, / và dấu phẩy")
		}
	}
	return nil
}

// CronCommand is a single line of at most 1000 characters.
func CronCommand(c string) error {
	if strings.TrimSpace(c) == "" || len(c) > 1000 || strings.ContainsAny(c, "\r\n\x00") {
		return errors.New("lệnh cron phải là một dòng, tối đa 1000 ký tự")
	}
	return nil
}
