package remote_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SpliceFS/common/remote"
	"github.com/SpliceFS/common/wire"
)

// A newer server must serve every older client of the same main version. The client of
// each released minor is kept here as the requests it makes and the answers it relies on,
// recorded once from this package and replayed against the current handler on every run,
// so that proof needs no old program.

//nolint:gochecknoglobals // the flag package registers flags as package variables
var updateExchanges = flag.Bool("update-exchanges", false,
	"record the requests of this client into testdata/exchanges/client-vMAIN.MINOR.json")

// recordedClient is the version the recorder writes the file of. Raise it with the client
// minor; the file of every older minor stays as it was recorded.
const recordedClient = "v0.5.0"

const exchangesDir = "testdata/exchanges"

// Placeholders stand for values that differ from run to run or must not be kept.
const (
	tokenPlaceholder    = "<token>"
	passwordPlaceholder = "<password>"
	nodeURLPlaceholder  = "<node-url>"
)

// readHeaders are the response headers package remote reads.
func readHeaders() []string {
	return []string{
		"Content-Range", "Content-Type", "ETag", "Last-Modified", "Retry-After",
		wire.HeaderHeadDigest, wire.HeaderTailDigest, wire.HeaderServerVersion,
	}
}

type exchangeFile struct {
	Client    string     `json:"client"`
	Exchanges []exchange `json:"exchanges"`
}

// exchange is one request and its answer. Before names the change to the node that
// happened just before the request.
type exchange struct {
	Before   string           `json:"before,omitempty"`
	Request  recordedRequest  `json:"request"`
	Response recordedResponse `json:"response"`
}

type recordedRequest struct {
	Method string            `json:"method"`
	Path   string            `json:"path"`
	Header map[string]string `json:"header,omitempty"`
	Body   json.RawMessage   `json:"body,omitempty"`
}

// recordedResponse keeps a JSON body as it is and any other body as its digest.
type recordedResponse struct {
	Status     int               `json:"status"`
	Header     map[string]string `json:"header,omitempty"`
	Body       json.RawMessage   `json:"body,omitempty"`
	BodySHA256 string            `json:"body_sha256,omitempty"`
	BodySize   int               `json:"body_size"`
}

// steps are the changes to a node that a recording can name.
func steps() map[string]func(*fixture) error {
	return map[string]func(*fixture) error{
		"change media/a.txt": func(env *fixture) error {
			return env.media.Replace("a.txt", []byte("9876543210"), epoch().Add(time.Second))
		},
		"limit logins":  func(env *fixture) error { env.auth.LimitLogins(); return nil },
		"expire tokens": func(env *fixture) error { env.clock.advance(tokenLife); return nil },
	}
}

func applyStep(env *fixture, name string) error {
	step, ok := steps()[name]
	if !ok {
		return fmt.Errorf("unknown step %q", name)
	}
	return step(env)
}

// placeholders maps real values to the placeholders that stand for them.
type placeholders map[string]string

func (p placeholders) apply(text string) string {
	pairs := make([]string, 0, 2*len(p))
	for _, value := range slices.Sorted(maps.Keys(p)) {
		pairs = append(pairs, value, p[value])
	}
	return strings.NewReplacer(pairs...).Replace(text)
}

// invert maps each placeholder back to its real value.
func (p placeholders) invert() placeholders {
	out := placeholders{}
	for value, placeholder := range p {
		out[placeholder] = value
	}
	return out
}

func (p placeholders) with(value, placeholder string) placeholders {
	out := maps.Clone(p)
	out[value] = placeholder
	return out
}

// basePlaceholders are the values every run of a fixture knows before the first request.
func basePlaceholders(env *fixture) placeholders {
	quoted := strconv.Quote(password)
	return placeholders{env.node.URL(): nodeURLPlaceholder, quoted: strconv.Quote(passwordPlaceholder)}
}

// loginToken returns the token of an answered login, or "" for any other answer.
func loginToken(method, path string, status int, body []byte) string {
	if method != http.MethodPost || !strings.HasSuffix(path, "/v1/login") || status != http.StatusOK {
		return ""
	}
	var resp wire.LoginResponse
	if json.Unmarshal(body, &resp) != nil {
		return ""
	}
	return resp.Token
}

