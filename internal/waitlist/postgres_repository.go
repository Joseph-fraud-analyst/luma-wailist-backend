package waitlist

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	registrationTable = "waitlist.waitlist_registration"
	counterTable      = "waitlist.waitlist_counter"
	configTable       = "waitlist.waitlist_config"
	auditTable        = "waitlist.waitlist_admin_audit"
)

const registrationColumns = `
	registration_id,
	email,
	position,
	launch_updates_consent,
	marketing_consent,
	consent_recorded_at,
	launch_updates_unsubscribed_at,
	marketing_unsubscribed_at,
	created_at
`

// PostgreSQLDB is implemented by pgxpool.Pool.
type PostgreSQLDB interface {
	Begin(ctx context.Context) (pgx.Tx, error)

	QueryRow(
		ctx context.Context,
		sql string,
		args ...any,
	) pgx.Row

	Exec(
		ctx context.Context,
		sql string,
		args ...any,
	) (pgconn.CommandTag, error)
}

type PostgresRepository struct {
	db PostgreSQLDB
}

func NewPostgresRepository(db PostgreSQLDB) *PostgresRepository {
	return &PostgresRepository{
		db: db,
	}
}

// =====================================================
// UUID GENERATION
// =====================================================

// generateUUID creates an RFC 4122 version 4 UUID.
func generateUUID() (string, error) {
	var id [16]byte

	if _, err := rand.Read(id[:]); err != nil {
		return "", fmt.Errorf("generate UUID: %w", err)
	}

	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80

	encoded := hex.EncodeToString(id[:])

	return encoded[0:8] + "-" +
		encoded[8:12] + "-" +
		encoded[12:16] + "-" +
		encoded[16:20] + "-" +
		encoded[20:32], nil
}

// =====================================================
// TRANSACTION CLEANUP
// =====================================================

func rollbackTransaction(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	// A rollback after a successful commit is harmless.
	_ = tx.Rollback(ctx)
}

// =====================================================
// REGISTRATION
// =====================================================

