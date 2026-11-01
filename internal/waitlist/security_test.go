package waitlist

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// =====================================================
// TEST HELPERS
// =====================================================

const (
	testAdminToken = "test-admin-token-with-at-least-32-characters"
	testAdminID    = "550e8400-e29b-41d4-a716-446655440000"
)

func newTestRequest(
	method string,
	target string,
) *http.Request {
	return httptest.NewRequest(method, target, nil)
}

func newTestResponse(
	handler http.Handler,
	request *http.Request,
) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

// =====================================================
// ADMIN AUTHENTICATION TESTS
// =====================================================

func TestAdminAuthMiddleware_RejectsMissingAuthorization(t *testing.T) {
	middleware := AdminAuthMiddleware(
		testAdminToken,
		testAdminID,
	)

	protectedHandler := middleware(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("protected handler should not be reached")
		}),
	)

	request := newTestRequest(
		http.MethodPut,
		"/v1/admin/waitlist/config",
	)

	response := newTestResponse(
		protectedHandler,
		request,
	)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusUnauthorized,
			response.Code,
		)
	}
}

func TestAdminAuthMiddleware_RejectsInvalidToken(t *testing.T) {
	middleware := AdminAuthMiddleware(
		testAdminToken,
		testAdminID,
	)

	protectedHandler := middleware(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("protected handler should not be reached")
		}),
	)

	request := newTestRequest(
		http.MethodPut,
		"/v1/admin/waitlist/config",
	)

	request.Header.Set(
		"Authorization",
		"Bearer incorrect-administrator-token",
	)

	response := newTestResponse(
		protectedHandler,
		request,
	)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusUnauthorized,
			response.Code,
		)
	}
}

func TestAdminAuthMiddleware_RejectsMalformedAuthorization(t *testing.T) {
	testCases := []struct {
		name  string
		value string
	}{
		{
			name:  "missing bearer prefix",
			value: testAdminToken,
		},
		{
			name:  "empty bearer token",
			value: "Bearer ",
		},
		{
			name:  "multiple tokens",
			value: "Bearer token extra",
		},
		{
			name:  "wrong authentication scheme",
			value: "Basic " + testAdminToken,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			middleware := AdminAuthMiddleware(
				testAdminToken,
				testAdminID,
			)

			protectedHandler := middleware(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					t.Fatal("protected handler should not be reached")
				}),
			)

			request := newTestRequest(
				http.MethodPut,
				"/v1/admin/waitlist/config",
			)

			request.Header.Set(
				"Authorization",
				testCase.value,
			)

			response := newTestResponse(
				protectedHandler,
				request,
			)

			if response.Code != http.StatusUnauthorized {
				t.Fatalf(
					"expected status %d, got %d",
					http.StatusUnauthorized,
					response.Code,
				)
			}
		})
	}
}

func TestAdminAuthMiddleware_AcceptsValidToken(t *testing.T) {
	middleware := AdminAuthMiddleware(
		testAdminToken,
		testAdminID,
	)

	handlerReached := false

	protectedHandler := middleware(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handlerReached = true
			w.WriteHeader(http.StatusOK)
		}),
	)

	request := newTestRequest(
		http.MethodPut,
		"/v1/admin/waitlist/config",
	)

	request.Header.Set(
		"Authorization",
		"Bearer "+testAdminToken,
	)

	response := newTestResponse(
		protectedHandler,
		request,
	)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusOK,
			response.Code,
		)
	}

	if !handlerReached {
		t.Fatal("expected protected handler to be reached")
	}
}

func TestAdminAuthMiddleware_InjectsConfiguredAdminID(t *testing.T) {
	middleware := AdminAuthMiddleware(
		testAdminToken,
		testAdminID,
	)

	protectedHandler := middleware(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			adminID, ok := r.Context().
				Value(adminIDContextKey{}).(string)

			if !ok {
				t.Error("expected administrator ID in request context")
				return
			}

			if adminID != testAdminID {
				t.Errorf(
					"expected administrator ID %q, got %q",
					testAdminID,
					adminID,
				)
			}

			w.WriteHeader(http.StatusOK)
		}),
	)

	request := newTestRequest(
		http.MethodPut,
		"/v1/admin/waitlist/config",
	)

	request.Header.Set(
		"Authorization",
		"Bearer "+testAdminToken,
	)

	response := newTestResponse(
		protectedHandler,
		request,
	)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusOK,
			response.Code,
		)
	}
}

