package api

import (
	"errors"
	"time"
)

var (
	// ErrBadCredentials means the user is unknown or disabled, or the password is wrong.
	// The three cases are deliberately indistinguishable.
	ErrBadCredentials = errors.New("bad credentials")
	// ErrRateLimited means the client address has made too many login attempts.
	ErrRateLimited = errors.New("too many login attempts")
	// ErrInvalidToken means the token is malformed, uses another algorithm, names an
	// unknown key, has a bad signature or is not valid yet. The error wraps the reason.
	ErrInvalidToken = errors.New("invalid token")
	// ErrExpiredToken means the token is genuine but past its expiry.
	ErrExpiredToken = errors.New("expired token")
)

// allShares is the entry in a share list that allows every share.
const allShares = "*"

// Claims is what a valid token states.
type Claims struct {
	// User is the user name the token was issued to.
	User string
	// Shares lists the shares the token allows; "*" means every share.
	Shares []string
	// Expires is the expiry time carried by the token.
	Expires time.Time
}

// Allows reports whether the token allows the named share.
func (c Claims) Allows(share string) bool {
	return allowsShare(c.Shares, share)
}

// allowsShare reports whether shares lists share or the wildcard.
func allowsShare(shares []string, share string) bool {
	for _, s := range shares {
		if s == allShares || s == share {
			return true
		}
	}
	return false
}
