package validate

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var (
	cronFieldRe = regexp.MustCompile(`^(\*|[0-9]+(-[0-9]+)?)(/[0-9]+)?(,(\*|[0-9]+(-[0-9]+)?)(/[0-9]+)?)*$`)
	cronMacros  = []string{"@hourly", "@daily", "@weekly", "@monthly", "@yearly", "@annually", "@midnight"}
	cronFields  = []struct {
		name   string
		lo, hi int
	}{{"phút", 0, 59}, {"giờ", 0, 23}, {"ngày", 1, 31}, {"tháng", 1, 12}, {"thứ", 0, 7}}
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
	for i, x := range f {
		if !cronFieldRe.MatchString(x) {
			return errors.New("lịch chạy chỉ dùng số, *, -, / và dấu phẩy")
		}
		if err := cronRange(x, cronFields[i].lo, cronFields[i].hi); err != nil {
			return fmt.Errorf("trường %s %q: %s (cho phép %d–%d)", cronFields[i].name, x, err, cronFields[i].lo, cronFields[i].hi)
		}
	}
	return nil
}

// cronRange checks the numbers of one (syntactically valid) field.
func cronRange(field string, lo, hi int) error {
	for _, item := range strings.Split(field, ",") {
		base, step, hasStep := strings.Cut(item, "/")
		if hasStep {
			if n, _ := strconv.Atoi(step); n < 1 {
				return errors.New("bước phải lớn hơn 0")
			}
		}
		if base == "*" {
			continue
		}
		a, b, isRange := strings.Cut(base, "-")
		from, _ := strconv.Atoi(a)
		to := from
		if isRange {
			to, _ = strconv.Atoi(b)
		}
		if from < lo || to > hi {
			return errors.New("giá trị ngoài khoảng")
		}
		if from > to {
			return errors.New("khoảng bị ngược")
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
