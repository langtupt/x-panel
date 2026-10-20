package accounts

import "context"

// enforceBandwidth blocks the sites of accounts that used up their package's
// monthly bandwidth, and unblocks them in a new month or after an upgrade.
// It runs after each stats sample.
func (s *Service) enforceBandwidth(ctx context.Context) error {
	type change struct {
		id    int64
		block bool
	}
	var changes []change
	rows, err := s.pool.Query(ctx, `
		WITH used AS (
			SELECT user_id, sum(bw_bytes) AS bytes FROM resource_samples
			WHERE at >= date_trunc('month', now()) GROUP BY user_id)
		SELECT u.id, COALESCE(p.bandwidth_gb > 0 AND COALESCE(used.bytes, 0) >= p.bandwidth_gb::bigint << 30, false) AS over,
			COALESCE(u.bw_blocked_month = date_trunc('month', now())::date, false) AS blocked
		FROM users u
		LEFT JOIN packages p ON p.id = u.package_id
		LEFT JOIN used ON used.user_id = u.id
		WHERE u.role = 'user' AND u.linux_uid IS NOT NULL`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var over, blocked bool
		if err := rows.Scan(&id, &over, &blocked); err != nil {
			rows.Close()
			return err
		}
		if over != blocked {
			changes = append(changes, change{id, over})
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, c := range changes {
		if err := s.setBandwidthBlock(ctx, c.id, c.block); err != nil {
			s.log.Warn("bandwidth limit", "user_id", c.id, "err", err)
		}
	}
	return nil
}

func (s *Service) setBandwidthBlock(ctx context.Context, id int64, block bool) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := `UPDATE users SET bw_blocked_month = NULL WHERE id = $1`
	if block {
		q = `UPDATE users SET bw_blocked_month = date_trunc('month', now())::date WHERE id = $1`
	}
	if _, err := tx.Exec(ctx, q, id); err != nil {
		return err
	}
	u, err := s.users.WithTx(tx).ByID(ctx, id)
	if err != nil {
		return err
	}
	if _, err := s.syncWeb(ctx, tx, u); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	action := "account.bandwidth_unblock"
	if block {
		action = "account.bandwidth_block"
	}
	s.log.Info(action, "user", u.Username)
	return nil
}