func TestAdminAuthMiddleware_RejectsInvalidConfiguration(t *testing.T) {
	testCases := []struct {
		name  string
		token string
		id    string
	}{
		{
			name:  "empty token",
			token: "",
			id:    testAdminID,
		},
		{
			name:  "short token",
			token: "short-token",
			id:    testAdminID,
		},
		{
			name:  "missing administrator ID",
			token: testAdminToken,
			id:    "",
		},
		{
			name:  "invalid administrator UUID",
			token: testAdminToken,
			id:    "not-a-valid-uuid",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			middleware := AdminAuthMiddleware(
				testCase.token,
				testCase.id,
			)

			protectedHandler := middleware(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					t.Fatal("protected handler should not be reached")
				}),
			)

			request := newTestRequest(
				http.MethodPut,
				"/v1/admin/waitlist/config",
			)

			request.Header.Set(
				"Authorization",
				"Bearer "+testAdminToken,
			)

			response := newTestResponse(
				protectedHandler,
				request,
			)

			if response.Code != http.StatusServiceUnavailable {
				t.Fatalf(
					"expected status %d, got %d",
					http.StatusServiceUnavailable,
					response.Code,
				)
			}
		})
	}
}

// =====================================================
// REGISTRATION RATE LIMITER TESTS
// =====================================================

func TestRegistrationRateLimiter_AllowsRequestsWithinLimit(t *testing.T) {
	limiter := NewRegistrationRateLimiter(
		5,
		time.Minute,
	)

	called := 0

	handler := limiter.Middleware(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called++
			w.WriteHeader(http.StatusOK)
		}),
	)

	for i := 0; i < 5; i++ {
		request := newTestRequest(
			http.MethodPost,
			"/v1/waitlist/register",
		)

		request.RemoteAddr = "192.0.2.10:12345"

		response := newTestResponse(handler, request)

		if response.Code != http.StatusOK {
			t.Fatalf(
				"request %d: expected status %d, got %d",
				i+1,
				http.StatusOK,
				response.Code,
			)
		}
	}

	if called != 5 {
		t.Fatalf(
			"expected handler to be called 5 times, got %d",
			called,
		)
	}
}

func TestRegistrationRateLimiter_RejectsRequestsOverLimit(t *testing.T) {
	limiter := NewRegistrationRateLimiter(
		5,
		time.Minute,
	)

	called := 0

	handler := limiter.Middleware(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called++
			w.WriteHeader(http.StatusOK)
		}),
	)

	var lastResponse *httptest.ResponseRecorder

	for i := 0; i < 6; i++ {
		request := newTestRequest(
			http.MethodPost,
			"/v1/waitlist/register",
		)

		request.RemoteAddr = "192.0.2.20:12345"

		lastResponse = newTestResponse(handler, request)
	}

	if lastResponse.Code != http.StatusTooManyRequests {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusTooManyRequests,
			lastResponse.Code,
		)
	}

	if called != 5 {
		t.Fatalf(
			"expected handler to be called 5 times, got %d",
			called,
		)
	}

	if lastResponse.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header")
	}
}

