package accounts

import (
	"context"
	"errors"
	"slices"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"xpanel/apps/api/internal/apperr"
	"xpanel/apps/api/internal/db"
	"xpanel/apps/api/internal/mailrepo"
	"xpanel/apps/api/internal/settings"
	"xpanel/apps/api/internal/users"
	"xpanel/pkg/agentproto"
	"xpanel/pkg/validate"
)

// Mailbox logins are checked by Dovecot on every connection (with a cache);
// a lower cost than panel passwords keeps IMAP logins fast.
const mailBcryptCost = 10

type MailDomainInput struct {
	UserID *int64 `json:"user_id"`
	Domain string `json:"domain"`
}

type MailboxInput struct {
	LocalPart string `json:"local_part"`
	Password  string `json:"password"`
	QuotaMB   int    `json:"quota_mb"`
}

type MailboxUpdate struct {
	Password *string `json:"password"`
	QuotaMB  *int    `json:"quota_mb"`
}

type AliasInput struct {
	LocalPart    string   `json:"local_part"`
	Destinations []string `json:"destinations"`
}

type MailDomainDetail struct {
	*mailrepo.Domain
	MailboxList []mailrepo.Mailbox `json:"mailbox_list"`
	AliasList   []mailrepo.Alias   `json:"alias_list"`
	DNS         []MailDNSRecord    `json:"dns"`
	// MB the package still allows for new or bigger mailboxes; nil = no limit.
	MailFreeMB *int `json:"mail_free_mb"`
}

type MailDNSRecord struct {
	Type     string `json:"type"`
	Name     string `json:"name"`
	Value    string `json:"value"`
	Priority int    `json:"priority,omitempty"`
}

type MailStatus struct {
	agentproto.MailStatus
	ServerIP string `json:"server_ip"`
}

func (s *Service) mail() *mailrepo.Repo { return mailrepo.NewRepo(s.pool) }

func (s *Service) MailStatus(ctx context.Context) (*MailStatus, error) {
	var st MailStatus
	if err := s.agent.Call(ctx, agentproto.ActionMailStatus, nil, &st.MailStatus); err != nil {
		return nil, apperr.Agent(err)
	}
	return &st, nil
}

// syncMail pushes the complete mail configuration to the agent; q decides
// which (possibly uncommitted) state is sent.
func (s *Service) syncMail(ctx context.Context, q db.Querier) (*agentproto.MailSyncResult, error) {
	r := mailrepo.NewRepo(q)
	domains, err := r.AllDomains(ctx)
	if err != nil {
		return nil, err
	}
	boxes, suspended, err := r.MailboxesForSync(ctx)
	if err != nil {
		return nil, err
	}
	aliases, err := r.Aliases(ctx, 0)
	if err != nil {
		return nil, err
	}
	p := agentproto.MailSyncParams{}
	if p.Hostname, err = settings.NewRepo(q).Get(ctx, settings.PanelDomain); err != nil {
		return nil, err
	}
	for _, d := range domains {
		p.Domains = append(p.Domains, agentproto.MailDomain{Domain: d.Domain})
	}
	for _, b := range boxes {
		p.Mailboxes = append(p.Mailboxes, agentproto.MailBox{Address: b.Address, PasswordHash: b.PasswordHash,
			QuotaMB: b.QuotaMB, Suspended: suspended[b.ID]})
	}
	for _, a := range aliases {
		p.Aliases = append(p.Aliases, agentproto.MailForward{Source: a.Address, Destinations: a.Destinations})
	}
	var res agentproto.MailSyncResult
	if err := s.agent.Call(ctx, agentproto.ActionMailSync, p, &res); err != nil {
		return nil, apperr.Agent(err)
	}
	for domain, rec := range res.DKIM {
		if err := r.SetDKIM(ctx, domain, rec.Name, rec.Value); err != nil {
			return nil, err
		}
	}
	return &res, nil
}

