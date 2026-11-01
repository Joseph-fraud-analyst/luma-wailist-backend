package waitlist

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// =====================================================
// TEST DATABASE SETUP
// =====================================================

func openIntegrationRepository(
	t *testing.T,
) (*PostgresRepository, *pgxpool.Pool) {

	t.Helper()

	databaseURL := os.Getenv("WAITLIST_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip(
			"WAITLIST_TEST_DATABASE_URL is not set; " +
				"skipping PostgreSQL integration test",
		)
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		15*time.Second,
	)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}

	var databaseName string

	if err := pool.QueryRow(
		ctx,
		"SELECT current_database()",
	).Scan(&databaseName); err != nil {
		pool.Close()
		t.Fatalf("identify test database: %v", err)
	}

	// Never run destructive integration tests against a
	// production or ordinary development database.
	if !strings.HasSuffix(databaseName, "_test") {
		pool.Close()
		t.Fatalf(
			"unsafe test database %q: database name must end in _test",
			databaseName,
		)
	}

	var migrationReady bool

	err = pool.QueryRow(
		ctx,
		`
			SELECT
				to_regclass('waitlist.waitlist_registration') IS NOT NULL
				AND to_regclass('waitlist.waitlist_counter') IS NOT NULL
				AND to_regclass('waitlist.waitlist_config') IS NOT NULL
		`,
	).Scan(&migrationReady)

	if err != nil {
		pool.Close()
		t.Fatalf("verify waitlist migration: %v", err)
	}

	if !migrationReady {
		pool.Close()
		t.Fatalf(
			"waitlist migration is not applied to %s",
			databaseName,
		)
	}

	repository := NewPostgresRepository(pool)

	resetIntegrationDatabase(t, pool)

	t.Cleanup(func() {
		pool.Close()
	})

	return repository, pool
}

// resetIntegrationDatabase is destructive.
// It must only be used with the dedicated _test database.
func resetIntegrationDatabase(
	t *testing.T,
	pool *pgxpool.Pool,
) {
	t.Helper()

	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	_, err := pool.Exec(
		ctx,
		`
			DELETE FROM waitlist.waitlist_registration;

			DELETE FROM waitlist.waitlist_config;

			UPDATE waitlist.waitlist_counter
			SET last_position = 0
			WHERE counter_id = 1;
		`,
	)

	if err != nil {
		t.Fatalf("reset integration test data: %v", err)
	}
}

// =====================================================
// SEQUENTIAL POSITION TEST
// =====================================================

func TestPostgresRegisterSequentialPositions(t *testing.T) {
	repository, _ := openIntegrationRepository(t)

	ctx := context.Background()

	first, err := repository.Register(
		ctx,
		RegistrationInput{
			Email: "first@example.com",
		},
	)

	if err != nil {
		t.Fatalf("register first resident: %v", err)
	}

	if first.Registration.Position != 1 {
		t.Fatalf(
			"expected first position 1, got %d",
			first.Registration.Position,
		)
	}

	if first.TotalRegistered != 1 {
		t.Fatalf(
			"expected total 1, got %d",
			first.TotalRegistered,
		)
	}

	second, err := repository.Register(
		ctx,
		RegistrationInput{
			Email: "second@example.com",
		},
	)

	if err != nil {
		t.Fatalf("register second resident: %v", err)
	}

	if second.Registration.Position != 2 {
		t.Fatalf(
			"expected second position 2, got %d",
			second.Registration.Position,
		)
	}

	if second.TotalRegistered != 2 {
		t.Fatalf(
			"expected total 2, got %d",
			second.TotalRegistered,
		)
	}
}

// =====================================================
// DUPLICATE EMAIL TEST
// =====================================================

func TestPostgresDuplicateEmailReturnsExistingPosition(
	t *testing.T,
) {
	repository, _ := openIntegrationRepository(t)

	ctx := context.Background()

	first, err := repository.Register(
		ctx,
		RegistrationInput{
			Email: "resident@example.com",
		},
	)

	if err != nil {
		t.Fatalf("first registration: %v", err)
	}

	second, err := repository.Register(
		ctx,
		RegistrationInput{
			Email: "resident@example.com",
		},
	)

	if err != nil {
		t.Fatalf("duplicate registration: %v", err)
	}

	if !second.IsExisting {
		t.Fatal("expected duplicate registration to be identified")
	}

	if second.Registration.Position != first.Registration.Position {
		t.Fatalf(
			"expected existing position %d, got %d",
			first.Registration.Position,
			second.Registration.Position,
		)
	}

	if second.TotalRegistered != 1 {
		t.Fatalf(
			"expected total 1, got %d",
			second.TotalRegistered,
		)
	}
}

