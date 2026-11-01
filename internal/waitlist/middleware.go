package waitlist

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// =====================================================
// ADMIN AUTHENTICATION
// =====================================================

type adminIDContextKey struct{}

// AdminAuthMiddleware protects administrator endpoints.
//
// The configured bearer token authenticates the request.
// The configured administrator UUID identifies the actor
// for database audit records.
//
// This is a single-administrator authentication mechanism.
// It is not a multi-user identity management system.
func AdminAuthMiddleware(
	expectedToken string,
	adminID string,
) func(http.Handler) http.Handler {

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {

				expectedToken = strings.TrimSpace(expectedToken)
				adminID = strings.TrimSpace(adminID)

				if len(expectedToken) < 32 || !validUUID(adminID) {
					writeError(
						w,
						http.StatusServiceUnavailable,
						"Administrator authentication is not configured.",
					)
					return
				}

				authorization := strings.TrimSpace(
					r.Header.Get("Authorization"),
				)

				parts := strings.Fields(authorization)

				if len(parts) != 2 ||
					!strings.EqualFold(parts[0], "Bearer") {

					writeError(
						w,
						http.StatusUnauthorized,
						"Administrator authentication required.",
					)
					return
				}

				providedToken := parts[1]

				providedHash := sha256.Sum256(
					[]byte(providedToken),
				)

				expectedHash := sha256.Sum256(
					[]byte(expectedToken),
				)

				if subtle.ConstantTimeCompare(
					providedHash[:],
					expectedHash[:],
				) != 1 {

					writeError(
						w,
						http.StatusUnauthorized,
						"Invalid administrator credentials.",
					)
					return
				}

				// Identity comes from server configuration,
				// never from client-supplied request data.
				ctx := context.WithValue(
					r.Context(),
					adminIDContextKey{},
					adminID,
				)

				next.ServeHTTP(
					w,
					r.WithContext(ctx),
				)
			},
		)
	}
}

// =====================================================
// PUBLIC REGISTRATION RATE LIMITER
// =====================================================

type rateLimitEntry struct {
	count       int
	windowStart time.Time
}

type RegistrationRateLimiter struct {
	mu sync.Mutex

	entries map[string]rateLimitEntry

	limit  int
	window time.Duration
	now    func() time.Time

	// Avoid scanning the entire map on every request.
	requestCount uint64
}

func NewRegistrationRateLimiter(
	limit int,
	window time.Duration,
) *RegistrationRateLimiter {

	if limit < 1 {
		limit = 5
	}

	if window <= 0 {
		window = time.Minute
	}

	return &RegistrationRateLimiter{
		entries: make(map[string]rateLimitEntry),
		limit:   limit,
		window:  window,
		now:     time.Now,
	}
}

// clientIP obtains the immediate network peer address.
//
// Forwarded headers are deliberately not trusted here.
// Only use forwarded IP addresses if the application
// is behind a correctly configured trusted proxy.
func clientIP(r *http.Request) string {
	remoteAddr := strings.TrimSpace(r.RemoteAddr)

	if remoteAddr == "" {
		return "unknown"
	}

	host, _, err := net.SplitHostPort(remoteAddr)

	if err == nil && host != "" {
		return host
	}

	// Handles addresses without a port, including IPv6.
	return remoteAddr
}

// Middleware applies a fixed-window registration limit.
//
// The default configured limit is five attempts per IP
// address per minute.
//
// This limiter is process-local. It is not distributed
// across multiple application instances.
func (l *RegistrationRateLimiter) Middleware(
	next http.Handler,
) http.Handler {

	return http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {

			ip := clientIP(r)
			now := l.now()

			l.mu.Lock()

			entry, exists := l.entries[ip]

			if !exists ||
				now.Sub(entry.windowStart) >= l.window {

				entry = rateLimitEntry{
					count:       0,
					windowStart: now,
				}
			}

			if entry.count >= l.limit {

				retryAfter := l.window -
					now.Sub(entry.windowStart)

				if retryAfter < 0 {
					retryAfter = 0
				}

				retrySeconds := int(
					retryAfter.Seconds(),
				) + 1

				w.Header().Set(
					"Retry-After",
					strconv.Itoa(retrySeconds),
				)

				l.entries[ip] = entry

				l.mu.Unlock()

				writeError(
					w,
					http.StatusTooManyRequests,
					"Too many registration attempts. Please try again later.",
				)

				return
			}

			entry.count++

			l.entries[ip] = entry

			l.requestCount++

			// Opportunistically remove expired entries
			// every 256 requests rather than scanning the
			// map on every incoming registration request.
			if l.requestCount%256 == 0 {

				for key, value := range l.entries {

					if now.Sub(value.windowStart) >= l.window {
						delete(l.entries, key)
					}
				}
			}

			l.mu.Unlock()

			next.ServeHTTP(w, r)
		},
	)
}

