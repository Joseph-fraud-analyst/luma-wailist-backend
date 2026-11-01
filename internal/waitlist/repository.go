package waitlist

import "context"

type Repository interface {
	Register(context.Context, RegistrationInput) (RegistrationResult, error)

	GetRegistrationByEmail(context.Context, string) (Registration, error)

	GetTotalRegistered(context.Context) (int64, error)

	GetConfig(context.Context) (WaitlistConfig, error)

	UpdateConfig(context.Context, WaitlistConfigInput) (WaitlistConfig, error)

	UnsubscribeLaunchUpdates(context.Context, string) (Registration, error)

	UnsubscribeMarketing(context.Context, string) (Registration, error)

	UnsubscribeLaunchUpdatesByID(context.Context, string) (Registration, error)

	UnsubscribeMarketingByID(context.Context, string) (Registration, error)
}
