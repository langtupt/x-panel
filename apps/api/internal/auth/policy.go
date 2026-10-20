package auth

import "time"

// LoginPolicy is configured on the Firewall page. Failures are counted over
// 15 minutes; 0 disables that lock.
type LoginPolicy struct {
	IPFails      int
	IPBan        time.Duration
	AccountFails int
	AccountLock  time.Duration
}

// SetLoginPolicy changes the lockout thresholds. onIP runs when an address
// gets locked (to ban it in the server firewall), onAccount when an account
// does (to alert its owner).
func (s *Service) SetLoginPolicy(p LoginPolicy, onIP func(ip string, d time.Duration), onAccount func(username string, d time.Duration)) {
	s.limiter.set(p.IPFails, p.IPBan)
	s.accountLimiter.set(p.AccountFails, p.AccountLock)
	s.limiter.mu.Lock()
	s.limiter.onLock = onIP
	s.limiter.mu.Unlock()
	s.accountLimiter.mu.Lock()
	s.accountLimiter.onLock = onAccount
	s.accountLimiter.mu.Unlock()
}

// LockedIPs and LockedAccounts list current panel login locks.
func (s *Service) LockedIPs() []Lock      { return s.limiter.Locks() }
func (s *Service) LockedAccounts() []Lock { return s.accountLimiter.Locks() }

func (s *Service) UnlockIP(ip string) bool        { return s.limiter.Unlock(ip) }
func (s *Service) UnlockAccount(name string) bool { return s.accountLimiter.Unlock(name) }
