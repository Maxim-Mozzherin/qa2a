package netutil

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
)

var (
	ErrUnsafeDestination = errors.New("подключение к внутренним/приватным адресам заблокировано политикой безопасности")
	ErrInvalidScheme     = errors.New("разрешены только протоколы http и https")
	ErrEmptyHost         = errors.New("не указан хост назначения")
)

// IsSafeDestinationIP проверяет, не принадлежит ли IP-адрес внутренним, приватным,
// loopback или облачным метаданным (RFC 1918, RFC 3927, RFC 4291).
func IsSafeDestinationIP(ip net.IP) bool {
	if ip == nil {
		return false
	}

	// 1. Loopback (127.0.0.0/8, ::1)
	if ip.IsLoopback() {
		return false
	}

	// 2. Unspecified (0.0.0.0, ::)
	if ip.IsUnspecified() {
		return false
	}

	// 3. Multicast
	if ip.IsMulticast() {
		return false
	}

	// 4. Link-local unicast (169.254.0.0/16, fe80::/10) - Cloud Metadata
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}

	// IPv4 checks
	if ipv4 := ip.To4(); ipv4 != nil {
		// Broadcast
		if ipv4.Equal(net.IPv4bcast) {
			return false
		}

		// Private RFC 1918:
		// 10.0.0.0/8
		if ipv4[0] == 10 {
			return false
		}
		// 172.16.0.0/12
		if ipv4[0] == 172 && (ipv4[1] >= 16 && ipv4[1] <= 31) {
			return false
		}
		// 192.168.0.0/16
		if ipv4[0] == 192 && ipv4[1] == 168 {
			return false
		}
		// Carrier-grade NAT (100.64.0.0/10)
		if ipv4[0] == 100 && (ipv4[1] >= 64 && ipv4[1] <= 127) {
			return false
		}
		// Cloud metadata specific: 169.254.169.254
		if ipv4[0] == 169 && ipv4[1] == 254 {
			return false
		}
		return true
	}

	// IPv6 private ranges
	// Unique Local Address (fc00::/7)
	if len(ip) == net.IPv6len && (ip[0]&0xfe == 0xfc) {
		return false
	}

	return true
}

// ValidateHost проверяет URL на отсутствие SSRF-векторов.
func ValidateHost(rawURL string) error {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return ErrEmptyHost
	}

	if !strings.HasPrefix(trimmed, "http://") && !strings.HasPrefix(trimmed, "https://") {
		trimmed = "https://" + trimmed
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("некорректный формат URL: %w", err)
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return ErrInvalidScheme
	}

	hostname := parsed.Hostname()
	if hostname == "" {
		return ErrEmptyHost
	}

	// Проверяем прямое указание IP
	if directIP := net.ParseIP(hostname); directIP != nil {
		if !IsSafeDestinationIP(directIP) {
			return fmt.Errorf("%w: %s", ErrUnsafeDestination, hostname)
		}
		return nil
	}

	// Резолвим DNS
	ips, err := net.LookupIP(hostname)
	if err != nil {
		return fmt.Errorf("ошибка разрешения DNS для %s: %w", hostname, err)
	}

	if len(ips) == 0 {
		return fmt.Errorf("DNS не вернул IP-адресов для %s", hostname)
	}

	for _, ip := range ips {
		if !IsSafeDestinationIP(ip) {
			return fmt.Errorf("%w: %s разрешается в небезопасный IP %s", ErrUnsafeDestination, hostname, ip.String())
		}
	}

	return nil
}

// NewSafeHTTPTransport создает *http.Transport с защитой от SSRF и DNS-rebinding атак
// на этапе открытия сокета (Control callback).
func NewSafeHTTPTransport(dialTimeout time.Duration) *http.Transport {
	dialer := &net.Dialer{
		Timeout:   dialTimeout,
		KeepAlive: 30 * time.Second,
		Control: func(network, address string, c syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				host = address
			}
			ip := net.ParseIP(host)
			if ip != nil && !IsSafeDestinationIP(ip) {
				return fmt.Errorf("%w: попытка подключения к %s", ErrUnsafeDestination, host)
			}
			return nil
		},
	}

	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		MaxIdleConns:          50,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}
