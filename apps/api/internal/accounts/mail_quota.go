package accounts

import (
	"context"

	"xpanel/apps/api/internal/apperr"
	"xpanel/apps/api/internal/db"
	"xpanel/apps/api/internal/packages"
	"xpanel/apps/api/internal/users"
)

// Mail is stored as the shared vmail user, outside the account's filesystem
// quota. When the package limits disk space, every mailbox must have a size
// and their sum is taken off the files quota, so files + mail stay within
// the package. minFilesMB is always left for the websites.
const minFilesMB = 100

// mailAllocMB is the total size of an account's mailboxes.
func mailAllocMB(ctx context.Context, q db.Querier, username string) (int, error) {
	var n int
	err := q.QueryRow(ctx, `SELECT COALESCE(sum(mb.quota_mb), 0) FROM mailboxes mb
		JOIN mail_domains d ON d.id = mb.domain_id JOIN users u ON u.id = d.user_id
		WHERE u.username = $1`, username).Scan(&n)
	return n, err
}

// checkMailQuota validates a mailbox size of quotaMB replacing oldMB within
// the owner's package.
func (s *Service) checkMailQuota(ctx context.Context, q db.Querier, owner *users.User, oldMB, quotaMB int) error {
	if owner.PackageID == nil {
		return nil
	}
	p, err := packages.NewRepo(q).ByID(ctx, *owner.PackageID)
	if err != nil {
		return err
	}
	if p.DiskMB <= 0 {
		return nil
	}
	if quotaMB <= 0 {
		return apperr.Invalid("gói %q giới hạn %d MB: hộp thư phải có dung lượng cụ thể", p.Name, p.DiskMB)
	}
	alloc, err := mailAllocMB(ctx, q, owner.Username)
	if err != nil {
		return err
	}
	if free := p.DiskMB - minFilesMB - (alloc - oldMB); quotaMB > free {
		if free < 0 {
			free = 0
		}
		return apperr.Conflict("gói %q còn %d MB cho email (tổng %d MB, đã cấp cho hộp thư %d MB, giữ %d MB cho website)",
			p.Name, free, p.DiskMB, alloc-oldMB, minFilesMB)
	}
	return nil
}

// filesQuotaMB is the files quota of an account after its mail allocation.
func filesQuotaMB(diskMB, mailMB int) int {
	if diskMB <= 0 {
		return 0
	}
	if v := diskMB - mailMB; v >= minFilesMB {
		return v
	}
	return minFilesMB
}

// reapplyQuota re-reads the package and mail allocation and sets the quota.
func (s *Service) reapplyQuota(ctx context.Context, q db.Querier, owner *users.User) error {
	if owner.PackageID == nil || owner.LinuxUID == nil {
		return nil
	}
	p, err := packages.NewRepo(q).ByID(ctx, *owner.PackageID)
	if err != nil {
		return err
	}
	return s.applyQuota(ctx, q, owner.Username, p)
}

// checkMailFits refuses a package too small for the account's mailboxes.
func checkMailFits(ctx context.Context, q db.Querier, username string, p *packages.Package) error {
	var alloc, unlimited int
	if err := q.QueryRow(ctx, `SELECT COALESCE(sum(mb.quota_mb), 0), count(*) FILTER (WHERE mb.quota_mb = 0)
		FROM mailboxes mb JOIN mail_domains d ON d.id = mb.domain_id JOIN users u ON u.id = d.user_id
		WHERE u.username = $1`, username).Scan(&alloc, &unlimited); err != nil {
		return err
	}
	if unlimited > 0 {
		return apperr.Conflict("tài khoản có %d hộp thư không giới hạn dung lượng; đặt dung lượng cho chúng trước khi chuyển sang gói %q", unlimited, p.Name)
	}
	if alloc+minFilesMB > p.DiskMB {
		return apperr.Conflict("hộp thư của tài khoản đã được cấp %d MB, gói %q (%d MB) không đủ", alloc, p.Name, p.DiskMB)
	}
	return nil
}
