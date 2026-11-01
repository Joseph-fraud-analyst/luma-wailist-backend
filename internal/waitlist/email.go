package waitlist

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

// EmailSender sends transactional waitlist emails.
type EmailSender interface {
	SendWelcomeEmail(
		ctx context.Context,
		recipient string,
	) error
}

// SendGridEmailSender sends email through the
// SendGrid Web API.
type SendGridEmailSender struct {
	apiKey      string
	senderEmail string
	client      *http.Client
}

// NewSendGridEmailSender creates a SendGrid email sender.
//
// The sender email must be verified in SendGrid.
func NewSendGridEmailSender(
	apiKey string,
	senderEmail string,
) (*SendGridEmailSender, error) {
	apiKey = strings.TrimSpace(apiKey)
	senderEmail = strings.TrimSpace(senderEmail)

	if apiKey == "" {
		return nil, errors.New(
			"SENDGRID_API_KEY is required",
		)
	}

	if senderEmail == "" {
		return nil, errors.New(
			"SendGrid sender email is required",
		)
	}

	return &SendGridEmailSender{
		apiKey:      apiKey,
		senderEmail: senderEmail,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}, nil
}

// SendWelcomeEmail sends the Lumaya waitlist
// confirmation email.
func (s *SendGridEmailSender) SendWelcomeEmail(
	ctx context.Context,
	recipient string,
) error {
	payload := sendGridMailRequest{
		Personalizations: []sendGridPersonalization{
			{
				To: []sendGridAddress{
					{
						Email: recipient,
					},
				},
			},
		},
		From: sendGridAddress{
			Email: s.senderEmail,
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
		return fmt.Errorf(
			"encode welcome email: %w",
			err,
		)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"https://api.sendgrid.com/v3/mail/send",
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf(
			"create SendGrid request: %w",
			err,
		)
	}

	request.Header.Set(
		"Authorization",
		"Bearer "+s.apiKey,
	)

	request.Header.Set(
		"Content-Type",
		"application/json",
	)

	response, err := s.client.Do(request)
	if err != nil {
		return fmt.Errorf(
			"send welcome email: %w",
			err,
		)
	}
	defer response.Body.Close()

	// SendGrid returns 202 Accepted when the message
	// has been accepted for processing.
	if response.StatusCode != http.StatusAccepted {
		return fmt.Errorf(
			"SendGrid returned unexpected status: %d",
			response.StatusCode,
		)
	}

	return nil
}

// =====================================================
// SENDGRID REQUEST STRUCTURES
// =====================================================

type sendGridMailRequest struct {
	Personalizations []sendGridPersonalization `json:"personalizations"`
	From             sendGridAddress           `json:"from"`
	Subject          string                    `json:"subject"`
	Content          []sendGridContent         `json:"content"`
}

type sendGridPersonalization struct {
	To []sendGridAddress `json:"to"`
}

type sendGridAddress struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

type sendGridContent struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// =====================================================
// LUMAYA WELCOME EMAIL CONTENT
// =====================================================

func welcomeEmailText() string {
	return `Welcome to the Lumaya Waitlist

Thank you for joining us.

Lumaya is being built to help you understand what's happening around you, how changing conditions may affect you, and what you can do next.

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
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
</head>
<body style="margin:0; padding:0; background-color:#f6f9fc; font-family:Arial, sans-serif; color:#1f2937;">
  <table width="100%" cellpadding="0" cellspacing="0" role="presentation">
    <tr>
      <td align="center" style="padding:40px 20px;">
        <table width="100%" cellpadding="0" cellspacing="0" role="presentation" style="max-width:600px; background-color:#ffffff; border-radius:16px;">
          <tr>
            <td style="padding:48px 40px;">
              <h1 style="margin:0 0 24px; font-size:28px; color:#1463FF;">
                Welcome to the Lumaya Waitlist
              </h1>

              <p style="font-size:16px; line-height:1.7;">
                Thank you for joining us.
              </p>

              <p style="font-size:16px; line-height:1.7;">
                Lumaya is being built to help you understand what’s happening around you,
                how changing conditions may affect you, and what you can do next.
              </p>

              <p style="font-size:16px; line-height:1.7;">
                We're grateful to have you with us from the beginning. As we prepare for
                launch, we'll keep you updated and share more about the journey ahead.
              </p>

              <p style="font-size:18px; font-weight:bold; margin-top:32px; color:#1463FF;">
                Light with purpose.
              </p>

              <p style="font-size:16px;">
                — The Lumaya Team
              </p>
            </td>
          </tr>
        </table>
      </td>
    </tr>
  </table>
</body>
</html>`
}
