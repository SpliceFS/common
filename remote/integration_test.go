package remote_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpliceFS/common/api"
	"github.com/SpliceFS/common/apitest"
	"github.com/SpliceFS/common/remote"
	"github.com/SpliceFS/common/wire"
)

// This file runs package remote against the real API handler over loopback TCP, so the
// two halves of the protocol are proven to agree, not only each against its own picture
// of the other.

const (
	password    = "pw"
	smallFile   = "media/a.txt"
	smallText   = "0123456789"
	pageFile    = "media/sub/page.html"
	oddFile     = "media/odd dir/n ä%#?.txt"
	bigFile     = "media/big.bin"
	bigSize     = 128 << 10
	cutAfter    = 64 << 10
	tokenLife   = time.Hour
	serverVer   = "v0.4.2"
	clientVer   = "v0.4.0"
	defaultBase = "/api"
)

func epoch() time.Time { return time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC) }

// clock is the injected clock of the authenticator; a test moves it to expire a token.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// setup describes the node and client of one case. The zero value is the default.
type setup struct {
	serverVersion, clientVersion string
	noBasePath                   bool
	// wrap, when set, wraps the transport of the client, so a test can watch or change
	// what goes over the wire.
	wrap func(http.RoundTripper) http.RoundTripper
}

// basePath is where the node serves the API.
func (cfg setup) basePath() string {
	if cfg.noBasePath {
		return ""
	}
	return defaultBase
}

// withDefaults fills in the empty fields.
func (cfg setup) withDefaults() setup {
	if cfg.serverVersion == "" {
		cfg.serverVersion = serverVer
	}
	if cfg.clientVersion == "" {
		cfg.clientVersion = clientVer
	}
	return cfg
}

// fixture is a node with the shares media and docs and the users alice, who may read
// media, and root, who may read every share; and a client of that node.
type fixture struct {
	clock  *clock
	media  *apitest.Share
	auth   *apitest.Auth
	node   *apitest.Node
	http   *http.Client
	client *remote.Node
	token  string
}

func start(t *testing.T, cfg setup) *fixture {
	t.Helper()
	cfg = cfg.withDefaults()
	env := &fixture{clock: &clock{now: epoch()}, media: apitest.NewShare(epoch())}
	docs := apitest.NewShare(epoch())
	put(t, docs, "d.txt", "docs")
	put(t, env.media, "a.txt", smallText)
	put(t, env.media, "empty.txt", "")
	put(t, env.media, "sub/page.html", pageText())
	put(t, env.media, "odd dir/n \u00e4%#?.txt", "odd")
	put(t, env.media, "big.bin", strings.Repeat("b", bigSize))
	env.media.SetSpace(wire.Space{Total: 100, Free: 40})
	env.auth = apitest.NewAuth(env.clock.Now, tokenLife)
	env.auth.AddUser("alice", password, "media")
	env.auth.AddUser("root", password, "*")
	nodes := &apitest.NodeList{}
	env.node = apitest.Start(t, api.Options{
		BasePath: cfg.basePath(), Node: "east", Version: cfg.serverVersion,
		Shares: map[string]api.Share{"media": env.media, "docs": docs},
		Auth:   env.auth, Nodes: nodes,
	})
	nodes.Set(wire.Nodes{Cluster: "c1", Nodes: []wire.Node{{Name: "east", URL: env.node.URL()}}})
	env.http = env.node.Client()
	if cfg.wrap != nil {
		env.http.Transport = cfg.wrap(env.http.Transport)
	}
	t.Cleanup(env.http.CloseIdleConnections)
	client, err := remote.New("east", env.node.URL(), cfg.clientVersion, env.http)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	env.client = client
	return env
}

func pageText() string { return strings.Repeat("<p>hello</p>\n", 200) }

func put(t *testing.T, share *apitest.Share, name, content string) {
	t.Helper()
	if err := share.Replace(name, []byte(content), epoch()); err != nil {
		t.Fatalf("Replace %s: %v", name, err)
	}
}

