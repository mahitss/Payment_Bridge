package auth

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// clientBucket tracks attempt counts within a sliding time window.
type clientBucket struct {
	attempts    int
	windowStart time.Time
}

// RateLimiter provides in-memory, thread-safe rate limiting for sensitive authentication endpoints.
type RateLimiter struct {
	mu             sync.Mutex
	buckets        map[string]*clientBucket
	maxAttempts    int
	window         time.Duration
	blockDuration  time.Duration
	trustedProxies []*net.IPNet
	cleanupTicker  *time.Ticker
	stopCleanup    chan struct{}
}

// Default trusted CIDR ranges (loopback and RFC1918 / RFC4193 private networks).
var defaultTrustedCIDRs = []string{
	"127.0.0.0/8",    // IPv4 Loopback
	"::1/128",        // IPv6 Loopback
	"10.0.0.0/8",     // RFC1918 Private
	"172.16.0.0/12",  // RFC1918 Private (Docker default bridges)
	"192.168.0.0/16", // RFC1918 Private
	"fc00::/7",       // IPv6 Unique Local
}

func parseCIDRs(cidrs []string) []*net.IPNet {
	var ipnets []*net.IPNet
	for _, cidr := range cidrs {
		_, ipnet, err := net.ParseCIDR(cidr)
		if err == nil {
			ipnets = append(ipnets, ipnet)
		}
	}
	return ipnets
}

// NewRateLimiter creates a new RateLimiter instance.
func NewRateLimiter(maxAttempts int, window time.Duration, blockDuration time.Duration) *RateLimiter {
	rl := &RateLimiter{
		buckets:        make(map[string]*clientBucket),
		maxAttempts:    maxAttempts,
		window:         window,
		blockDuration:  blockDuration,
		trustedProxies: parseCIDRs(defaultTrustedCIDRs),
		cleanupTicker:  time.NewTicker(5 * time.Minute),
		stopCleanup:    make(chan struct{}),
	}

	go rl.startCleanupLoop()
	return rl
}

func (rl *RateLimiter) startCleanupLoop() {
	for {
		select {
		case <-rl.cleanupTicker.C:
			rl.Cleanup()
		case <-rl.stopCleanup:
			rl.cleanupTicker.Stop()
			return
		}
	}
}

// Stop stops background cleanup goroutines.
func (rl *RateLimiter) Stop() {
	close(rl.stopCleanup)
}

// Cleanup removes stale bucket entries that are outside the active time window.
func (rl *RateLimiter) Cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	for key, b := range rl.buckets {
		if now.Sub(b.windowStart) > rl.window+rl.blockDuration {
			delete(rl.buckets, key)
		}
	}
}

// Allow checks whether an attempt for the specified key is permitted.
// If not allowed, it returns false and the remaining retry-after duration in seconds.
func (rl *RateLimiter) Allow(key string) (bool, int) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, exists := rl.buckets[key]
	if !exists {
		rl.buckets[key] = &clientBucket{
			attempts:    1,
			windowStart: now,
		}
		return true, 0
	}

	// If the current window has expired, reset
	if now.Sub(b.windowStart) > rl.window {
		b.attempts = 1
		b.windowStart = now
		return true, 0
	}

	// Check if attempts exceeded limit
	if b.attempts >= rl.maxAttempts {
		remaining := rl.window - now.Sub(b.windowStart)
		if remaining < time.Second {
			remaining = time.Second
		}
		retryAfterSeconds := int(remaining.Seconds())
		if retryAfterSeconds <= 0 {
			retryAfterSeconds = 1
		}
		return false, retryAfterSeconds
	}

	b.attempts++
	return true, 0
}

// ExtractClientIP securely resolves the client IP.
// Forwarded headers (X-Forwarded-For, X-Real-IP) are ONLY trusted if the direct socket connection
// originates from a recognized loopback or internal proxy network. Direct external connections ignore XFF.
func (rl *RateLimiter) ExtractClientIP(r *http.Request) string {
	rawHost := r.RemoteAddr
	host, _, err := net.SplitHostPort(rawHost)
	if err != nil {
		host = rawHost
	}

	remoteIP := net.ParseIP(host)
	if remoteIP == nil {
		return host
	}

	// Verify if direct remote address is a trusted proxy
	isTrusted := false
	for _, trusted := range rl.trustedProxies {
		if trusted.Contains(remoteIP) {
			isTrusted = true
			break
		}
	}

	// Only inspect forwarded headers when connection comes from trusted internal proxy
	if isTrusted {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			// Find the rightmost untrusted IP in the X-Forwarded-For chain
			for i := len(parts) - 1; i >= 0; i-- {
				candidate := strings.TrimSpace(parts[i])
				ip := net.ParseIP(candidate)
				if ip != nil {
					candidateTrusted := false
					for _, trusted := range rl.trustedProxies {
						if trusted.Contains(ip) {
							candidateTrusted = true
							break
						}
					}
					if !candidateTrusted {
						return candidate
					}
				}
			}
			// If all in chain were trusted proxies, use first entry if valid
			first := strings.TrimSpace(parts[0])
			if net.ParseIP(first) != nil {
				return first
			}
		}

		if xRealIP := r.Header.Get("X-Real-IP"); xRealIP != "" {
			trimmed := strings.TrimSpace(xRealIP)
			if net.ParseIP(trimmed) != nil {
				return trimmed
			}
		}
	}

	return host
}

// Middleware creates an HTTP middleware that enforces rate limiting for a specific action.
func (rl *RateLimiter) Middleware(action string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clientIP := rl.ExtractClientIP(r)
		key := fmt.Sprintf("%s:%s", action, clientIP)

		allowed, retryAfter := rl.Allow(key)
		if !allowed {
			w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
			writeJSON(w, http.StatusTooManyRequests, map[string]interface{}{
				"error":               "too many requests, please try again later",
				"retry_after_seconds": retryAfter,
			})
			return
		}

		next(w, r)
	}
}
