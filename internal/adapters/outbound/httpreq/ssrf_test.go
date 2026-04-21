package httpreq

import (
	"fmt"
	"net"
	"testing"
)

func TestValidateURL(t *testing.T) {
	// fakeLookup returns the given IPs unconditionally.
	fakeLookup := func(ips ...net.IP) func(string) ([]net.IP, error) {
		return func(_ string) ([]net.IP, error) { return ips, nil }
	}
	errLookup := func(err error) func(string) ([]net.IP, error) {
		return func(_ string) ([]net.IP, error) { return nil, err }
	}

	tests := []struct {
		name        string
		cfg         Config
		rawURL      string
		lookup      func(string) ([]net.IP, error)
		wantErrSub  string // non-empty → expect error containing this substring
	}{
		{
			name:   "public IPv4 passes",
			cfg:    Config{},
			rawURL: "http://example.com",
			lookup: fakeLookup(net.ParseIP("8.8.8.8")),
		},
		{
			name:       "RFC1918 10.x is blocked",
			cfg:        Config{},
			rawURL:     "http://10.0.0.1",
			lookup:     fakeLookup(net.ParseIP("10.0.0.1")),
			wantErrSub: "SSRF block",
		},
		{
			name:       "loopback 127.x is blocked",
			cfg:        Config{},
			rawURL:     "http://127.0.0.1",
			lookup:     fakeLookup(net.ParseIP("127.0.0.1")),
			wantErrSub: "SSRF block",
		},
		{
			name:       "localhost resolving to 127.0.0.1 is blocked",
			cfg:        Config{},
			rawURL:     "http://localhost",
			lookup:     fakeLookup(net.ParseIP("127.0.0.1")),
			wantErrSub: "SSRF block",
		},
		{
			name:       "IPv6 loopback ::1 is blocked",
			cfg:        Config{},
			rawURL:     "http://example.com",
			lookup:     fakeLookup(net.ParseIP("::1")),
			wantErrSub: "SSRF block",
		},
		{
			name:   "AllowPrivateNetworks bypasses block",
			cfg:    Config{AllowPrivateNetworks: true},
			rawURL: "http://10.0.0.1",
			lookup: fakeLookup(net.ParseIP("10.0.0.1")),
		},
		{
			name:       "ftp scheme rejected",
			cfg:        Config{},
			rawURL:     "ftp://example.com",
			lookup:     fakeLookup(net.ParseIP("8.8.8.8")),
			wantErrSub: "not allowed",
		},
		{
			name:       "empty url — empty host",
			cfg:        Config{},
			rawURL:     "http://",
			lookup:     fakeLookup(),
			wantErrSub: "empty host",
		},
		{
			name:       "IPv6 link-local fe80::1 is blocked",
			cfg:        Config{},
			rawURL:     "http://[fe80::1]",
			lookup:     fakeLookup(net.ParseIP("fe80::1")),
			wantErrSub: "SSRF block",
		},
		{
			name:       "IPv4 multicast 224.0.0.1 is blocked",
			cfg:        Config{},
			rawURL:     "http://example.com",
			lookup:     fakeLookup(net.ParseIP("224.0.0.1")),
			wantErrSub: "SSRF block",
		},
		{
			name:       "lookup error propagates",
			cfg:        Config{},
			rawURL:     "http://example.com",
			lookup:     errLookup(fmt.Errorf("dns failure")),
			wantErrSub: "resolve",
		},
		{
			name:       "RFC1918 172.16.x blocked",
			cfg:        Config{},
			rawURL:     "http://example.com",
			lookup:     fakeLookup(net.ParseIP("172.16.0.1")),
			wantErrSub: "SSRF block",
		},
		{
			name:       "RFC1918 192.168.x blocked",
			cfg:        Config{},
			rawURL:     "http://example.com",
			lookup:     fakeLookup(net.ParseIP("192.168.1.1")),
			wantErrSub: "SSRF block",
		},
		{
			name:       "RFC 6598 100.64.x blocked",
			cfg:        Config{},
			rawURL:     "http://example.com",
			lookup:     fakeLookup(net.ParseIP("100.64.0.1")),
			wantErrSub: "SSRF block",
		},
		{
			name:       "link-local 169.254.x blocked",
			cfg:        Config{},
			rawURL:     "http://example.com",
			lookup:     fakeLookup(net.ParseIP("169.254.0.1")),
			wantErrSub: "SSRF block",
		},
		{
			name:       "IPv6 unique local fc00:: blocked",
			cfg:        Config{},
			rawURL:     "http://example.com",
			lookup:     fakeLookup(net.ParseIP("fc00::1")),
			wantErrSub: "SSRF block",
		},
		{
			name:       "IPv6 multicast ff02:: blocked",
			cfg:        Config{},
			rawURL:     "http://example.com",
			lookup:     fakeLookup(net.ParseIP("ff02::1")),
			wantErrSub: "SSRF block",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.validateURL(tc.rawURL, tc.lookup)
			if tc.wantErrSub == "" {
				if err != nil {
					t.Errorf("expected no error, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErrSub)
			}
			if !containsStr(err.Error(), tc.wantErrSub) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantErrSub)
			}
		})
	}
}

func TestIsPrivate(t *testing.T) {
	priv := []string{
		"10.0.0.1", "10.255.255.255",
		"172.16.0.1", "172.31.255.255",
		"192.168.0.1", "192.168.255.255",
		"127.0.0.1", "127.255.255.255",
		"169.254.0.1",
		"100.64.0.1", "100.127.255.255",
		"0.0.0.1",
		"224.0.0.1", "239.255.255.255",
		"::1",
		"fe80::1", "fe80::ffff:ffff:ffff:ffff",
		"fc00::1", "fdff:ffff:ffff:ffff::",
		"ff02::1",
	}
	pub := []string{
		"8.8.8.8", "1.1.1.1", "203.0.113.1",
		"2001:db8::1",
	}

	for _, s := range priv {
		ip := net.ParseIP(s)
		if ip == nil {
			t.Fatalf("invalid test IP: %s", s)
		}
		if !isPrivate(ip) {
			t.Errorf("isPrivate(%s) = false, want true", s)
		}
	}
	for _, s := range pub {
		ip := net.ParseIP(s)
		if ip == nil {
			t.Fatalf("invalid test IP: %s", s)
		}
		if isPrivate(ip) {
			t.Errorf("isPrivate(%s) = true, want false", s)
		}
	}
}

func TestHostAllowlistWarn(t *testing.T) {
	cfg := Config{HostAllowlist: []string{"example.com", "trusted.io"}}
	if w := cfg.hostAllowlistWarn("http://example.com/path"); w != "" {
		t.Errorf("expected empty warn for allowed host, got: %q", w)
	}
	if w := cfg.hostAllowlistWarn("http://untrusted.com"); w == "" {
		t.Error("expected warn for unlisted host, got empty string")
	}
	// Empty allowlist: no warn.
	empty := Config{}
	if w := empty.hostAllowlistWarn("http://whatever.com"); w != "" {
		t.Errorf("expected empty warn with empty allowlist, got: %q", w)
	}
}

func containsStr(s, sub string) bool {
	return len(sub) == 0 || len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStrHelper(s, sub))
}

func containsStrHelper(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