// recorder is a transport that keeps every exchange that passes through it.
type recorder struct {
	next      http.RoundTripper
	env       *fixture
	marks     placeholders
	before    string
	exchanges []exchange
}

func (r *recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	sent, err := readAll(req.Body)
	if err != nil {
		return nil, err
	}
	req.Body = io.NopCloser(bytes.NewReader(sent))
	resp, err := r.next.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	got, err := readAll(resp.Body)
	if err != nil {
		return nil, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(got))
	if token := loginToken(req.Method, req.URL.Path, resp.StatusCode, got); token != "" {
		r.marks = r.marks.with(token, tokenPlaceholder)
	}
	r.exchanges = append(r.exchanges, exchange{
		Before:   r.before,
		Request:  recordRequest(req, sent, r.env.node.URL(), r.marks),
		Response: recordResponse(resp.StatusCode, resp.Header, got, r.marks),
	})
	r.before = ""
	return resp, nil
}

// CloseIdleConnections lets http.Client.CloseIdleConnections reach the wrapped transport.
func (r *recorder) CloseIdleConnections() {
	if closer, ok := r.next.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

// step changes the node and marks the next exchange with the change.
func (r *recorder) step(name string) error {
	r.before = name
	return applyStep(r.env, name)
}

func readAll(body io.ReadCloser) ([]byte, error) {
	if body == nil {
		return nil, nil
	}
	data, err := io.ReadAll(body)
	if closeErr := body.Close(); err == nil {
		err = closeErr
	}
	return data, err
}

func recordRequest(req *http.Request, body []byte, nodeURL string, marks placeholders) recordedRequest {
	header := map[string]string{}
	for name := range req.Header {
		header[name] = marks.apply(strings.Join(req.Header.Values(name), ", "))
	}
	out := recordedRequest{
		Method: req.Method,
		Path:   marks.apply(strings.TrimPrefix(req.URL.String(), nodeURL)),
		Header: header,
	}
	if len(body) > 0 {
		out.Body = json.RawMessage(marks.apply(string(body)))
	}
	return out
}

func recordResponse(status int, header http.Header, body []byte, marks placeholders) recordedResponse {
	kept := map[string]string{}
	for _, name := range readHeaders() {
		if values := header.Values(name); len(values) > 0 {
			kept[name] = marks.apply(strings.Join(values, ", "))
		}
	}
	out := recordedResponse{Status: status, Header: kept, BodySize: len(body)}
	if isJSONType(header.Get("Content-Type")) && json.Valid(body) {
		out.Body = json.RawMessage(marks.apply(string(body)))
		return out
	}
	digest := sha256.Sum256(body)
	out.BodySHA256 = hex.EncodeToString(digest[:])
	return out
}

func isJSONType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && mediaType == "application/json"
}

func errOf[T any](_ T, err error) error { return err }

