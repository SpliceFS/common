package remote

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"slices"
	"time"
)

var (
	// ErrNoHost means TLSConfig was given no host name to verify a server for.
	ErrNoHost = errors.New("remote: no host name to verify the server for")
	// ErrInvalidClusterKey means a cluster key is not the base64 of a 32-byte Ed25519
	// public key.
	ErrInvalidClusterKey = errors.New("remote: invalid cluster key")
)

// The errors below say why a certificate is not trusted as a cluster node's.
var (
	errNotClusterSigned = errors.New("not signed by the cluster key")
	errOutsideValidity  = errors.New("signed by the cluster key but outside its validity time")
	errNoCertificate    = errors.New("remote: the server sent no certificate")
)

// TLSOptions say which servers a client trusts. The zero value trusts what the system's
// roots vouch for.
type TLSOptions struct {
	// ClusterKey is the public key of the cluster, from ParseClusterKey. A server is then
	// also trusted when its certificate was signed by it and is within its validity time.
	// Host names are not checked in that case: every holder of the cluster key is the
	// cluster. Empty means no cluster key.
	ClusterKey ed25519.PublicKey
	// Roots are the authorities that vouch for a server's host name. Nil means the system's.
	Roots *x509.CertPool
	// Now returns the time a certificate is checked at. Nil means time.Now.
	Now func() time.Time
	// Dial connects for TLSDialer, which runs the handshake over the connection it returns,
	// so a client keeps its own dialer and adds only the trust check. Nil means the
	// DialContext of a zero net.Dialer. TLSConfig does not use it.
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)
}

// ParseClusterKey decodes a cluster key as `splicefsdctl cluster show` prints it: the
// standard base64 of the 32-byte Ed25519 public key.
func ParseClusterKey(text string) (ed25519.PublicKey, error) {
	key, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		return nil, fmt.Errorf("%w: not base64: %w", ErrInvalidClusterKey, err)
	}
	if err := checkClusterKey(key); err != nil {
		return nil, err
	}
	return key, nil
}

func checkClusterKey(key []byte) error {
	if len(key) != ed25519.PublicKeySize {
		return fmt.Errorf("%w: %d bytes, want %d", ErrInvalidClusterKey, len(key), ed25519.PublicKeySize)
	}
	return nil
}

// TLSConfig returns the TLS configuration for an *http.Client that talks to the node at
// host, the host name or IP address of the node's URL without its port. Without a cluster
// key it is the standard configuration, which verifies the server's chain against the
// roots for host. With one, the verification is replaced by one that accepts what the
// standard one accepts and, besides, a certificate the cluster key signed. Every
// connection is verified, resumed ones too. The configuration is bound to host, so a
// client of several nodes needs one transport for each.
func TLSConfig(host string, opts TLSOptions) (*tls.Config, error) {
	if host == "" {
		return nil, ErrNoHost
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host, RootCAs: opts.Roots, Time: opts.Now}
	if len(opts.ClusterKey) == 0 {
		return cfg, nil
	}
	if err := checkClusterKey(opts.ClusterKey); err != nil {
		return nil, err
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	check := trust{host: host, clusterKey: slices.Clone(opts.ClusterKey), roots: opts.Roots, now: now}
	// This switches off only the built-in verification. VerifyConnection replaces it and
	// runs on every connection, so no certificate goes unchecked.
	cfg.InsecureSkipVerify = true
	cfg.VerifyConnection = check.verify
	return cfg, nil
}

// trust verifies the certificate of a server for TLSConfig. It holds copies, so its caller
// cannot change what it trusts.
type trust struct {
	host       string
	clusterKey ed25519.PublicKey
	roots      *x509.CertPool
	now        func() time.Time
}

// verify trusts a server whose certificate the cluster key signed, or which the roots vouch
// for under host. The host comes from the configuration, not from the connection: a
// connection to an IP address carries no server name, and an empty name would switch off
// the host check of the roots.
func (t trust) verify(state tls.ConnectionState) error {
	if len(state.PeerCertificates) == 0 {
		return errNoCertificate
	}
	clusterErr := t.verifyClusterSigned(state.PeerCertificates[0])
	if clusterErr == nil {
		return nil
	}
	rootsErr := t.verifyForHost(state.PeerCertificates)
	if rootsErr == nil {
		return nil
	}
	return &tls.CertificateVerificationError{
		UnverifiedCertificates: state.PeerCertificates,
		Err:                    fmt.Errorf("%w; not trusted for host %s either: %w", clusterErr, t.host, rootsErr),
	}
}

// verifyClusterSigned checks the signature of the cluster key on the certificate and
// that it is valid now. Possession of the certificate's own key is proven by the TLS
// handshake.
func (t trust) verifyClusterSigned(leaf *x509.Certificate) error {
	if leaf.SignatureAlgorithm != x509.PureEd25519 ||
		!ed25519.Verify(t.clusterKey, leaf.RawTBSCertificate, leaf.Signature) {
		return errNotClusterSigned
	}
	if now := t.now(); now.Before(leaf.NotBefore) || now.After(leaf.NotAfter) {
		return errOutsideValidity
	}
	return nil
}

// verifyForHost is the verification of crypto/tls, which InsecureSkipVerify switches off.
func (t trust) verifyForHost(certs []*x509.Certificate) error {
	intermediates := x509.NewCertPool()
	for _, cert := range certs[1:] {
		intermediates.AddCert(cert)
	}
	_, err := certs[0].Verify(x509.VerifyOptions{
		Roots: t.roots, CurrentTime: t.now(), DNSName: t.host, Intermediates: intermediates,
	})
	//nolint:wrapcheck // verify names the host and the cluster key beside it.
	return err
}
