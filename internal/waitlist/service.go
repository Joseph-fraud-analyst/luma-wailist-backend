package waitlist

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// =====================================================
// LAUNCH STATUS
// =====================================================

type LaunchStatus string

const (
	LaunchStatusUpcoming LaunchStatus = "UPCOMING"
	LaunchStatusLaunched LaunchStatus = "LAUNCHED"
)

// =====================================================
// SERVICE RESPONSE MODELS
// =====================================================

type LaunchInfo struct {
	LaunchAt time.Time    `json:"launch_at"`
	Timezone string       `json:"timezone"`
	Status   LaunchStatus `json:"status"`
}

type WaitlistSummary struct {
	TotalRegistered int64 `json:"total_registered"`
}

// =====================================================
// SERVICE
// =====================================================

type Service struct {
	repository Repository
	clock      func() time.Time
}

func NewService(repository Repository) *Service {
	return &Service{
		repository: repository,
		clock:      time.Now,
	}
}

// =====================================================
// REGISTRATION
// =====================================================

func (s *Service) Register(
	ctx context.Context,
	input RegistrationInput,
) (RegistrationResult, error) {

	// Validate and normalise the email before
	// forwarding the request to the repository.
	email, err := NormalizeEmail(input.Email)
	if err != nil {
		return RegistrationResult{}, ErrInvalidEmail
	}

	input.Email = email

	result, err := s.repository.Register(ctx, input)
	if err != nil {
		return RegistrationResult{}, fmt.Errorf(
			"register waitlist resident: %w",
			err,
		)
	}

	return result, nil
}

// =====================================================
// REGISTRATION LOOKUP
// =====================================================

func (s *Service) GetRegistrationByEmail(
	ctx context.Context,
	email string,
) (Registration, error) {

	normalised, err := NormalizeEmail(email)
	if err != nil {
		return Registration{}, ErrInvalidEmail
	}

	registration, err := s.repository.GetRegistrationByEmail(
		ctx,
		normalised,
	)

	if err != nil {
		return Registration{}, fmt.Errorf(
			"retrieve waitlist registration: %w",
			err,
		)
	}

	return registration, nil
}

// =====================================================
// TOTAL REGISTRATION COUNT
// =====================================================

func (s *Service) GetSummary(
	ctx context.Context,
) (WaitlistSummary, error) {

	total, err := s.repository.GetTotalRegistered(ctx)
	if err != nil {
		return WaitlistSummary{}, fmt.Errorf(
			"retrieve waitlist summary: %w",
			err,
		)
	}

	return WaitlistSummary{
		TotalRegistered: total,
	}, nil
}

// =====================================================
// LAUNCH CONFIGURATION
// =====================================================

func (s *Service) GetLaunchInfo(
	ctx context.Context,
) (LaunchInfo, error) {

	config, err := s.repository.GetConfig(ctx)
	if err != nil {
		return LaunchInfo{}, fmt.Errorf(
			"retrieve launch configuration: %w",
			err,
		)
	}

	status := LaunchStatusUpcoming

	if !s.clock().Before(config.LaunchAt) {
		status = LaunchStatusLaunched
	}

	return LaunchInfo{
		LaunchAt: config.LaunchAt,
		Timezone: config.Timezone,
		Status:   status,
	}, nil
}

// =====================================================
// UPDATE LAUNCH CONFIGURATION
// =====================================================

func (s *Service) UpdateLaunchConfig(
	ctx context.Context,
	input WaitlistConfigInput,
) (WaitlistConfig, error) {

	input.Timezone = strings.TrimSpace(input.Timezone)

	if input.LaunchAt.IsZero() || input.Timezone == "" {
		return WaitlistConfig{}, ErrInvalidConfig
	}

	if _, err := time.LoadLocation(input.Timezone); err != nil {
		return WaitlistConfig{}, ErrInvalidConfig
	}

	config, err := s.repository.UpdateConfig(ctx, input)
	if err != nil {
		return WaitlistConfig{}, fmt.Errorf(
			"update launch configuration: %w",
			err,
		)
	}

	return config, nil
}

// =====================================================
// CONSENT
// =====================================================

// Consent choices are collected explicitly at registration.
// The repository persists the choices and their recording time.
// A duplicate registration does not silently change consent.

// =====================================================
// UNSUBSCRIBE FROM LAUNCH UPDATES
// =====================================================

func (s *Service) UnsubscribeLaunchUpdates(
	ctx context.Context,
	email string,
) (Registration, error) {

	normalised, err := NormalizeEmail(email)
	if err != nil {
		return Registration{}, ErrInvalidEmail
	}

	registration, err := s.repository.UnsubscribeLaunchUpdates(
		ctx,
		normalised,
	)

	if err != nil {
		if errors.Is(err, ErrRegistrationNotFound) {
			return Registration{}, ErrRegistrationNotFound
		}

		return Registration{}, fmt.Errorf(
			"unsubscribe from launch updates: %w",
			err,
		)
	}

	return registration, nil
}

// =====================================================
// UNSUBSCRIBE FROM MARKETING
// =====================================================

func (s *Service) UnsubscribeMarketing(
	ctx context.Context,
	email string,
) (Registration, error) {

	normalised, err := NormalizeEmail(email)
	if err != nil {
		return Registration{}, ErrInvalidEmail
	}

	registration, err := s.repository.UnsubscribeMarketing(
		ctx,
		normalised,
	)

	if err != nil {
		if errors.Is(err, ErrRegistrationNotFound) {
			return Registration{}, ErrRegistrationNotFound
		}

		return Registration{}, fmt.Errorf(
			"unsubscribe from marketing: %w",
			err,
		)
	}

	return registration, nil
}

func (s *Service) UnsubscribeLaunchUpdatesByID(
	ctx context.Context,
	registrationID string,
) (Registration, error) {

	if registrationID == "" {
		return Registration{}, ErrInvalidUnsubscribeToken
	}

	registration, err := s.repository.UnsubscribeLaunchUpdatesByID(
		ctx,
		registrationID,
	)

	if err != nil {
		if errors.Is(err, ErrRegistrationNotFound) {
			return Registration{}, ErrRegistrationNotFound
		}

		return Registration{}, fmt.Errorf(
			"unsubscribe from launch updates: %w",
			err,
		)
	}

	return registration, nil
}

func (s *Service) UnsubscribeMarketingByID(
	ctx context.Context,
	registrationID string,
) (Registration, error) {

	if registrationID == "" {
		return Registration{}, ErrInvalidUnsubscribeToken
	}

	registration, err := s.repository.UnsubscribeMarketingByID(
		ctx,
		registrationID,
	)

	if err != nil {
		if errors.Is(err, ErrRegistrationNotFound) {
			return Registration{}, ErrRegistrationNotFound
		}

		return Registration{}, fmt.Errorf(
			"unsubscribe from marketing: %w",
			err,
		)
	}

	return registration, nil
}
