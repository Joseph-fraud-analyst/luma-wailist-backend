package app

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lumaya-wishlist-backend/internal/waitlist"
)

type DBPinger interface {
	Ping(ctx context.Context) error
}

type Server struct {
	handler http.Handler
	db      DBPinger
}

// NewServer initialises the HTTP application.
//
// It does not create databases, modify database permissions,
// or run database migrations. The supplied PostgreSQL pool
// must already be connected to the dedicated waitlist database.
func NewServer(
	pool *pgxpool.Pool,
) (*Server, error) {
	if pool == nil {
		return nil, errors.New(
			"database connection pool is required",
		)
	}

	// --------------------------------------------------
	// Administrator configuration
	// --------------------------------------------------

	adminToken := strings.TrimSpace(
		os.Getenv("ADMIN_API_TOKEN"),
	)

	if len(adminToken) < 32 {
		return nil, errors.New(
			"ADMIN_API_TOKEN must contain at least 32 characters",
		)
	}

	adminID := strings.TrimSpace(
		os.Getenv("ADMIN_ID"),
	)

	if !validUUID(adminID) {
		return nil, errors.New(
			"ADMIN_ID must be a valid UUID",
		)
	}

	// --------------------------------------------------
	// Unsubscribe token configuration
	// --------------------------------------------------

	unsubscribeSecret := os.Getenv(
		"UNSUBSCRIBE_TOKEN_SECRET",
	)

	tokenManager, err :=
		waitlist.NewUnsubscribeTokenManager(
			unsubscribeSecret,
		)

	if err != nil {
		return nil, err
	}
	// --------------------------------------------------
	// Email configuration
	// --------------------------------------------------

	sendGridAPIKey := strings.TrimSpace(
		os.Getenv("SENDGRID_API_KEY"),
	)

	sendGridSenderEmail := strings.TrimSpace(
		os.Getenv("SENDGRID_SENDER_EMAIL"),
	)

	slog.Info(
		"SendGrid configuration loaded",
		"api_key_present", sendGridAPIKey != "",
		"api_key_length", len(sendGridAPIKey),
		"sender_email_present", sendGridSenderEmail != "",
		"api_key_prefix", firstCharacters(sendGridAPIKey, 3),
		"sender_email", sendGridSenderEmail,
	)

	emailSender, err := waitlist.NewSendGridEmailSender(
		sendGridAPIKey,
		sendGridSenderEmail,
	)

	if err != nil {
		return nil, err
	}

	// --------------------------------------------------
	// CORS configuration
	// --------------------------------------------------

	origins := parseAllowedOrigins(
		os.Getenv("CORS_ALLOWED_ORIGINS"),
	)

	// --------------------------------------------------
	// Application dependencies
	// --------------------------------------------------

	repository :=
		waitlist.NewPostgresRepository(pool)

	service :=
		waitlist.NewService(repository)

	handler :=
		waitlist.NewHandler(
			service,
			tokenManager,
			emailSender,
		)
	// --------------------------------------------------
	// HTTP router
	// --------------------------------------------------

	router := chi.NewRouter()

	// Request identification and panic recovery.
	router.Use(
		middleware.RequestID,
	)

	router.Use(
		middleware.Recoverer,
	)

	// Security response headers.
	router.Use(
		securityHeaders,
	)

	// Install CORS once for the entire application.
	//
	// CORS_ALLOWED_ORIGINS must contain the exact
	// frontend origin, for example:
	//
	// http://localhost:5173
	router.Use(
		waitlist.CORSMiddleware(origins),
	)

	server := &Server{
		handler: router,
		db:      pool,
	}

	// --------------------------------------------------
	// Health checks
	// --------------------------------------------------

	router.Get(
		"/health/live",
		server.liveHandler,
	)

	router.Get(
		"/health/ready",
		server.readyHandler,
	)

	// --------------------------------------------------
	// Waitlist API
	// --------------------------------------------------

	waitlist.RegisterRoutes(
		router,
		handler,
		waitlist.RouteConfig{
			AdminToken: adminToken,
			AdminID:    adminID,

			AllowedOrigins: origins,
		},
	)

	return server, nil
}

