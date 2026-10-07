package ip

import "net/netip"

var (
	ipv6GlobalUnicastPrefix = netip.MustParsePrefix("2000::/3")
	ipv4SpecialPrefixes     = [...]netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/8"),
		netip.MustParsePrefix("100.64.0.0/10"),
		netip.MustParsePrefix("192.0.2.0/24"),
		netip.MustParsePrefix("192.88.99.0/24"),
		netip.MustParsePrefix("198.18.0.0/15"),
		netip.MustParsePrefix("198.51.100.0/24"),
		netip.MustParsePrefix("203.0.113.0/24"),
		netip.MustParsePrefix("240.0.0.0/4"),
	}
	ipv4ProtocolAssignmentPrefix = netip.MustParsePrefix("192.0.0.0/24")
	ipv6SpecialPrefixes          = [...]netip.Prefix{
		netip.MustParsePrefix("2001::/23"),
		netip.MustParsePrefix("2001:2::/48"),
		netip.MustParsePrefix("2001:db8::/32"),
		netip.MustParsePrefix("2002::/16"),
		netip.MustParsePrefix("3ffe::/16"),
		netip.MustParsePrefix("3fff::/20"),
		netip.MustParsePrefix("5f00::/16"),
	}
	ipv6GloballyReachableProtocolPrefixes = [...]netip.Prefix{
		netip.MustParsePrefix("2001:1::1/128"),
		netip.MustParsePrefix("2001:1::2/128"),
		netip.MustParsePrefix("2001:1::3/128"),
		netip.MustParsePrefix("2001:3::/32"),
		netip.MustParsePrefix("2001:4:112::/48"),
		netip.MustParsePrefix("2001:20::/28"),
		netip.MustParsePrefix("2001:30::/28"),
	}
)

// PublicClientIP returns a normalized IP address only when it is globally
// reachable; malformed and special-purpose addresses return an empty string.
func PublicClientIP(raw string) string {
	addr, err := netip.ParseAddr(raw)
	if err != nil || addr.Zone() != "" {
		return ""
	}

	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() ||
		addr.IsUnspecified() || addr.IsMulticast() || addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() {
		return ""
	}

	if addr.Is4() {
		for _, prefix := range ipv4SpecialPrefixes {
			if prefix.Contains(addr) {
				return ""
			}
		}
		if ipv4ProtocolAssignmentPrefix.Contains(addr) &&
			addr != netip.MustParseAddr("192.0.0.9") &&
			addr != netip.MustParseAddr("192.0.0.10") {
			return ""
		}
		return addr.String()
	}

	if !ipv6GlobalUnicastPrefix.Contains(addr) {
		return ""
	}
	for _, prefix := range ipv6SpecialPrefixes {
		if prefix.Contains(addr) {
			for _, allowed := range ipv6GloballyReachableProtocolPrefixes {
				if allowed.Contains(addr) {
					return addr.String()
				}
			}
			return ""
		}
	}
	return addr.String()
}
