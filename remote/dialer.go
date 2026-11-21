package remote

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
)

// TLSDialer returns a dial function for http.Transport.DialTLSContext. It splits the
// address into host and port, builds the TLSConfig of opts for that host, connects and
// runs the handshake under ctx, so ending the context ends a handshake the server never
// answers. A transport made with it trusts what opts say for the host it dials, IP
// addresses included, and the options belong to one cluster: a client of several clusters
// needs one transport for each. The connection it returns is already secured, so the
// transport adds no handshake of its own, and its TLSHandshakeTimeout does not apply: ctx
// bounds the dial and the handshake.
func TLSDialer(opts TLSOptions) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("remote: dial %s: %w", addr, err)
		}
		cfg, err := TLSConfig(host, opts)
		if err != nil {
			return nil, err
		}
		var dialer net.Dialer
		raw, err := dialer.DialContext(ctx, network, addr)
		if err != nil {
			return nil, fmt.Errorf("remote: dial %s: %w", addr, err)
		}
		conn := tls.Client(raw, cfg)
		if err = conn.HandshakeContext(ctx); err != nil {
			//nolint:errcheck // the handshake error is the one to report; a failed close adds nothing
			_ = raw.Close()
			return nil, fmt.Errorf("remote: TLS handshake with %s: %w", addr, err)
		}
		return conn, nil
	}
}