// =====================================================
// EMAIL NORMALISATION TEST
// =====================================================

func TestPostgresEmailNormalisation(t *testing.T) {
	repository, _ := openIntegrationRepository(t)

	ctx := context.Background()

	result, err := repository.Register(
		ctx,
		RegistrationInput{
			Email: "  RESIDENT@EXAMPLE.COM  ",
		},
	)

	if err != nil {
		t.Fatalf("register normalised email: %v", err)
	}

	if result.Registration.Email != "resident@example.com" {
		t.Fatalf(
			"expected normalised email resident@example.com, got %q",
			result.Registration.Email,
		)
	}

	duplicate, err := repository.Register(
		ctx,
		RegistrationInput{
			Email: "resident@example.com",
		},
	)

	if err != nil {
		t.Fatalf("register normalised duplicate: %v", err)
	}

	if !duplicate.IsExisting {
		t.Fatal("expected normalised email to match existing registration")
	}

	if duplicate.Registration.Position != result.Registration.Position {
		t.Fatal("normalised duplicate received a different position")
	}
}

// =====================================================
// INVALID EMAIL TEST
// =====================================================

func TestPostgresRejectsInvalidEmail(t *testing.T) {
	repository, _ := openIntegrationRepository(t)

	_, err := repository.Register(
		context.Background(),
		RegistrationInput{
			Email: "not-an-email",
		},
	)

	if !errors.Is(err, ErrInvalidEmail) {
		t.Fatalf(
			"expected ErrInvalidEmail, got %v",
			err,
		)
	}

	total, err := repository.GetTotalRegistered(context.Background())
	if err != nil {
		t.Fatalf("get total registrations: %v", err)
	}

	if total != 0 {
		t.Fatalf(
			"invalid email must not create a registration; got total %d",
			total,
		)
	}
}

// =====================================================
// TRANSACTION ROLLBACK TEST
// =====================================================

func TestPostgresRegistrationFailureRollsBackCounter(
	t *testing.T,
) {
	repository, pool := openIntegrationRepository(t)

	ctx := context.Background()

	// Install a temporary test-only trigger that forces
	// an insert failure for one specific email.
	_, err := pool.Exec(
		ctx,
		`
			CREATE OR REPLACE FUNCTION
				waitlist.reject_test_registration()
			RETURNS TRIGGER
			LANGUAGE plpgsql
			AS $$
			BEGIN
				IF NEW.email = 'rollback@example.com' THEN
					RAISE EXCEPTION 'forced integration test failure';
				END IF;

				RETURN NEW;
			END;
			$$;
		`,
	)

	if err != nil {
		t.Fatalf("create test trigger function: %v", err)
	}

	_, err = pool.Exec(
		ctx,
		`
			CREATE TRIGGER test_reject_registration
			BEFORE INSERT ON waitlist.waitlist_registration
			FOR EACH ROW
			EXECUTE FUNCTION waitlist.reject_test_registration();
		`,
	)

	if err != nil {
		_, _ = pool.Exec(
			ctx,
			`DROP FUNCTION IF EXISTS waitlist.reject_test_registration() CASCADE`,
		)
		t.Fatalf("create test trigger: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer cancel()

		_, _ = pool.Exec(
			cleanupCtx,
			`
				DROP TRIGGER IF EXISTS test_reject_registration
				ON waitlist.waitlist_registration;

				DROP FUNCTION IF EXISTS
					waitlist.reject_test_registration();
			`,
		)
	})

	// This registration increments the counter but its
	// insert is deliberately rejected by PostgreSQL.
	_, err = repository.Register(
		ctx,
		RegistrationInput{
			Email: "rollback@example.com",
		},
	)

	if err == nil {
		t.Fatal("expected forced registration failure")
	}

	var lastPosition int64

	err = pool.QueryRow(
		ctx,
		`
			SELECT last_position
			FROM waitlist.waitlist_counter
			WHERE counter_id = 1
		`,
	).Scan(&lastPosition)

	if err != nil {
		t.Fatalf("read counter after rollback: %v", err)
	}

	if lastPosition != 0 {
		t.Fatalf(
			"expected counter to roll back to 0, got %d",
			lastPosition,
		)
	}

	total, err := repository.GetTotalRegistered(ctx)
	if err != nil {
		t.Fatalf("get total after rollback: %v", err)
	}

	if total != 0 {
		t.Fatalf(
			"expected zero registrations after rollback, got %d",
			total,
		)
	}

	// Remove the test trigger so a valid registration
	// can prove that the next successful position is 1.
	_, err = pool.Exec(
		ctx,
		`
			DROP TRIGGER IF EXISTS test_reject_registration
			ON waitlist.waitlist_registration;

			DROP FUNCTION IF EXISTS
				waitlist.reject_test_registration();
		`,
	)

	if err != nil {
		t.Fatalf("remove test trigger: %v", err)
	}

	result, err := repository.Register(
		ctx,
		RegistrationInput{
			Email: "successful@example.com",
		},
	)

	if err != nil {
		t.Fatalf("register after rollback: %v", err)
	}

	if result.Registration.Position != 1 {
		t.Fatalf(
			"expected first successful registration position 1, got %d",
			result.Registration.Position,
		)
	}
}