// clientSession is what a client does with a node: everything the real client did against
// a released server in the old compatibility job, and the failures it must tell apart.
func clientSession(ctx context.Context, env *fixture, rec *recorder) error {
	node := env.client
	calls := []struct {
		what string
		call func() error
		want error
	}{
		{"health", func() error { return errOf(node.Health(ctx)) }, nil},
		{
			"login with a wrong password",
			func() error { return errOf(node.Login(ctx, "alice", "wrong")) }, remote.ErrUnauthorized,
		},
		{"login", func() error { return loginAs(ctx, env, "alice") }, nil},
		{"nodes", func() error { return errOf(node.Nodes(ctx, env.token)) }, nil},
		{"list the root", func() error { return errOf(node.List(ctx, env.token, "")) }, nil},
		{"list a share", func() error { return errOf(node.List(ctx, env.token, "media")) }, nil},
		{"list a folder", func() error { return errOf(node.List(ctx, env.token, "media/sub")) }, nil},
		{"list an odd folder", func() error { return errOf(node.List(ctx, env.token, "media/odd dir")) }, nil},
		{"space", func() error { return errOf(node.Space(ctx, env.token, "media")) }, nil},
		{"head of an empty file", func() error { return errOf(node.Head(ctx, env.token, "media/empty.txt")) }, nil},
		{"read a small file", func() error { return errOf(env.readRange(ctx, smallFile, 3, 4)) }, nil},
		{"read the first block", func() error { return errOf(env.readRange(ctx, bigFile, 0, bigSize/2)) }, nil},
		{"read the second block", func() error { return errOf(env.readRange(ctx, bigFile, bigSize/2, bigSize/2)) }, nil},
		{"read an odd name", func() error { return errOf(env.readRange(ctx, oddFile, 0, 3)) }, nil},
		{"list a share not allowed", func() error { return errOf(node.List(ctx, env.token, "docs")) }, remote.ErrForbidden},
		{
			"head of a missing file",
			func() error { return errOf(node.Head(ctx, env.token, "media/none.txt")) }, remote.ErrNotFound,
		},
		{"list of a file", func() error { return errOf(node.List(ctx, env.token, smallFile)) }, remote.ErrNotFound},
		{"head of a folder", func() error { return errOf(node.Head(ctx, env.token, "media/sub")) }, remote.ErrNotFound},
		{"read a file that changed", func() error { return readChanged(ctx, env, rec) }, remote.ErrChanged},
		{"login while limited", func() error { return loginLimited(ctx, env, rec) }, remote.ErrRateLimited},
		{"expired token", func() error { return nodesExpired(ctx, env, rec) }, remote.ErrUnauthorized},
	}
	for _, step := range calls {
		if err := step.call(); !errors.Is(err, step.want) {
			return fmt.Errorf("%s: %w, want %w", step.what, err, step.want)
		}
	}
	return nil
}

func loginAs(ctx context.Context, env *fixture, user string) error {
	resp, err := env.client.Login(ctx, user, password)
	env.token = resp.Token
	return err
}

func readChanged(ctx context.Context, env *fixture, rec *recorder) error {
	etag, err := env.etag(ctx, smallFile)
	if err != nil {
		return err
	}
	if err := rec.step("change media/a.txt"); err != nil {
		return err
	}
	return errOf(env.readPinned(ctx, smallFile, etag, 0, 4))
}

func loginLimited(ctx context.Context, env *fixture, rec *recorder) error {
	if err := rec.step("limit logins"); err != nil {
		return err
	}
	return errOf(env.client.Login(ctx, "alice", password))
}

func nodesExpired(ctx context.Context, env *fixture, rec *recorder) error {
	if err := rec.step("expire tokens"); err != nil {
		return err
	}
	return errOf(env.client.Nodes(ctx, env.token))
}

// fileName is the file of the minor of a client version: v0.5.0 is client-v0.5.json.
func fileName(clientVersion string) string {
	minor := clientVersion[:strings.LastIndex(clientVersion, ".")]
	return filepath.Join(exchangesDir, "client-"+minor+".json")
}

