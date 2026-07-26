package tlsfingerprint

import (
	"context"
	"crypto/tls"
	"hash/fnv"
	"net"
	"strconv"
	"strings"

	utls "github.com/refraction-networking/utls"
)

// ALPNHTTP11 is the only ALPN protocol the fingerprint transport can speak.
//
// The transport built by the gateway for fingerprinted upstreams is hard-wired to
// HTTP/1.1: it installs a custom DialTLSContext whose connection is a *utls.UConn
// rather than a *tls.Conn, so net/http never populates the TLS state used to switch
// to HTTP/2. Advertising "h2" would therefore let the server negotiate a protocol we
// cannot speak — the connection dies with a protocol error on the first request.
//
// This matches the real client anyway: Claude Code (Node.js 24.x) negotiates
// http/1.1, which is why the captured JA4 ends in "h1".
const ALPNHTTP11 = "http/1.1"

// IsSupportedALPN reports whether the fingerprint transport can speak the given
// ALPN protocol. See ALPNHTTP11 for why "h2" is rejected.
func IsSupportedALPN(proto string) bool {
	return strings.TrimSpace(proto) == ALPNHTTP11
}

// UnsupportedALPNProtocols returns the entries of the list that the fingerprint
// transport cannot speak. An empty list (meaning "use the built-in default") and a
// list containing only http/1.1 both return nil.
func UnsupportedALPNProtocols(protocols []string) []string {
	var bad []string
	for _, p := range protocols {
		if !IsSupportedALPN(p) {
			bad = append(bad, p)
		}
	}
	return bad
}

// CacheKey returns a stable identifier for the fingerprint a profile produces.
// Two profiles that yield the same ClientHello share a key, so upstream clients can
// be pooled across them; any edit to the profile changes the key, which lets the
// connection pool detect the change and rebuild the transport instead of silently
// serving the old fingerprint until the client is evicted.
func (p *Profile) CacheKey() string {
	if p == nil {
		return "none"
	}
	h := fnv.New64a()
	writeStr := func(s string) {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{0})
	}
	writeU16s := func(vals []uint16) {
		for _, v := range vals {
			_, _ = h.Write([]byte{byte(v >> 8), byte(v)})
		}
		_, _ = h.Write([]byte{0})
	}

	writeStr(p.Name)
	if p.EnableGREASE {
		writeStr("grease")
	}
	writeU16s(p.CipherSuites)
	writeU16s(p.Curves)
	writeU16s(p.PointFormats)
	writeU16s(p.SignatureAlgorithms)
	for _, a := range p.ALPNProtocols {
		writeStr(a)
	}
	writeU16s(p.SupportedVersions)
	writeU16s(p.KeyShareGroups)
	writeU16s(p.PSKModes)
	writeU16s(p.Extensions)

	return strconv.FormatUint(h.Sum64(), 16)
}

// uTLSConn adapts a *utls.UConn to the ConnectionState() shape expected by HTTP
// clients that type-assert for a TLS connection.
type uTLSConn struct {
	*utls.UConn
}

func (c *uTLSConn) ConnectionState() tls.ConnectionState {
	return toStdConnectionState(c.UConn.ConnectionState())
}

// NewTLSHandshakeFunc returns a TLS handshake hook that applies the profile's
// fingerprint to an already-established plaintext connection.
//
// The signature matches req's Client.SetTLSHandshake, which hands over a plain conn
// after it has done its own proxy dialing/CONNECT — so this works through HTTP and
// SOCKS5 proxies without the per-scheme dialers the gateway transport needs.
func NewTLSHandshakeFunc(profile *Profile) func(ctx context.Context, addr string, plainConn net.Conn) (net.Conn, *tls.ConnectionState, error) {
	return func(ctx context.Context, addr string, plainConn net.Conn) (net.Conn, *tls.ConnectionState, error) {
		uconn, err := Handshake(ctx, plainConn, profile, addr)
		if err != nil {
			return nil, nil, err
		}
		state := toStdConnectionState(uconn.ConnectionState())
		return &uTLSConn{UConn: uconn}, &state, nil
	}
}

func toStdConnectionState(cs utls.ConnectionState) tls.ConnectionState {
	return tls.ConnectionState{
		Version:                     cs.Version,
		HandshakeComplete:           cs.HandshakeComplete,
		DidResume:                   cs.DidResume,
		CipherSuite:                 cs.CipherSuite,
		NegotiatedProtocol:          cs.NegotiatedProtocol,
		NegotiatedProtocolIsMutual:  cs.NegotiatedProtocolIsMutual,
		ServerName:                  cs.ServerName,
		PeerCertificates:            cs.PeerCertificates,
		VerifiedChains:              cs.VerifiedChains,
		SignedCertificateTimestamps: cs.SignedCertificateTimestamps,
		OCSPResponse:                cs.OCSPResponse,
		TLSUnique:                   cs.TLSUnique,
	}
}
