package ratelimit

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestGetClientIP_AntiSpoofing(t *testing.T) {
	// 1. Untrusted remote IP trying to spoof X-Forwarded-For
	req1, _ := http.NewRequest("GET", "/", nil)
	req1.RemoteAddr = "198.51.100.55:54321"
	req1.Header.Set("X-Forwarded-For", "8.8.8.8, 1.1.1.1")
	req1.Header.Set("X-Real-IP", "9.9.9.9")

	ip1 := GetClientIP(req1)
	if ip1 != "198.51.100.55" {
		t.Errorf("GetClientIP trusted X-Forwarded-For from untrusted remote IP! got %q, want %q", ip1, "198.51.100.55")
	}

	// 2. Trusted proxy (localhost) with X-Forwarded-For
	req2, _ := http.NewRequest("GET", "/", nil)
	req2.RemoteAddr = "127.0.0.1:39102"
	req2.Header.Set("X-Forwarded-For", "203.0.113.195, 10.0.0.2")

	ip2 := GetClientIP(req2)
	if ip2 != "203.0.113.195" {
		t.Errorf("GetClientIP failed to extract X-Forwarded-For from trusted proxy: got %q, want %q", ip2, "203.0.113.195")
	}

	// 3. Trusted proxy (docker bridge 172.17.0.1) with X-Real-IP
	req3, _ := http.NewRequest("GET", "/", nil)
	req3.RemoteAddr = "172.17.0.1:41234"
	req3.Header.Set("X-Real-IP", "198.51.100.77")

	ip3 := GetClientIP(req3)
	if ip3 != "198.51.100.77" {
		t.Errorf("GetClientIP failed to extract X-Real-IP from docker bridge: got %q, want %q", ip3, "198.51.100.77")
	}
}

func TestLimiter_RateAndBlock(t *testing.T) {
	limiter := NewLimiter(3, 100*time.Millisecond, 200*time.Millisecond, 100)
	defer limiter.Stop()

	key := "test-client"

	// 1, 2, 3 must be allowed
	for i := 1; i <= 3; i++ {
		allowed, _ := limiter.Allow(key)
		if !allowed {
			t.Fatalf("attempt %d should be allowed", i)
		}
	}

	// 4th must be rejected and blocked
	allowed, rem := limiter.Allow(key)
	if allowed {
		t.Fatalf("attempt 4 should be rejected")
	}
	if rem <= 0 {
		t.Errorf("expected positive block duration, got %v", rem)
	}

	// Wait for block to expire (200ms)
	time.Sleep(250 * time.Millisecond)

	// Should be allowed again
	allowed, _ = limiter.Allow(key)
	if !allowed {
		t.Fatalf("should be allowed after block expired")
	}
}

func TestLimiter_BoundedCapacity(t *testing.T) {
	maxCap := 50
	limiter := NewLimiter(5, time.Minute, time.Minute, maxCap)
	defer limiter.Stop()

	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("ip-%d", i)
		limiter.Allow(key)
	}

	limiter.mu.Lock()
	mapSize := len(limiter.records)
	limiter.mu.Unlock()

	if mapSize > maxCap {
		t.Errorf("limiter map exceeded max capacity: got %d, max %d", mapSize, maxCap)
	}
}
