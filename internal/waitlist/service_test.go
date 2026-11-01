package waitlist

import (
	"context"
	"errors"
	"testing"
	"time"
)

// =====================================================
// FAKE REPOSITORY
// =====================================================

type fakeWaitlistRepository struct {
	registerResult RegistrationResult
	registerError  error

	registration Registration
	lookupError  error

	totalCount int64
	countError error

	config      WaitlistConfig
	configError error

	updatedConfig WaitlistConfig
	updateError   error

	unsubscribedLaunch Registration
	unsubscribedMarket Registration

	unsubscribeLaunchError error
	unsubscribeMarketError error

	registerCalls int
}

func (f *fakeWaitlistRepository) Register(
	ctx context.Context,
	input RegistrationInput,
) (RegistrationResult, error) {
	f.registerCalls++

	if f.registerError != nil {
		return RegistrationResult{}, f.registerError
	}

	return f.registerResult, nil
}

func (f *fakeWaitlistRepository) GetRegistrationByEmail(
	ctx context.Context,
	email string,
) (Registration, error) {
	if f.lookupError != nil {
		return Registration{}, f.lookupError
	}

	return f.registration, nil
}

func (f *fakeWaitlistRepository) GetTotalRegistered(
	ctx context.Context,
) (int64, error) {
	return f.totalCount, f.countError
}

func (f *fakeWaitlistRepository) GetConfig(
	ctx context.Context,
) (WaitlistConfig, error) {
	return f.config, f.configError
}

func (f *fakeWaitlistRepository) UpdateConfig(
	ctx context.Context,
	input WaitlistConfigInput,
) (WaitlistConfig, error) {
	if f.updateError != nil {
		return WaitlistConfig{}, f.updateError
	}

	return f.updatedConfig, nil
}

func (f *fakeWaitlistRepository) UnsubscribeLaunchUpdates(
	ctx context.Context,
	email string,
) (Registration, error) {
	if f.unsubscribeLaunchError != nil {
		return Registration{}, f.unsubscribeLaunchError
	}

	return f.unsubscribedLaunch, nil
}

func (f *fakeWaitlistRepository) UnsubscribeMarketing(
	ctx context.Context,
	email string,
) (Registration, error) {
	if f.unsubscribeMarketError != nil {
		return Registration{}, f.unsubscribeMarketError
	}

	return f.unsubscribedMarket, nil
}

// =====================================================
// REGISTRATION TESTS
// =====================================================

func TestServiceRegisterNormalisesEmail(t *testing.T) {
	repository := &fakeWaitlistRepository{
		registerResult: RegistrationResult{
			Registration: Registration{
				Email:    "resident@example.com",
				Position: 1,
			},
			TotalRegistered: 1,
		},
	}

	service := NewService(repository)

	result, err := service.Register(
		context.Background(),
		RegistrationInput{
			Email: " RESIDENT@EXAMPLE.COM ",
		},
	)

	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	if result.Registration.Email != "resident@example.com" {
		t.Fatalf(
			"expected normalised email, got %q",
			result.Registration.Email,
		)
	}

	if repository.registerCalls != 1 {
		t.Fatalf(
			"expected one repository call, got %d",
			repository.registerCalls,
		)
	}
}

func TestServiceRegisterRejectsInvalidEmail(t *testing.T) {
	repository := &fakeWaitlistRepository{}
	service := NewService(repository)

	_, err := service.Register(
		context.Background(),
		RegistrationInput{
			Email: "invalid-email",
		},
	)

	if !errors.Is(err, ErrInvalidEmail) {
		t.Fatalf(
			"expected ErrInvalidEmail, got %v",
			err,
		)
	}

	if repository.registerCalls != 0 {
		t.Fatal("repository must not be called for invalid email")
	}
}

