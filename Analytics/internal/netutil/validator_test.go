package netutil

import (
	"net"
	"testing"
)

func TestIsSafeDestinationIP(t *testing.T) {
	unsafeIPs := []string{
		"127.0.0.1",
		"127.0.0.2",
		"0.0.0.0",
		"10.0.0.1",
		"10.254.0.1",
		"172.16.0.1",
		"172.31.255.255",
		"192.168.1.1",
		"192.168.0.254",
		"169.254.169.254", // Cloud Metadata
		"169.254.1.1",
		"100.64.0.1",     // CGNAT
		"224.0.0.1",      // Multicast
		"255.255.255.255",
		"::1",            // IPv6 loopback
		"::",             // IPv6 unspecified
		"fc00::1",        // IPv6 ULA
		"fe80::1",        // IPv6 link-local
	}

	for _, ipStr := range unsafeIPs {
		ip := net.ParseIP(ipStr)
		if IsSafeDestinationIP(ip) {
			t.Errorf("expected IP %s to be unsafe, but was classified as safe", ipStr)
		}
	}

	safeIPs := []string{
		"8.8.8.8",
		"1.1.1.1",
		"217.16.20.1",
		"95.163.200.1",
	}

	for _, ipStr := range safeIPs {
		ip := net.ParseIP(ipStr)
		if !IsSafeDestinationIP(ip) {
			t.Errorf("expected IP %s to be safe, but was classified as unsafe", ipStr)
		}
	}
}

func TestValidateHost_Security(t *testing.T) {
	unsafeHosts := []string{
		"http://127.0.0.1",
		"http://127.0.0.1:8080",
		"http://localhost",
		"http://10.0.0.5:5432",
		"http://192.168.1.1",
		"http://169.254.169.254/latest/meta-data/",
		"ftp://example.com",
		"gopher://example.com",
		"",
	}

	for _, host := range unsafeHosts {
		if err := ValidateHost(host); err == nil {
			t.Errorf("expected host %q to fail validation, but succeeded", host)
		}
	}
}
