package remote

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"math/big"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/SpliceFS/common/apitest"
)

// trustNow is the time every certificate of these tests is checked at. The verification
// takes it as an option, so no test depends on the date it runs on.
func trustNow() time.Time { return time.Date(2026, 2, 15, 12, 0, 0, 0, time.UTC) }

func clusterKey(t *testing.T) apitest.ClusterKey {
	t.Helper()
	key, err := apitest.NewClusterKey()
	if err != nil {
		t.Fatalf("NewClusterKey: %v", err)
	}
	return key
}

// issued returns the certificate key issues for names, valid from the given number of days
// around trustNow.
func issued(t *testing.T, key apitest.ClusterKey, names []string, daysBefore, daysAfter int) *x509.Certificate {
	t.Helper()
	day := 24 * time.Hour
	from := trustNow().Add(-time.Duration(daysBefore) * day)
	until := trustNow().Add(time.Duration(daysAfter) * day)
	chain, err := key.IssueNode(names, from, until)
	if err != nil {
		t.Fatalf("IssueNode: %v", err)
	}
	cert, err := x509.ParseCertificate(chain.Certificate[0])
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return cert
}

// selfSigned returns the certificate of a test TLS server: self-signed, for 127.0.0.1 and
// valid around trustNow.
func selfSigned(t *testing.T) *x509.Certificate {
	t.Helper()
	srv := httptest.NewTLSServer(nil)
	t.Cleanup(srv.Close)
	return srv.Certificate()
}

func poolOf(certs ...*x509.Certificate) *x509.CertPool {
	pool := x509.NewCertPool()
	for _, cert := range certs {
		pool.AddCert(cert)
	}
	return pool
}

func peers(certs ...*x509.Certificate) tls.ConnectionState {
	return tls.ConnectionState{PeerCertificates: certs}
}

func TestClusterKeyTrust(t *testing.T) {
	t.Parallel()
	key, other := clusterKey(t), clusterKey(t)
	own := selfSigned(t)
	tests := []struct {
		name string
		host string
		// roots are the roots a certificate is checked against besides the cluster key.
		roots *x509.CertPool
		cert  *x509.Certificate
		// wantErr is nil when the certificate must be trusted.
		wantErr error
	}{
		{
			name: "signed by the cluster key, names ignored", host: "127.0.0.1", roots: poolOf(),
			cert: issued(t, key, []string{"other.example.test"}, 30, 60),
		},
		{
			name: "signed by the cluster key, no names", host: "127.0.0.1", roots: poolOf(),
			cert: issued(t, key, nil, 30, 60),
		},
		{
			name: "signed by another key", host: "127.0.0.1", roots: poolOf(),
			cert: issued(t, other, []string{"127.0.0.1"}, 30, 60), wantErr: errNotClusterSigned,
		},
		{
			name: "signed by the cluster key but expired", host: "127.0.0.1", roots: poolOf(),
			cert: issued(t, key, []string{"127.0.0.1"}, 60, -1), wantErr: errOutsideValidity,
		},
		{
			name: "signed by the cluster key but not valid yet", host: "127.0.0.1", roots: poolOf(),
			cert: issued(t, key, []string{"127.0.0.1"}, -1, 60), wantErr: errOutsideValidity,
		},
		{
			name: "self-signed", host: "127.0.0.1", roots: poolOf(),
			cert: own, wantErr: errNotClusterSigned,
		},
		{
			name: "self-signed, against the system roots", host: "127.0.0.1",
			cert: own, wantErr: errNotClusterSigned,
		},
		{
			name: "trusted by the roots for its host", host: "127.0.0.1", roots: poolOf(own), cert: own,
		},
		{
			name: "trusted by the roots, but for another host", host: "other.example.test", roots: poolOf(own),
			cert: own, wantErr: errNotClusterSigned,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := TLSConfig(tt.host, TLSOptions{ClusterKey: key.Public(), Roots: tt.roots, Now: trustNow})
			if err != nil {
				t.Fatalf("TLSConfig: %v", err)
			}
			err = cfg.VerifyConnection(peers(tt.cert))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("VerifyConnection = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil {
				return
			}
			if _, ok := errors.AsType[*tls.CertificateVerificationError](err); !ok {
				t.Errorf("VerifyConnection = %T, want the error type the standard verification returns", err)
			}
		})
	}
}