func TestRegistrationRateLimiter_ResetsAfterWindow(t *testing.T) {
	limiter := NewRegistrationRateLimiter(
		1,
		time.Minute,
	)

	currentTime := time.Date(
		2026,
		time.October,
		30,
		12,
		0,
		0,
		0,
		time.UTC,
	)

	limiter.now = func() time.Time {
		return currentTime
	}

	handler := limiter.Middleware(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	makeRequest := func() *httptest.ResponseRecorder {
		request := newTestRequest(
			http.MethodPost,
			"/v1/waitlist/register",
		)

		request.RemoteAddr = "192.0.2.30:12345"

		return newTestResponse(handler, request)
	}

	first := makeRequest()

	if first.Code != http.StatusOK {
		t.Fatalf("expected first request to succeed, got %d", first.Code)
	}

	second := makeRequest()

	if second.Code != http.StatusTooManyRequests {
		t.Fatalf(
			"expected second request to be rate limited, got %d",
			second.Code,
		)
	}

	currentTime = currentTime.Add(time.Minute)

	third := makeRequest()

	if third.Code != http.StatusOK {
		t.Fatalf(
			"expected request after window reset to succeed, got %d",
			third.Code,
		)
	}
}

func TestRegistrationRateLimiter_SeparatesClientIPs(t *testing.T) {
	limiter := NewRegistrationRateLimiter(
		1,
		time.Minute,
	)

	handler := limiter.Middleware(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	requests := []string{
		"192.0.2.40:12345",
		"192.0.2.41:12345",
	}

	for _, remoteAddr := range requests {
		request := newTestRequest(
			http.MethodPost,
			"/v1/waitlist/register",
		)

		request.RemoteAddr = remoteAddr

		response := newTestResponse(handler, request)

		if response.Code != http.StatusOK {
			t.Errorf(
				"expected independent IP request to succeed, got %d",
				response.Code,
			)
		}
	}
}

func TestRegistrationRateLimiter_ConcurrentRequests(t *testing.T) {
	const (
		limit       = 5
		concurrency = 20
	)

	limiter := NewRegistrationRateLimiter(
		limit,
		time.Minute,
	)

	var successful int
	var rateLimited int

	var resultMu sync.Mutex
	var waitGroup sync.WaitGroup

	handler := limiter.Middleware(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	for i := 0; i < concurrency; i++ {
		waitGroup.Add(1)

		go func() {
			defer waitGroup.Done()

			request := newTestRequest(
				http.MethodPost,
				"/v1/waitlist/register",
			)

			request.RemoteAddr = "192.0.2.50:12345"

			response := newTestResponse(handler, request)

			resultMu.Lock()
			defer resultMu.Unlock()

			switch response.Code {
			case http.StatusOK:
				successful++

			case http.StatusTooManyRequests:
				rateLimited++

			default:
				t.Errorf(
					"unexpected response status: %d",
					response.Code,
				)
			}
		}()
	}

	waitGroup.Wait()

	if successful != limit {
		t.Errorf(
			"expected %d successful requests, got %d",
			limit,
			successful,
		)
	}

	if rateLimited != concurrency-limit {
		t.Errorf(
			"expected %d rate-limited requests, got %d",
			concurrency-limit,
			rateLimited,
		)
	}
}

// =====================================================
// CORS TESTS
// =====================================================

func TestCORSMiddleware_AllowsConfiguredOrigin(t *testing.T) {
	middleware := CORSMiddleware(
		[]string{
			"https://lumayapower.co.uk",
			"https://www.lumayapower.co.uk",
		},
	)

	handler := middleware(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	request := newTestRequest(
		http.MethodGet,
		"/v1/waitlist/count",
	)

	request.Header.Set(
		"Origin",
		"https://lumayapower.co.uk",
	)

	response := newTestResponse(handler, request)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusOK,
			response.Code,
		)
	}

	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "https://lumayapower.co.uk" {
		t.Errorf(
			"unexpected allowed origin: %q",
			got,
		)
	}

	if got := response.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Errorf(
			"credentials must not be enabled, got %q",
			got,
		)
	}
}

func TestCORSMiddleware_RejectsUnconfiguredOrigin(t *testing.T) {
	middleware := CORSMiddleware(
		[]string{
			"https://lumayapower.co.uk",
		},
	)

	handler := middleware(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("handler should not be reached")
		}),
	)

	request := newTestRequest(
		http.MethodGet,
		"/v1/waitlist/count",
	)

	request.Header.Set(
		"Origin",
		"https://malicious.example",
	)

	response := newTestResponse(handler, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusForbidden,
			response.Code,
		)
	}
}

func TestCORSMiddleware_AllowsRequestWithoutOrigin(t *testing.T) {
	middleware := CORSMiddleware(
		[]string{
			"https://lumayapower.co.uk",
		},
	)

	handler := middleware(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	request := newTestRequest(
		http.MethodGet,
		"/v1/waitlist/count",
	)

	response := newTestResponse(handler, request)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusOK,
			response.Code,
		)
	}
}

func TestCORSMiddleware_AllowsValidPreflight(t *testing.T) {
	middleware := CORSMiddleware(
		[]string{
			"https://lumayapower.co.uk",
		},
	)

	handler := middleware(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("preflight request should not reach application handler")
		}),
	)

	request := newTestRequest(
		http.MethodOptions,
		"/v1/waitlist/register",
	)

	request.Header.Set(
		"Origin",
		"https://lumayapower.co.uk",
	)

	request.Header.Set(
		"Access-Control-Request-Method",
		"POST",
	)

	request.Header.Set(
		"Access-Control-Request-Headers",
		"content-type, authorization",
	)

	response := newTestResponse(handler, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusNoContent,
			response.Code,
		)
	}

	if response.Header().Get("Access-Control-Allow-Origin") != "https://lumayapower.co.uk" {
		t.Fatal("expected configured origin in preflight response")
	}
}

func TestCORSMiddleware_RejectsDisallowedPreflightMethod(t *testing.T) {
	middleware := CORSMiddleware(
		[]string{
			"https://lumayapower.co.uk",
		},
	)

	handler := middleware(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("preflight request should not reach application handler")
		}),
	)

	request := newTestRequest(
		http.MethodOptions,
		"/v1/waitlist/register",
	)

	request.Header.Set(
		"Origin",
		"https://lumayapower.co.uk",
	)

	request.Header.Set(
		"Access-Control-Request-Method",
		"DELETE",
	)

	response := newTestResponse(handler, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusForbidden,
			response.Code,
		)
	}
}

