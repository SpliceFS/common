package apitest

import (
	"crypto"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/SpliceFS/common/api"
)

// ErrRunning means Restart was called on a node that is serving.
var ErrRunning = errors.New("apitest: node is running")

// Node is api.NewHandler served with TLS over real TCP on 127.0.0.1, the way a server
// serves by default. Its certificate is made in memory for this node alone; Client
// trusts it, unless the certificate was given (see StartWithCertificate). It holds its port from
// Start to the end of the test, also while it is stopped: a stopped node accepts each
// connection and closes it at once, so the client sees a node that is gone, and no
// other listener can take the port before Restart.
type Node struct {
	mu      sync.Mutex
	port    *httptest.Server
	handler http.Handler
	url     string
	srv     *httptest.Server
	gate    *gate
	ident   identity
	// accepted is closed when accept has returned.
	accepted chan struct{}
}

// Start serves api.NewHandler(opts) on a new port of 127.0.0.1 until the test ends.
func Start(tb testing.TB, opts api.Options) *Node {
	tb.Helper()
	return startWith(tb, opts, newECDSAKey)
}

// StartWithCertificate is Start with a certificate the caller made, for example one that
// ClusterKey.IssueNode issued. Client trusts no certificate of such a node, so a test
// builds the client that trusts it, and a test that expects a refusal gets one.
func StartWithCertificate(tb testing.TB, opts api.Options, certificate tls.Certificate) *Node {
	tb.Helper()
	return begin(tb, opts, identity{certificate: certificate, pool: x509.NewCertPool()})
}

// startWith is Start with the maker of the certificate key, so a test can make it fail.
func startWith(tb testing.TB, opts api.Options, newKey func() (crypto.Signer, error)) *Node {
	tb.Helper()
	ident, err := newIdentity(newKey)
	if err != nil {
		tb.Fatalf("%v", err)
		return nil
	}
	return begin(tb, opts, ident)
}

// begin serves the handler of opts with the certificate of ident.
func begin(tb testing.TB, opts api.Options, ident identity) *Node {
	tb.Helper()
	// An unstarted test server holds a port on 127.0.0.1, opened and closed the way
	// httptest does it; the node takes its connections and serves them with servers of
	// its own.
	port := httptest.NewUnstartedServer(nil)
	n := &Node{
		port:     port,
		handler:  api.NewHandler(opts),
		url:      "https://" + port.Listener.Addr().String() + opts.BasePath,
		ident:    ident,
		accepted: make(chan struct{}),
	}
	n.serve()
	go n.accept()
	tb.Cleanup(n.close)
	return n
}

// URL returns the base URL of the API, base path included, for remote.New.
func (n *Node) URL() string {
	return n.url
}

// Client returns a new HTTP client that trusts this node's certificate and no other.
// Each has a transport of its own, so its idle connections are its caller's.
func (n *Node) Client() *http.Client {
	return &http.Client{Transport: &http.Transport{
		TLSClientConfig:   &tls.Config{RootCAs: n.ident.pool, MinVersion: tls.VersionTLS13},
		ForceAttemptHTTP2: true,
	}}
}

// Stop drops every connection, also those in the middle of a response, and makes the
// node refuse new ones until Restart. Stopping a stopped node does nothing.
func (n *Node) Stop() {
	n.mu.Lock()
	srv := n.srv
	n.srv, n.gate = nil, nil
	n.mu.Unlock()
	if srv == nil {
		return
	}
	srv.CloseClientConnections()
	srv.Close()
}

// Restart serves again on the same port after Stop.
func (n *Node) Restart() error {
	n.mu.Lock()
	running := n.srv != nil
	n.mu.Unlock()
	if running {
		return ErrRunning
	}
	n.serve()
	return nil
}

// serve starts a server that takes its connections from the gate.
func (n *Node) serve() {
	ready := &gate{conns: make(chan net.Conn), done: make(chan struct{}), addr: n.port.Listener.Addr()}
	//nolint:gosec // A server for tests needs no header timeout.
	srv := &httptest.Server{
		Listener: ready,
		Config:   &http.Server{Handler: n.handler, ErrorLog: quietLog()},
		TLS:      &tls.Config{Certificates: []tls.Certificate{n.ident.certificate}, MinVersion: tls.VersionTLS13},
	}
	srv.StartTLS()
	n.mu.Lock()
	n.srv, n.gate = srv, ready
	n.mu.Unlock()
}

// quietLog discards what the server logs: a client a test made to fail its handshake
// is not news.
func quietLog() *log.Logger {
	return log.New(io.Discard, "", 0)
}

// accept hands each connection to the running server, or closes it while the node is
// stopped. It ends when the port is closed at the end of the test.
func (n *Node) accept() {
	defer close(n.accepted)
	for {
		conn, err := n.port.Listener.Accept()
		if err != nil {
			return
		}
		n.mu.Lock()
		g := n.gate
		n.mu.Unlock()
		if g == nil || !g.deliver(conn) {
			//nolint:errcheck // The connection is refused; how its closing went changes nothing.
			_ = conn.Close()
		}
	}
}

// close stops the node, frees its port and waits for accept to end, so nothing of the
// node runs after its test.
func (n *Node) close() {
	n.Stop()
	n.port.Close()
	<-n.accepted
}

// gate is the listener of one server: it yields the connections accept hands it, until
// the server closes it.
type gate struct {
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
	addr  net.Addr
}

// Accept returns the next connection, or net.ErrClosed once the gate is closed.
func (g *gate) Accept() (net.Conn, error) {
	select {
	case conn := <-g.conns:
		return conn, nil
	case <-g.done:
		return nil, net.ErrClosed
	}
}

// Close ends Accept. The port stays open; the node owns it.
func (g *gate) Close() error {
	g.once.Do(func() { close(g.done) })
	return nil
}

// Addr is the address of the node's port.
func (g *gate) Addr() net.Addr {
	return g.addr
}

// deliver gives conn to the server, and reports false when the server has stopped.
func (g *gate) deliver(conn net.Conn) bool {
	select {
	case g.conns <- conn:
		return true
	case <-g.done:
		return false
	}
}