// login logs the client in as user, when user is not empty, and keeps the token.
func (env *fixture) login(t *testing.T, user string) {
	t.Helper()
	if user == "" {
		return
	}
	resp, err := env.client.Login(context.Background(), user, password)
	if err != nil {
		t.Fatalf("Login %s: %v", user, err)
	}
	env.token = resp.Token
}

// etag returns the tag the client learns from HEAD for a file of media.
func (env *fixture) etag(ctx context.Context, file string) (string, error) {
	meta, err := env.client.Head(ctx, env.token, file)
	return meta.ETag, err
}

// readRange reads a block of a file pinned to the tag HEAD gives now, and returns its
// bytes and the first error.
func (env *fixture) readRange(ctx context.Context, file string, off, length int64) ([]byte, error) {
	etag, err := env.etag(ctx, file)
	if err != nil {
		return nil, err
	}
	return env.readPinned(ctx, file, etag, off, length)
}

func (env *fixture) readPinned(ctx context.Context, file, etag string, off, length int64) ([]byte, error) {
	body, err := env.client.Range(ctx, env.token, file, etag, off, length)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(body)
	if closeErr := body.Close(); err == nil {
		err = closeErr
	}
	return data, err
}

func names(entries []wire.Entry, err error) ([]string, error) {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Name)
	}
	return out, err
}

func sum(data string) string {
	digest := sha256.Sum256([]byte(data))
	return hex.EncodeToString(digest[:])
}