func (r *PostgresRepository) Register(
	ctx context.Context,
	input RegistrationInput,
) (RegistrationResult, error) {

	if r.db == nil {
		return RegistrationResult{}, errors.New(
			"waitlist database is unavailable",
		)
	}

	email, err := NormalizeEmail(input.Email)
	if err != nil {
		return RegistrationResult{}, err
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return RegistrationResult{}, fmt.Errorf(
			"begin registration transaction: %w",
			err,
		)
	}

	defer rollbackTransaction(tx)

	// 1. Lock the counter row.
	//
	// This serialises new registrations. The counter update
	// and registration insert occur in the same transaction.
	var lastPosition int64

	err = tx.QueryRow(
		ctx,
		`
			SELECT last_position
			FROM `+counterTable+`
			WHERE counter_id = 1
			FOR UPDATE
		`,
	).Scan(&lastPosition)

	if err != nil {
		return RegistrationResult{}, fmt.Errorf(
			"lock waitlist counter: %w",
			err,
		)
	}

	// 2. Check whether this email is already registered.
	//
	// Existing registrations retain their original position
	// and consent preferences. Duplicate submissions do not
	// silently change previously recorded consent.
	existing, err := scanRegistration(
		tx.QueryRow(
			ctx,
			`
				SELECT `+registrationColumns+`
				FROM `+registrationTable+`
				WHERE email = $1
			`,
			email,
		),
	)

	if err == nil {
		var total int64

		err = tx.QueryRow(
			ctx,
			`
				SELECT COUNT(*)
				FROM `+registrationTable+`
			`,
		).Scan(&total)

		if err != nil {
			return RegistrationResult{}, fmt.Errorf(
				"count waitlist registrations: %w",
				err,
			)
		}

		if err := tx.Commit(ctx); err != nil {
			return RegistrationResult{}, fmt.Errorf(
				"commit duplicate registration lookup: %w",
				err,
			)
		}

		return RegistrationResult{
			Registration:    existing,
			IsExisting:      true,
			TotalRegistered: total,
		}, nil
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		return RegistrationResult{}, fmt.Errorf(
			"check existing registration: %w",
			err,
		)
	}

	// 3. Allocate the next permanent position.
	//
	// The counter update rolls back if the subsequent insert
	// or transaction commit fails.
	var nextPosition int64

	err = tx.QueryRow(
		ctx,
		`
			UPDATE `+counterTable+`
			SET last_position = last_position + 1
			WHERE counter_id = 1
			RETURNING last_position
		`,
	).Scan(&nextPosition)

	if err != nil {
		return RegistrationResult{}, fmt.Errorf(
			"allocate waitlist position: %w",
			err,
		)
	}

	// 4. Generate registration ID.
	registrationID, err := generateUUID()
	if err != nil {
		return RegistrationResult{}, err
	}

	// 5. Insert the registration.
	var registration Registration

	err = tx.QueryRow(
		ctx,
		`
			INSERT INTO `+registrationTable+` (
				registration_id,
				email,
				position,
				launch_updates_consent,
				marketing_consent,
				consent_recorded_at
			)
			VALUES ($1, $2, $3, $4, $5, NOW())
			RETURNING `+registrationColumns,
		registrationID,
		email,
		nextPosition,
		input.LaunchUpdatesConsent,
		input.MarketingConsent,
	).Scan(
		&registration.RegistrationID,
		&registration.Email,
		&registration.Position,
		&registration.LaunchUpdatesConsent,
		&registration.MarketingConsent,
		&registration.ConsentRecordedAt,
		&registration.LaunchUpdatesUnsubscribedAt,
		&registration.MarketingUnsubscribedAt,
		&registration.CreatedAt,
	)

	if err != nil {
		return RegistrationResult{}, fmt.Errorf(
			"insert waitlist registration: %w",
			err,
		)
	}

	// 6. Retrieve the genuine registration count.
	var total int64

	err = tx.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM `+registrationTable+`
		`,
	).Scan(&total)

	if err != nil {
		return RegistrationResult{}, fmt.Errorf(
			"count waitlist registrations: %w",
			err,
		)
	}

	// 7. Commit the counter and registration together.
	if err := tx.Commit(ctx); err != nil {
		return RegistrationResult{}, fmt.Errorf(
			"commit waitlist registration: %w",
			err,
		)
	}

	return RegistrationResult{
		Registration:    registration,
		IsExisting:      false,
		TotalRegistered: total,
	}, nil
}

// =====================================================
// REGISTRATION LOOKUP
// =====================================================

func (r *PostgresRepository) GetRegistrationByEmail(
	ctx context.Context,
	email string,
) (Registration, error) {

	if r.db == nil {
		return Registration{}, errors.New(
			"waitlist database is unavailable",
		)
	}

	normalised, err := NormalizeEmail(email)
	if err != nil {
		return Registration{}, err
	}

	return scanRegistration(
		r.db.QueryRow(
			ctx,
			`
				SELECT `+registrationColumns+`
				FROM `+registrationTable+`
				WHERE email = $1
			`,
			normalised,
		),
	)
}

// =====================================================
// REGISTRATION COUNT
// =====================================================

func (r *PostgresRepository) GetTotalRegistered(
	ctx context.Context,
) (int64, error) {

	if r.db == nil {
		return 0, errors.New(
			"waitlist database is unavailable",
		)
	}

	var total int64

	err := r.db.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM `+registrationTable+`
		`,
	).Scan(&total)

	if err != nil {
		return 0, fmt.Errorf(
			"get total registrations: %w",
			err,
		)
	}

	return total, nil
}

// =====================================================
// WAITLIST CONFIGURATION
// =====================================================

func (r *PostgresRepository) GetConfig(
	ctx context.Context,
) (WaitlistConfig, error) {

	if r.db == nil {
		return WaitlistConfig{}, errors.New(
			"waitlist database is unavailable",
		)
	}

	var config WaitlistConfig
	var updatedBy pgtype.Text

	err := r.db.QueryRow(
		ctx,
		`
			SELECT
				config_id,
				launch_at,
				timezone,
				updated_at,
				updated_by
			FROM `+configTable+`
			WHERE config_key = 'default'
		`,
	).Scan(
		&config.ConfigID,
		&config.LaunchAt,
		&config.Timezone,
		&config.UpdatedAt,
		&updatedBy,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return WaitlistConfig{}, ErrConfigNotFound
	}

	if err != nil {
		return WaitlistConfig{}, fmt.Errorf(
			"get waitlist configuration: %w",
			err,
		)
	}

	if updatedBy.Valid {
		value := updatedBy.String
		config.UpdatedBy = &value
	}

	return config, nil
}

// =====================================================
// UPDATE WAITLIST CONFIGURATION
// =====================================================