// =====================================================
// CORS
// =====================================================

// CORSMiddleware permits requests only from explicitly
// configured frontend origins.
//
// Wildcard origins and credentialed cross-origin requests
// are deliberately not enabled.
func CORSMiddleware(
	allowedOrigins []string,
) func(http.Handler) http.Handler {

	allowed := make(map[string]struct{}, len(allowedOrigins))

	for _, origin := range allowedOrigins {

		origin = strings.TrimSpace(origin)

		if origin == "" {
			continue
		}

		allowed[origin] = struct{}{}
	}

	allowedMethods := "GET, POST, PUT, OPTIONS"
	allowedHeaders := "Content-Type, Authorization"

	return func(next http.Handler) http.Handler {

		return http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {

				origin := strings.TrimSpace(
					r.Header.Get("Origin"),
				)

				if origin != "" {

					if _, ok := allowed[origin]; !ok {

						writeError(
							w,
							http.StatusForbidden,
							"Origin not allowed.",
						)

						return
					}

					w.Header().Set(
						"Access-Control-Allow-Origin",
						origin,
					)

					addVaryHeader(w, "Origin")

					w.Header().Set(
						"Access-Control-Allow-Methods",
						allowedMethods,
					)

					w.Header().Set(
						"Access-Control-Allow-Headers",
						allowedHeaders,
					)

					w.Header().Set(
						"Access-Control-Max-Age",
						"600",
					)
				}

				// Handle browser preflight requests.
				if r.Method == http.MethodOptions {

					if origin != "" {

						requestedMethod := strings.TrimSpace(
							r.Header.Get("Access-Control-Request-Method"),
						)

						if requestedMethod != "" &&
							!isAllowedCORSMethod(requestedMethod) {

							writeError(
								w,
								http.StatusForbidden,
								"Requested method is not allowed.",
							)

							return
						}

						requestedHeaders := strings.TrimSpace(
							r.Header.Get("Access-Control-Request-Headers"),
						)

						if requestedHeaders != "" &&
							!areAllowedCORSHeaders(requestedHeaders) {

							writeError(
								w,
								http.StatusForbidden,
								"Requested headers are not allowed.",
							)

							return
						}
					}

					w.WriteHeader(http.StatusNoContent)
					return
				}

				next.ServeHTTP(w, r)
			},
		)
	}
}

// =====================================================
// CORS HELPERS
// =====================================================

func isAllowedCORSMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodGet,
		http.MethodPost,
		http.MethodPut:
		return true

	default:
		return false
	}
}

func areAllowedCORSHeaders(headers string) bool {

	allowed := map[string]struct{}{
		"content-type":  {},
		"authorization": {},
	}

	for _, header := range strings.Split(headers, ",") {

		header = strings.ToLower(
			strings.TrimSpace(header),
		)

		if header == "" {
			continue
		}

		if _, ok := allowed[header]; !ok {
			return false
		}
	}

	return true
}

// addVaryHeader preserves existing Vary values.
func addVaryHeader(
	w http.ResponseWriter,
	value string,
) {

	for _, existing := range w.Header().Values("Vary") {

		for _, item := range strings.Split(existing, ",") {

			if strings.EqualFold(
				strings.TrimSpace(item),
				value,
			) {
				return
			}
		}
	}

	w.Header().Add("Vary", value)
}