// TestProtocolAnswers checks what the client returns for calls that succeed.
func TestProtocolAnswers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tests := []struct {
		name  string
		setup setup
		user  string
		call  func(*fixture) (any, error)
		want  func(*fixture) any
	}{
		{
			name: "health reports version and node",
			call: func(env *fixture) (any, error) { return env.client.Health(ctx) },
			want: func(*fixture) any { return wire.Health{Status: "ok", Version: serverVer, Node: "east"} },
		},
		{
			name:  "a development server is accepted",
			setup: setup{serverVersion: "dev"},
			call:  func(env *fixture) (any, error) { return env.client.Health(ctx) },
			want:  func(*fixture) any { return wire.Health{Status: "ok", Version: "dev", Node: "east"} },
		},
		{
			name:  "a development client accepts any server",
			setup: setup{serverVersion: "v0.1.0", clientVersion: "dev"},
			call:  func(env *fixture) (any, error) { return env.client.Health(ctx) },
			want:  func(*fixture) any { return wire.Health{Status: "ok", Version: "v0.1.0", Node: "east"} },
		},
		{
			name: "login",
			call: func(env *fixture) (any, error) {
				resp, err := env.client.Login(ctx, "alice", password)
				return resp.Expires, err
			},
			want: func(*fixture) any { return epoch().Add(tokenLife) },
		},
		{
			name: "node list",
			user: "alice",
			call: func(env *fixture) (any, error) { return env.client.Nodes(ctx, env.token) },
			want: func(env *fixture) any {
				return wire.Nodes{Cluster: "c1", Nodes: []wire.Node{{Name: "east", URL: env.node.URL()}}}
			},
		},
		{
			name: "tree root lists only the allowed shares",
			user: "alice",
			call: func(env *fixture) (any, error) { return names(env.client.List(ctx, env.token, "")) },
			want: func(*fixture) any { return []string{"media"} },
		},
		{
			name: "tree root of a user of every share",
			user: "root",
			call: func(env *fixture) (any, error) { return names(env.client.List(ctx, env.token, "")) },
			want: func(*fixture) any { return []string{"docs", "media"} },
		},
		{
			name: "tree of a share",
			user: "alice",
			call: func(env *fixture) (any, error) { return env.client.List(ctx, env.token, "media/sub") },
			want: func(env *fixture) any {
				info, _ := env.media.Stat("sub/page.html") //nolint:errcheck // The fixture made the file.
				return []wire.Entry{{Name: "page.html", Size: info.Size, MTime: epoch(), ETag: info.ETag}}
			},
		},
		{
			name: "space",
			user: "alice",
			call: func(env *fixture) (any, error) { return env.client.Space(ctx, env.token, "media") },
			want: func(*fixture) any { return wire.Space{Total: 100, Free: 40} },
		},
		{
			name: "head has size, time, tag and both digests",
			user: "alice",
			call: func(env *fixture) (any, error) { return env.client.Head(ctx, env.token, bigFile) },
			want: func(env *fixture) any {
				info, _ := env.media.Stat("big.bin") //nolint:errcheck // The fixture made the file.
				part := sum(strings.Repeat("b", wire.HeadDigestBytes))
				return remote.Meta{Size: bigSize, MTime: epoch(), ETag: info.ETag, HeadDigest: part, TailDigest: part}
			},
		},
		{
			name: "range gives the exact bytes",
			user: "alice",
			call: func(env *fixture) (any, error) { return env.readRange(ctx, smallFile, 3, 4) },
			want: func(*fixture) any { return []byte("3456") },
		},
		{
			name: "range of the last byte",
			user: "alice",
			call: func(env *fixture) (any, error) { return env.readRange(ctx, smallFile, 9, 1) },
			want: func(*fixture) any { return []byte("9") },
		},
		{
			name: "range of a compressible file is plain",
			user: "alice",
			call: func(env *fixture) (any, error) { return env.readRange(ctx, pageFile, 0, int64(len(pageText()))) },
			want: func(*fixture) any { return []byte(pageText()) },
		},
		{
			name:  "no base path",
			setup: setup{noBasePath: true},
			user:  "alice",
			call:  func(env *fixture) (any, error) { return env.readRange(ctx, smallFile, 0, 3) },
			want:  func(*fixture) any { return []byte("012") },
		},
		{
			name: "odd names are listed",
			user: "alice",
			call: func(env *fixture) (any, error) { return names(env.client.List(ctx, env.token, "media/odd dir")) },
			want: func(*fixture) any { return []string{"n \u00e4%#?.txt"} },
		},
		{
			name: "odd names are read",
			user: "alice",
			call: func(env *fixture) (any, error) { return env.readRange(ctx, oddFile, 0, 3) },
			want: func(*fixture) any { return []byte("odd") },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := start(t, tt.setup)
			env.login(t, tt.user)
			got, err := tt.call(env)
			if want := tt.want(env); err != nil || !reflect.DeepEqual(got, want) {
				t.Errorf("got %+v, %v; want %+v", got, err, want)
			}
		})
	}
}

// rangeAfterChange reads a block pinned to the tag the file had before it changed.
func rangeAfterChange(ctx context.Context) func(*fixture) error {
	return func(env *fixture) error {
		etag, err := env.etag(ctx, smallFile)
		if err != nil {
			return err
		}
		if replaceErr := env.media.Replace("a.txt", []byte("9876543210"), epoch().Add(time.Second)); replaceErr != nil {
			return replaceErr
		}
		_, err = env.readPinned(ctx, smallFile, etag, 0, 4)
		return err
	}
}

// rangeOfCutFile reads a file the node drops mid-body. The cut is past what the server
// buffers, so the answer has started when the connection drops.
func rangeOfCutFile(ctx context.Context) func(*fixture) error {
	return func(env *fixture) error {
		etag, err := env.etag(ctx, bigFile)
		if err != nil {
			return err
		}
		env.media.Cut("big.bin", cutAfter)
		_, err = env.readPinned(ctx, bigFile, etag, 0, bigSize)
		return err
	}
}

