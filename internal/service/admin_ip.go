package service

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"

	"github.com/harlew-dev/lewbox/internal/aerolvm"
)

// clusterAdminIP validates AddClusterRequest.AdminIP: an IP address at which
// admin_url's host answers, for a new cluster whose domain doesn't resolve
// yet. It is only meaningful when admin_url names a host, not an IP.
func clusterAdminIP(raw, adminURL string) (netip.Addr, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return netip.Addr{}, nil
	}
	addr, err := netip.ParseAddr(raw)
	if err != nil || addr.Zone() != "" {
		return netip.Addr{}, invalid("admin_ip must be an IP address, e.g. 203.0.113.10")
	}
	u, err := url.Parse(adminURL)
	if err != nil {
		return netip.Addr{}, invalid("admin_url is not a URL")
	}
	if _, err := netip.ParseAddr(u.Hostname()); err == nil {
		return netip.Addr{}, invalid("admin_ip is only for an admin_url with a host name; admin_url already names an IP")
	}
	return addr.Unmap(), nil
}

// pinnedHostOptions returns AerolVM client options that connect to ip, on
// the same port, when a request goes to host and host doesn't resolve. A
// host that resolves is dialled as usual, so a domain already pointed
// elsewhere (a load balancer) keeps working. TLS still verifies the
// certificate against host. With no ip it returns nothing.
func pinnedHostOptions(adminURL string, ip netip.Addr) []aerolvm.Option {
	if !ip.IsValid() {
		return nil
	}
	u, err := url.Parse(adminURL)
	if err != nil || u.Hostname() == "" {
		return nil
	}
	host := strings.ToLower(u.Hostname())
	dialer := &net.Dialer{}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := dialer.DialContext(ctx, network, address)
		if err == nil {
			return conn, nil
		}
		h, port, splitErr := net.SplitHostPort(address)
		if _, unresolved := errors.AsType[*net.DNSError](err); !unresolved || splitErr != nil || !strings.EqualFold(h, host) {
			return nil, err
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
	}
	return []aerolvm.Option{aerolvm.WithHTTPClient(&http.Client{Transport: transport})}
}
