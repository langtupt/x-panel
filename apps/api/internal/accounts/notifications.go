package accounts

import (
	"context"
	"time"

	"xpanel/apps/api/internal/users"
	"xpanel/pkg/agentproto"
)

type Notification struct {
	ID        int64      `json:"id"`
	Kind      string     `json:"kind"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	Link      string     `json:"link"`
	ReadAt    *time.Time `json:"read_at"`
	CreatedAt time.Time  `json:"created_at"`
}

// notify records an in-panel notification for the account and its parent
// reseller (if any), and mails the account when it has an address and has
// not turned mail alerts off. Best effort: failures are only logged.
func (s *Service) notify(ctx context.Context, u *users.User, kind, title, body, link string) {
	ids := []int64{u.ID}
	if u.ParentID != nil {
		ids = append(ids, *u.ParentID)
	}
	for _, id := range ids {
		if _, err := s.pool.Exec(ctx, `INSERT INTO notifications (user_id, kind, title, body, link) VALUES ($1,$2,$3,$4,$5)`,
			id, kind, title, body, link); err != nil {
			s.log.Warn("notification", "err", err)
		}
	}
	if u.Email == "" {
		return
	}
	if kind == "malware" {
		if c, err := s.malwareConfig(ctx, u.ID); err != nil || !c.NotifyEmail {
			return
		}
	}
	if err := s.agent.Call(ctx, agentproto.ActionNotifyEmail, agentproto.NotifyEmailParams{
		To: u.Email, Subject: "[X-Panel] " + title, Body: body}, nil); err != nil {
		s.log.Warn("notification email", "user", u.Username, "err", err)
	}
}

func (s *Service) Notifications(ctx context.Context, actor *users.User) ([]Notification, int, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, kind, title, body, link, read_at, created_at FROM notifications
		WHERE user_id=$1 ORDER BY id DESC LIMIT 50`, actor.ID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Notification{}
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.Kind, &n.Title, &n.Body, &n.Link, &n.ReadAt, &n.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, n)
	}
	var unread int
	err = s.pool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE user_id=$1 AND read_at IS NULL`, actor.ID).Scan(&unread)
	return out, unread, err
}

func (s *Service) MarkNotificationsRead(ctx context.Context, actor *users.User) error {
	_, err := s.pool.Exec(ctx, `UPDATE notifications SET read_at=now() WHERE user_id=$1 AND read_at IS NULL`, actor.ID)
	if err == nil {
		_, err = s.pool.Exec(ctx, `DELETE FROM notifications WHERE user_id=$1 AND created_at < now() - interval '90 days'`, actor.ID)
	}
	return err
}