// TestRecordExchanges writes the file of recordedClient. It runs only when asked to, so a
// normal run never rewrites a recording.
func TestRecordExchanges(t *testing.T) {
	t.Parallel()
	if !*updateExchanges {
		t.Skip("records only with -update-exchanges")
	}
	var rec *recorder
	env := start(t, setup{
		serverVersion: recordedClient, clientVersion: recordedClient,
		wrap: func(next http.RoundTripper) http.RoundTripper {
			rec = &recorder{next: next}
			return rec
		},
	})
	rec.env = env
	rec.marks = basePlaceholders(env)
	if err := clientSession(context.Background(), env, rec); err != nil {
		t.Fatal(err)
	}
	// Placeholders are written as they are, not as JSON escapes, so the replay finds them.
	var data bytes.Buffer
	encoder := json.NewEncoder(&data)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(exchangeFile{Client: recordedClient, Exchanges: rec.exchanges}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fileName(recordedClient), data.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func loadExchanges(t *testing.T) []exchangeFile {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(exchangesDir, "*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no recorded exchanges in %s: %v", exchangesDir, err)
	}
	files := make([]exchangeFile, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var file exchangeFile
		if err := json.Unmarshal(data, &file); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		files = append(files, file)
	}
	return files
}

// replay sends every request of a file to a new node and returns each difference that
// would break that client. The node runs the client's own version, the oldest server the
// version rule lets it use.
func replay(t *testing.T, file exchangeFile, wrap func(http.RoundTripper) http.RoundTripper) []string {
	t.Helper()
	env := start(t, setup{serverVersion: file.Client, clientVersion: file.Client, wrap: wrap})
	marks := basePlaceholders(env)
	var diffs []string
	for i := range file.Exchanges {
		recorded := &file.Exchanges[i]
		if recorded.Before != "" {
			if err := applyStep(env, recorded.Before); err != nil {
				t.Fatalf("exchange %d: %v", i, err)
			}
		}
		answer := send(t, env, recorded.Request, marks.invert())
		if token := loginToken(recorded.Request.Method, recorded.Request.Path, answer.status, answer.body); token != "" {
			marks = marks.with(token, tokenPlaceholder)
		}
		got := recordResponse(answer.status, answer.header, answer.body, marks)
		for _, diff := range compareResponse(got, recorded.Response) {
			diffs = append(diffs, fmt.Sprintf("exchange %d, %s %s: %s", i, recorded.Request.Method, recorded.Request.Path, diff))
		}
	}
	return diffs
}

func send(t *testing.T, env *fixture, request recordedRequest, values placeholders) rawAnswer {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), request.Method,
		env.node.URL()+values.apply(request.Path), strings.NewReader(values.apply(string(request.Body))))
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range request.Header {
		req.Header.Set(name, values.apply(value))
	}
	resp, err := env.http.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", request.Method, request.Path, err)
	}
	body, err := io.ReadAll(resp.Body)
	if closeErr := resp.Body.Close(); err != nil || closeErr != nil {
		t.Fatalf("read body: %v, close: %v", err, closeErr)
	}
	return rawAnswer{status: resp.StatusCode, header: resp.Header, body: body}
}

// compareResponse lists what in got would break a client that relies on want. A JSON
// body may gain fields, because the client ignores fields it does not know.
func compareResponse(got, want recordedResponse) []string {
	var diffs []string
	if got.Status != want.Status {
		diffs = append(diffs, fmt.Sprintf("status %d, want %d", got.Status, want.Status))
	}
	for name, value := range want.Header {
		if !sameHeader(name, got.Header[name], value) {
			diffs = append(diffs, fmt.Sprintf("header %s %q, want %q", name, got.Header[name], value))
		}
	}
	if !sameBody(got, want) {
		diffs = append(diffs, fmt.Sprintf("body %s%s, want %s%s", got.Body, got.BodySHA256, want.Body, want.BodySHA256))
	}
	return diffs
}

func sameHeader(name, got, want string) bool {
	if name == "Content-Type" {
		gotType, _, gotErr := mime.ParseMediaType(got)
		wantType, _, wantErr := mime.ParseMediaType(want)
		return gotErr == nil && wantErr == nil && gotType == wantType
	}
	return got == want
}

func sameBody(got, want recordedResponse) bool {
	if want.Body == nil {
		return got.BodySHA256 == want.BodySHA256 && got.BodySize == want.BodySize
	}
	var gotValue, wantValue any
	if json.Unmarshal(got.Body, &gotValue) != nil || json.Unmarshal(want.Body, &wantValue) != nil {
		return false
	}
	return jsonContains(gotValue, wantValue)
}

// jsonPair is a value of a replayed body and the recorded value at the same place.
type jsonPair struct{ got, want any }

// jsonContains reports whether got has every value of want, with the same value; objects
// in got may have more keys. It walks both with a stack of the places still to compare.
func jsonContains(got, want any) bool {
	stack := []jsonPair{{got, want}}
	for len(stack) > 0 {
		next := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		children, ok := jsonChildren(next)
		if !ok {
			return false
		}
		stack = append(stack, children...)
	}
	return true
}