// accessibleMailDomain hides domains of accounts the actor may not manage.
func (s *Service) accessibleMailDomain(ctx context.Context, actor *users.User, id int64) (*mailrepo.Domain, *users.User, error) {
	d, err := s.mail().Domain(ctx, id)
	if errors.Is(err, mailrepo.ErrNotFound) {
		return nil, nil, apperr.NotFound("không tìm thấy tên miền mail")
	}
	if err != nil {
		return nil, nil, err
	}
	owner, err := s.users.ByID(ctx, d.UserID)
	if err != nil {
		return nil, nil, err
	}
	if owner.ID != actor.ID && !users.CanManage(actor, owner) {
		return nil, nil, apperr.NotFound("không tìm thấy tên miền mail")
	}
	return d, owner, nil
}

func (s *Service) ListMailDomains(ctx context.Context, actor *users.User) ([]mailrepo.Domain, error) {
	return s.mail().Domains(ctx, actor.ID, string(actor.Role))
}

func (s *Service) mailHostname(ctx context.Context) string {
	if h, _ := settings.NewRepo(s.pool).Get(ctx, settings.PanelDomain); h != "" {
		return h
	}
	var st agentproto.MailStatus
	if err := s.agent.Call(ctx, agentproto.ActionMailStatus, nil, &st); err == nil {
		return st.Hostname
	}
	return ""
}

func (s *Service) MailDomain(ctx context.Context, actor *users.User, id int64) (*MailDomainDetail, error) {
	d, owner, err := s.accessibleMailDomain(ctx, actor, id)
	if err != nil {
		return nil, err
	}
	out := &MailDomainDetail{Domain: d}
	if out.MailboxList, err = s.mail().Mailboxes(ctx, id); err != nil {
		return nil, err
	}
	var usage agentproto.MailUsage
	if err := s.agent.Call(ctx, agentproto.ActionMailUsage, agentproto.MailUsageParams{Domain: d.Domain}, &usage); err == nil {
		for i := range out.MailboxList {
			// No directory yet: nothing delivered.
			mb := float64(usage.Bytes[out.MailboxList[i].LocalPart]) / (1 << 20)
			out.MailboxList[i].UsedMB = &mb
		}
	}
	if owner.PackageID != nil {
		if p, err := s.pkgs.ByID(ctx, *owner.PackageID); err == nil && p.DiskMB > 0 {
			if alloc, err := mailAllocMB(ctx, s.pool, owner.Username); err == nil {
				free := max(p.DiskMB-minFilesMB-alloc, 0)
				out.MailFreeMB = &free
			}
		}
	}
	if out.AliasList, err = s.mail().Aliases(ctx, id); err != nil {
		return nil, err
	}
	var info agentproto.SystemInfo
	_ = s.agent.Call(ctx, agentproto.ActionSystemInfo, nil, &info)
	host := s.mailHostname(ctx)
	// A hostname that cannot be published in DNS is useless as MX target.
	if host == "" || !strings.Contains(host, ".") || strings.HasSuffix(host, ".localdomain") || strings.HasSuffix(host, ".local") {
		host = "mail." + d.Domain
	}
	out.DNS = []MailDNSRecord{
		{Type: "MX", Name: d.Domain, Value: host, Priority: 10},
		{Type: "TXT", Name: d.Domain, Value: "v=spf1 mx a:" + host + " ~all"},
		{Type: "TXT", Name: "_dmarc." + d.Domain, Value: "v=DMARC1; p=none; rua=mailto:postmaster@" + d.Domain},
	}
	if d.DKIMName != "" {
		out.DNS = append(out.DNS, MailDNSRecord{Type: "TXT", Name: d.DKIMName, Value: d.DKIMValue})
	}
	for _, ip := range info.IPAddresses {
		if strings.Count(ip, ".") == 3 && !strings.HasPrefix(ip, "127.") {
			out.DNS = append([]MailDNSRecord{{Type: "A", Name: host, Value: ip}}, out.DNS...)
			break
		}
	}
	return out, nil
}

