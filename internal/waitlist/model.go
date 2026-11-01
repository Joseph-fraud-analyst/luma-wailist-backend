package waitlist

import (
	"errors"
	"net/mail"
	"strings"
	"time"
)

var (
	ErrInvalidEmail         = errors.New("invalid email address")
	ErrRegistrationNotFound = errors.New("registration not found")
	ErrConfigNotFound       = errors.New("waitlist configuration not found")
	ErrInvalidConfig        = errors.New("invalid waitlist configuration")
)

type Registration struct {
	RegistrationID string `json:"registration_id"`
	Email          string `json:"-"`
	Position       int64  `json:"position"`

	LaunchUpdatesConsent bool `json:"-"`
	MarketingConsent     bool `json:"-"`

	ConsentRecordedAt time.Time `json:"-"`

	LaunchUpdatesUnsubscribedAt *time.Time `json:"-"`
	MarketingUnsubscribedAt     *time.Time `json:"-"`

	CreatedAt time.Time `json:"created_at"`
}

type RegistrationInput struct {
	Email string

	LaunchUpdatesConsent bool
	MarketingConsent     bool
}

type RegistrationResult struct {
	Registration    Registration
	IsExisting      bool
	TotalRegistered int64
}

type WaitlistConfig struct {
	ConfigID  string    `json:"config_id"`
	LaunchAt  time.Time `json:"launch_at"`
	Timezone  string    `json:"timezone"`
	UpdatedAt time.Time `json:"updated_at"`
	UpdatedBy *string   `json:"-"`
}

type WaitlistConfigInput struct {
	LaunchAt  time.Time
	Timezone  string
	UpdatedBy *string
}

// NormalizeEmail ensures email addresses are stored
// in a consistent lowercase, trimmed format.
func NormalizeEmail(email string) (string, error) {
	normalised := strings.ToLower(strings.TrimSpace(email))

	if normalised == "" || len(normalised) > 254 {
		return "", ErrInvalidEmail
	}

	parsed, err := mail.ParseAddress(normalised)
	if err != nil {
		return "", ErrInvalidEmail
	}

	// Reject display-name formats such as:
	// Edmond <edmond@example.com>
	if parsed.Address != normalised {
		return "", ErrInvalidEmail
	}

	return normalised, nil
}
