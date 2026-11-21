package apitest

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/SpliceFS/common/api"
)

// The period of a certificate a test issues.
func validFrom() time.Time  { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
func validUntil() time.Time { return time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC) }

func mustClusterKey(t *testing.T) ClusterKey {
	t.Helper()
	key, err := NewClusterKey()
	if err != nil {
		t.Fatalf("NewClusterKey: %v", err)
	}
	return key
}

func TestClusterKeysDiffer(t *testing.T) {
	t.Parallel()
	first, second := mustClusterKey(t), mustClusterKey(t)
	if len(first.Public()) != ed25519.PublicKeySize {
		t.Fatalf("public key has %d bytes, want %d", len(first.Public()), ed25519.PublicKeySize)
	}
	if first.Public().Equal(second.Public()) {
		t.Error("two cluster keys are the same")
	}
}

func TestClusterKeyKeepsItsPublicKey(t *testing.T) {
	t.Parallel()
	key := mustClusterKey(t)
	want := key.Public()
	key.Public()[0]++
	if !key.Public().Equal(want) {
		t.Error("a caller changed the cluster key through Public")
	}
}

func issuedLeaf(t *testing.T, key ClusterKey, names []string) *x509.Certificate {
	t.Helper()
	cert, err := key.IssueNode(names, validFrom(), validUntil())
	if err != nil {
		t.Fatalf("IssueNode: %v", err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return leaf
}

func TestIssueNodeCertificateIsSignedByTheClusterKey(t *testing.T) {
	t.Parallel()
	key := mustClusterKey(t)
	leaf := issuedLeaf(t, key, []string{"east.example.test"})
	if leaf.SignatureAlgorithm != x509.PureEd25519 {
		t.Errorf("signature algorithm = %v, want PureEd25519", leaf.SignatureAlgorithm)
	}
	if !ed25519.Verify(key.Public(), leaf.RawTBSCertificate, leaf.Signature) {
		t.Error("the signature does not verify with the cluster key")
	}
	if public, ok := leaf.PublicKey.(*ecdsa.PublicKey); !ok || public.Curve != elliptic.P256() {
		t.Errorf("node key = %T, want an ECDSA P-256 key", leaf.PublicKey)
	}
	if leaf.IsCA {
		t.Error("a node certificate is a CA")
	}
}

func TestIssueNodeCertificateHoldsNamesAndValidity(t *testing.T) {
	t.Parallel()
	leaf := issuedLeaf(t, mustClusterKey(t), []string{"east.example.test", "192.0.2.7", "::1"})
	if !slices.Equal(leaf.DNSNames, []string{"east.example.test"}) {
		t.Errorf("DNSNames = %v", leaf.DNSNames)
	}
	wantIPs := []net.IP{net.ParseIP("192.0.2.7"), net.ParseIP("::1")}
	if !slices.EqualFunc(leaf.IPAddresses, wantIPs, net.IP.Equal) {
		t.Errorf("IPAddresses = %v, want %v", leaf.IPAddresses, wantIPs)
	}
	if !leaf.NotBefore.Equal(validFrom()) || !leaf.NotAfter.Equal(validUntil()) {
		t.Errorf("valid %v to %v, want %v to %v", leaf.NotBefore, leaf.NotAfter, validFrom(), validUntil())
	}
}

func TestClusterKeyFailures(t *testing.T) {
	t.Parallel()
	errBroken := errors.New("broken")
	if _, err := clusterKeyFrom(failingReader{errBroken}); !errors.Is(err, errBroken) {
		t.Errorf("clusterKeyFrom with a failing source = %v, want %v", err, errBroken)
	}
	key := mustClusterKey(t)
	newKey := func() (crypto.Signer, error) { return nil, errBroken }
	if _, err := key.issueNodeWith(newKey, nil, validFrom(), validUntil()); !errors.Is(err, errBroken) {
		t.Errorf("issueNodeWith with a failing node key = %v, want %v", err, errBroken)
	}
	// A DNS name must be ASCII to be encoded in a certificate.
	if _, err := key.IssueNode([]string{"\u00e4.example.test"}, validFrom(), validUntil()); err == nil {
		t.Error("IssueNode accepted a name a certificate cannot hold")
	}
}

// failingReader is a source of randomness that fails.
type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

func TestStartWithCertificateServesIt(t *testing.T) {
	t.Parallel()
	key := mustClusterKey(t)
	// Valid on every date a test can run on, so that the test does not depend on the clock.
	forever := time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)
	cert, err := key.IssueNode([]string{"east.example.test"}, time.Unix(0, 0), forever)
	if err != nil {
		t.Fatalf("IssueNode: %v", err)
	}
	node := StartWithCertificate(t, api.Options{Node: "east", Nodes: &NodeList{}}, cert)
	// This client looks at the certificate and trusts it for nothing.
	looking := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	t.Cleanup(looking.CloseIdleConnections)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, node.URL()+"/v1/health", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := looking.Do(req)
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	if err = resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if got := resp.TLS.PeerCertificates[0].Raw; !bytes.Equal(got, cert.Certificate[0]) {
		t.Error("the node served another certificate")
	}
	// Trusting a certificate the node did not make is the client's work, not Client's.
	var refused *tls.CertificateVerificationError
	if _, err = health(t, node.Client(), node.URL()); !errors.As(err, &refused) {
		t.Errorf("Client of a node with a given certificate got %v, want a refused certificate", err)
	}
}