func TestClusterKeyTrustNamesBothReasonsForARefusal(t *testing.T) {
	t.Parallel()
	key := clusterKey(t)
	cfg, err := TLSConfig("127.0.0.1", TLSOptions{ClusterKey: key.Public(), Roots: poolOf(), Now: trustNow})
	if err != nil {
		t.Fatal(err)
	}
	err = cfg.VerifyConnection(peers(issued(t, clusterKey(t), []string{"127.0.0.1"}, 30, 60)))
	var unknown x509.UnknownAuthorityError
	if !errors.Is(err, errNotClusterSigned) || !errors.As(err, &unknown) {
		t.Errorf("VerifyConnection = %v, want why the cluster key and why the roots refused it", err)
	}
	err = cfg.VerifyConnection(peers(issued(t, key, []string{"127.0.0.1"}, 60, -1)))
	var invalid x509.CertificateInvalidError
	if !errors.Is(err, errOutsideValidity) || !errors.As(err, &invalid) {
		t.Errorf("VerifyConnection = %v, want why the cluster key and why the roots refused it", err)
	}
}

func TestVerifyRefusesAnAnswerWithoutCertificate(t *testing.T) {
	t.Parallel()
	cfg, err := TLSConfig("127.0.0.1", TLSOptions{ClusterKey: clusterKey(t).Public()})
	if err != nil {
		t.Fatal(err)
	}
	if err = cfg.VerifyConnection(tls.ConnectionState{}); !errors.Is(err, errNoCertificate) {
		t.Errorf("VerifyConnection = %v, want %v", err, errNoCertificate)
	}
}

func TestVerifyFollowsIntermediates(t *testing.T) {
	t.Parallel()
	chain := chainOf(t, "node.example.test")
	opts := TLSOptions{ClusterKey: clusterKey(t).Public(), Roots: poolOf(chain.root), Now: trustNow}
	cfg, err := TLSConfig("node.example.test", opts)
	if err != nil {
		t.Fatal(err)
	}
	if err = cfg.VerifyConnection(peers(chain.leaf, chain.intermediate)); err != nil {
		t.Errorf("a chain with its intermediate: %v", err)
	}
	if err = cfg.VerifyConnection(peers(chain.leaf)); err == nil {
		t.Error("a chain without its intermediate was trusted")
	}
}

// certChain is a root, an intermediate the root signed and a leaf the intermediate signed.
type certChain struct{ root, intermediate, leaf *x509.Certificate }

// chainOf makes a chain whose leaf is for name, all valid around trustNow.
func chainOf(t *testing.T, name string) certChain {
	t.Helper()
	rootKey, middleKey, leafKey := ecKey(t), ecKey(t), ecKey(t)
	rootName := pkix.Name{CommonName: "root"}
	root := signed(t, &x509.Certificate{Subject: rootName, IsCA: true}, nil, rootKey, rootKey)
	middleName := pkix.Name{CommonName: "intermediate"}
	middle := signed(t, &x509.Certificate{Subject: middleName, IsCA: true}, root, middleKey, rootKey)
	leaf := &x509.Certificate{Subject: pkix.Name{CommonName: name}, DNSNames: []string{name}}
	return certChain{root: root, intermediate: middle, leaf: signed(t, leaf, middle, leafKey, middleKey)}
}

func ecKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// signed makes the certificate of template for the key subject, signed by issuerKey as
// issuer; a nil issuer makes it self-signed.
func signed(t *testing.T, template, issuer *x509.Certificate, subject, issuerKey *ecdsa.PrivateKey) *x509.Certificate {
	t.Helper()
	template.SerialNumber = big.NewInt(1)
	template.NotBefore = trustNow().Add(-time.Hour)
	template.NotAfter = trustNow().Add(time.Hour)
	template.BasicConstraintsValid = true
	template.KeyUsage = x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign
	if issuer == nil {
		issuer = template
	}
	der, err := x509.CreateCertificate(rand.Reader, template, issuer, &subject.PublicKey, issuerKey)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

func TestTLSConfigWithoutClusterKeyUsesTheStandardVerification(t *testing.T) {
	t.Parallel()
	roots := poolOf()
	for _, key := range [][]byte{nil, {}} {
		cfg, err := TLSConfig("east.example.test", TLSOptions{ClusterKey: key, Roots: roots, Now: trustNow})
		if err != nil {
			t.Fatalf("TLSConfig: %v", err)
		}
		if cfg.InsecureSkipVerify || cfg.VerifyConnection != nil || cfg.VerifyPeerCertificate != nil {
			t.Errorf("a config without a cluster key replaces the verification: %+v", cfg)
		}
		if cfg.RootCAs != roots || cfg.ServerName != "east.example.test" || cfg.Time().Compare(trustNow()) != 0 {
			t.Errorf("config roots, name or time not as given: %+v", cfg)
		}
	}
}

func TestTLSConfigDefaults(t *testing.T) {
	t.Parallel()
	cfg, err := TLSConfig("east.example.test", TLSOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RootCAs != nil || cfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("RootCAs = %v, MinVersion = %#x, want the system roots and TLS 1.2", cfg.RootCAs, cfg.MinVersion)
	}
}

func TestTLSConfigRefuses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		host    string
		key     []byte
		wantErr error
	}{
		{name: "no host", host: "", wantErr: ErrNoHost},
		{name: "no host with a key", host: "", key: make([]byte, 32), wantErr: ErrNoHost},
		{name: "short key", host: "east.example.test", key: make([]byte, 31), wantErr: ErrInvalidClusterKey},
		{name: "long key", host: "east.example.test", key: make([]byte, 33), wantErr: ErrInvalidClusterKey},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := TLSConfig(tt.host, TLSOptions{ClusterKey: tt.key})
			if !errors.Is(err, tt.wantErr) || cfg != nil {
				t.Errorf("TLSConfig = %v, %v, want no config and %v", cfg, err, tt.wantErr)
			}
		})
	}
}

func TestTLSConfigKeepsItsOwnCopyOfTheKey(t *testing.T) {
	t.Parallel()
	key := clusterKey(t)
	given := key.Public()
	cfg, err := TLSConfig("127.0.0.1", TLSOptions{ClusterKey: given, Roots: poolOf(), Now: trustNow})
	if err != nil {
		t.Fatal(err)
	}
	clear(given)
	if err = cfg.VerifyConnection(peers(issued(t, key, nil, 30, 60))); err != nil {
		t.Errorf("a caller changed the key after TLSConfig: %v", err)
	}
}

func TestParseClusterKey(t *testing.T) {
	t.Parallel()
	keyOf := func(size int) []byte { return bytes.Repeat([]byte{7}, size) }
	tests := []struct {
		name    string
		text    string
		want    []byte
		wantErr bool
	}{
		{name: "32 bytes", text: base64.StdEncoding.EncodeToString(keyOf(32)), want: keyOf(32)},
		{name: "empty", text: "", wantErr: true},
		{name: "not base64", text: "not base64!", wantErr: true},
		{name: "16 bytes", text: base64.StdEncoding.EncodeToString(keyOf(16)), wantErr: true},
		{name: "33 bytes", text: base64.StdEncoding.EncodeToString(keyOf(33)), wantErr: true},
		{name: "URL alphabet", text: strings.Repeat("-", 43) + "=", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseClusterKey(tt.text)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidClusterKey) {
					t.Errorf("ParseClusterKey(%q) error = %v, want ErrInvalidClusterKey", tt.text, err)
				}
				return
			}
			if err != nil || !bytes.Equal(got, tt.want) {
				t.Errorf("ParseClusterKey(%q) = %x, %v, want %x", tt.text, got, err, tt.want)
			}
		})
	}
}
