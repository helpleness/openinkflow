package http

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"time"
)

var nonPublicPrefixes = mustParsePrefixes(
	"0.0.0.0/8",
	"10.0.0.0/8",
	"100.64.0.0/10",
	"127.0.0.0/8",
	"169.254.0.0/16",
	"172.16.0.0/12",
	"192.0.0.0/24",
	"192.0.2.0/24",
	"192.31.196.0/24",
	"192.52.193.0/24",
	"192.88.99.0/24",
	"192.168.0.0/16",
	"192.175.48.0/24",
	"198.18.0.0/15",
	"198.51.100.0/24",
	"203.0.113.0/24",
	"224.0.0.0/4",
	"240.0.0.0/4",
	"::/96",
	"::ffff:0:0:0/96",
	"64:ff9b::/96",
	"64:ff9b:1::/48",
	"100::/64",
	"100:0:0:1::/64",
	"2001::/23",
	"2001:db8::/32",
	"2002::/16",
	"2620:4f:8000::/48",
	"3fff::/20",
	"5f00::/16",
	"fc00::/7",
	"fe80::/10",
	"fec0::/10",
	"ff00::/8",
)

type publicDialer struct {
	dialer net.Dialer
}

func newPublicDialer(timeout, keepAlive time.Duration) *publicDialer {
	return &publicDialer{
		dialer: net.Dialer{
			Timeout:   timeout,
			KeepAlive: keepAlive,
		},
	}
}

// DialContext resolves the hostname once, validates every DNS answer, and
// connects to a validated IP literal. This closes the usual DNS-rebinding gap
// between validation and connection.
func (dialer *publicDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, fmt.Errorf("unsupported remote network %q", network)
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("invalid remote address: %w", err)
	}
	portValue, err := strconv.ParseUint(port, 10, 16)
	// The per-session bearerTransport checks the origin, including its port.
	if err != nil || portValue == 0 {
		return nil, errors.New("invalid remote endpoint port")
	}

	addresses, err := resolvePublicAddresses(ctx, host)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for _, candidate := range addresses {
		if network == "tcp4" && !candidate.Is4() {
			continue
		}
		if network == "tcp6" && !candidate.Is6() {
			continue
		}
		connection, dialErr := dialer.dialer.DialContext(ctx, network, net.JoinHostPort(candidate.String(), port))
		if dialErr == nil {
			return connection, nil
		}
		lastErr = dialErr
	}
	if lastErr == nil {
		return nil, errors.New("remote hostname has no compatible public IP addresses")
	}
	return nil, fmt.Errorf("dial remote endpoint: %w", lastErr)
}

func resolvePublicAddresses(ctx context.Context, host string) ([]netip.Addr, error) {
	if address, err := parseIPAddress(host); err == nil {
		if !isPublicAddress(address) {
			return nil, errors.New("remote endpoint resolved to a non-public IP address")
		}
		return []netip.Addr{address.Unmap()}, nil
	}

	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("resolve remote endpoint: %w", err)
	}
	if len(addresses) == 0 {
		return nil, errors.New("remote hostname has no IP addresses")
	}
	result := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		if !isPublicAddress(address) {
			return nil, errors.New("remote endpoint resolved to a non-public IP address")
		}
		result = append(result, address.WithZone("").Unmap())
	}
	return result, nil
}

func parseIPAddress(host string) (netip.Addr, error) {
	address, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, err
	}
	return address.WithZone("").Unmap(), nil
}

func isPublicAddress(address netip.Addr) bool {
	if !address.IsValid() {
		return false
	}
	address = address.WithZone("").Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range nonPublicPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func mustParsePrefixes(values ...string) []netip.Prefix {
	prefixes := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefixes = append(prefixes, netip.MustParsePrefix(value))
	}
	return prefixes
}
