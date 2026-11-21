package remote_test

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/SpliceFS/common/api"
	"github.com/SpliceFS/common/apitest"
	"github.com/SpliceFS/common/remote"
)

// These tests use a transport whose TLS connections are made by remote.TLSDialer, the way
// a client with one transport for each cluster does.

// nodeAt returns the node's URL with its host replaced, the port kept.
func nodeAt(t *testing.T, node *apitest.Node, host string) string {
	t.Helper()
	address, err := url.Parse(node.URL())
	if err != nil {
		t.Fatal(err)
	}
	address.Host = net.JoinHostPort(host, address.Port())
	return address.String()
}

// dialerClient returns a client of the node at nodeURL whose transport dials with TLSDialer.
func dialerClient(t *testing.T, nodeURL string, opts remote.TLSOptions) *remote.Node {
	t.Helper()
	httpClient := &http.Client{Transport: &http.Transport{DialTLSContext: remote.TLSDialer(opts)}}
	t.Cleanup(httpClient.CloseIdleConnections)
	client, err := remote.New("east", nodeURL, clientVer, httpClient)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}

// rootsOf returns the roots of the node's own client, which trust the node's certificate.
func rootsOf(t *testing.T, node *apitest.Node) *x509.CertPool {
	t.Helper()
	transport, ok := node.Client().Transport.(*http.Transport)
	if !ok {
		t.Fatalf("the client of a node has a %T, want an *http.Transport", node.Client().Transport)
	}
	return transport.TLSClientConfig.RootCAs
}

func wantRefusedCertificate(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, remote.ErrUnavailable) || !strings.Contains(err.Error(), "failed to verify certificate") {
		t.Errorf("error = %v, want the node refused as unavailable for its certificate", err)
	}
}

func TestTLSDialerReachesAClusterNodeByNameAndByIP(t *testing.T) {
	t.Parallel()
	key := newClusterKey(t)
	for _, host := range []string{"127.0.0.1", "localhost"} {
		t.Run(host, func(t *testing.T) {
			t.Parallel()
			node := clusterNode(t, key, time.Unix(0, 0), time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC))
			client := dialerClient(t, nodeAt(t, node, host), remote.TLSOptions{ClusterKey: key.Public()})
			if health, err := client.Health(context.Background()); err != nil || health.Node != "east" {
				t.Errorf("Health = %+v, %v; want the node's answer", health, err)
			}
		})
	}
}

func TestTLSDialerChecksTheHostItDialed(t *testing.T) {
	t.Parallel()
	node := apitest.Start(t, api.Options{Node: "east", Version: serverVer, Nodes: &apitest.NodeList{}})
	roots := rootsOf(t, node)
	other := newClusterKey(t).Public()
	tests := []struct {
		name    string
		host    string
		key     ed25519.PublicKey
		trusted bool
	}{
		{name: "IP address of the certificate, with a cluster key", host: "127.0.0.1", key: other, trusted: true},
		{name: "IP address of the certificate, without one", host: "127.0.0.1", trusted: true},
		{name: "another name, with a cluster key", host: "localhost", key: other},
		{name: "another name, without one", host: "localhost"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := dialerClient(t, nodeAt(t, node, tt.host), remote.TLSOptions{ClusterKey: tt.key, Roots: roots})
			_, err := client.Health(context.Background())
			if tt.trusted {
				if err != nil {
					t.Errorf("Health: %v", err)
				}
				return
			}
			wantRefusedCertificate(t, err)
		})
	}
}

func TestTLSDialerRefusesACertificateTheKeyDidNotSign(t *testing.T) {
	t.Parallel()
	always, never := time.Unix(0, 0), time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)
	node := clusterNode(t, newClusterKey(t), always, never)
	for _, key := range []ed25519.PublicKey{newClusterKey(t).Public(), nil} {
		client := dialerClient(t, node.URL(), remote.TLSOptions{ClusterKey: key})
		_, err := client.Health(context.Background())
		wantRefusedCertificate(t, err)
	}
}

func TestTLSDialerFailures(t *testing.T) {
	t.Parallel()
	ended, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		name string
		opts remote.TLSOptions
		ctx  context.Context //nolint:containedctx // The case says which context the dial gets.
		addr string
		// wantErr is the error the dial must wrap; nil means any error.
		wantErr error
	}{
		{name: "no port", ctx: context.Background(), addr: "127.0.0.1"},
		{name: "no host", ctx: context.Background(), addr: ":443", wantErr: remote.ErrNoHost},
		{
			name: "invalid cluster key", ctx: context.Background(), addr: "127.0.0.1:443",
			opts: remote.TLSOptions{ClusterKey: []byte{1}}, wantErr: remote.ErrInvalidClusterKey,
		},
		{name: "context ended before the dial", ctx: ended, addr: "127.0.0.1:443", wantErr: context.Canceled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			conn, err := remote.TLSDialer(tt.opts)(tt.ctx, "tcp", tt.addr)
			if conn != nil {
				t.Error("a failed dial returned a connection")
			}
			if err == nil || (tt.wantErr != nil && !errors.Is(err, tt.wantErr)) {
				t.Errorf("dial error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// ignore drops an error that does not matter to a test server: it ends a connection the
// client may have ended first.
func ignore(error) {}

// oneConn is a server of one connection.
type oneConn struct {
	addr string
	// accepted receives a value when the server has the connection, and closed is closed
	// once the client has closed it.
	accepted, closed <-chan struct{}
}

// oneConnServer accepts one connection and passes it to greet, which may answer the TLS
// handshake. The server then reads the connection to its end.
func oneConnServer(t *testing.T, greet func(net.Conn)) oneConn {
	t.Helper()
	var config net.ListenConfig
	listener, err := config.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	accepted, closed := make(chan struct{}, 1), make(chan struct{})
	go func() {
		defer close(closed)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		accepted <- struct{}{}
		greet(conn)
		_, err = io.Copy(io.Discard, conn)
		ignore(err)
		ignore(conn.Close())
	}()
	t.Cleanup(func() {
		ignore(listener.Close())
		<-closed
	})
	return oneConn{addr: listener.Addr().String(), accepted: accepted, closed: closed}
}

func TestTLSDialerEndsAHangingHandshakeWhenTheContextEnds(t *testing.T) {
	t.Parallel()
	// A server that never answers the handshake.
	server := oneConnServer(t, func(net.Conn) {})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error)
	go func() {
		conn, err := remote.TLSDialer(remote.TLSOptions{})(ctx, "tcp", server.addr)
		if conn != nil {
			err = errors.New("a dial that was cancelled returned a connection")
		}
		result <- err
	}()
	<-server.accepted
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Errorf("dial error = %v, want context.Canceled", err)
	}
	<-server.closed
}

func TestTLSDialerClosesTheConnectionOfARefusedCertificate(t *testing.T) {
	t.Parallel()
	cert, err := newClusterKey(t).IssueNode([]string{"node.example.test"}, time.Unix(0, 0), time.Unix(86400, 0))
	if err != nil {
		t.Fatalf("IssueNode: %v", err)
	}
	serverTLS := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13}
	// The server's handshake fails, because the client refuses the certificate.
	server := oneConnServer(t, func(conn net.Conn) {
		ignore(tls.Server(conn, serverTLS).HandshakeContext(context.Background()))
	})
	conn, err := remote.TLSDialer(remote.TLSOptions{})(context.Background(), "tcp", server.addr)
	if conn != nil || err == nil {
		t.Errorf("dial = %v, %v; want the certificate refused", conn, err)
	}
	<-server.closed
}
