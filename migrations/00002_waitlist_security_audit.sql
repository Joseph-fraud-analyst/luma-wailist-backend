
-- +goose Up

CREATE TABLE waitlist.waitlist_admin_audit (
    audit_id UUID PRIMARY KEY,

    admin_id VARCHAR(100) NOT NULL,

    action VARCHAR(50) NOT NULL,

    previous_launch_at TIMESTAMPTZ,
    new_launch_at TIMESTAMPTZ,

    previous_timezone VARCHAR(100),
    new_timezone VARCHAR(100),

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_waitlist_admin_audit_action
        CHECK (action = 'UPDATE_LAUNCH_CONFIG')
);

CREATE INDEX idx_waitlist_admin_audit_created_at
    ON waitlist.waitlist_admin_audit (created_at DESC);


-- +goose Down

DROP TABLE IF EXISTS waitlist.waitlist_admin_audit;
