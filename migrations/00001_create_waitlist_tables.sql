-- +goose Up

CREATE SCHEMA IF NOT EXISTS waitlist;

CREATE TABLE waitlist.waitlist_registration (
    registration_id UUID PRIMARY KEY,
    email VARCHAR(254) NOT NULL,
    position BIGINT NOT NULL,

    launch_updates_consent BOOLEAN NOT NULL DEFAULT FALSE,
    marketing_consent BOOLEAN NOT NULL DEFAULT FALSE,

    consent_recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    launch_updates_unsubscribed_at TIMESTAMPTZ,
    marketing_unsubscribed_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_waitlist_email UNIQUE (email),
    CONSTRAINT uq_waitlist_position UNIQUE (position),

    CONSTRAINT chk_waitlist_position CHECK (position > 0),

    CONSTRAINT chk_waitlist_email
        CHECK (email = LOWER(BTRIM(email)) AND LENGTH(email) > 0)
);

CREATE TABLE waitlist.waitlist_counter (
    counter_id SMALLINT PRIMARY KEY CHECK (counter_id = 1),
    last_position BIGINT NOT NULL DEFAULT 0
        CHECK (last_position >= 0)
);

INSERT INTO waitlist.waitlist_counter (counter_id, last_position)
VALUES (1, 0);

CREATE TABLE waitlist.waitlist_config (
    config_id UUID PRIMARY KEY,
    config_key VARCHAR(30) NOT NULL UNIQUE DEFAULT 'default',
    launch_at TIMESTAMPTZ NOT NULL,
    timezone VARCHAR(100) NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by UUID,

    CONSTRAINT chk_waitlist_config_key
        CHECK (config_key = 'default'),

    CONSTRAINT chk_waitlist_timezone
        CHECK (LENGTH(BTRIM(timezone)) > 0)
);

CREATE INDEX idx_waitlist_registration_created
    ON waitlist.waitlist_registration (created_at DESC);

-- +goose Down

DROP SCHEMA IF EXISTS waitlist CASCADE;