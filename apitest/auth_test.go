package apitest

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/SpliceFS/common/api"
)

// clock is a time a test moves on by hand.
type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func TestLoginAndVerify(t *testing.T) {
	t.Parallel()
	now := &clock{now: testTime()}
	auth := NewAuth(now.Now, time.Minute)
	auth.AddUser("alice", "secret", "media", "docs")
	token, expires, err := auth.Login(context.Background(), "127.0.0.1", "alice", "secret")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if !expires.Equal(testTime().Add(time.Minute)) {
		t.Errorf("expires = %v, want a minute after %v", expires, testTime())
	}
	claims, err := auth.Verify(token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	wantShares := []string{"media", "docs"}
	if claims.User != "alice" || !slices.Equal(claims.Shares, wantShares) || !claims.Expires.Equal(expires) {
		t.Errorf("claims = %+v", claims)
	}
	second, _, err := auth.Login(context.Background(), "127.0.0.1", "alice", "secret")
	if err != nil || second == token {
		t.Errorf("second login = %q, %v; want a new token", second, err)
	}
	now.now = expires
	if _, err = auth.Verify(token); !errors.Is(err, api.ErrExpiredToken) {
		t.Errorf("Verify at expiry = %v, want ErrExpiredToken", err)
	}
}

func TestLoginFailures(t *testing.T) {
	t.Parallel()
	auth := NewAuth(testTime, time.Minute)
	auth.AddUser("alice", "secret")
	for _, login := range [][2]string{{"alice", "wrong"}, {"bob", "secret"}} {
		if _, _, err := auth.Login(context.Background(), "", login[0], login[1]); !errors.Is(err, api.ErrBadCredentials) {
			t.Errorf("Login(%s, %s) = %v, want ErrBadCredentials", login[0], login[1], err)
		}
	}
	auth.LimitLogins()
	if _, _, err := auth.Login(context.Background(), "", "alice", "secret"); !errors.Is(err, api.ErrRateLimited) {
		t.Errorf("limited Login = %v, want ErrRateLimited", err)
	}
	auth.AllowLogins()
	if _, _, err := auth.Login(context.Background(), "", "alice", "secret"); err != nil {
		t.Errorf("Login after AllowLogins = %v", err)
	}
}

func TestVerifyUnknownToken(t *testing.T) {
	t.Parallel()
	if _, err := NewAuth(testTime, time.Minute).Verify("nonsense"); !errors.Is(err, api.ErrInvalidToken) {
		t.Errorf("Verify = %v, want ErrInvalidToken", err)
	}
}