// TestProtocolErrors checks the error the client returns for each way a call fails.
func TestProtocolErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	health := func(env *fixture) error { _, err := env.client.Health(ctx); return err }
	nodes := func(token string) func(*fixture) error {
		return func(env *fixture) error { _, err := env.client.Nodes(ctx, token); return err }
	}
	list := func(dir string) func(*fixture) error {
		return func(env *fixture) error { _, err := env.client.List(ctx, env.token, dir); return err }
	}
	headOf := func(file string) func(*fixture) error {
		return func(env *fixture) error { _, err := env.client.Head(ctx, env.token, file); return err }
	}
	login := func(user, pass string) func(*fixture) error {
		return func(env *fixture) error { _, err := env.client.Login(ctx, user, pass); return err }
	}
	tests := []struct {
		name    string
		setup   setup
		user    string
		prepare func(*fixture) error
		call    func(*fixture) error
		want    error
	}{
		{name: "older server minor", setup: setup{serverVersion: "v0.3.9"}, call: health, want: remote.ErrIncompatible},
		{name: "other server main", setup: setup{serverVersion: "v1.4.0"}, call: health, want: remote.ErrIncompatible},
		{name: "bad credentials", call: login("alice", "wrong"), want: remote.ErrUnauthorized},
		{name: "unknown user", call: login("nobody", password), want: remote.ErrUnauthorized},
		{
			name:    "rate limited",
			prepare: func(env *fixture) error { env.auth.LimitLogins(); return nil },
			call:    login("alice", password),
			want:    remote.ErrRateLimited,
		},
		{name: "token missing", call: nodes(""), want: remote.ErrUnauthorized},
		{name: "token invalid", call: nodes("nonsense"), want: remote.ErrUnauthorized},
		{
			name:    "token expired",
			user:    "alice",
			prepare: func(env *fixture) error { env.clock.advance(tokenLife); return nil },
			call:    func(env *fixture) error { return nodes(env.token)(env) },
			want:    remote.ErrUnauthorized,
		},
		{name: "list of a share not allowed", user: "alice", call: list("docs"), want: remote.ErrForbidden},
		{name: "list of a missing share not allowed", user: "alice", call: list("nope"), want: remote.ErrForbidden},
		{name: "head in a share not allowed", user: "alice", call: headOf("docs/d.txt"), want: remote.ErrForbidden},
		{name: "head in a missing share not allowed", user: "alice", call: headOf("nope/d.txt"), want: remote.ErrForbidden},
		{name: "missing share when all are allowed", user: "root", call: list("nope"), want: remote.ErrNotFound},
		{name: "missing file", user: "alice", call: headOf("media/none.txt"), want: remote.ErrNotFound},
		{name: "missing directory", user: "alice", call: list("media/none"), want: remote.ErrNotFound},
		{name: "list of a file", user: "alice", call: list(smallFile), want: remote.ErrNotFound},
		{name: "head of a directory", user: "alice", call: headOf("media/sub"), want: remote.ErrNotFound},
		{name: "range after the file changed", user: "alice", call: rangeAfterChange(ctx), want: remote.ErrChanged},
		{name: "connection dropped mid-body", user: "alice", call: rangeOfCutFile(ctx), want: io.ErrUnexpectedEOF},
		{
			name:    "stopped node",
			prepare: func(env *fixture) error { env.node.Stop(); return nil },
			call:    health,
			want:    remote.ErrUnavailable,
		},
		{
			name: "cancelled context",
			call: func(env *fixture) error {
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				_, err := env.client.Health(cancelled)
				return err
			},
			want: context.Canceled,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := start(t, tt.setup)
			env.login(t, tt.user)
			if err := prepare(env, tt.prepare); err != nil {
				t.Fatalf("prepare: %v", err)
			}
			if err := tt.call(env); !errors.Is(err, tt.want) {
				t.Errorf("error = %v, want %v", err, tt.want)
			}
		})
	}
}

// prepare runs step, when there is one, on env.
func prepare(env *fixture, step func(*fixture) error) error {
	if step == nil {
		return nil
	}
	return step(env)
}

