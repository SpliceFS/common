package api

import (
	"context"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/netip"
	"slices"
	"time"

	"github.com/SpliceFS/common/wire"
)

// Share is read-only access to one shared directory. Errors that mean "no such name"
// are ErrNotFound, ErrNotDir and ErrIsDir; any other error is reported to the client as
// an internal error.
type Share interface {
	// Stat describes the file or directory at name; "" is the share root.
	Stat(name string) (Info, error)
	// List returns the visible entries of a directory, sorted by name.
	List(name string) ([]Info, error)
	// Open opens a regular file. The File's Info describes the file that was opened.
	Open(name string) (File, error)
	// Digests returns the head digest, the tail digest and the Info of a regular file.
	Digests(name string) (FileDigests, Info, error)
	// Space reports the size and free space of the file system holding the share.
	Space() (wire.Space, error)
}

// File is an open file. The caller closes it.
type File interface {
	io.ReadSeeker
	io.Closer
	// Info describes the file that was actually opened.
	Info() Info
}

// Authenticator logs users in and checks their tokens.
type Authenticator interface {
	// Login returns a token and its expiry, or ErrBadCredentials or ErrRateLimited.
	Login(ctx context.Context, clientAddr, user, password string) (string, time.Time, error)
	// Verify returns the claims of a token, or ErrInvalidToken or ErrExpiredToken.
	Verify(token string) (Claims, error)
}

// NodeLister returns the node list of the cluster.
type NodeLister interface {
	Nodes() (wire.Nodes, error)
}

// Options configures the handler returned by NewHandler.
type Options struct {
	// BasePath is the URL prefix of the API: "" or a path that starts with a slash,
	// does not end with one and contains no pattern syntax.
	BasePath string
	// Node is this node's name, and Version the program version. Both are reported by
	// the health endpoint.
	Node, Version string
	// Shares maps a share name to the share.
	Shares map[string]Share
	// Auth logs users in and verifies tokens.
	Auth Authenticator
	// Nodes supplies the node list.
	Nodes NodeLister
	// TrustedProxies are the networks whose X-Forwarded-For header is believed.
	TrustedProxies []netip.Prefix
	// Logger receives the access log and errors. The default discards everything.
	Logger *slog.Logger
	// Now returns the current time, for access log durations. The default is time.Now.
	Now func() time.Time
}

// server holds what the endpoint handlers share.
type server struct {
	node       string
	version    string
	base       string
	shares     map[string]Share
	shareNames []string
	auth       Authenticator
	nodes      NodeLister
	log        *slog.Logger
}

// NewHandler returns the handler of the API. It is safe for concurrent use.
func NewHandler(opts Options) http.Handler {
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	srv := &server{
		node:       opts.Node,
		version:    opts.Version,
		base:       opts.BasePath,
		shares:     opts.Shares,
		shareNames: slices.Sorted(maps.Keys(opts.Shares)),
		auth:       opts.Auth,
		nodes:      opts.Nodes,
		log:        log,
	}
	handler := noSniff(srv.routes())
	handler = reportVersion(opts.Version, handler)
	handler = recoverPanics(log, handler)
	handler = logAccess(log, now, handler)
	return resolveClient(opts.TrustedProxies, handler)
}