func firstCharacters(value string, length int) string {
	if len(value) <= length {
		return value
	}

	return value[:length]
}

// Handler exposes the configured HTTP handler.
func (s *Server) Handler() http.Handler {
	return s.handler
}

// =====================================================
// HEALTH CHECKS
// =====================================================

func (s *Server) liveHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	writeJSON(
		w,
		http.StatusOK,
		map[string]interface{}{
			"success": true,
			"status":  "alive",
		},
	)
}

func (s *Server) readyHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel := context.WithTimeout(
		r.Context(),
		3*time.Second,
	)
	defer cancel()

	if s.db == nil {
		writeJSON(
			w,
			http.StatusServiceUnavailable,
			map[string]interface{}{
				"success": false,
				"status":  "not_ready",
			},
		)
		return
	}

	if err := s.db.Ping(ctx); err != nil {
		// Never expose database connection details.
		writeJSON(
			w,
			http.StatusServiceUnavailable,
			map[string]interface{}{
				"success": false,
				"status":  "not_ready",
			},
		)
		return
	}

	writeJSON(
		w,
		http.StatusOK,
		map[string]interface{}{
			"success":  true,
			"status":   "ready",
			"database": "connected",
		},
	)
}

// =====================================================
// HTTP HELPERS
// =====================================================

func writeJSON(
	w http.ResponseWriter,
	status int,
	payload interface{},
) {
	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(payload)
}

// =====================================================
// SECURITY HEADERS
// =====================================================

func securityHeaders(
	next http.Handler,
) http.Handler {
	return http.HandlerFunc(
		func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			w.Header().Set(
				"X-Content-Type-Options",
				"nosniff",
			)

			w.Header().Set(
				"X-Frame-Options",
				"DENY",
			)

			w.Header().Set(
				"Referrer-Policy",
				"no-referrer",
			)

			w.Header().Set(
				"Cache-Control",
				"no-store",
			)

			next.ServeHTTP(w, r)
		},
	)
}

// =====================================================
// CORS CONFIGURATION
// =====================================================

// parseAllowedOrigins converts a comma-separated
// environment variable into an exact origin allowlist.
func parseAllowedOrigins(
	value string,
) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	rawOrigins := strings.Split(
		value,
		",",
	)

	origins := make(
		[]string,
		0,
		len(rawOrigins),
	)

	for _, origin := range rawOrigins {
		origin = strings.TrimSpace(origin)

		if origin == "" {
			continue
		}

		origins = append(
			origins,
			origin,
		)
	}

	return origins
}

// =====================================================
// UUID VALIDATION
// =====================================================

func validUUID(
	value string,
) bool {
	if len(value) != 36 {
		return false
	}

	for i, character := range value {
		if i == 8 ||
			i == 13 ||
			i == 18 ||
			i == 23 {

			if character != '-' {
				return false
			}

			continue
		}

		isDigit :=
			character >= '0' &&
				character <= '9'

		isLowerHex :=
			character >= 'a' &&
				character <= 'f'

		isUpperHex :=
			character >= 'A' &&
				character <= 'F'

		if !isDigit &&
			!isLowerHex &&
			!isUpperHex {
			return false
		}
	}

	return true
}

// =====================================================
// APPLICATION LOGGER
// =====================================================

// Keep structured application logging available.
//
// Never log:
//   - Resident email addresses
//   - Unsubscribe tokens
//   - Authorization headers
//   - Request bodies
//   - Sensitive registration information
func applicationLogger() *slog.Logger {
	return slog.New(
		slog.NewJSONHandler(
			os.Stdout,
			nil,
		),
	)
}
