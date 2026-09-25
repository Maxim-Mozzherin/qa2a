package ratelimit

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Trusted proxy networks: loopback and private subnets
var trustedCIDRs []*net.IPNet

func init() {
	cidrs := []string{
		"127.0.0.0/8",    // IPv4 loopback
		"::1/128",        // IPv6 loopback
		"10.0.0.0/8",     // RFC 1918
		"172.16.0.0/12",  // RFC 1918 & Docker default bridge
		"192.168.0.0/16", // RFC 1918
		"169.254.0.0/16", // Link-local
	}
	for _, c := range cidrs {
		_, ipNet, err := net.ParseCIDR(c)
		if err == nil {
			trustedCIDRs = append(trustedCIDRs, ipNet)
		}
	}
}

// isTrustedProxy checks if the given remote IP address belongs to a trusted reverse proxy
func isTrustedProxy(ip net.IP) bool {
	if ip == nil {
		return false
	}
	for _, cidr := range trustedCIDRs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// GetClientIP securely extracts the client IP address.
// If RemoteAddr is a trusted proxy, it inspects X-Forwarded-For (first entry) or X-Real-IP.
// If RemoteAddr is NOT a trusted proxy (direct client connection), X-Forwarded-For is ignored to prevent spoofing.
func GetClientIP(r *http.Request) string {
	remoteHost, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remoteHost = r.RemoteAddr
	}
	remoteIP := net.ParseIP(strings.TrimSpace(remoteHost))

	if isTrustedProxy(remoteIP) {
		// Trust proxy headers
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			clientIP := strings.TrimSpace(parts[0])
			if net.ParseIP(clientIP) != nil {
				return clientIP
			}
		}
		if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
			if net.ParseIP(xri) != nil {
				return xri
			}
		}
	}

	if remoteIP != nil {
		return remoteIP.String()
	}
	return remoteHost
}

type clientRecord struct {
	count     int
	resetAt   time.Time
	blockedAt time.Time
}

// Limiter provides thread-safe, bounded in-memory rate limiting with automatic TTL eviction.
type Limiter struct {
	mu         sync.Mutex
	records    map[string]*clientRecord
	maxRecords int
	limit      int
	window     time.Duration
	blockTime  time.Duration
	stopCh     chan struct{}
}

// NewLimiter creates a new bounded rate limiter.
// - limit: max allowed attempts in the window before blocking
// - window: sliding reset duration
// - blockTime: duration to block once limit is exceeded
// - maxRecords: hard upper bound on map size to prevent memory exhaustion (e.g. 10000)
func NewLimiter(limit int, window, blockTime time.Duration, maxRecords int) *Limiter {
	l := &Limiter{
		records:    make(map[string]*clientRecord),
		maxRecords: maxRecords,
		limit:      limit,
		window:     window,
		blockTime:  blockTime,
		stopCh:     make(chan struct{}),
	}
	go l.cleanupLoop()
	return l
}

// Allow checks if the given key is allowed to make a request.
// Returns (allowed, remainingBlockTime).
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	rec, exists := l.records[key]

	if exists {
		// Check if currently blocked
		if !rec.blockedAt.IsZero() {
			if now.Before(rec.blockedAt.Add(l.blockTime)) {
				remaining := rec.blockedAt.Add(l.blockTime).Sub(now)
				return false, remaining
			}
			// Block expired, reset record
			rec.blockedAt = time.Time{}
			rec.count = 0
			rec.resetAt = now.Add(l.window)
		}

		// Check if window expired
		if now.After(rec.resetAt) {
			rec.count = 0
			rec.resetAt = now.Add(l.window)
		}

		rec.count++
		if rec.count > l.limit {
			rec.blockedAt = now
			return false, l.blockTime
		}
		return true, 0
	}

	// New entry: check bounded capacity
	if len(l.records) >= l.maxRecords {
		l.evictOldestOrExpired(now)
	}

	l.records[key] = &clientRecord{
		count:   1,
		resetAt: now.Add(l.window),
	}
	return true, 0
}

// RecordSuccess resets or clears the rate limit counter for a key upon successful action.
func (l *Limiter) RecordSuccess(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.records, key)
}

func (l *Limiter) evictOldestOrExpired(now time.Time) {
	// First pass: remove expired entries
	for k, rec := range l.records {
		if (rec.blockedAt.IsZero() && now.After(rec.resetAt)) ||
			(!rec.blockedAt.IsZero() && now.After(rec.blockedAt.Add(l.blockTime))) {
			delete(l.records, k)
		}
	}

	// If still at capacity, delete an entry to bound memory
	if len(l.records) >= l.maxRecords {
		for k := range l.records {
			delete(l.records, k)
			break
		}
	}
}

func (l *Limiter) cleanupLoop() {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			l.mu.Lock()
			now := time.Now()
			for k, rec := range l.records {
				if (rec.blockedAt.IsZero() && now.After(rec.resetAt)) ||
					(!rec.blockedAt.IsZero() && now.After(rec.blockedAt.Add(l.blockTime))) {
					delete(l.records, k)
				}
			}
			l.mu.Unlock()
		case <-l.stopCh:
			return
		}
	}
}

// Stop cleanly terminates the background cleanup goroutine.
func (l *Limiter) Stop() {
	select {
	case <-l.stopCh:
	default:
		close(l.stopCh)
	}
}
