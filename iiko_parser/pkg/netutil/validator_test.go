package netutil

import (
	"net"
	"testing"
	"time"
)

func TestIsSafeDestinationIP(t *testing.T) {
	testCases := []struct {
		name     string
		ip       string
		expected bool
	}{
		{"IPv4 Loopback 127.0.0.1", "127.0.0.1", false},
		{"IPv4 Loopback 127.1.2.3", "127.1.2.3", false},
		{"IPv6 Loopback ::1", "::1", false},
		{"IPv4 Private 10.0.0.1", "10.0.0.1", false},
		{"IPv4 Private 10.255.255.255", "10.255.255.255", false},
		{"IPv4 Private 172.16.0.1", "172.16.0.1", false},
		{"IPv4 Private 172.31.255.255", "172.31.255.255", false},
		{"IPv4 Private 192.168.1.1", "192.168.1.1", false},
		{"IPv4 Carrier NAT 100.64.0.1", "100.64.0.1", false},
		{"IPv4 Cloud Metadata 169.254.169.254", "169.254.169.254", false},
		{"IPv4 Unspecified 0.0.0.0", "0.0.0.0", false},
		{"IPv4 Broadcast 255.255.255.255", "255.255.255.255", false},
		{"IPv6 Link Local fe80::1", "fe80::1", false},
		{"IPv6 Unique Local fc00::1", "fc00::1", false},
		{"IPv6 Unique Local fd00::1", "fd00::1", false},
		{"Public Google DNS 8.8.8.8", "8.8.8.8", true},
		{"Public Cloudflare DNS 1.1.1.1", "1.1.1.1", true},
		{"Public IP 93.184.216.34", "93.184.216.34", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			parsed := net.ParseIP(tc.ip)
			if parsed == nil {
				t.Fatalf("failed to parse IP %s", tc.ip)
			}
			result := IsSafeDestinationIP(parsed)
			if result != tc.expected {
				t.Errorf("for IP %s: expected safe=%v, got %v", tc.ip, tc.expected, result)
			}
		})
	}

	// Nil IP check
	if IsSafeDestinationIP(nil) {
		t.Errorf("expected false for nil IP")
	}
}

func TestValidateHost(t *testing.T) {
	// Empty host
	if err := ValidateHost(""); err != ErrEmptyHost {
		t.Errorf("expected ErrEmptyHost, got %v", err)
	}

	// Loopback IP direct
	if err := ValidateHost("127.0.0.1"); err == nil {
		t.Errorf("expected error for 127.0.0.1 direct IP, got nil")
	}

	// Private IP direct
	if err := ValidateHost("192.168.1.100:8080"); err == nil {
		t.Errorf("expected error for private direct IP, got nil")
	}

	// Cloud metadata IP
	if err := ValidateHost("http://169.254.169.254/latest/meta-data"); err == nil {
		t.Errorf("expected error for cloud metadata IP, got nil")
	}

	// Invalid scheme
	if err := ValidateHost("ftp://example.com"); err != ErrInvalidScheme {
		t.Errorf("expected ErrInvalidScheme, got %v", err)
	}
}

func TestNewSafeHTTPTransport(t *testing.T) {
	transport := NewSafeHTTPTransport(5 * time.Second)
	if transport == nil {
		t.Fatal("expected non-nil transport")
	}
	if transport.MaxIdleConns != 50 {
		t.Errorf("expected MaxIdleConns=50, got %d", transport.MaxIdleConns)
	}
}
