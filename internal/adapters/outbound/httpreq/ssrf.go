package httpreq

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// privateRangesV4 is the list of CIDR-like prefixes blocked by default
// (RFC 1918 + RFC 6598 + loopback + link-local).
var privateRangesV4 = []*net.IPNet{
	mustCIDR("10.0.0.0/8"),
	mustCIDR("172.16.0.0/12"),
	mustCIDR("192.168.0.0/16"),
	mustCIDR("127.0.0.0/8"),
	mustCIDR("169.254.0.0/16"),
	mustCIDR("100.64.0.0/10"),
	mustCIDR("0.0.0.0/8"),
	mustCIDR("224.0.0.0/4"), // multicast
}

var privateRangesV6 = []*net.IPNet{
	mustCIDR("::1/128"),   // loopback
	mustCIDR("fe80::/10"), // link-local
	mustCIDR("fc00::/7"),  // unique local
	mustCIDR("ff00::/8"),  // multicast
}

func mustCIDR(s string) *net.IPNet {
	_, ipnet, err := net.ParseCIDR(s)
	if err != nil {
		panic("ssrf: invalid CIDR " + s)
	}
	return ipnet
}

// validateURL parses and validates the request URL: scheme must be
// http or https; host must be non-empty; if SSRF is strict, the host's
// resolved IPs must not fall in any private range.
func (c Config) validateURL(rawURL string, lookup func(host string) ([]net.IP, error)) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme %q not allowed (http|https only)", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("url has empty host")
	}
	if c.AllowPrivateNetworks {
		return nil
	}
	if lookup == nil {
		lookup = net.LookupIP
	}
	ips, err := lookup(host)
	if err != nil {
		return fmt.Errorf("resolve %q: %w", host, err)
	}
	for _, ip := range ips {
		if isPrivate(ip) {
			return fmt.Errorf("host %q resolves to private IP %s (SSRF block; set AllowPrivateNetworks=true to override)", host, ip)
		}
	}
	return nil
}

func isPrivate(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil {
		for _, r := range privateRangesV4 {
			if r.Contains(ip4) {
				return true
			}
		}
		return false
	}
	for _, r := range privateRangesV6 {
		if r.Contains(ip) {
			return true
		}
	}
	return false
}

// hostAllowlistWarn returns a non-empty warning if the URL's host is
// not in the allowlist. Empty allowlist disables the check.
func (c Config) hostAllowlistWarn(rawURL string) string {
	if len(c.HostAllowlist) == 0 {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	host := u.Hostname()
	for _, h := range c.HostAllowlist {
		if strings.EqualFold(h, host) {
			return ""
		}
	}
	return fmt.Sprintf("host %q not in HostAllowlist (logged-only in Phase 1)", host)
}
