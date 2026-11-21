package apitest

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"io"
	"math/big"
	"net"
	"time"
)

// ClusterKey is the Ed25519 key pair of a cluster, made in memory. It issues node
// certificates the way a server does that signs its own with the cluster key, so servers
// and clients can be tested against them.
type ClusterKey struct {
	public  ed25519.PublicKey
	private ed25519.PrivateKey
}

// NewClusterKey makes a new cluster key pair.
func NewClusterKey() (ClusterKey, error) {
	return clusterKeyFrom(rand.Reader)
}

// clusterKeyFrom is NewClusterKey with the source of randomness, so a test can make it fail.
func clusterKeyFrom(random io.Reader) (ClusterKey, error) {
	public, private, err := ed25519.GenerateKey(random)
	if err != nil {
		return ClusterKey{}, fmt.Errorf("apitest: cluster key: %w", err)
	}
	return ClusterKey{public: public, private: private}, nil
}

// Public returns a copy of the 32-byte public key, which is what a client is given.
func (k ClusterKey) Public() ed25519.PublicKey {
	return append(ed25519.PublicKey(nil), k.public...)
}

// IssueNode makes a certificate for a new ECDSA P-256 node key, signed by the cluster
// key and valid from notBefore to notAfter. Each name is a host name, or an IP address
// when it parses as one. Pass it to StartWithCertificate to serve it.
func (k ClusterKey) IssueNode(names []string, notBefore, notAfter time.Time) (tls.Certificate, error) {
	return k.issueNodeWith(newECDSAKey, names, notBefore, notAfter)
}

// issueNodeWith is IssueNode with the maker of the node key, so a test can make it fail.
func (k ClusterKey) issueNodeWith(
	newKey func() (crypto.Signer, error), names []string, notBefore, notAfter time.Time,
) (tls.Certificate, error) {
	nodeKey, err := newKey()
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("apitest: node key: %w", err)
	}
	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "apitest node"},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	for _, name := range names {
		if ip := net.ParseIP(name); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else {
			template.DNSNames = append(template.DNSNames, name)
		}
	}
	issuer := x509.Certificate{Subject: pkix.Name{CommonName: "apitest cluster"}}
	der, err := x509.CreateCertificate(rand.Reader, &template, &issuer, nodeKey.Public(), k.private)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("apitest: node certificate: %w", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: nodeKey}, nil
}
