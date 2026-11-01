package email

import (
	"context"
	"errors"
	"strings"
)

var ErrInvalidRecipient = errors.New("invalid email recipient")

// Sender defines the email capability required by the application.
// The waitlist does not depend directly on a specific email provider.
type Sender interface {
	SendWelcomeEmail(
		ctx context.Context,
		recipient string,
	) error
}

func validateRecipient(email string) error {
	if strings.TrimSpace(email) == "" {
		return ErrInvalidRecipient
	}

	return nil
}
