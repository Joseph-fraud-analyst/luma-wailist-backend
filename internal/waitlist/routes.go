package waitlist

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// =====================================================
// ROUTE CONFIGURATION
// =====================================================

type RouteConfig struct {
	AdminToken     string
	AdminID        string
	AllowedOrigins []string
}

// =====================================================
// ROUTE REGISTRATION
// =====================================================

// RegisterRoutes registers all waitlist API endpoints.
//
// CORS is intentionally configured separately so the main
// application can install it once at the router level.
func RegisterRoutes(
	router chi.Router,
	handler *Handler,
	config RouteConfig,
) {
	// Public registration rate limiter.
	//
	// Default:
	// 5 registration attempts per IP address per minute.
	registrationLimiter := NewRegistrationRateLimiter(
		5,
		time.Minute,
	)

	// -------------------------------------------------
	// PUBLIC WAITLIST ENDPOINTS
	// -------------------------------------------------

	router.Route("/v1/waitlist", func(r chi.Router) {

		// Register a resident on the waitlist.
		r.With(
			registrationLimiter.Middleware,
		).Post(
			"/register",
			handler.Register,
		)

		// Return the current number of registrations.
		r.Get(
			"/count",
			handler.Count,
		)

		// Return public launch information.
		r.Get(
			"/config",
			handler.GetConfig,
		)

		// Process a signed unsubscribe token.
		//
		// This is a GET endpoint because unsubscribe links
		// are included directly in welcome and future emails.
		//
		// The token is cryptographically signed and verified
		// by the UnsubscribeTokenManager before any preference
		// is changed.
		r.Get(
			"/unsubscribe",
			handler.Unsubscribe,
		)
	})

	// -------------------------------------------------
	// ADMINISTRATOR ENDPOINTS
	// -------------------------------------------------

	router.With(
		AdminAuthMiddleware(
			config.AdminToken,
			config.AdminID,
		),
	).Put(
		"/v1/admin/waitlist/config",
		handler.UpdateConfig,
	)
}

// =====================================================
// STANDALONE HTTP HANDLER
// =====================================================

// NewHTTPHandler creates a standalone HTTP handler.
//
// Use this when the waitlist package is being served
// independently. Do not use it inside app.NewServer
// if CORS is already installed on the application router.
func NewHTTPHandler(
	service *Service,
	tokens *UnsubscribeTokenManager,
	emailSender EmailSender,
	config RouteConfig,
) http.Handler {

	router := chi.NewRouter()

	handler := NewHandler(
		service,
		tokens,
		emailSender,
	)

	// Apply CORS once at the router level.
	router.Use(
		CORSMiddleware(config.AllowedOrigins),
	)

	RegisterRoutes(
		router,
		handler,
		config,
	)

	return router
}