// =====================================================
// CONCURRENT REGISTRATION TEST
// =====================================================

func TestPostgresConcurrentRegistrationsRemainSequential(
	t *testing.T,
) {
	repository, _ := openIntegrationRepository(t)

	const registrationCount = 50

	ctx, cancel := context.WithTimeout(
		context.Background(),
		60*time.Second,
	)
	defer cancel()

	type registrationResult struct {
		position int64
		err      error
	}

	results := make(chan registrationResult, registrationCount)

	var wg sync.WaitGroup

	for i := 0; i < registrationCount; i++ {
		wg.Add(1)

		go func(index int) {
			defer wg.Done()

			email := fmt.Sprintf(
				"resident-%d@example.com",
				index,
			)

			result, err := repository.Register(
				ctx,
				RegistrationInput{
					Email: email,
				},
			)

			if err != nil {
				results <- registrationResult{err: err}
				return
			}

			results <- registrationResult{
				position: result.Registration.Position,
			}
		}(i)
	}

	wg.Wait()
	close(results)

	positions := make([]int64, 0, registrationCount)

	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent registration failed: %v", result.err)
		}

		positions = append(positions, result.position)
	}

	if len(positions) != registrationCount {
		t.Fatalf(
			"expected %d registrations, got %d",
			registrationCount,
			len(positions),
		)
	}

	sort.Slice(positions, func(i, j int) bool {
		return positions[i] < positions[j]
	})

	for i, position := range positions {
		expected := int64(i + 1)

		if position != expected {
			t.Fatalf(
				"expected sequential position %d, got %d",
				expected,
				position,
			)
		}
	}

	total, err := repository.GetTotalRegistered(ctx)
	if err != nil {
		t.Fatalf("get total registrations: %v", err)
	}

	if total != registrationCount {
		t.Fatalf(
			"expected total %d, got %d",
			registrationCount,
			total,
		)
	}
}

// =====================================================
// CONCURRENT DUPLICATE EMAIL TEST
// =====================================================

func TestPostgresConcurrentDuplicateRegistrations(
	t *testing.T,
) {
	repository, _ := openIntegrationRepository(t)

	const requestCount = 20

	ctx, cancel := context.WithTimeout(
		context.Background(),
		60*time.Second,
	)
	defer cancel()

	type registrationResult struct {
		position int64
		existing bool
		err      error
	}

	results := make(chan registrationResult, requestCount)

	var wg sync.WaitGroup

	for i := 0; i < requestCount; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			result, err := repository.Register(
				ctx,
				RegistrationInput{
					Email: "same-resident@example.com",
				},
			)

			if err != nil {
				results <- registrationResult{err: err}
				return
			}

			results <- registrationResult{
				position: result.Registration.Position,
				existing: result.IsExisting,
			}
		}()
	}

	wg.Wait()
	close(results)

	var allocatedPosition int64

	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent duplicate registration: %v", result.err)
		}

		if allocatedPosition == 0 {
			allocatedPosition = result.position
		}

		if result.position != allocatedPosition {
			t.Fatalf(
				"duplicate email received positions %d and %d",
				allocatedPosition,
				result.position,
			)
		}
	}

	total, err := repository.GetTotalRegistered(ctx)
	if err != nil {
		t.Fatalf("get total registrations: %v", err)
	}

	if total != 1 {
		t.Fatalf(
			"expected exactly one registration, got %d",
			total,
		)
	}

	if allocatedPosition != 1 {
		t.Fatalf(
			"expected duplicate requests to share position 1, got %d",
			allocatedPosition,
		)
	}
}

