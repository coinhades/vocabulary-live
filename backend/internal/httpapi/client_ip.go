package httpapi

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

func (s *Server) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	peer = peer.Unmap()
	if !s.trustedProxy(peer) {
		return peer.String()
	}
	forwarded := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	if len(forwarded) > 32 {
		return peer.String()
	}
	current := peer
	// Walk from the connection toward the client; untrusted hops cannot vouch
	// for any addresses to their left.
	for i := len(forwarded) - 1; i >= 0 && s.trustedProxy(current); i-- {
		address, err := netip.ParseAddr(strings.TrimSpace(forwarded[i]))
		if err != nil || address.Zone() != "" {
			return peer.String()
		}
		current = address.Unmap()
	}
	return current.String()
}

func (s *Server) trustedProxy(address netip.Addr) bool {
	for _, prefix := range s.config.TrustedProxies {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}
