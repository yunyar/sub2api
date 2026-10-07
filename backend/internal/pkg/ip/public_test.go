package ip

import "testing"

func TestPublicClientIP(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "public IPv4 Google DNS", raw: "8.8.8.8", want: "8.8.8.8"},
		{name: "public IPv4 Cloudflare DNS", raw: "1.1.1.1", want: "1.1.1.1"},
		{name: "public IPv6 Cloudflare DNS", raw: "2606:4700:4700::1111", want: "2606:4700:4700::1111"},
		{name: "public IPv4 protocol assignment", raw: "192.0.0.9", want: "192.0.0.9"},
		{name: "public IPv4 protocol assignment alternate", raw: "192.0.0.10", want: "192.0.0.10"},
		{name: "IPv4 mapped IPv6 normalized", raw: "::ffff:8.8.8.8", want: "8.8.8.8"},
		{name: "public IPv6 protocol anycast", raw: "2001:1::1", want: "2001:1::1"},
		{name: "public IPv6 AMT assignment", raw: "2001:3::1", want: "2001:3::1"},
		{name: "public IPv6 AS112 assignment", raw: "2001:4:112::1", want: "2001:4:112::1"},
		{name: "public IPv6 ORCHIDv2 assignment", raw: "2001:20::1", want: "2001:20::1"},
		{name: "public IPv6 Drone Remote ID assignment", raw: "2001:30::1", want: "2001:30::1"},

		{name: "unspecified IPv4", raw: "0.0.0.0", want: ""},
		{name: "reserved IPv4 zero range", raw: "0.1.2.3", want: ""},
		{name: "private IPv4 10/8", raw: "10.1.2.3", want: ""},
		{name: "private IPv4 172.16/12", raw: "172.16.0.1", want: ""},
		{name: "private IPv4 192.168/16", raw: "192.168.1.1", want: ""},
		{name: "shared address space CGNAT", raw: "100.64.0.1", want: ""},
		{name: "IPv4 loopback", raw: "127.0.0.1", want: ""},
		{name: "IPv4 link local", raw: "169.254.1.1", want: ""},
		{name: "IPv4 protocol assignment reserved", raw: "192.0.0.8", want: ""},
		{name: "IPv4 protocol assignment reserved after exception", raw: "192.0.0.11", want: ""},
		{name: "IPv4 documentation TEST-NET-1", raw: "192.0.2.1", want: ""},
		{name: "IPv4 deprecated 6to4 relay anycast", raw: "192.88.99.1", want: ""},
		{name: "IPv4 benchmarking", raw: "198.18.0.1", want: ""},
		{name: "IPv4 documentation TEST-NET-2", raw: "198.51.100.1", want: ""},
		{name: "IPv4 documentation TEST-NET-3", raw: "203.0.113.1", want: ""},
		{name: "IPv4 multicast", raw: "224.0.0.1", want: ""},
		{name: "IPv4 reserved", raw: "240.0.0.1", want: ""},
		{name: "IPv4 limited broadcast", raw: "255.255.255.255", want: ""},
		{name: "mapped private IPv4", raw: "::ffff:192.168.1.1", want: ""},
		{name: "IPv6 unspecified", raw: "::", want: ""},
		{name: "IPv6 loopback", raw: "::1", want: ""},
		{name: "IPv6 unique local", raw: "fd12:3456::1", want: ""},
		{name: "IPv6 link local", raw: "fe80::1", want: ""},
		{name: "IPv6 multicast", raw: "ff02::1", want: ""},
		{name: "IPv6 outside global unicast 2000::/3", raw: "4000::1", want: ""},
		{name: "IPv6 IETF protocol assignment", raw: "2001::1", want: ""},
		{name: "IPv6 Teredo transition", raw: "2001:0:4136:e378:8000:63bf:3fff:fdd2", want: ""},
		{name: "IPv6 benchmarking", raw: "2001:2::1", want: ""},
		{name: "IPv6 documentation RFC 3849", raw: "2001:db8::1", want: ""},
		{name: "IPv6 6to4 transition", raw: "2002:c000:0201::1", want: ""},
		{name: "IPv6 legacy 6bone reserved", raw: "3ffe::1", want: ""},
		{name: "IPv6 documentation RFC 9637", raw: "3fff::1", want: ""},
		{name: "IPv6 SRv6 SID special-purpose block", raw: "5f00::1", want: ""},
		{name: "IPv6 scoped address", raw: "2606:4700::1%eth0", want: ""},
		{name: "hostname", raw: "example.com", want: ""},
		{name: "IPv4 with port", raw: "8.8.8.8:443", want: ""},
		{name: "IPv6 with brackets and port", raw: "[2606:4700::1]:443", want: ""},
		{name: "surrounding whitespace", raw: " 8.8.8.8 ", want: ""},
		{name: "empty", raw: "", want: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := PublicClientIP(test.raw); got != test.want {
				t.Errorf("PublicClientIP(%q) = %q, want %q", test.raw, got, test.want)
			}
		})
	}
}
