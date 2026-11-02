package waitlist

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"strings"
	"time"
)

const sendGridMailEndpoint = "https://api.sendgrid.com/v3/mail/send"

// welcomeHeroContentID is the Content-ID the HTML uses to show the
// animated image: <img src="cid:lumaya-hero">.
const welcomeHeroContentID = "lumaya-hero"

// The animated image is compiled into the program and sent with every
// welcome email as an inline attachment, so no image hosting is needed.
//
//go:embed assets/welcome_hero.gif
var welcomeHeroGIF []byte

var welcomeHeroBase64 = base64.StdEncoding.EncodeToString(welcomeHeroGIF)

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

	if !strings.HasPrefix(apiKey, "SG.") {
		return nil, errors.New(
			`SENDGRID_API_KEY must be a SendGrid API key starting with "SG." (not a Twilio SID or token)`,
		)
	}

	if senderEmail == "" {
		return nil, errors.New(
			"SENDGRID_SENDER_EMAIL is required",
		)
	}

	if !isValidEmail(senderEmail) {
		return nil, errors.New(
			"SENDGRID_SENDER_EMAIL must be a valid email address",
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
	recipient = strings.TrimSpace(recipient)

	if !isValidEmail(recipient) {
		return errors.New(
			"invalid welcome email recipient",
		)
	}

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
		Subject: "You’re on the Lumaya waitlist",
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
		Attachments: []sendGridAttachment{
			{
				Content:     welcomeHeroBase64,
				Type:        "image/gif",
				Filename:    "lumaya-welcome.gif",
				Disposition: "inline",
				ContentID:   welcomeHeroContentID,
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
		sendGridMailEndpoint,
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

	// SendGrid normally returns 202 Accepted when the
	// message has been accepted for processing.
	if response.StatusCode != http.StatusAccepted {
		responseBody, _ := io.ReadAll(
			io.LimitReader(response.Body, 4096),
		)

		return fmt.Errorf(
			"SendGrid returned status %d: %s",
			response.StatusCode,
			strings.TrimSpace(string(responseBody)),
		)
	}

	return nil
}

// isValidEmail performs basic address validation.
func isValidEmail(email string) bool {
	parsed, err := mail.ParseAddress(email)

	if err != nil {
		return false
	}

	return parsed.Address == email
}

// =====================================================
// SENDGRID REQUEST STRUCTURES
// =====================================================

type sendGridMailRequest struct {
	Personalizations []sendGridPersonalization `json:"personalizations"`
	From             sendGridAddress           `json:"from"`
	Subject          string                    `json:"subject"`
	Content          []sendGridContent         `json:"content"`
	Attachments      []sendGridAttachment      `json:"attachments,omitempty"`
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

type sendGridAttachment struct {
	Content     string `json:"content"` // base64
	Type        string `json:"type"`
	Filename    string `json:"filename"`
	Disposition string `json:"disposition"`
	ContentID   string `json:"content_id"`
}

// =====================================================
// LUMAYA WELCOME EMAIL CONTENT
// =====================================================

func welcomeEmailText() string {
	return `You’re on the Lumaya waitlist

Thank you for joining the Lumaya waitlist. We’re glad to have you with us from the very beginning.

Lumaya is your personal climate assistant. It’s being built to help you understand what’s happening around you, how weather and climate conditions may affect your day, and what to do next.

What to expect:
- Real-time updates: stay informed as conditions change.
- Nearby support: find help close to you when you need it.
- Trusted services: practical guidance you can rely on.

We’ll email you as soon as Lumaya is ready.

Light with purpose.

— The Lumaya Team

You’re receiving this email because you joined the Lumaya waitlist.`
}

func welcomeEmailHTML() string {
	return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <meta name="color-scheme" content="light only">
  <title>You’re on the Lumaya waitlist</title>
</head>
<body style="margin:0; padding:0; background-color:#eef4fd;">
  <div style="display:none; max-height:0; overflow:hidden; opacity:0; color:#eef4fd;">
    You’re on the list. We’ll email you as soon as Lumaya is ready.&nbsp;&zwnj;&nbsp;&zwnj;&nbsp;&zwnj;
  </div>

  <table role="presentation" width="100%" cellpadding="0" cellspacing="0" bgcolor="#eef4fd" style="background-color:#eef4fd;">
    <tr>
      <td align="center" style="padding:32px 16px;">
        <table role="presentation" width="600" cellpadding="0" cellspacing="0" style="width:100%; max-width:600px;">

          <tr>
            <td align="center" style="padding:0 0 20px; font-family:'Helvetica Neue', Helvetica, Arial, sans-serif; font-size:30px; font-weight:700; letter-spacing:-0.5px; color:#0b1546;">
              Lumaya
            </td>
          </tr>

          <tr>
            <td bgcolor="#ffffff" style="background-color:#ffffff; border-radius:20px; padding:32px 40px 40px;">

              <img src="cid:` + welcomeHeroContentID + `" width="520" alt="The Lumaya orb surrounded by floating envelopes, sunshine and clouds"
                   style="display:block; width:100%; max-width:520px; height:auto; margin:0 auto; border:0;">

              <h1 style="margin:24px 0 16px; text-align:center; font-family:'Helvetica Neue', Helvetica, Arial, sans-serif; font-size:30px; line-height:1.2; font-weight:800; color:#0b1546;">
                You’re on the list!
              </h1>

              <p style="margin:0 0 16px; font-family:'Helvetica Neue', Helvetica, Arial, sans-serif; font-size:16px; line-height:1.7; color:#374366;">
                Thank you for joining the Lumaya waitlist. We’re glad to have you with us from the very beginning.
              </p>

              <p style="margin:0 0 24px; font-family:'Helvetica Neue', Helvetica, Arial, sans-serif; font-size:16px; line-height:1.7; color:#374366;">
                Lumaya is your personal climate assistant. It’s being built to help you understand what’s happening around you,
                how weather and climate conditions may affect your day, and what to do next.
              </p>

              <table role="presentation" width="100%" cellpadding="0" cellspacing="0" bgcolor="#f3f7ff" style="background-color:#f3f7ff; border-radius:14px;">
                <tr>
                  <td style="padding:22px 24px;">
                    <p style="margin:0 0 12px; font-family:'Helvetica Neue', Helvetica, Arial, sans-serif; font-size:13px; font-weight:700; letter-spacing:2px; text-transform:uppercase; color:#4f6fd0;">
                      What to expect
                    </p>
                    <p style="margin:0 0 10px; font-family:'Helvetica Neue', Helvetica, Arial, sans-serif; font-size:15px; line-height:1.6; color:#374366;">
                      <span style="color:#0159ff; font-weight:700;">&#9679;&nbsp;Real-time updates</span><br>
                      <span style="padding-left:18px;">Stay informed as conditions change.</span>
                    </p>
                    <p style="margin:0 0 10px; font-family:'Helvetica Neue', Helvetica, Arial, sans-serif; font-size:15px; line-height:1.6; color:#374366;">
                      <span style="color:#0159ff; font-weight:700;">&#9679;&nbsp;Nearby support</span><br>
                      <span style="padding-left:18px;">Find help close to you when you need it.</span>
                    </p>
                    <p style="margin:0; font-family:'Helvetica Neue', Helvetica, Arial, sans-serif; font-size:15px; line-height:1.6; color:#374366;">
                      <span style="color:#0159ff; font-weight:700;">&#9679;&nbsp;Trusted services</span><br>
                      <span style="padding-left:18px;">Practical guidance you can rely on.</span>
                    </p>
                  </td>
                </tr>
              </table>

              <p style="margin:24px 0 0; font-family:'Helvetica Neue', Helvetica, Arial, sans-serif; font-size:16px; line-height:1.7; color:#374366;">
                We’ll email you as soon as Lumaya is ready.
              </p>

              <p style="margin:28px 0 4px; font-family:'Helvetica Neue', Helvetica, Arial, sans-serif; font-size:18px; font-weight:700; color:#0159ff;">
                Light with purpose.
              </p>

              <p style="margin:0; font-family:'Helvetica Neue', Helvetica, Arial, sans-serif; font-size:16px; color:#374366;">
                — The Lumaya Team
              </p>
            </td>
          </tr>

          <tr>
            <td align="center" style="padding:20px 20px 0; font-family:'Helvetica Neue', Helvetica, Arial, sans-serif; font-size:12px; line-height:1.6; color:#7b88ab;">
              You’re receiving this email because you joined the Lumaya waitlist.
            </td>
          </tr>

        </table>
      </td>
    </tr>
  </table>
</body>
</html>`
}