func (s *Service) CreateMailDomain(ctx context.Context, actor *users.User, in MailDomainInput) (*mailrepo.Domain, error) {
	owner, err := s.siteOwner(ctx, actor, in.UserID)
	if err != nil {
		return nil, err
	}
	domain := strings.ToLower(strings.TrimSpace(in.Domain))
	if err := validate.Domain(domain); err != nil {
		return nil, apperr.Invalid("%s", err.Error())
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err := s.users.WithTx(tx).LockForUpdate(ctx, owner.ID); err != nil {
		return nil, err
	}
	r := mailrepo.NewRepo(tx)
	if taken, err := r.SiteOwnedByOther(ctx, domain, owner.ID); err != nil {
		return nil, err
	} else if taken {
		return nil, apperr.Conflict("tên miền %s thuộc website của tài khoản khác", domain)
	}
	d := &mailrepo.Domain{UserID: owner.ID, Username: owner.Username, Domain: domain}
	if err := r.CreateDomain(ctx, d); errors.Is(err, mailrepo.ErrTaken) {
		return nil, apperr.Conflict("tên miền %s đã có dịch vụ mail", domain)
	} else if err != nil {
		return nil, err
	}
	if _, err := s.syncMail(ctx, tx); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		_, _ = s.syncMail(context.WithoutCancel(ctx), s.pool)
		return nil, err
	}
	return s.mail().Domain(ctx, d.ID)
}

func (s *Service) DeleteMailDomain(ctx context.Context, actor *users.User, id int64) (*mailrepo.Domain, error) {
	d, owner, err := s.accessibleMailDomain(ctx, actor, id)
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err := mailrepo.NewRepo(tx).DeleteDomain(ctx, id); err != nil {
		return nil, err
	}
	if _, err := s.syncMail(ctx, tx); err != nil {
		return nil, err
	}
	// The freed mail space goes back to the files quota.
	if err := s.reapplyQuota(ctx, tx, owner); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	// Mail is no longer routed here; remove the stored messages and DKIM key.
	if err := s.agent.Call(ctx, agentproto.ActionMailDeleteBox, agentproto.MailDeleteParams{Domain: d.Domain}, nil); err != nil {
		return nil, apperr.Agent(err)
	}
	return d, nil
}

func hashMailPassword(pw string) (string, error) {
	if err := validate.DBPassword(pw); err != nil {
		return "", apperr.Invalid("mật khẩu hộp thư: 10–64 ký tự, gồm chữ, số và !@#$%%^&*()_+=.,:;?~-")
	}
	b, err := bcrypt.GenerateFromPassword([]byte(pw), mailBcryptCost)
	return string(b), err
}

func (s *Service) CreateMailbox(ctx context.Context, actor *users.User, domainID int64, in MailboxInput) (*mailrepo.Mailbox, error) {
	d, owner, err := s.accessibleMailDomain(ctx, actor, domainID)
	if err != nil {
		return nil, err
	}
	local := strings.ToLower(strings.TrimSpace(in.LocalPart))
	if err := validate.LocalPart(local); err != nil {
		return nil, apperr.Invalid("%s", err.Error())
	}
	if in.QuotaMB < 0 || in.QuotaMB > 1<<20 {
		return nil, apperr.Invalid("dung lượng hộp thư không hợp lệ")
	}
	hash, err := hashMailPassword(in.Password)
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err := s.users.WithTx(tx).LockForUpdate(ctx, owner.ID); err != nil {
		return nil, err
	}
	r := mailrepo.NewRepo(tx)
	if owner.PackageID != nil {
		p, err := s.pkgs.WithTx(tx).ByID(ctx, *owner.PackageID)
		if err != nil {
			return nil, err
		}
		n, err := r.CountMailboxes(ctx, owner.ID)
		if err != nil {
			return nil, err
		}
		if p.MaxMailboxes > 0 && n >= p.MaxMailboxes {
			return nil, apperr.Conflict("gói %q chỉ cho phép %d hộp thư", p.Name, p.MaxMailboxes)
		}
	}
	if err := s.checkMailQuota(ctx, tx, owner, 0, in.QuotaMB); err != nil {
		return nil, err
	}
	if taken, err := r.LocalPartTaken(ctx, domainID, local); err != nil {
		return nil, err
	} else if taken {
		return nil, apperr.Conflict("%s@%s đã tồn tại", local, d.Domain)
	}
	m := &mailrepo.Mailbox{DomainID: domainID, LocalPart: local, PasswordHash: hash, QuotaMB: in.QuotaMB}
	if err := r.CreateMailbox(ctx, m); errors.Is(err, mailrepo.ErrTaken) {
		return nil, apperr.Conflict("%s@%s đã tồn tại", local, d.Domain)
	} else if err != nil {
		return nil, err
	}
	if _, err := s.syncMail(ctx, tx); err != nil {
		return nil, err
	}
	if err := s.reapplyQuota(ctx, tx, owner); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		_, _ = s.syncMail(context.WithoutCancel(ctx), s.pool)
		return nil, err
	}
	return s.mail().Mailbox(ctx, m.ID)
}

