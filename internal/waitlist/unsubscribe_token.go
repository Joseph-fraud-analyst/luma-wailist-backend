package waitlist

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrInvalidUnsubscribeToken = errors.New("invalid or expired unsubscribe token")

type UnsubscribeChannel string

const (
	UnsubscribeLaunchUpdates UnsubscribeChannel = "launch_updates"
	UnsubscribeMarketing     UnsubscribeChannel = "marketing"
)

type UnsubscribeClaims struct {
	RegistrationID string             `json:"rid"`
	Channel        UnsubscribeChannel `json:"channel"`
	ExpiresAt      int64              `json:"exp"`
}

type UnsubscribeTokenManager struct {
	secret []byte
	clock  func() time.Time
}

func NewUnsubscribeTokenManager(secret string) (*UnsubscribeTokenManager, error) {
	if len(secret) < 32 {
		return nil, errors.New(
			"unsubscribe token secret must contain at least 32 characters",
		)
	}

	return &UnsubscribeTokenManager{
		secret: []byte(secret),
		clock:  time.Now,
	}, nil
}

func (m *UnsubscribeTokenManager) Generate(
	registrationID string,
	channel UnsubscribeChannel,
	expiresAt time.Time,
) (string, error) {

	if strings.TrimSpace(registrationID) == "" {
		return "", ErrInvalidUnsubscribeToken
	}

	if channel != UnsubscribeLaunchUpdates &&
		channel != UnsubscribeMarketing {
		return "", ErrInvalidUnsubscribeToken
	}

	if !expiresAt.After(m.clock()) {
		return "", ErrInvalidUnsubscribeToken
	}

	claims := UnsubscribeClaims{
		RegistrationID: registrationID,
		Channel:        channel,
		ExpiresAt:      expiresAt.Unix(),
	}

	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("encode unsubscribe claims: %w", err)
	}

	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	signature := m.sign(encodedPayload)

	return encodedPayload + "." +
		base64.RawURLEncoding.EncodeToString(signature), nil
}

func (m *UnsubscribeTokenManager) Verify(
	token string,
) (UnsubscribeClaims, error) {

	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return UnsubscribeClaims{}, ErrInvalidUnsubscribeToken
	}

	providedSignature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return UnsubscribeClaims{}, ErrInvalidUnsubscribeToken
	}

	expectedSignature := m.sign(parts[0])

	if !hmac.Equal(providedSignature, expectedSignature) {
		return UnsubscribeClaims{}, ErrInvalidUnsubscribeToken
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return UnsubscribeClaims{}, ErrInvalidUnsubscribeToken
	}

	var claims UnsubscribeClaims

	if err := json.Unmarshal(payload, &claims); err != nil {
		return UnsubscribeClaims{}, ErrInvalidUnsubscribeToken
	}

	if claims.RegistrationID == "" ||
		(claims.Channel != UnsubscribeLaunchUpdates &&
			claims.Channel != UnsubscribeMarketing) ||
		claims.ExpiresAt <= m.clock().Unix() {
		return UnsubscribeClaims{}, ErrInvalidUnsubscribeToken
	}

	return claims, nil
}

func (m *UnsubscribeTokenManager) sign(payload string) []byte {
	mac := hmac.New(sha256.New, m.secret)
	_, _ = mac.Write([]byte(payload))
	return mac.Sum(nil)
}