func TestCORSMiddleware_RejectsDisallowedPreflightHeader(t *testing.T) {
	middleware := CORSMiddleware(
		[]string{
			"https://lumayapower.co.uk",
		},
	)

	handler := middleware(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("preflight request should not reach application handler")
		}),
	)

	request := newTestRequest(
		http.MethodOptions,
		"/v1/waitlist/register",
	)

	request.Header.Set(
		"Origin",
		"https://lumayapower.co.uk",
	)

	request.Header.Set(
		"Access-Control-Request-Method",
		"POST",
	)

	request.Header.Set(
		"Access-Control-Request-Headers",
		"content-type, x-unauthorised-header",
	)

	response := newTestResponse(handler, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusForbidden,
			response.Code,
		)
	}
}

func TestCORSMiddleware_DoesNotAllowWildcardOrigin(t *testing.T) {
	middleware := CORSMiddleware(
		[]string{"*"},
	)

	handler := middleware(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	request := newTestRequest(
		http.MethodGet,
		"/v1/waitlist/count",
	)

	request.Header.Set(
		"Origin",
		"https://lumayapower.co.uk",
	)

	response := newTestResponse(handler, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf(
			"expected wildcard origin to be rejected, got %d",
			response.Code,
		)
	}
}

func TestCORSMiddleware_PreservesExistingVaryHeaders(t *testing.T) {
	middleware := CORSMiddleware(
		[]string{"https://lumayapower.co.uk"},
	)

	handler := middleware(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("Vary", "Accept-Encoding")
			w.WriteHeader(http.StatusOK)
		}),
	)

	request := newTestRequest(
		http.MethodGet,
		"/v1/waitlist/count",
	)

	request.Header.Set(
		"Origin",
		"https://lumayapower.co.uk",
	)

	response := newTestResponse(handler, request)

	vary := strings.Join(
		response.Header().Values("Vary"),
		",",
	)

	if !strings.Contains(vary, "Origin") {
		t.Errorf("expected Vary to contain Origin, got %q", vary)
	}

	if !strings.Contains(vary, "Accept-Encoding") {
		t.Errorf("expected existing Vary header to be preserved, got %q", vary)
	}
}

// =====================================================
// ROUTE SECURITY TESTS
// =====================================================

func TestRegisterRoutes_ProtectsAdminConfiguration(t *testing.T) {
	router := chi.NewRouter()

	// The handler's service is deliberately nil because
	// the request must be rejected before reaching it.
	handler := &Handler{}

	RegisterRoutes(
		router,
		handler,
		RouteConfig{
			AdminToken: testAdminToken,
			AdminID:    testAdminID,
		},
	)

	request := newTestRequest(
		http.MethodPut,
		"/v1/admin/waitlist/config",
	)

	response := newTestResponse(router, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusUnauthorized,
			response.Code,
		)
	}
}

func TestRegisterRoutes_RejectsInvalidAdminToken(t *testing.T) {
	router := chi.NewRouter()

	handler := &Handler{}

	RegisterRoutes(
		router,
		handler,
		RouteConfig{
			AdminToken: testAdminToken,
			AdminID:    testAdminID,
		},
	)

	request := newTestRequest(
		http.MethodPut,
		"/v1/admin/waitlist/config",
	)

	request.Header.Set(
		"Authorization",
		"Bearer incorrect-token",
	)

	response := newTestResponse(router, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusUnauthorized,
			response.Code,
		)
	}
}

func TestRegisterRoutes_RejectsUnconfiguredAdminIdentity(t *testing.T) {
	router := chi.NewRouter()

	handler := &Handler{}

	RegisterRoutes(
		router,
		handler,
		RouteConfig{
			AdminToken: testAdminToken,
			AdminID:    "invalid-uuid",
		},
	)

	request := newTestRequest(
		http.MethodPut,
		"/v1/admin/waitlist/config",
	)

	request.Header.Set(
		"Authorization",
		"Bearer "+testAdminToken,
	)

	response := newTestResponse(router, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusServiceUnavailable,
			response.Code,
		)
	}
}

// =====================================================
// CONTEXT ISOLATION TEST
// =====================================================

func TestAdminAuthMiddleware_DoesNotTrustClientSuppliedAdminID(t *testing.T) {
	middleware := AdminAuthMiddleware(
		testAdminToken,
		testAdminID,
	)

	protectedHandler := middleware(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			adminID, ok := r.Context().
				Value(adminIDContextKey{}).(string)

			if !ok || adminID != testAdminID {
				t.Errorf(
					"expected configured admin identity %q, got %q",
					testAdminID,
					adminID,
				)
			}

			w.WriteHeader(http.StatusOK)
		}),
	)

	request := newTestRequest(
		http.MethodPut,
		"/v1/admin/waitlist/config",
	)

	request.Header.Set(
		"Authorization",
		"Bearer "+testAdminToken,
	)

	// A client must not be able to choose the audit identity.
	request.Header.Set(
		"X-Admin-ID",
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
	)

	request = request.WithContext(
		context.Background(),
	)

	response := newTestResponse(
		protectedHandler,
		request,
	)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusOK,
			response.Code,
		)
	}
}