func (s *Service) accessibleMailbox(ctx context.Context, actor *users.User, id int64) (*mailrepo.Mailbox, *users.User, error) {
	m, err := s.mail().Mailbox(ctx, id)
	if errors.Is(err, mailrepo.ErrNotFound) {
		return nil, nil, apperr.NotFound("không tìm thấy hộp thư")
	}
	if err != nil {
		return nil, nil, err
	}
	_, owner, err := s.accessibleMailDomain(ctx, actor, m.DomainID)
	if err != nil {
		return nil, nil, apperr.NotFound("không tìm thấy hộp thư")
	}
	return m, owner, nil
}

func (s *Service) UpdateMailbox(ctx context.Context, actor *users.User, id int64, in MailboxUpdate) (*mailrepo.Mailbox, error) {
	m, owner, err := s.accessibleMailbox(ctx, actor, id)
	if err != nil {
		return nil, err
	}
	if in.Password != nil {
		if m.PasswordHash, err = hashMailPassword(*in.Password); err != nil {
			return nil, err
		}
	}
	oldQuota := m.QuotaMB
	if in.QuotaMB != nil {
		if *in.QuotaMB < 0 || *in.QuotaMB > 1<<20 {
			return nil, apperr.Invalid("dung lượng hộp thư không hợp lệ")
		}
		m.QuotaMB = *in.QuotaMB
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if in.QuotaMB != nil && m.QuotaMB != oldQuota {
		if err := s.users.WithTx(tx).LockForUpdate(ctx, owner.ID); err != nil {
			return nil, err
		}
		if err := s.checkMailQuota(ctx, tx, owner, oldQuota, m.QuotaMB); err != nil {
			return nil, err
		}
	}
	if err := mailrepo.NewRepo(tx).UpdateMailbox(ctx, m); err != nil {
		return nil, err
	}
	if _, err := s.syncMail(ctx, tx); err != nil {
		return nil, err
	}
	if in.QuotaMB != nil && m.QuotaMB != oldQuota {
		if err := s.reapplyQuota(ctx, tx, owner); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		_, _ = s.syncMail(context.WithoutCancel(ctx), s.pool)
		return nil, err
	}
	return m, nil
}

func (s *Service) DeleteMailbox(ctx context.Context, actor *users.User, id int64) (*mailrepo.Mailbox, error) {
	m, owner, err := s.accessibleMailbox(ctx, actor, id)
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err := mailrepo.NewRepo(tx).DeleteMailbox(ctx, id); err != nil {
		return nil, err
	}
	if _, err := s.syncMail(ctx, tx); err != nil {
		return nil, err
	}
	if err := s.reapplyQuota(ctx, tx, owner); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	if err := s.agent.Call(ctx, agentproto.ActionMailDeleteBox, agentproto.MailDeleteParams{Address: m.Address}, nil); err != nil {
		return nil, apperr.Agent(err)
	}
	return m, nil
}

func (s *Service) CreateAlias(ctx context.Context, actor *users.User, domainID int64, in AliasInput) (*mailrepo.Alias, error) {
	d, owner, err := s.accessibleMailDomain(ctx, actor, domainID)
	if err != nil {
		return nil, err
	}
	local := strings.ToLower(strings.TrimSpace(in.LocalPart))
	if err := validate.LocalPart(local); err != nil {
		return nil, apperr.Invalid("%s", err.Error())
	}
	dests, err := aliasDestinations(in.Destinations, local+"@"+d.Domain)
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err := s.users.WithTx(tx).LockForUpdate(ctx, owner.ID); err != nil {
		return nil, err
	}
	r := mailrepo.NewRepo(tx)
	if taken, err := r.LocalPartTaken(ctx, domainID, local); err != nil {
		return nil, err
	} else if taken {
		return nil, apperr.Conflict("%s@%s đã tồn tại", local, d.Domain)
	}
	a := &mailrepo.Alias{DomainID: domainID, LocalPart: local, Destinations: dests}
	if err := r.CreateAlias(ctx, a); errors.Is(err, mailrepo.ErrTaken) {
		return nil, apperr.Conflict("%s@%s đã tồn tại", local, d.Domain)
	} else if err != nil {
		return nil, err
	}
	if _, err := s.syncMail(ctx, tx); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		_, _ = s.syncMail(context.WithoutCancel(ctx), s.pool)
		return nil, err
	}
	return s.mail().Alias(ctx, a.ID)
}

