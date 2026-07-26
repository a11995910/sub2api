package tlsfingerprint

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The fingerprint transport can only speak HTTP/1.1, so anything else must be
// rejected before it reaches a ClientHello. See ALPNHTTP11.
func TestUnsupportedALPNProtocols(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{name: "empty means built-in default", input: nil, want: nil},
		{name: "http/1.1 only", input: []string{"http/1.1"}, want: nil},
		{name: "h2 rejected", input: []string{"h2"}, want: []string{"h2"}},
		{name: "h2 alongside http/1.1 still rejected", input: []string{"h2", "http/1.1"}, want: []string{"h2"}},
		{name: "h3 rejected", input: []string{"h3", "spdy/3.1"}, want: []string{"h3", "spdy/3.1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, UnsupportedALPNProtocols(tt.input))
		})
	}
}

func TestCacheKeyDistinguishesFingerprints(t *testing.T) {
	base := &Profile{
		Name:          "node",
		CipherSuites:  []uint16{0x1301, 0x1302},
		Curves:        []uint16{29, 23},
		ALPNProtocols: []string{"http/1.1"},
		Extensions:    []uint16{0, 10, 11},
	}

	t.Run("stable across calls", func(t *testing.T) {
		require.Equal(t, base.CacheKey(), base.CacheKey())
	})

	t.Run("equal profiles share a key", func(t *testing.T) {
		clone := *base
		require.Equal(t, base.CacheKey(), clone.CacheKey())
	})

	t.Run("nil is its own key", func(t *testing.T) {
		var nilProfile *Profile
		require.Equal(t, "none", nilProfile.CacheKey())
		require.NotEqual(t, nilProfile.CacheKey(), base.CacheKey())
	})

	// Every field that changes the ClientHello must change the key, otherwise a
	// pooled client keeps serving the old fingerprint after an admin edits the profile.
	mutations := map[string]func(p *Profile){
		"name":          func(p *Profile) { p.Name = "chrome" },
		"grease":        func(p *Profile) { p.EnableGREASE = true },
		"cipher_suites": func(p *Profile) { p.CipherSuites = []uint16{0x1301} },
		"curves":        func(p *Profile) { p.Curves = []uint16{29} },
		"point_formats": func(p *Profile) { p.PointFormats = []uint16{0} },
		"sig_algs":      func(p *Profile) { p.SignatureAlgorithms = []uint16{0x0403} },
		"alpn":          func(p *Profile) { p.ALPNProtocols = []string{"h2"} },
		"versions":      func(p *Profile) { p.SupportedVersions = []uint16{0x0304} },
		"key_shares":    func(p *Profile) { p.KeyShareGroups = []uint16{29} },
		"psk_modes":     func(p *Profile) { p.PSKModes = []uint16{1} },
		"extensions":    func(p *Profile) { p.Extensions = []uint16{0, 10} },
	}
	for field, mutate := range mutations {
		t.Run("changing "+field+" changes the key", func(t *testing.T) {
			mutated := *base
			mutate(&mutated)
			require.NotEqual(t, base.CacheKey(), mutated.CacheKey())
		})
	}
}

// The returned connection must expose the standard TLS state, because HTTP clients
// type-assert for it when deciding how to drive the connection.
var _ interface{ ConnectionState() tls.ConnectionState } = (*uTLSConn)(nil)

func TestNewTLSHandshakeFuncPerformsHandshakeAndClosesConnOnFailure(t *testing.T) {
	// A TLS server with a self-signed cert: the handshake gets far enough to verify
	// the certificate and then fails, which proves the hook really drove a handshake
	// rather than handing the plaintext conn straight back.
	srv := newLocalTLSServer(t)
	handshake := NewTLSHandshakeFunc(&Profile{Name: "test"})

	plainConn, err := net.Dial("tcp", srv.Addr().String())
	require.NoError(t, err)

	conn, state, err := handshake(context.Background(), srv.Addr().String(), plainConn)
	require.Error(t, err, "self-signed cert should fail verification")
	require.Nil(t, conn)
	require.Nil(t, state)

	// The hook owns the conn once it takes it; a leaked socket per failed handshake
	// would slowly exhaust the process.
	_, readErr := plainConn.Read(make([]byte, 1))
	require.Error(t, readErr, "expected the failed handshake to have closed the conn")
}

func TestNewTLSHandshakeFuncPropagatesDialFailure(t *testing.T) {
	// Closed conn: the handshake cannot write its ClientHello.
	local, remote := net.Pipe()
	require.NoError(t, remote.Close())

	handshake := NewTLSHandshakeFunc(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	conn, state, err := handshake(ctx, "example.com:443", local)
	require.Error(t, err)
	require.Nil(t, conn)
	require.Nil(t, state)
}

// newLocalTLSServer starts a TLS listener with a self-signed certificate and returns it.
func newLocalTLSServer(t *testing.T) net.Listener {
	t.Helper()

	cert := selfSignedCert(t)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				// Drive the server side of the handshake, then drop the connection.
				if tc, ok := conn.(*tls.Conn); ok {
					_ = tc.Handshake()
				}
				_ = conn.Close()
			}()
		}
	}()

	return ln
}

func selfSignedCert(t *testing.T) tls.Certificate {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	require.NoError(t, err)

	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}
