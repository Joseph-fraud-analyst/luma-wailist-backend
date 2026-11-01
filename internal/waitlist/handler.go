package waitlist

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

type Handler struct {
	service     *Service
	tokens      *UnsubscribeTokenManager
	emailSender EmailSender
}

func NewHandler(
	service *Service,
	tokens *UnsubscribeTokenManager,
	emailSender EmailSender,
) *Handler {
	return &Handler{
		service:     service,
		tokens:      tokens,
		emailSender: emailSender,
	}
}

// =====================================================
// REQUEST / RESPONSE STRUCTURES
// =====================================================

type registerRequest struct {
	Email string `json:"email"`

	LaunchUpdatesConsent bool `json:"launch_updates_consent"`
	MarketingConsent     bool `json:"marketing_consent"`
}

type registerResponse struct {
	Message string `json:"message"`

	Position        int64 `json:"position"`
	TotalRegistered int64 `json:"total_registered"`

	AlreadyRegistered bool `json:"already_registered"`
}

type countResponse struct {
	TotalRegistered int64 `json:"total_registered"`
}

type configResponse struct {
	LaunchAt time.Time    `json:"launch_at"`
	Timezone string       `json:"timezone"`
	Status   LaunchStatus `json:"status"`
}

type updateConfigRequest struct {
	LaunchAt time.Time `json:"launch_at"`
	Timezone string    `json:"timezone"`
}

type errorResponse struct {
	Error string `json:"error"`
}

type unsubscribeResponse struct {
	Message string `json:"message"`
}

// =====================================================
// JSON HELPERS
// =====================================================

func writeJSON(
	w http.ResponseWriter,
	status int,
	payload any,
) {
	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(
	w http.ResponseWriter,
	status int,
	message string,
) {
	writeJSON(
		w,
		status,
		errorResponse{
			Error: message,
		},
	)
}

func decodeJSON(
	w http.ResponseWriter,
	r *http.Request,
	destination any,
) error {
	r.Body = http.MaxBytesReader(
		w,
		r.Body,
		16*1024,
	)

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(destination); err != nil {
		return err
	}

	var extra any

	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New(
			"request must contain exactly one JSON object",
		)
	}

	return nil
}

// =====================================================
// PUBLIC REGISTRATION
// =====================================================
func (h *Handler) Register(
	w http.ResponseWriter,
	r *http.Request,
) {
	var request registerRequest

	if err := decodeJSON(
		w,
		r,
		&request,
	); err != nil {
		writeError(
			w,
			http.StatusBadRequest,
			"Invalid request body.",
		)
		return
	}

	result, err := h.service.Register(
		r.Context(),
		RegistrationInput{
			Email: request.Email,

			LaunchUpdatesConsent: request.LaunchUpdatesConsent,

			MarketingConsent: request.MarketingConsent,
		},
	)

	if err != nil {
		if errors.Is(err, ErrInvalidEmail) {
			writeError(
				w,
				http.StatusUnprocessableEntity,
				"Please provide a valid email address.",
			)
			return
		}

		writeError(
			w,
			http.StatusInternalServerError,
			"Registration could not be completed.",
		)
		return
	}

	status := http.StatusCreated

	message :=
		"Your waitlist registration was successful."

	if result.IsExisting {
		status = http.StatusOK

		message =
			"This email is already registered."
	}

	// Send the welcome email only for a genuinely
	// new registration. A duplicate registration
	// must not trigger another welcome email.
	//
	// Email delivery failure does not undo a successful
	// database registration.
	if !result.IsExisting &&
		h.emailSender != nil {

		_ = h.emailSender.SendWelcomeEmail(
			r.Context(),
			result.Registration.Email,
		)
	}

	writeJSON(
		w,
		status,
		registerResponse{
			Message: message,

			Position: result.Registration.Position,

			TotalRegistered: result.TotalRegistered,

			AlreadyRegistered: result.IsExisting,
		},
	)
}

// =====================================================
// PUBLIC REGISTRATION COUNT
// =====================================================

func (h *Handler) Count(
	w http.ResponseWriter,
	r *http.Request,
) {
	summary, err := h.service.GetSummary(
		r.Context(),
	)

	if err != nil {
		writeError(
			w,
			http.StatusInternalServerError,
			"Unable to retrieve waitlist count.",
		)
		return
	}

	writeJSON(
		w,
		http.StatusOK,
		countResponse{
			TotalRegistered: summary.TotalRegistered,
		},
	)
}

