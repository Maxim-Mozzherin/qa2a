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
		"10.254.1.1",
		"172.16.0.1",
		"172.31.255.255",
		"192.168.0.1",
		"192.168.1.100",
		"169.254.169.254", // Cloud metadata
		"169.254.1.1",
		"255.255.255.255",
		"100.64.0.1",
		"::1",
		"::",
	}

	for _, ipStr := range unsafeIPs {
		ip := net.ParseIP(ipStr)
		if IsSafeDestinationIP(ip) {
			t.Errorf("expected IP %s to be flagged as unsafe, but got safe", ipStr)
		}
	}

	safeIPs := []string{
		"8.8.8.8",
		"1.1.1.1",
		"93.184.216.34",
	}

	for _, ipStr := range safeIPs {
		ip := net.ParseIP(ipStr)
		if !IsSafeDestinationIP(ip) {
			t.Errorf("expected IP %s to be safe, but got unsafe", ipStr)
		}
	}
}

func TestValidateHost_BlocksSSRF(t *testing.T) {
	unsafeURLs := []string{
		"http://localhost:8080",
		"https://127.0.0.1/resto",
		"http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.5:8080",
		"http://192.168.1.1",
		"ftp://example.com",
	}

	for _, u := range unsafeURLs {
		if err := ValidateHost(u); err == nil {
			t.Errorf("expected URL %s to be rejected by ValidateHost, but got nil error", u)
		}
	}
}
