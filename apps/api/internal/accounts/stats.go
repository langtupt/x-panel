package accounts

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"xpanel/apps/api/internal/apperr"
	"xpanel/apps/api/internal/users"
	"xpanel/pkg/agentproto"
)

const (
	statsInterval  = 5 * time.Minute
	statsRetention = 35 * 24 * time.Hour
)

// cpuMark is the previous cumulative CPU reading of an account, to turn the
// cgroup counter into a percentage.
type cpuMark struct {
	usec uint64
	at   time.Time
}

var (
	cpuMu    sync.Mutex
	cpuMarks = map[string]cpuMark{}
)

// RunStats samples every hosting account's resource use every 5 minutes.
func (s *Service) RunStats(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(10 * time.Second):
	}
	interval := statsInterval
	// End-to-end tests sample faster.
	if d, err := time.ParseDuration(os.Getenv("XPANEL_STATS_INTERVAL")); err == nil && d >= 5*time.Second {
		interval = d
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		if err := s.sampleStats(ctx); err != nil && ctx.Err() == nil {
			s.log.Warn("resource stats", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (s *Service) sampleStats(ctx context.Context) error {
	type account struct {
		id       int64
		username string
	}
	var accounts []account
	byName := map[string]*agentproto.StatsAccount{}
	rows, err := s.pool.Query(ctx, `SELECT id, username FROM users WHERE role='user' AND linux_uid IS NOT NULL ORDER BY id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var a account
		if err := rows.Scan(&a.id, &a.username); err != nil {
			rows.Close()
			return err
		}
		accounts = append(accounts, a)
		byName[a.username] = &agentproto.StatsAccount{Username: a.username}
	}
	rows.Close()
	if len(accounts) == 0 {
		return nil
	}
	collectNames := func(q string, add func(*agentproto.StatsAccount, string)) error {
		rows, err := s.pool.Query(ctx, q)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var user, name string
			if err := rows.Scan(&user, &name); err != nil {
				return err
			}
			if a := byName[user]; a != nil {
				add(a, name)
			}
		}
		return rows.Err()
	}
	if err := collectNames(`SELECT u.username, s.domain FROM sites s JOIN users u ON u.id=s.user_id`,
		func(a *agentproto.StatsAccount, d string) { a.Domains = append(a.Domains, d) }); err != nil {
		return err
	}
	if err := collectNames(`SELECT u.username, d.domain FROM mail_domains d JOIN users u ON u.id=d.user_id`,
		func(a *agentproto.StatsAccount, d string) { a.MailDomains = append(a.MailDomains, d) }); err != nil {
		return err
	}
	dbOwner := map[string]string{}
	if err := collectNames(`SELECT u.username, d.name FROM databases d JOIN users u ON u.id=d.user_id`,
		func(a *agentproto.StatsAccount, name string) { dbOwner[name] = a.Username }); err != nil {
		return err
	}

	p := agentproto.StatsParams{}
	for _, a := range accounts {
		p.Accounts = append(p.Accounts, *byName[a.username])
	}
	var res agentproto.StatsResult
	if err := s.agent.Call(ctx, agentproto.ActionStatsCollect, p, &res); err != nil {
		return err
	}
	// Disk and database sizes are best effort: quotas may be off.
	var quota agentproto.QuotaReport
	_ = s.agent.Call(ctx, agentproto.ActionQuotaReport, nil, &quota)
	var sizes agentproto.DBSizes
	_ = s.agent.Call(ctx, agentproto.ActionDBSizes, nil, &sizes)
	dbBytes := map[string]int64{}
	for name, n := range sizes.Sizes {
		if owner, ok := dbOwner[name]; ok {
			dbBytes[owner] += n
		}
	}

	batch := 0
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	cpuMu.Lock()
	defer cpuMu.Unlock()
	for _, a := range accounts {
		m, ok := res.Accounts[a.username]
		if !ok {
			continue
		}
		var cpu *float64
		if prev, ok := cpuMarks[a.username]; ok && m.CPUUsageUsec >= prev.usec && res.At.After(prev.at) {
			v := float64(m.CPUUsageUsec-prev.usec) / float64(res.At.Sub(prev.at).Microseconds()) * 100
			cpu = &v
		}
		cpuMarks[a.username] = cpuMark{usec: m.CPUUsageUsec, at: res.At}
		var disk *int
		if quota.Enabled {
			if u, ok := quota.Users[a.username]; ok {
				d := u.DiskUsedMB
				disk = &d
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO resource_samples
			(user_id, at, cpu_pct, mem_mb, tasks, disk_mb, db_mb, mail_mb, bw_bytes, requests)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT DO NOTHING`,
			a.id, res.At, cpu, int(m.MemoryBytes>>20), m.Tasks, disk, int(dbBytes[a.username]>>20),
			int(m.MailBytes>>20), m.BandwidthBytes, m.Requests); err != nil {
			return err
		}
		batch++
	}
	if _, err := tx.Exec(ctx, `DELETE FROM resource_samples WHERE at < $1`, time.Now().Add(-statsRetention)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// AccountStats is one account's current use, totals for the range and a
// chart series.
type AccountStats struct {
	UserID     int64       `json:"user_id"`
	Username   string      `json:"username"`
	ParentName *string     `json:"parent_name"`
	Status     string      `json:"status"`
	Package    *StatsLimit `json:"package"`
	Latest     *StatsPoint `json:"latest"`
	// Totals over the requested range and the current calendar month.
	BandwidthBytes      int64        `json:"bandwidth_bytes"`
	Requests            int64        `json:"requests"`
	MonthBandwidthBytes int64        `json:"month_bandwidth_bytes"`
	Points              []StatsPoint `json:"points"`
}

type StatsLimit struct {
	Name        string `json:"name"`
	DiskMB      int    `json:"disk_mb"`
	CPUPct      int    `json:"cpu_pct"`
	MemMB       int    `json:"mem_mb"`
	BandwidthGB int    `json:"bandwidth_gb"`
}

type StatsPoint struct {
	At       time.Time `json:"at"`
	CPUPct   *float64  `json:"cpu_pct"`
	MemMB    int       `json:"mem_mb"`
	Tasks    int       `json:"tasks,omitempty"`
	DiskMB   *int      `json:"disk_mb,omitempty"`
	DBMB     int       `json:"db_mb,omitempty"`
	MailMB   int       `json:"mail_mb,omitempty"`
	BWBytes  int64     `json:"bw_bytes"`
	Requests int64     `json:"requests,omitempty"`
}

var statRanges = map[string]struct{ span, bucket time.Duration }{
	"24h": {24 * time.Hour, 30 * time.Minute},
	"7d":  {7 * 24 * time.Hour, 3 * time.Hour},
	"30d": {30 * 24 * time.Hour, 12 * time.Hour},
}

// Stats lists the accounts the actor may see: every hosting account (admin),
// the reseller's customers, or only the user itself.
func (s *Service) Stats(ctx context.Context, actor *users.User, rangeName string, only int64) ([]AccountStats, error) {
	rg, ok := statRanges[rangeName]
	if !ok {
		return nil, apperr.Invalid("khoảng thời gian phải là 24h, 7d hoặc 30d")
	}
	where, args := `u.role='user' AND u.linux_uid IS NOT NULL`, []any{}
	switch actor.Role {
	case users.RoleAdmin:
	case users.RoleReseller:
		where += ` AND u.parent_id=$1`
		args = append(args, actor.ID)
	default:
		where += ` AND u.id=$1`
		args = append(args, actor.ID)
	}
	if only > 0 {
		args = append(args, only)
		where += fmt.Sprintf(` AND u.id=$%d`, len(args))
	}
	rows, err := s.pool.Query(ctx, `SELECT u.id, u.username, pu.username, u.status,
		p.name, p.disk_mb, p.cpu_pct, p.mem_mb, p.bandwidth_gb
		FROM users u LEFT JOIN users pu ON pu.id=u.parent_id LEFT JOIN packages p ON p.id=u.package_id
		WHERE `+where+` ORDER BY u.username`, args...)
	if err != nil {
		return nil, err
	}
	var out []AccountStats
	for rows.Next() {
		var a AccountStats
		var pname *string
		var l StatsLimit
		var disk, cpu, mem, bw *int
		if err := rows.Scan(&a.UserID, &a.Username, &a.ParentName, &a.Status, &pname, &disk, &cpu, &mem, &bw); err != nil {
			rows.Close()
			return nil, err
		}
		if pname != nil {
			l.Name, l.DiskMB, l.CPUPct, l.MemMB, l.BandwidthGB = *pname, *disk, *cpu, *mem, *bw
			a.Package = &l
		}
		out = append(out, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if only > 0 && len(out) == 0 {
		return nil, apperr.NotFound("không tìm thấy tài khoản")
	}

	since := time.Now().Add(-rg.span)
	monthStart := time.Date(time.Now().Year(), time.Now().Month(), 1, 0, 0, 0, 0, time.Local)
	for i := range out {
		a := &out[i]
		var latest StatsPoint
		err := s.pool.QueryRow(ctx, `SELECT at, cpu_pct, mem_mb, tasks, disk_mb, db_mb, mail_mb, bw_bytes, requests
			FROM resource_samples WHERE user_id=$1 ORDER BY at DESC LIMIT 1`, a.UserID).
			Scan(&latest.At, &latest.CPUPct, &latest.MemMB, &latest.Tasks, &latest.DiskMB, &latest.DBMB,
				&latest.MailMB, &latest.BWBytes, &latest.Requests)
		if err == nil {
			a.Latest = &latest
		}
		if err := s.pool.QueryRow(ctx, `SELECT COALESCE(sum(bw_bytes),0), COALESCE(sum(requests),0)
			FROM resource_samples WHERE user_id=$1 AND at >= $2`, a.UserID, since).Scan(&a.BandwidthBytes, &a.Requests); err != nil {
			return nil, err
		}
		if err := s.pool.QueryRow(ctx, `SELECT COALESCE(sum(bw_bytes),0) FROM resource_samples
			WHERE user_id=$1 AND at >= $2`, a.UserID, monthStart).Scan(&a.MonthBandwidthBytes); err != nil {
			return nil, err
		}
		prows, err := s.pool.Query(ctx, `SELECT date_bin($3::interval, at, TIMESTAMPTZ '2000-01-01') AS b,
			avg(cpu_pct)::float8, max(mem_mb), sum(bw_bytes)::bigint
			FROM resource_samples WHERE user_id=$1 AND at >= $2 GROUP BY b ORDER BY b`,
			a.UserID, since, fmt.Sprintf("%d seconds", int(rg.bucket.Seconds())))
		if err != nil {
			return nil, err
		}
		a.Points = []StatsPoint{}
		for prows.Next() {
			var pt StatsPoint
			if err := prows.Scan(&pt.At, &pt.CPUPct, &pt.MemMB, &pt.BWBytes); err != nil {
				prows.Close()
				return nil, err
			}
			a.Points = append(a.Points, pt)
		}
		prows.Close()
	}
	if out == nil {
		out = []AccountStats{}
	}
	return out, nil
}