// =====================================================
// PUBLIC LAUNCH CONFIGURATION
// =====================================================

func (h *Handler) GetConfig(
	w http.ResponseWriter,
	r *http.Request,
) {
	info, err := h.service.GetLaunchInfo(
		r.Context(),
	)

	if err != nil {
		if errors.Is(err, ErrConfigNotFound) {
			writeError(
				w,
				http.StatusServiceUnavailable,
				"Launch information is not available yet.",
			)
			return
		}

		writeError(
			w,
			http.StatusInternalServerError,
			"Unable to retrieve launch information.",
		)
		return
	}

	writeJSON(
		w,
		http.StatusOK,
		configResponse{
			LaunchAt: info.LaunchAt,
			Timezone: info.Timezone,
			Status:   info.Status,
		},
	)
}

// =====================================================
// ADMIN LAUNCH CONFIGURATION
// =====================================================

func (h *Handler) UpdateConfig(
	w http.ResponseWriter,
	r *http.Request,
) {
	var request updateConfigRequest

	if err := decodeJSON(
		w,
		r,
		&request,
	); err != nil {
		writeError(
			w,
			http.StatusBadRequest,
			"Invalid request body.",
		)
		return
	}

	// Administrator identity is supplied by the
	// authentication middleware, never by the client.
	adminID, _ := r.Context().Value(
		adminIDContextKey{},
	).(string)

	if strings.TrimSpace(adminID) == "" {
		writeError(
			w,
			http.StatusUnauthorized,
			"Administrator authentication required.",
		)
		return
	}

	config, err := h.service.UpdateLaunchConfig(
		r.Context(),
		WaitlistConfigInput{
			LaunchAt:  request.LaunchAt,
			Timezone:  request.Timezone,
			UpdatedBy: &adminID,
		},
	)

	if err != nil {
		if errors.Is(err, ErrInvalidConfig) {
			writeError(
				w,
				http.StatusUnprocessableEntity,
				"Invalid launch date or timezone.",
			)
			return
		}

		writeError(
			w,
			http.StatusInternalServerError,
			"Unable to update launch configuration.",
		)
		return
	}

	writeJSON(
		w,
		http.StatusOK,
		configResponse{
			LaunchAt: config.LaunchAt,
			Timezone: config.Timezone,
			Status:   LaunchStatusUpcoming,
		},
	)
}

// =====================================================
// SECURE UNSUBSCRIBE
// =====================================================

func (h *Handler) Unsubscribe(
	w http.ResponseWriter,
	r *http.Request,
) {
	if h.tokens == nil {
		writeError(
			w,
			http.StatusServiceUnavailable,
			"Unsubscribe service is unavailable.",
		)
		return
	}

	token := strings.TrimSpace(
		r.URL.Query().Get("token"),
	)

	if token == "" {
		writeError(
			w,
			http.StatusBadRequest,
			"Unsubscribe token is required.",
		)
		return
	}

	claims, err := h.tokens.Verify(token)

	if err != nil {
		writeError(
			w,
			http.StatusBadRequest,
			"Invalid or expired unsubscribe link.",
		)
		return
	}

	switch claims.Channel {
	case UnsubscribeLaunchUpdates:
		_, err = h.service.UnsubscribeLaunchUpdatesByID(
			r.Context(),
			claims.RegistrationID,
		)

	case UnsubscribeMarketing:
		_, err = h.service.UnsubscribeMarketingByID(
			r.Context(),
			claims.RegistrationID,
		)

	default:
		writeError(
			w,
			http.StatusBadRequest,
			"Invalid unsubscribe request.",
		)
		return
	}

	if err != nil {
		if errors.Is(
			err,
			ErrRegistrationNotFound,
		) {
			writeError(
				w,
				http.StatusNotFound,
				"Registration not found.",
			)
			return
		}

		writeError(
			w,
			http.StatusInternalServerError,
			"Unable to process unsubscribe request.",
		)
		return
	}

	writeJSON(
		w,
		http.StatusOK,
		unsubscribeResponse{
			Message: "Your communication preference has been updated.",
		},
	)
}