func (r *PostgresRepository) UpdateConfig(
	ctx context.Context,
	input WaitlistConfigInput,
) (WaitlistConfig, error) {

	if r.db == nil {
		return WaitlistConfig{}, errors.New(
			"waitlist database is unavailable",
		)
	}

	timezone := strings.TrimSpace(input.Timezone)

	if timezone == "" || input.LaunchAt.IsZero() {
		return WaitlistConfig{}, ErrInvalidConfig
	}

	if _, err := time.LoadLocation(timezone); err != nil {
		return WaitlistConfig{}, ErrInvalidConfig
	}

	if input.UpdatedBy == nil {
		return WaitlistConfig{}, ErrInvalidConfig
	}

	adminID := strings.TrimSpace(*input.UpdatedBy)

	if !validUUID(adminID) {
		return WaitlistConfig{}, ErrInvalidConfig
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return WaitlistConfig{}, fmt.Errorf(
			"begin configuration update: %w",
			err,
		)
	}

	defer rollbackTransaction(tx)

	// 1. Acquire the shared counter lock.
	//
	// The counter is a permanent singleton row. Using it here
	// serialises configuration updates, including the case
	// where the default configuration does not exist yet.
	var lastPosition int64

	err = tx.QueryRow(
		ctx,
		`
			SELECT last_position
			FROM `+counterTable+`
			WHERE counter_id = 1
			FOR UPDATE
		`,
	).Scan(&lastPosition)

	if err != nil {
		return WaitlistConfig{}, fmt.Errorf(
			"lock waitlist configuration: %w",
			err,
		)
	}

	// 2. Read the previous configuration.
	var previousLaunchAt pgtype.Timestamptz
	var previousTimezone pgtype.Text

	err = tx.QueryRow(
		ctx,
		`
			SELECT launch_at, timezone
			FROM `+configTable+`
			WHERE config_key = 'default'
			FOR UPDATE
		`,
	).Scan(
		&previousLaunchAt,
		&previousTimezone,
	)

	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return WaitlistConfig{}, fmt.Errorf(
			"read previous launch configuration: %w",
			err,
		)
	}

	// 3. Generate a new configuration UUID.
	configID, err := generateUUID()
	if err != nil {
		return WaitlistConfig{}, err
	}

	// 4. Insert or update the configuration.
	var config WaitlistConfig

	err = tx.QueryRow(
		ctx,
		`
			INSERT INTO `+configTable+` (
				config_id,
				config_key,
				launch_at,
				timezone,
				updated_at,
				updated_by
			)
			VALUES ($1, 'default', $2, $3, NOW(), $4)
			ON CONFLICT (config_key)
			DO UPDATE SET
				launch_at = EXCLUDED.launch_at,
				timezone = EXCLUDED.timezone,
				updated_at = NOW(),
				updated_by = EXCLUDED.updated_by
			RETURNING
				config_id,
				launch_at,
				timezone,
				updated_at,
				updated_by
		`,
		configID,
		input.LaunchAt,
		timezone,
		adminID,
	).Scan(
		&config.ConfigID,
		&config.LaunchAt,
		&config.Timezone,
		&config.UpdatedAt,
		&config.UpdatedBy,
	)

	if err != nil {
		return WaitlistConfig{}, fmt.Errorf(
			"update waitlist configuration: %w",
			err,
		)
	}

	// 5. Generate an independent audit ID.
	auditID, err := generateUUID()
	if err != nil {
		return WaitlistConfig{}, err
	}

	// Convert nullable previous values into SQL parameters.
	var previousLaunchValue any
	var previousTimezoneValue any

	if previousLaunchAt.Valid {
		previousLaunchValue = previousLaunchAt.Time
	}

	if previousTimezone.Valid {
		previousTimezoneValue = previousTimezone.String
	}

	// 6. Record the administrator's change.
	//
	// Configuration update and audit insertion are part of
	// the same transaction. If audit insertion fails, the
	// configuration change also rolls back.
	_, err = tx.Exec(
		ctx,
		`
			INSERT INTO `+auditTable+` (
				audit_id,
				admin_id,
				action,
				previous_launch_at,
				new_launch_at,
				previous_timezone,
				new_timezone
			)
			VALUES (
				$1,
				$2,
				'UPDATE_LAUNCH_CONFIG',
				$3,
				$4,
				$5,
				$6
			)
		`,
		auditID,
		adminID,
		previousLaunchValue,
		config.LaunchAt,
		previousTimezoneValue,
		config.Timezone,
	)

	if err != nil {
		return WaitlistConfig{}, fmt.Errorf(
			"record configuration audit: %w",
			err,
		)
	}

	// 7. Commit configuration and audit together.
	if err := tx.Commit(ctx); err != nil {
		return WaitlistConfig{}, fmt.Errorf(
			"commit configuration update: %w",
			err,
		)
	}

	return config, nil
}

// =====================================================
// SHARED REGISTRATION SCANNER
// =====================================================

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRegistration(
	row rowScanner,
) (Registration, error) {

	var registration Registration

	var launchUnsubscribedAt pgtype.Timestamptz
	var marketingUnsubscribedAt pgtype.Timestamptz

	err := row.Scan(
		&registration.RegistrationID,
		&registration.Email,
		&registration.Position,
		&registration.LaunchUpdatesConsent,
		&registration.MarketingConsent,
		&registration.ConsentRecordedAt,
		&launchUnsubscribedAt,
		&marketingUnsubscribedAt,
		&registration.CreatedAt,
	)

	if err != nil {
		return Registration{}, err
	}

	if launchUnsubscribedAt.Valid {
		value := launchUnsubscribedAt.Time
		registration.LaunchUpdatesUnsubscribedAt = &value
	}

	if marketingUnsubscribedAt.Valid {
		value := marketingUnsubscribedAt.Time
		registration.MarketingUnsubscribedAt = &value
	}

	return registration, nil
}

