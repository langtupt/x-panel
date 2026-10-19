package packages

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"xpanel/apps/api/internal/users"
	"xpanel/pkg/agentproto"
)

// PHPVersions available for hosting (sury on Debian, ondrej PPA on Ubuntu).
var PHPVersions = agentproto.PHPVersions

// Validate checks a package's own fields and normalises fields that do not
// apply to its kind.
func Validate(p *Package) error {
	p.Name = strings.TrimSpace(p.Name)
	if n := utf8.RuneCountInString(p.Name); n < 1 || n > 64 {
		return fmt.Errorf("tên gói phải từ 1 đến 64 ký tự")
	}
	if p.Kind != users.RoleReseller && p.Kind != users.RoleUser {
		return fmt.Errorf("loại gói phải là reseller hoặc user")
	}
	for _, f := range fields(&p.Limits) {
		if *f.v < 0 {
			return fmt.Errorf("%s không được âm", f.label)
		}
	}
	if p.CPUPct > 6400 {
		return fmt.Errorf("CPU tối đa 6400%% (64 nhân)")
	}
	if p.PHPVersions == nil {
		p.PHPVersions = []string{}
	}
	for _, v := range p.PHPVersions {
		if !agentproto.ValidPHPVersion(v) {
			return fmt.Errorf("phiên bản PHP %q không được hỗ trợ", v)
		}
	}
	slices.Sort(p.PHPVersions)
	p.PHPVersions = slices.Compact(p.PHPVersions)
	if p.Kind == users.RoleUser {
		p.MaxUsers = 0
		p.Oversell = false
	}
	return nil
}

type field struct {
	label string
	v     *int
}

func fields(l *Limits) []field {
	return []field{
		{"Dung lượng ổ cứng", &l.DiskMB}, {"Số inode", &l.Inodes}, {"Băng thông", &l.BandwidthGB},
		{"Số website", &l.MaxSites}, {"Số database", &l.MaxDBs}, {"Số tài khoản FTP", &l.MaxFTP},
		{"Số cron", &l.MaxCron}, {"Số hộp thư", &l.MaxMailboxes}, {"Số tài khoản", &l.MaxUsers}, {"CPU", &l.CPUPct}, {"RAM", &l.MemMB},
	}
}

// WithinParent checks that a reseller's user package does not exceed any
// per-account limit of the reseller's own package.
func WithinParent(child, parent Limits) error {
	pf, cf := fields(&parent), fields(&child)
	for i := range pf {
		if pf[i].label == "Số tài khoản" {
			continue
		}
		p, c := *pf[i].v, *cf[i].v
		if p > 0 && (c == 0 || c > p) {
			return fmt.Errorf("%s vượt giới hạn gói đại lý (tối đa %d)", cf[i].label, p)
		}
	}
	if len(parent.PHPVersions) > 0 {
		if len(child.PHPVersions) == 0 {
			return fmt.Errorf("phải chọn phiên bản PHP trong số: %s", strings.Join(parent.PHPVersions, ", "))
		}
		for _, v := range child.PHPVersions {
			if !slices.Contains(parent.PHPVersions, v) {
				return fmt.Errorf("PHP %s không có trong gói đại lý", v)
			}
		}
	}
	return nil
}

// CheckCapacity verifies a reseller can take on addUsers more accounts and
// addDiskMB more allocated disk. The account limit always applies; the disk
// pool only when the reseller package does not allow overselling.
func CheckCapacity(reseller Limits, oversell bool, curUsers, curDiskMB, addUsers, addDiskMB int) error {
	if reseller.MaxUsers > 0 && curUsers+addUsers > reseller.MaxUsers {
		return fmt.Errorf("đã đạt giới hạn %d tài khoản của gói đại lý", reseller.MaxUsers)
	}
	if !oversell && reseller.DiskMB > 0 && curDiskMB+addDiskMB > reseller.DiskMB {
		return fmt.Errorf("vượt dung lượng gói đại lý: đã cấp %d MB, thêm %d MB, tối đa %d MB",
			curDiskMB, addDiskMB, reseller.DiskMB)
	}
	return nil
}
