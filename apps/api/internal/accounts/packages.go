package accounts

import (
	"context"
	"errors"

	"xpanel/apps/api/internal/apperr"
	"xpanel/apps/api/internal/packages"
	"xpanel/apps/api/internal/users"
)

func (s *Service) ListPackages(ctx context.Context, actor *users.User) ([]packages.Package, error) {
	return s.pkgs.ListByOwner(ctx, actor.ID)
}

func (s *Service) CreatePackage(ctx context.Context, actor *users.User, p packages.Package) (*packages.Package, error) {
	if actor.Role == users.RoleReseller && p.Kind != users.RoleUser {
		return nil, apperr.Forbidden("đại lý chỉ tạo được gói cho người dùng")
	}
	if err := packages.Validate(&p); err != nil {
		return nil, apperr.Invalid("%s", err.Error())
	}
	if err := s.withinResellerPackage(ctx, actor, p.Limits); err != nil {
		return nil, err
	}
	p.OwnerID = actor.ID
	if err := s.pkgs.Create(ctx, &p); errors.Is(err, packages.ErrTaken) {
		return nil, apperr.Conflict("%s", err.Error())
	} else if err != nil {
		return nil, err
	}
	return &p, nil
}

// UpdatePackage changes limits and re-applies the disk quota of every
// account on the package. The kind of a package cannot change.
func (s *Service) UpdatePackage(ctx context.Context, actor *users.User, id int64, in packages.Package) (*packages.Package, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	pr := s.pkgs.WithTx(tx)

	cur, err := s.ownPackage(ctx, pr, actor, id)
	if err != nil {
		return nil, err
	}
	in.ID, in.OwnerID, in.Kind, in.CreatedAt = cur.ID, cur.OwnerID, cur.Kind, cur.CreatedAt
	if err := packages.Validate(&in); err != nil {
		return nil, apperr.Invalid("%s", err.Error())
	}
	if err := s.withinResellerPackage(ctx, actor, in.Limits); err != nil {
		return nil, err
	}

	if actor.Role == users.RoleReseller && actor.PackageID != nil {
		if err := s.users.WithTx(tx).LockForUpdate(ctx, actor.ID); err != nil {
			return nil, err
		}
		rp, err := pr.ByID(ctx, *actor.PackageID)
		if err != nil {
			return nil, err
		}
		_, otherDisk, err := pr.Allocation(ctx, actor.ID, 0, id)
		if err != nil {
			return nil, err
		}
		if err := packages.CheckCapacity(rp.Limits, rp.Oversell, 0, otherDisk, 0, cur.UsersCount*in.DiskMB); err != nil {
			return nil, apperr.Conflict("%s", err.Error())
		}
	}

	if err := pr.Update(ctx, &in); errors.Is(err, packages.ErrTaken) {
		return nil, apperr.Conflict("%s", err.Error())
	} else if err != nil {
		return nil, err
	}
	if in.Kind == users.RoleUser && (in.DiskMB != cur.DiskMB || in.Inodes != cur.Inodes) {
		names, err := pr.Usernames(ctx, id)
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			if err := s.applyQuota(ctx, tx, name, &in); err != nil {
				return nil, err
			}
		}
	}
	if in.Kind == users.RoleUser && (in.CPUPct != cur.CPUPct || in.MemMB != cur.MemMB) {
		names, err := pr.Usernames(ctx, id)
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			u, err := s.users.WithTx(tx).ByUsername(ctx, name)
			if err != nil {
				return nil, err
			}
			if _, err := s.syncWeb(ctx, tx, u); err != nil {
				return nil, err
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.pkgs.ByID(ctx, id)
}

func (s *Service) DeletePackage(ctx context.Context, actor *users.User, id int64) (*packages.Package, error) {
	p, err := s.ownPackage(ctx, s.pkgs, actor, id)
	if err != nil {
		return nil, err
	}
	if p.UsersCount > 0 {
		return nil, apperr.Conflict("gói đang được %d tài khoản sử dụng", p.UsersCount)
	}
	return p, s.pkgs.Delete(ctx, id)
}

func (s *Service) ownPackage(ctx context.Context, pr *packages.Repo, actor *users.User, id int64) (*packages.Package, error) {
	p, err := pr.ByID(ctx, id)
	if errors.Is(err, packages.ErrNotFound) || (err == nil && p.OwnerID != actor.ID) {
		return nil, apperr.NotFound("không tìm thấy gói dịch vụ")
	}
	return p, err
}

// withinResellerPackage: a reseller's user packages may not exceed the
// per-account limits of the reseller's own package.
func (s *Service) withinResellerPackage(ctx context.Context, actor *users.User, l packages.Limits) error {
	if actor.Role != users.RoleReseller || actor.PackageID == nil {
		return nil
	}
	rp, err := s.pkgs.ByID(ctx, *actor.PackageID)
	if err != nil {
		return err
	}
	if err := packages.WithinParent(l, rp.Limits); err != nil {
		return apperr.Invalid("%s", err.Error())
	}
	return nil
}

// Overview is what a reseller or user sees about their own plan.
type Overview struct {
	Package      *packages.Package `json:"package"`
	QuotaEnabled bool              `json:"quota_enabled"`
	DiskUsedMB   *int              `json:"disk_used_mb"`
	// Reseller only: what has been handed out to customers.
	AllocatedUsers  *int `json:"allocated_users,omitempty"`
	AllocatedDiskMB *int `json:"allocated_disk_mb,omitempty"`
}

func (s *Service) Overview(ctx context.Context, me *users.User) (*Overview, error) {
	ov := &Overview{}
	if me.PackageID != nil {
		p, err := s.pkgs.ByID(ctx, *me.PackageID)
		if err != nil {
			return nil, err
		}
		ov.Package = p
	}
	switch me.Role {
	case users.RoleUser:
		list := []users.User{*me}
		ov.QuotaEnabled = s.AttachUsage(ctx, list)
		ov.DiskUsedMB = list[0].DiskUsedMB
	case users.RoleReseller:
		n, disk, err := s.pkgs.Allocation(ctx, me.ID, 0, 0)
		if err != nil {
			return nil, err
		}
		ov.AllocatedUsers, ov.AllocatedDiskMB = &n, &disk
	}
	return ov, nil
}