// =====================================================
// UNSUBSCRIBE HELPERS
// =====================================================

func (r *PostgresRepository) unsubscribeByEmail(
	ctx context.Context,
	email string,
	channel UnsubscribeChannel,
) (Registration, error) {

	if r.db == nil {
		return Registration{}, errors.New(
			"waitlist database is unavailable",
		)
	}

	normalised, err := NormalizeEmail(email)
	if err != nil {
		return Registration{}, err
	}

	var query string

	switch channel {

	case UnsubscribeLaunchUpdates:
		query = `
			UPDATE ` + registrationTable + `
			SET
				launch_updates_consent = FALSE,
				launch_updates_unsubscribed_at =
					COALESCE(launch_updates_unsubscribed_at, NOW())
			WHERE email = $1
			RETURNING ` + registrationColumns

	case UnsubscribeMarketing:
		query = `
			UPDATE ` + registrationTable + `
			SET
				marketing_consent = FALSE,
				marketing_unsubscribed_at =
					COALESCE(marketing_unsubscribed_at, NOW())
			WHERE email = $1
			RETURNING ` + registrationColumns

	default:
		return Registration{}, ErrInvalidUnsubscribeToken
	}

	registration, err := scanRegistration(
		r.db.QueryRow(ctx, query, normalised),
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return Registration{}, ErrRegistrationNotFound
	}

	if err != nil {
		return Registration{}, fmt.Errorf(
			"unsubscribe registration by email: %w",
			err,
		)
	}

	return registration, nil
}

// =====================================================
// UNSUBSCRIBE BY EMAIL
// Internal repository operations only.
// Public unsubscribe requests must use signed tokens.
// =====================================================

func (r *PostgresRepository) UnsubscribeLaunchUpdates(
	ctx context.Context,
	email string,
) (Registration, error) {

	return r.unsubscribeByEmail(
		ctx,
		email,
		UnsubscribeLaunchUpdates,
	)
}

func (r *PostgresRepository) UnsubscribeMarketing(
	ctx context.Context,
	email string,
) (Registration, error) {

	return r.unsubscribeByEmail(
		ctx,
		email,
		UnsubscribeMarketing,
	)
}

// =====================================================
// UNSUBSCRIBE BY REGISTRATION ID
// =====================================================

func (r *PostgresRepository) unsubscribeByID(
	ctx context.Context,
	registrationID string,
	channel UnsubscribeChannel,
) (Registration, error) {

	if r.db == nil {
		return Registration{}, errors.New(
			"waitlist database is unavailable",
		)
	}

	registrationID = strings.TrimSpace(registrationID)

	if !validUUID(registrationID) {
		return Registration{}, ErrInvalidUnsubscribeToken
	}

	var query string

	switch channel {

	case UnsubscribeLaunchUpdates:
		query = `
			UPDATE ` + registrationTable + `
			SET
				launch_updates_consent = FALSE,
				launch_updates_unsubscribed_at =
					COALESCE(launch_updates_unsubscribed_at, NOW())
			WHERE registration_id = $1
			RETURNING ` + registrationColumns

	case UnsubscribeMarketing:
		query = `
			UPDATE ` + registrationTable + `
			SET
				marketing_consent = FALSE,
				marketing_unsubscribed_at =
					COALESCE(marketing_unsubscribed_at, NOW())
			WHERE registration_id = $1
			RETURNING ` + registrationColumns

	default:
		return Registration{}, ErrInvalidUnsubscribeToken
	}

	registration, err := scanRegistration(
		r.db.QueryRow(
			ctx,
			query,
			registrationID,
		),
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return Registration{}, ErrRegistrationNotFound
	}

	if err != nil {
		return Registration{}, fmt.Errorf(
			"unsubscribe registration by ID: %w",
			err,
		)
	}

	return registration, nil
}

func (r *PostgresRepository) UnsubscribeLaunchUpdatesByID(
	ctx context.Context,
	registrationID string,
) (Registration, error) {

	return r.unsubscribeByID(
		ctx,
		registrationID,
		UnsubscribeLaunchUpdates,
	)
}

func (r *PostgresRepository) UnsubscribeMarketingByID(
	ctx context.Context,
	registrationID string,
) (Registration, error) {

	return r.unsubscribeByID(
		ctx,
		registrationID,
		UnsubscribeMarketing,
	)
}