// =====================================================
// TOTAL REGISTRATION COUNT TEST
// =====================================================

func TestPostgresTotalRegistrationCount(t *testing.T) {
	repository, _ := openIntegrationRepository(t)

	ctx := context.Background()

	for i := 1; i <= 5; i++ {
		_, err := repository.Register(
			ctx,
			RegistrationInput{
				Email: fmt.Sprintf("count-%d@example.com", i),
			},
		)

		if err != nil {
			t.Fatalf("registration %d failed: %v", i, err)
		}
	}

	total, err := repository.GetTotalRegistered(ctx)
	if err != nil {
		t.Fatalf("get total registrations: %v", err)
	}

	if total != 5 {
		t.Fatalf("expected 5 registrations, got %d", total)
	}
}

// =====================================================
// CONFIGURATION TEST
// =====================================================

func TestPostgresWaitlistConfiguration(t *testing.T) {
	repository, _ := openIntegrationRepository(t)

	ctx := context.Background()

	_, err := repository.GetConfig(ctx)

	if !errors.Is(err, ErrConfigNotFound) {
		t.Fatalf(
			"expected ErrConfigNotFound before configuration is created, got %v",
			err,
		)
	}

	launchAt := time.Date(
		2027,
		time.January,
		11,
		12,
		0,
		0,
		0,
		time.UTC,
	)

	config, err := repository.UpdateConfig(
		ctx,
		WaitlistConfigInput{
			LaunchAt: launchAt,
			Timezone: "Europe/London",
		},
	)

	if err != nil {
		t.Fatalf("update waitlist configuration: %v", err)
	}

	if !config.LaunchAt.Equal(launchAt) {
		t.Fatalf(
			"expected launch time %v, got %v",
			launchAt,
			config.LaunchAt,
		)
	}

	if config.Timezone != "Europe/London" {
		t.Fatalf(
			"expected timezone Europe/London, got %s",
			config.Timezone,
		)
	}

	retrieved, err := repository.GetConfig(ctx)
	if err != nil {
		t.Fatalf("retrieve waitlist configuration: %v", err)
	}

	if retrieved.ConfigID != config.ConfigID {
		t.Fatal("configuration ID changed unexpectedly")
	}

	if !retrieved.LaunchAt.Equal(launchAt) {
		t.Fatal("retrieved launch time does not match saved launch time")
	}

	if retrieved.Timezone != "Europe/London" {
		t.Fatal("retrieved timezone does not match saved timezone")
	}
}

// =====================================================
// CONFIGURATION UPDATE TEST
// =====================================================

func TestPostgresWaitlistConfigurationUpdate(t *testing.T) {
	repository, _ := openIntegrationRepository(t)

	ctx := context.Background()

	firstLaunch := time.Date(
		2027,
		time.January,
		11,
		12,
		0,
		0,
		0,
		time.UTC,
	)

	first, err := repository.UpdateConfig(
		ctx,
		WaitlistConfigInput{
			LaunchAt: firstLaunch,
			Timezone: "Europe/London",
		},
	)

	if err != nil {
		t.Fatalf("create initial configuration: %v", err)
	}

	secondLaunch := time.Date(
		2027,
		time.February,
		1,
		9,
		0,
		0,
		0,
		time.UTC,
	)

	second, err := repository.UpdateConfig(
		ctx,
		WaitlistConfigInput{
			LaunchAt: secondLaunch,
			Timezone: "Africa/Lagos",
		},
	)

	if err != nil {
		t.Fatalf("update configuration: %v", err)
	}

	if first.ConfigID != second.ConfigID {
		t.Fatal("configuration upsert created a second configuration")
	}

	if !second.LaunchAt.Equal(secondLaunch) {
		t.Fatal("configuration launch date was not updated")
	}

	if second.Timezone != "Africa/Lagos" {
		t.Fatalf(
			"expected Africa/Lagos, got %s",
			second.Timezone,
		)
	}
}

// =====================================================
// INVALID CONFIGURATION TEST
// =====================================================

func TestPostgresRejectsInvalidConfiguration(t *testing.T) {
	repository, _ := openIntegrationRepository(t)

	tests := []struct {
		name  string
		input WaitlistConfigInput
	}{
		{
			name: "missing launch date",
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
			_, err := repository.UpdateConfig(
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