func TestRestartedNodeAnswersAgain(t *testing.T) {
	t.Parallel()
	env := start(t, setup{})
	env.node.Stop()
	if err := env.node.Restart(); err != nil {
		t.Fatal(err)
	}
	if _, err := env.client.Health(context.Background()); err != nil {
		t.Errorf("Health after Restart: %v", err)
	}
}

// rawAnswer is what a request the client package never sends got back.
type rawAnswer struct {
	status int
	header http.Header
	body   []byte
}

func (env *fixture) raw(t *testing.T, method, path string, header http.Header, body string) rawAnswer {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, env.node.URL()+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(req.Header, header)
	req.Header.Set("Authorization", "Bearer "+env.token)
	resp, err := env.http.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	data, err := io.ReadAll(resp.Body)
	if closeErr := resp.Body.Close(); err != nil || closeErr != nil {
		t.Fatalf("read body: %v, close: %v", err, closeErr)
	}
	return rawAnswer{status: resp.StatusCode, header: resp.Header, body: data}
}

// TestProtocolRequestsTheClientNeverSends checks answers the client must be able to rely
// on, for requests it cannot make itself.
func TestProtocolRequestsTheClientNeverSends(t *testing.T) {
	t.Parallel()
	jsonBody := http.Header{"Content-Type": {"application/json"}}
	tests := []struct {
		name     string
		method   string
		path     string
		header   http.Header
		body     string
		status   int
		encoding string
	}{
		{name: "unknown path carries the version", method: http.MethodGet, path: "/v1/nothing", status: 404},
		{name: "login body null", method: http.MethodPost, path: "/v1/login", header: jsonBody, body: "null", status: 400},
		{name: "login body cut", method: http.MethodPost, path: "/v1/login", header: jsonBody, body: "{", status: 400},
		{
			name: "login body with trailing data", method: http.MethodPost, path: "/v1/login", header: jsonBody,
			body: `{"user":"alice"} {}`, status: 400,
		},
		{
			name: "login body with unknown field", method: http.MethodPost, path: "/v1/login", header: jsonBody,
			body: `{"user":"alice","extra":1}`, status: 400,
		},
		{
			name: "several ranges", method: http.MethodGet, path: "/v1/file/" + smallFile,
			header: http.Header{"Range": {"bytes=0-1,4-5"}}, status: 416,
		},
		{
			name: "whole read is compressed", method: http.MethodGet, path: "/v1/file/" + pageFile,
			header: http.Header{"Accept-Encoding": {"gzip"}}, status: 200, encoding: "gzip",
		},
		{
			name: "range is not compressed", method: http.MethodGet, path: "/v1/file/" + pageFile,
			header: http.Header{"Accept-Encoding": {"gzip"}, "Range": {"bytes=0-2"}}, status: 206,
		},
		{name: "parent segment", method: http.MethodGet, path: "/v1/file/media/sub/../a.txt", status: 404},
		{name: "dot segment", method: http.MethodGet, path: "/v1/file/media/./a.txt", status: 404},
		{name: "empty segment", method: http.MethodGet, path: "/v1/file/media//a.txt", status: 404},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := start(t, setup{})
			env.login(t, "alice")
			got := env.raw(t, tt.method, tt.path, tt.header, tt.body)
			if got.status != tt.status || got.header.Get("Content-Encoding") != tt.encoding {
				t.Errorf("status %d, encoding %q; want %d, %q", got.status, got.header.Get("Content-Encoding"),
					tt.status, tt.encoding)
			}
			if version := got.header.Get(wire.HeaderServerVersion); version != serverVer {
				t.Errorf("server version = %q, want %q", version, serverVer)
			}
		})
	}
}

func TestCompressedReadIsTheFile(t *testing.T) {
	t.Parallel()
	env := start(t, setup{})
	env.login(t, "alice")
	got := env.raw(t, http.MethodGet, "/v1/file/"+pageFile, http.Header{"Accept-Encoding": {"gzip"}}, "")
	reader, err := gzip.NewReader(bytes.NewReader(got.body))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := io.ReadAll(reader)
	if err != nil || string(plain) != pageText() {
		t.Errorf("gunzipped body differs from the file: %v", err)
	}
}