func TestServiceRegisterReturnsExistingPosition(t *testing.T) {
	repository := &fakeWaitlistRepository{
		registerResult: RegistrationResult{
			Registration: Registration{
				Email:    "resident@example.com",
				Position: 12,
			},
			IsExisting:      true,
			TotalRegistered: 12,
		},
	}

	service := NewService(repository)

	result, err := service.Register(
		context.Background(),
		RegistrationInput{
			Email: "resident@example.com",
		},
	)

	if err != nil {
		t.Fatalf("duplicate registration failed: %v", err)
	}

	if !result.IsExisting {
		t.Fatal("expected existing registration")
	}

	if result.Registration.Position != 12 {
		t.Fatalf(
			"expected existing position 12, got %d",
			result.Registration.Position,
		)
	}
}

// =====================================================
// REGISTRATION LOOKUP TESTS
// =====================================================

func TestServiceGetRegistrationRejectsInvalidEmail(t *testing.T) {
	repository := &fakeWaitlistRepository{}
	service := NewService(repository)

	_, err := service.GetRegistrationByEmail(
		context.Background(),
		"invalid",
	)

	if !errors.Is(err, ErrInvalidEmail) {
		t.Fatalf("expected ErrInvalidEmail, got %v", err)
	}
}

// =====================================================
// SUMMARY TESTS
// =====================================================

func TestServiceGetSummary(t *testing.T) {
	repository := &fakeWaitlistRepository{
		totalCount: 250,
	}

	service := NewService(repository)

	summary, err := service.GetSummary(context.Background())
	if err != nil {
		t.Fatalf("get summary: %v", err)
	}

	if summary.TotalRegistered != 250 {
		t.Fatalf(
			"expected 250 registrations, got %d",
			summary.TotalRegistered,
		)
	}
}

// =====================================================
// LAUNCH STATUS TESTS
// =====================================================

func TestServiceLaunchStatusUpcoming(t *testing.T) {
	launchAt := time.Date(
		2027, time.January, 11,
		12, 0, 0, 0, time.UTC,
	)

	repository := &fakeWaitlistRepository{
		config: WaitlistConfig{
			LaunchAt: launchAt,
			Timezone: "Europe/London",
		},
	}

	service := NewService(repository)

	service.clock = func() time.Time {
		return time.Date(
			2027, time.January, 10,
			12, 0, 0, 0, time.UTC,
		)
	}

	info, err := service.GetLaunchInfo(context.Background())
	if err != nil {
		t.Fatalf("get launch information: %v", err)
	}

	if info.Status != LaunchStatusUpcoming {
		t.Fatalf(
			"expected UPCOMING, got %s",
			info.Status,
		)
	}
}

func TestServiceLaunchStatusLaunched(t *testing.T) {
	launchAt := time.Date(
		2027, time.January, 11,
		12, 0, 0, 0, time.UTC,
	)

	repository := &fakeWaitlistRepository{
		config: WaitlistConfig{
			LaunchAt: launchAt,
			Timezone: "Europe/London",
		},
	}

	service := NewService(repository)

	// At the exact launch timestamp, status is LAUNCHED.
	service.clock = func() time.Time {
		return launchAt
	}

	info, err := service.GetLaunchInfo(context.Background())
	if err != nil {
		t.Fatalf("get launch information: %v", err)
	}

	if info.Status != LaunchStatusLaunched {
		t.Fatalf(
			"expected LAUNCHED, got %s",
			info.Status,
		)
	}
}

// =====================================================
// LAUNCH CONFIGURATION TESTS
// =====================================================

func TestServiceRejectsInvalidLaunchConfiguration(t *testing.T) {
	repository := &fakeWaitlistRepository{}
	service := NewService(repository)

	tests := []struct {
		name  string
		input WaitlistConfigInput
	}{
		{
			name: "missing launch time",
			input: WaitlistConfigInput{
				Timezone: "Europe/London",
			},
		},
		{
			name: "missing timezone",
			input: WaitlistConfigInput{
				LaunchAt: time.Date(
					2027, time.January, 11,
					12, 0, 0, 0, time.UTC,
				),
			},
		},
		{
			name: "invalid timezone",
			input: WaitlistConfigInput{
				LaunchAt: time.Date(
					2027, time.January, 11,
					12, 0, 0, 0, time.UTC,
				),
				Timezone: "Invalid/Timezone",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := service.UpdateLaunchConfig(
				context.Background(),
				test.input,
			)

			if !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf(
					"expected ErrInvalidConfig, got %v",
					err,
				)
			}
		})
	}
}

