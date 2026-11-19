package apitest

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/SpliceFS/common/api"
	"github.com/SpliceFS/common/wire"
)

func TestNodeListKeepsItsOwnCopy(t *testing.T) {
	t.Parallel()
	var list NodeList
	nodes := wire.Nodes{Cluster: "c", Nodes: []wire.Node{{Name: "east", URL: "http://east"}}}
	list.Set(nodes)
	nodes.Nodes[0].Name = "changed"
	got, err := list.Nodes()
	if err != nil || got.Cluster != "c" || got.Nodes[0].Name != "east" {
		t.Fatalf("Nodes = %+v, %v", got, err)
	}
	got.Nodes[0].Name = "changed"
	if again, err := list.Nodes(); err != nil || again.Nodes[0].Name != "east" {
		t.Errorf("a caller changed the list: %+v", again)
	}
}

// health asks the node for its health and returns the node name it reports, or the error.
func health(t *testing.T, client *http.Client, url string) (string, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url+"/v1/health", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	var body wire.Health
	err = json.NewDecoder(resp.Body).Decode(&body)
	if closeErr := resp.Body.Close(); err != nil || closeErr != nil {
		t.Fatalf("health body: %v, close: %v", err, closeErr)
	}
	return body.Node, nil
}

func TestStartStopRestart(t *testing.T) {
	t.Parallel()
	node := Start(t, api.Options{BasePath: "/api", Node: "east", Version: "v0.4.0", Nodes: &NodeList{}})
	if !strings.HasPrefix(node.URL(), "https://127.0.0.1:") || !strings.HasSuffix(node.URL(), "/api") {
		t.Fatalf("URL = %q", node.URL())
	}
	if name, err := health(t, node.Client(), node.URL()); err != nil || name != "east" {
		t.Fatalf("health = %q, %v", name, err)
	}
	if err := node.Restart(); !errors.Is(err, ErrRunning) {
		t.Errorf("Restart while running = %v, want ErrRunning", err)
	}
	node.Stop()
	node.Stop()
	if _, err := health(t, node.Client(), node.URL()); err == nil {
		t.Fatal("a stopped node answered")
	}
	if err := node.Restart(); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if name, err := health(t, node.Client(), node.URL()); err != nil || name != "east" {
		t.Errorf("health after Restart = %q, %v", name, err)
	}
}

func TestAStoppedGateTakesNoConnection(t *testing.T) {
	t.Parallel()
	closed := &gate{conns: make(chan net.Conn), done: make(chan struct{})}
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	if closed.deliver(nil) {
		t.Error("a closed gate took a connection")
	}
	if _, err := closed.Accept(); !errors.Is(err, net.ErrClosed) {
		t.Errorf("Accept = %v, want net.ErrClosed", err)
	}
}

func TestOnlyTheNodesClientTrustsIt(t *testing.T) {
	t.Parallel()
	node := Start(t, api.Options{Node: "east"})
	plain := &http.Client{Transport: &http.Transport{}}
	var unknown x509.UnknownAuthorityError
	if _, err := health(t, plain, node.URL()); !errors.As(err, &unknown) {
		t.Errorf("a client without the node's certificate got %v, want an unknown authority", err)
	}
	other := Start(t, api.Options{Node: "west"})
	if _, err := health(t, other.Client(), node.URL()); !errors.As(err, &unknown) {
		t.Errorf("another node's client got %v, want an unknown authority", err)
	}
}

func TestNewIdentityFailures(t *testing.T) {
	t.Parallel()
	errNoKey := errors.New("no key")
	tests := []struct {
		name   string
		newKey func() (crypto.Signer, error)
	}{
		{"key fails", func() (crypto.Signer, error) { return nil, errNoKey }},
		{"signing fails", func() (crypto.Signer, error) {
			key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			return failingSigner{key}, err
		}},
	}
	for _, tt := range tests {
		if _, err := newIdentity(tt.newKey); err == nil {
			t.Errorf("%s: newIdentity succeeded", tt.name)
		}
	}
}

// failingSigner has a real public key and refuses to sign.
type failingSigner struct{ key *ecdsa.PrivateKey }

func (s failingSigner) Public() crypto.PublicKey { return s.key.Public() }

func (failingSigner) Sign(io.Reader, []byte, crypto.SignerOpts) ([]byte, error) {
	return nil, errors.New("will not sign")
}

// fatalRecorder is a testing.TB whose Fatalf records the message instead of ending the
// test, so that a failing Start can be observed.
type fatalRecorder struct {
	testing.TB

	message string
}

func (*fatalRecorder) Helper() {}

func (f *fatalRecorder) Fatalf(format string, args ...any) { f.message = fmt.Sprintf(format, args...) }

func TestStartReportsAFailedIdentity(t *testing.T) {
	t.Parallel()
	recorder := &fatalRecorder{TB: t}
	failing := func() (crypto.Signer, error) { return nil, errors.New("no key") }
	if node := startWith(recorder, api.Options{}, failing); node != nil || !strings.Contains(recorder.message, "no key") {
		t.Errorf("start = %v, Fatalf %q; want nil and the cause", node, recorder.message)
	}
}
