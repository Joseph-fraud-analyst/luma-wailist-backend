package email

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const sendGridMailEndpoint = "https://api.sendgrid.com/v3/mail/send"

var ErrEmailNotConfigured = errors.New(
	"email service is not configured",
)

type SendGridSender struct {
	apiKey     string
	from       string
	httpClient *http.Client
}

func NewSendGridSender(
	apiKey string,
	from string,
) (*SendGridSender, error) {

	apiKey = strings.TrimSpace(apiKey)
	from = strings.TrimSpace(from)

	if apiKey == "" || from == "" {
		return nil, ErrEmailNotConfigured
	}

	return &SendGridSender{
		apiKey: apiKey,
		from:   from,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}, nil
}

type sendGridRequest struct {
	Personalizations []sendGridPersonalization `json:"personalizations"`
	From             sendGridEmail             `json:"from"`
	Subject          string                    `json:"subject"`
	Content          []sendGridContent         `json:"content"`
}

type sendGridPersonalization struct {
	To []sendGridEmail `json:"to"`
}

type sendGridEmail struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

type sendGridContent struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

func (s *SendGridSender) SendWelcomeEmail(
	ctx context.Context,
	recipient string,
) error {

	if err := validateRecipient(recipient); err != nil {
		return err
	}

	payload := sendGridRequest{
		Personalizations: []sendGridPersonalization{
			{
				To: []sendGridEmail{
					{Email: recipient},
				},
			},
		},
		From: sendGridEmail{
			Email: s.from,
			Name:  "Lumaya",
		},
		Subject: "Welcome to the Lumaya Waitlist",
		Content: []sendGridContent{
			{
				Type:  "text/plain",
				Value: welcomeEmailText(),
			},
			{
				Type:  "text/html",
				Value: welcomeEmailHTML(),
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal welcome email: %w", err)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		sendGridMailEndpoint,
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("create email request: %w", err)
	}

	request.Header.Set(
		"Authorization",
		"Bearer "+s.apiKey,
	)

	request.Header.Set(
		"Content-Type",
		"application/json",
	)

	response, err := s.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("send welcome email: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK ||
		response.StatusCode >= http.StatusMultipleChoices {

		return fmt.Errorf(
			"email provider returned status %d",
			response.StatusCode,
		)
	}

	return nil
}

func welcomeEmailText() string {
	return `Welcome to the Lumaya Waitlist

Thank you for joining us.

Lumaya is being built to help you understand what’s happening around you, how changing conditions may affect you, and what you can do next.

We're grateful to have you with us from the beginning. As we prepare for launch, we'll keep you updated and share more about the journey ahead.

Light with purpose.

— The Lumaya Team`
}

func welcomeEmailHTML() string {
	return `
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
</head>
<body>
  <h1>Welcome to the Lumaya Waitlist</h1>

  <p>Thank you for joining us.</p>

  <p>
    Lumaya is being built to help you understand what’s happening
    around you, how changing conditions may affect you, and what you
    can do next.
  </p>

  <p>
    We're grateful to have you with us from the beginning. As we
    prepare for launch, we'll keep you updated and share more about
    the journey ahead.
  </p>

  <p><strong>Light with purpose.</strong></p>

  <p>— The Lumaya Team</p>
</body>
</html>`
}