// The tests below connect a client to a node over real TLS. The node's certificate is
// signed by a cluster key and names a host the client does not connect to, so a success
// also shows that host names are not checked for a cluster-signed certificate.

func clusterNode(t *testing.T, signer apitest.ClusterKey, from, until time.Time) *apitest.Node {
	t.Helper()
	cert, err := signer.IssueNode([]string{"node.example.test"}, from, until)
	if err != nil {
		t.Fatalf("IssueNode: %v", err)
	}
	return apitest.StartWithCertificate(t, api.Options{
		Node: "east", Version: serverVer, Nodes: &apitest.NodeList{},
	}, cert)
}

// clientGiven returns the client of node that trusts what a client given key trusts; a
// nil key is a client without a cluster key.
func clientGiven(t *testing.T, node *apitest.Node, key ed25519.PublicKey) *remote.Node {
	t.Helper()
	address, err := url.Parse(node.URL())
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := remote.TLSConfig(address.Hostname(), remote.TLSOptions{ClusterKey: key})
	if err != nil {
		t.Fatalf("TLSConfig: %v", err)
	}
	httpClient := &http.Client{Transport: &http.Transport{TLSClientConfig: cfg}}
	t.Cleanup(httpClient.CloseIdleConnections)
	client, err := remote.New("east", node.URL(), clientVer, httpClient)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}

func newClusterKey(t *testing.T) apitest.ClusterKey {
	t.Helper()
	key, err := apitest.NewClusterKey()
	if err != nil {
		t.Fatalf("NewClusterKey: %v", err)
	}
	return key
}

func TestClusterKeyTrustOverTCP(t *testing.T) {
	t.Parallel()
	key, other := newClusterKey(t), newClusterKey(t)
	// Dates no test run can be outside of or inside of by the clock.
	past, pastEnd := time.Unix(0, 0), time.Unix(86400, 0)
	always, never := time.Unix(0, 0), time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)
	future := time.Date(9998, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		// signer signs the node's certificate, from and until bound it; given is the cluster
		// key the client is given, nil for none.
		signer      apitest.ClusterKey
		from, until time.Time
		given       ed25519.PublicKey
		trusted     bool
	}{
		{name: "client given the cluster key", signer: key, from: always, until: never, given: key.Public(), trusted: true},
		{name: "client given no key", signer: key, from: always, until: never},
		{name: "client given another key", signer: key, from: always, until: never, given: other.Public()},
		{name: "certificate signed by another key", signer: other, from: always, until: never, given: key.Public()},
		{name: "certificate expired", signer: key, from: past, until: pastEnd, given: key.Public()},
		{name: "certificate not valid yet", signer: key, from: future, until: never, given: key.Public()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := clientGiven(t, clusterNode(t, tt.signer, tt.from, tt.until), tt.given)
			health, err := client.Health(context.Background())
			if tt.trusted {
				if err != nil || health.Node != "east" {
					t.Fatalf("Health = %+v, %v; want the node's answer", health, err)
				}
				return
			}
			if !errors.Is(err, remote.ErrUnavailable) || !strings.Contains(err.Error(), "failed to verify certificate") {
				t.Errorf("Health error = %v, want the node refused as unavailable for its certificate", err)
			}
		})
	}
}

func TestClusterKeyClientRefusesASelfSignedNode(t *testing.T) {
	t.Parallel()
	node := apitest.Start(t, api.Options{Node: "east", Version: serverVer, Nodes: &apitest.NodeList{}})
	client := clientGiven(t, node, newClusterKey(t).Public())
	_, err := client.Health(context.Background())
	if !errors.Is(err, remote.ErrUnavailable) || !strings.Contains(err.Error(), "failed to verify certificate") {
		t.Errorf("Health error = %v, want the node refused as unavailable for its certificate", err)
	}
}