func TestServiceUpdateLaunchConfiguration(t *testing.T) {
	launchAt := time.Date(
		2027, time.January, 11,
		12, 0, 0, 0, time.UTC,
	)

	repository := &fakeWaitlistRepository{
		updatedConfig: WaitlistConfig{
			LaunchAt: launchAt,
			Timezone: "Europe/London",
		},
	}

	service := NewService(repository)

	config, err := service.UpdateLaunchConfig(
		context.Background(),
		WaitlistConfigInput{
			LaunchAt: launchAt,
			Timezone: " Europe/London ",
		},
	)

	if err != nil {
		t.Fatalf("update launch configuration: %v", err)
	}

	if config.Timezone != "Europe/London" {
		t.Fatalf("expected trimmed timezone, got %q", config.Timezone)
	}
}

// =====================================================
// UNSUBSCRIBE TESTS
// =====================================================

func TestServiceUnsubscribeLaunchUpdates(t *testing.T) {
	repository := &fakeWaitlistRepository{
		unsubscribedLaunch: Registration{
			Email:                       "resident@example.com",
			Position:                    5,
			LaunchUpdatesConsent:        false,
			MarketingConsent:            true,
			LaunchUpdatesUnsubscribedAt: timePointer(time.Now()),
		},
	}

	service := NewService(repository)

	result, err := service.UnsubscribeLaunchUpdates(
		context.Background(),
		" RESIDENT@EXAMPLE.COM ",
	)

	if err != nil {
		t.Fatalf("unsubscribe from launch updates: %v", err)
	}

	if result.LaunchUpdatesConsent {
		t.Fatal("launch updates consent should be false")
	}

	if !result.MarketingConsent {
		t.Fatal("marketing consent should remain unchanged")
	}

	if result.LaunchUpdatesUnsubscribedAt == nil {
		t.Fatal("unsubscribe timestamp should be recorded")
	}
}

func TestServiceUnsubscribeMarketing(t *testing.T) {
	repository := &fakeWaitlistRepository{
		unsubscribedMarket: Registration{
			Email:                   "resident@example.com",
			Position:                5,
			LaunchUpdatesConsent:    true,
			MarketingConsent:        false,
			MarketingUnsubscribedAt: timePointer(time.Now()),
		},
	}

	service := NewService(repository)

	result, err := service.UnsubscribeMarketing(
		context.Background(),
		"resident@example.com",
	)

	if err != nil {
		t.Fatalf("unsubscribe from marketing: %v", err)
	}

	if result.MarketingConsent {
		t.Fatal("marketing consent should be false")
	}

	if !result.LaunchUpdatesConsent {
		t.Fatal("launch updates consent should remain unchanged")
	}

	if result.MarketingUnsubscribedAt == nil {
		t.Fatal("unsubscribe timestamp should be recorded")
	}
}

func TestServiceUnsubscribeRejectsInvalidEmail(t *testing.T) {
	repository := &fakeWaitlistRepository{}
	service := NewService(repository)

	_, err := service.UnsubscribeMarketing(
		context.Background(),
		"invalid-email",
	)

	if !errors.Is(err, ErrInvalidEmail) {
		t.Fatalf("expected ErrInvalidEmail, got %v", err)
	}
}

// =====================================================
// TEST HELPER
// =====================================================

func timePointer(value time.Time) *time.Time {
	return &value
}

func (f *fakeWaitlistRepository) UnsubscribeLaunchUpdatesByID(
	ctx context.Context,
	registrationID string,
) (Registration, error) {
	return f.unsubscribedLaunch, f.unsubscribeLaunchError
}

func (f *fakeWaitlistRepository) UnsubscribeMarketingByID(
	ctx context.Context,
	registrationID string,
) (Registration, error) {
	return f.unsubscribedMarket, f.unsubscribeMarketError
}
