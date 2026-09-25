package httpapi

import (
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestClientIPTrustBoundary(t *testing.T) {
	s := &Server{config: Config{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24"), netip.MustParsePrefix("::1/128")}}}
	for _, tc := range []struct{ name, peer, forwarded, want string }{
		{"direct", "192.0.2.1:9000", "198.51.100.9", "192.0.2.1"},
		{"one proxy", "10.0.0.1:9000", "192.0.2.1", "192.0.2.1"},
		{"proxy chain", "10.0.0.1:9000", "192.0.2.1, 10.0.0.2", "192.0.2.1"},
		{"spoofed prefix", "10.0.0.1:9000", "198.51.100.9, 192.0.2.1", "192.0.2.1"},
		{"missing", "10.0.0.1:9000", "", "10.0.0.1"},
		{"invalid", "10.0.0.1:9000", "192.0.2.1, garbage", "10.0.0.1"},
		{"too many", "10.0.0.1:9000", strings.Repeat("10.0.0.2,", 33) + "192.0.2.1", "10.0.0.1"},
		{"ipv6", "[::1]:9000", "2001:db8::1", "2001:db8::1"},
		{"mapped", "[::ffff:192.0.2.1]:9000", "198.51.100.9", "192.0.2.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/api/session", nil)
			r.RemoteAddr = tc.peer
			r.Header.Set("X-Forwarded-For", tc.forwarded)
			if got := s.clientIP(r); got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestProxyClientsReceiveIndependentRateLimits(t *testing.T) {
	s := &Server{config: Config{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.1/32")}}}
	l := newLimiter(1, 1)
	r := httptest.NewRequest("GET", "/api/quizzes/VOCAB-DEMO", nil)
	r.RemoteAddr = "10.0.0.1:9000"
	r.Header.Set("X-Forwarded-For", "192.0.2.1")
	if !l.allow(s.clientIP(r)) || l.allow(s.clientIP(r)) {
		t.Fatal("first client limit not enforced")
	}
	r.Header.Set("X-Forwarded-For", "192.0.2.2")
	if !l.allow(s.clientIP(r)) {
		t.Fatal("one learner exhausted another learner's limit")
	}
	r.Header.Set("X-Forwarded-For", "198.51.100.9, 192.0.2.2")
	if l.allow(s.clientIP(r)) {
		t.Fatal("forged prefix bypassed rate limit")
	}
}