func (s *Service) DeleteAlias(ctx context.Context, actor *users.User, id int64) (*mailrepo.Alias, error) {
	a, err := s.mail().Alias(ctx, id)
	if errors.Is(err, mailrepo.ErrNotFound) {
		return nil, apperr.NotFound("không tìm thấy chuyển tiếp")
	}
	if err != nil {
		return nil, err
	}
	if _, _, err := s.accessibleMailDomain(ctx, actor, a.DomainID); err != nil {
		return nil, apperr.NotFound("không tìm thấy chuyển tiếp")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err := mailrepo.NewRepo(tx).DeleteAlias(ctx, id); err != nil {
		return nil, err
	}
	if _, err := s.syncMail(ctx, tx); err != nil {
		return nil, err
	}
	return a, tx.Commit(ctx)
}

// mailAccountChanged re-syncs mail after an account was suspended, resumed
// or deleted (q sees the new state). It is a no-op when the account has no
// mail domains or the mail server is not installed.
func (s *Service) mailAccountChanged(ctx context.Context, q db.Querier, owner *users.User, deleted []mailrepo.Domain) error {
	if len(deleted) == 0 {
		list, err := mailrepo.NewRepo(q).DomainsByUser(ctx, owner.ID)
		if err != nil || len(list) == 0 {
			return err
		}
	}
	if _, err := s.syncMail(ctx, q); err != nil {
		return err
	}
	for _, d := range deleted {
		if err := s.agent.Call(ctx, agentproto.ActionMailDeleteBox, agentproto.MailDeleteParams{Domain: d.Domain}, nil); err != nil {
			return apperr.Agent(err)
		}
	}
	return nil
}

// aliasDestinations cleans and checks a forwarder's destination list.
func aliasDestinations(in []string, self string) ([]string, error) {
	var dests []string
	for _, dst := range in {
		dst = strings.TrimSpace(dst)
		if dst == "" {
			continue
		}
		if err := validate.ForwardAddress(dst); err != nil {
			return nil, apperr.Invalid("%s", err.Error())
		}
		if strings.EqualFold(dst, self) {
			return nil, apperr.Invalid("không thể chuyển tiếp về chính địa chỉ này")
		}
		if !slices.Contains(dests, dst) {
			dests = append(dests, dst)
		}
	}
	if len(dests) == 0 || len(dests) > 50 {
		return nil, apperr.Invalid("cần 1–50 địa chỉ nhận")
	}
	return dests, nil
}

// UpdateAlias replaces a forwarder's destinations.
func (s *Service) UpdateAlias(ctx context.Context, actor *users.User, id int64, destinations []string) (*mailrepo.Alias, error) {
	a, err := s.mail().Alias(ctx, id)
	if errors.Is(err, mailrepo.ErrNotFound) {
		return nil, apperr.NotFound("không tìm thấy chuyển tiếp")
	}
	if err != nil {
		return nil, err
	}
	if _, _, err := s.accessibleMailDomain(ctx, actor, a.DomainID); err != nil {
		return nil, apperr.NotFound("không tìm thấy chuyển tiếp")
	}
	dests, err := aliasDestinations(destinations, a.Address)
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE mail_aliases SET destinations=$2 WHERE id=$1`, id, dests); err != nil {
		return nil, err
	}
	if _, err := s.syncMail(ctx, tx); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		_, _ = s.syncMail(context.WithoutCancel(ctx), s.pool)
		return nil, err
	}
	a.Destinations = dests
	return a, nil
}
