-- 000021_add_task_assignees.sql
-- Replaces the single-valued tasks.assignee_id column with a task_assignees
-- join table so a task can have multiple assignees.
--
-- This file is re-executed on every application startup (see
-- database.RunMigrationsFS), so every statement must be safe to run
-- repeatedly. The backfill + column drop can only run once (the source
-- column disappears after the first run), so they're wrapped in a DO block
-- that checks column existence first — a plain SELECT referencing
-- assignee_id would fail to even parse once the column is gone, so
-- ALTER TABLE ... DROP COLUMN IF EXISTS alone (the pattern used by prior
-- drop-column migrations in this directory) isn't enough here.
--
-- The DO block takes an ACCESS EXCLUSIVE lock on tasks before checking
-- column existence, so the check-then-drop is atomic across concurrently
-- starting replicas: without it, two replicas racing this migration on the
-- same first deploy could both observe "column exists" before either
-- commits, and the second one's DROP COLUMN would then fail once it
-- acquires the lock behind the first (the column would already be gone),
-- failing that replica's startup.

BEGIN;

CREATE TABLE IF NOT EXISTS task_assignees (
    task_id     UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    member_id   UUID NOT NULL REFERENCES project_members(id) ON DELETE CASCADE,
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (task_id, member_id)
);

CREATE INDEX IF NOT EXISTS idx_task_assignees_member ON task_assignees (member_id);

DO $$
BEGIN
    LOCK TABLE tasks IN ACCESS EXCLUSIVE MODE;
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'tasks' AND column_name = 'assignee_id'
    ) THEN
        EXECUTE '
            INSERT INTO task_assignees (task_id, member_id)
            SELECT id, assignee_id FROM tasks WHERE assignee_id IS NOT NULL
            ON CONFLICT DO NOTHING
        ';
        EXECUTE 'ALTER TABLE tasks DROP COLUMN assignee_id';
    END IF;
END $$;

COMMIT;
