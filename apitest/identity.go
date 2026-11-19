package apitest

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"time"
)

// identity is the certificate a node serves and the pool of its clients, which trusts
// that certificate only.
type identity struct {
	certificate tls.Certificate
	pool        *x509.CertPool
}

// newECDSAKey makes the key of a node's certificate. It returns a crypto.Signer because
// newIdentity takes any signer, so a test can give it one that fails.
func newECDSAKey() (crypto.Signer, error) {
	//nolint:wrapcheck // newIdentity wraps it with what failed.
	return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
}

// newIdentity makes a self-signed certificate for 127.0.0.1 in memory. Its validity
// spans every date a test can run on, so no test depends on the clock.
func newIdentity(newKey func() (crypto.Signer, error)) (identity, error) {
	key, err := newKey()
	if err != nil {
		return identity{}, fmt.Errorf("apitest: certificate key: %w", err)
	}
	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "apitest"},
		NotBefore:             time.Unix(0, 0),
		NotAfter:              time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, key.Public(), key)
	if err != nil {
		return identity{}, fmt.Errorf("apitest: certificate: %w", err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	return identity{certificate: tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, pool: pool}, nil
}