// jsonChildren compares one place: a scalar by value, an object or list by shape, and
// returns the places below it.
func jsonChildren(place jsonPair) ([]jsonPair, bool) {
	switch want := place.want.(type) {
	case map[string]any:
		got, ok := place.got.(map[string]any)
		if !ok {
			return nil, false
		}
		children := make([]jsonPair, 0, len(want))
		for key, value := range want {
			gotValue, found := got[key]
			if !found {
				return nil, false
			}
			children = append(children, jsonPair{gotValue, value})
		}
		return children, true
	case []any:
		got, ok := place.got.([]any)
		if !ok || len(got) != len(want) {
			return nil, false
		}
		children := make([]jsonPair, 0, len(want))
		for i := range want {
			children = append(children, jsonPair{got[i], want[i]})
		}
		return children, true
	default:
		return nil, place.got == place.want
	}
}

// TestRecordedClientsStillWork replays the recording of every supported client minor
// against the current handler.
func TestRecordedClientsStillWork(t *testing.T) {
	t.Parallel()
	for _, file := range loadExchanges(t) {
		t.Run(file.Client, func(t *testing.T) {
			t.Parallel()
			for _, diff := range replay(t, file, nil) {
				t.Error(diff)
			}
		})
	}
}

// breaking is a transport that changes the answers it passes on, as a server with a
// break in it would answer.
type breaking struct {
	next   http.RoundTripper
	change func(*http.Request, *http.Response)
}

func (b breaking) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := b.next.RoundTrip(req)
	if err == nil {
		b.change(req, resp)
	}
	return resp, err
}

// TestReplayFindsABreak plants a break into the answers and checks that the replay of
// the recording reports it.
func TestReplayFindsABreak(t *testing.T) {
	t.Parallel()
	file := loadExchanges(t)[0]
	tests := []struct {
		name   string
		change func(*http.Request, *http.Response)
	}{
		{
			name: "a range answered with 200",
			change: func(req *http.Request, resp *http.Response) {
				if req.Header.Get("Range") != "" {
					resp.StatusCode = http.StatusOK
				}
			},
		},
		{
			name:   "no server version",
			change: func(_ *http.Request, resp *http.Response) { resp.Header.Del(wire.HeaderServerVersion) },
		},
		{
			name: "another tag",
			change: func(_ *http.Request, resp *http.Response) {
				if resp.Header.Get("ETag") != "" {
					resp.Header.Set("ETag", `"other"`)
				}
			},
		},
		{
			name: "a JSON field renamed",
			change: func(req *http.Request, resp *http.Response) {
				if strings.HasSuffix(req.URL.Path, "/v1/health") {
					resp.Body = io.NopCloser(strings.NewReader(`{"state":"ok","version":"v0.5.0","node":"east"}`))
				}
			},
		},
		{
			name: "other bytes in a block",
			change: func(req *http.Request, resp *http.Response) {
				if req.Header.Get("Range") != "" {
					resp.Body = io.NopCloser(strings.NewReader("xxxx"))
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			wrap := func(next http.RoundTripper) http.RoundTripper { return breaking{next: next, change: tt.change} }
			if diffs := replay(t, file, wrap); len(diffs) == 0 {
				t.Error("the replay found no difference")
			}
		})
	}
}

// TestJSONContains covers the shapes a recorded body can have.
func TestJSONContains(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		got, want string
		ok        bool
	}{
		{name: "equal", got: `{"a":[1,{"b":"c"}]}`, want: `{"a":[1,{"b":"c"}]}`, ok: true},
		{name: "a field more", got: `{"a":1,"new":2}`, want: `{"a":1}`, ok: true},
		{name: "a field less", got: `{}`, want: `{"a":1}`},
		{name: "other value", got: `{"a":2}`, want: `{"a":1}`},
		{name: "object for list", got: `{}`, want: `[]`},
		{name: "list for object", got: `[]`, want: `{}`},
		{name: "shorter list", got: `[1]`, want: `[1,2]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got, want any
			if json.Unmarshal([]byte(tt.got), &got) != nil || json.Unmarshal([]byte(tt.want), &want) != nil {
				t.Fatal("bad test JSON")
			}
			if jsonContains(got, want) != tt.ok {
				t.Errorf("jsonContains(%s, %s) = %v", tt.got, tt.want, !tt.ok)
			}
		})
	}
}
