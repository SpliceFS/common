package apitest

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/SpliceFS/common/api"
)

// Auth is an in-memory api.Authenticator. Its clock is injected, so a test expires a
// token by moving the clock, never by waiting. It is safe for concurrent use.
type Auth struct {
	mu       sync.Mutex
	now      func() time.Time
	lifetime time.Duration
	users    map[string]account
	tokens   map[string]api.Claims
	limited  bool
	issued   int
}

// account is what Auth knows about one user.
type account struct {
	password string
	shares   []string
}

// NewAuth returns an Auth without users whose tokens are valid for lifetime, measured on
// the clock now.
func NewAuth(now func() time.Time, lifetime time.Duration) *Auth {
	return &Auth{now: now, lifetime: lifetime, users: map[string]account{}, tokens: map[string]api.Claims{}}
}

// AddUser adds or replaces the user name, who logs in with password and may read
// shares ("*" for every share).
func (a *Auth) AddUser(name, password string, shares ...string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.users[name] = account{password: password, shares: slices.Clone(shares)}
}

// LimitLogins makes every login fail with api.ErrRateLimited until AllowLogins.
func (a *Auth) LimitLogins() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.limited = true
}

// AllowLogins ends LimitLogins.
func (a *Auth) AllowLogins() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.limited = false
}

// Login returns a new token for the user, or api.ErrBadCredentials or api.ErrRateLimited.
func (a *Auth) Login(_ context.Context, _, user, password string) (string, time.Time, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.limited {
		return "", time.Time{}, api.ErrRateLimited
	}
	found, ok := a.users[user]
	if !ok || found.password != password {
		return "", time.Time{}, api.ErrBadCredentials
	}
	a.issued++
	token := "apitest-token-" + strconv.Itoa(a.issued)
	claims := api.Claims{User: user, Shares: slices.Clone(found.shares), Expires: a.now().Add(a.lifetime)}
	a.tokens[token] = claims
	return token, claims.Expires, nil
}

// Verify returns the claims of a token Login issued, api.ErrExpiredToken once the clock
// reaches its expiry, or api.ErrInvalidToken for any other token.
func (a *Auth) Verify(token string) (api.Claims, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	claims, ok := a.tokens[token]
	if !ok {
		return api.Claims{}, fmt.Errorf("%w: not issued by this node", api.ErrInvalidToken)
	}
	if !a.now().Before(claims.Expires) {
		return api.Claims{}, api.ErrExpiredToken
	}
	claims.Shares = slices.Clone(claims.Shares)
	return claims, nil
}
