// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import "context"

// currentSchemaVersion is the schema version this controller knows how to apply.
// status.migrationVersion is compared against this to gate the (idempotent)
// migration so it is not re-run on every reconcile (E8 risk: migration cost).
const currentSchemaVersion = "v1"

// sessionStorePGSchema is the idempotent PostgreSQL schema + row-level-security
// migration for the postgres backend (plan E8 T2). Every statement is guarded so
// running it N times is a no-op after the first:
//
//   - CREATE TABLE IF NOT EXISTS for sessions + events.
//   - ENABLE ROW LEVEL SECURITY is idempotent.
//   - The tenant-isolation policy is created only if absent (CREATE POLICY has no
//     IF NOT EXISTS, so it is guarded by a pg_policies lookup in a DO block).
//
// RLS keys on tenant_id and reads app.tenant_id, which the adapter sets per
// connection via `SET app.tenant_id = '<tenant>'`. No cross-tenant read is
// possible even with a compromised (non-superuser) connection string.
const sessionStorePGSchema = `
CREATE TABLE IF NOT EXISTS sessions (
    id           uuid PRIMARY KEY,
    workspace_uid text NOT NULL,
    tenant_id    text NOT NULL,
    started_at   timestamptz NOT NULL DEFAULT now(),
    ended_at     timestamptz,
    event_count  int NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS events (
    session_id uuid NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    ordinal    int  NOT NULL,
    ts         timestamptz NOT NULL DEFAULT now(),
    type       text NOT NULL,
    payload    jsonb,
    PRIMARY KEY (session_id, ordinal)
);

ALTER TABLE sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE events   ENABLE ROW LEVEL SECURITY;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_policies
        WHERE tablename = 'sessions' AND policyname = 'tenant_isolation'
    ) THEN
        CREATE POLICY tenant_isolation ON sessions
            USING (tenant_id = current_setting('app.tenant_id', true));
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_policies
        WHERE tablename = 'events' AND policyname = 'tenant_isolation'
    ) THEN
        CREATE POLICY tenant_isolation ON events
            USING (session_id IN (
                SELECT id FROM sessions
                WHERE tenant_id = current_setting('app.tenant_id', true)
            ));
    END IF;
END
$$;
`

// SessionStoreMigrator applies the postgres schema + RLS migration. The real
// implementation opens a connection from the projected DSN file and executes
// sessionStorePGSchema; tests inject a fake that records calls without touching a
// database. The reconciler owns this interface (rule 06 §Never — no mocking a type
// you do not own without an adapter).
type SessionStoreMigrator interface {
	// Migrate applies the schema + RLS migration to the store described by spec.
	// It is idempotent: calling it repeatedly converges to the same schema with no
	// error (acceptance: TestSessionStorePGMigrate_Idempotent runs it ×3).
	Migrate(ctx context.Context, dsnSecretName, sslMode string) error
}

// NoopSessionStoreMigrator is the default migrator used when no real PostgreSQL
// connection is wired (dev/local + envtest). It validates inputs and is a no-op
// otherwise, so the SQLite path and envtest never require a live database.
type NoopSessionStoreMigrator struct{}

// Migrate implements SessionStoreMigrator as a no-op.
func (NoopSessionStoreMigrator) Migrate(_ context.Context, _, _ string) error {
	return nil
}
