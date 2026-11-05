// Cleanup job (data-model.md 6, `17 * * * *`): nine retention tasks in order, each touching at most 500 rows per run
// (leftovers are picked up next hour). A failing task is logged and the remaining tasks still run.
import type { Env } from "../env";
import { all, batch, jsonList, run, stmt } from "../lib/db";
import { removeUploads } from "../lib/uploads";
import { now } from "../lib/time";
import { errFields } from "../lib/log";

const BATCH_LIMIT = 500;
const HOUR_MS = 3_600_000;
const DAY_MS = 86_400_000;
const STORY_GRACE_MS = HOUR_MS;
const ORPHAN_UPLOAD_MS = DAY_MS;
const SESSION_RETENTION_MS = 30 * DAY_MS;
const NOTIFICATION_RETENTION_MS = 90 * DAY_MS;
const REQUEST_RETENTION_MS = 30 * DAY_MS;
const INVITE_RETENTION_MS = 30 * DAY_MS;

/** 1. Stories expired more than an hour ago: R2 media + uploads.status='removed', then story_views and stories rows. */
async function expireStories(env: Env, t: number): Promise<void> {
  const rows = await all<{ id: string; media_upload_id: string | null }>(
    env.DB,
    "SELECT id, media_upload_id FROM stories WHERE expires_at < ?1 ORDER BY expires_at LIMIT ?2",
    t - STORY_GRACE_MS,
    BATCH_LIMIT,
  );
  if (rows.length === 0) return;
  // Media first: if R2 fails the story rows stay and the next run retries.
  await removeUploads(env, rows.flatMap((r) => (r.media_upload_id === null ? [] : [r.media_upload_id])));
  const ids = jsonList(rows.map((r) => r.id));
  await batch(env.DB, [
    stmt(env.DB, "DELETE FROM story_views WHERE story_id IN (SELECT value FROM json_each(?1))", ids),
    stmt(env.DB, "DELETE FROM stories WHERE id IN (SELECT value FROM json_each(?1))", ids),
  ]);
}

/** 2. Pending uploads never attached within 24 hours: R2 delete + status='removed'. */
async function removeOrphanUploads(env: Env, t: number): Promise<void> {
  const rows = await all<{ id: string }>(
    env.DB,
    "SELECT id FROM uploads WHERE status = 'pending' AND created_at < ?1 ORDER BY created_at LIMIT ?2",
    t - ORPHAN_UPLOAD_MS,
    BATCH_LIMIT,
  );
  await removeUploads(env, rows.map((r) => r.id));
}

/**
 * Tasks 3-9 are single statements whose target set is capped by a `LIMIT 500` subquery, so the row condition is
 * evaluated in the same statement that writes (no select-then-write race with a user setting a fresh status).
 */
const SIMPLE_TASKS: ReadonlyArray<{ name: string; sql: string; params: (t: number) => unknown[] }> = [
  {
    name: "sessions",
    sql: `DELETE FROM sessions WHERE id IN (
            SELECT id FROM sessions WHERE expires_at < ?1 OR revoked_at < ?1 LIMIT ?2)`,
    params: (t) => [t - SESSION_RETENTION_MS, BATCH_LIMIT],
  },
  {
    name: "notifications",
    sql: `DELETE FROM notifications WHERE id IN (
            SELECT id FROM notifications WHERE created_at < ?1 LIMIT ?2)`,
    params: (t) => [t - NOTIFICATION_RETENTION_MS, BATCH_LIMIT],
  },
  {
    name: "friend_requests",
    sql: `DELETE FROM friend_requests WHERE id IN (
            SELECT id FROM friend_requests WHERE status <> 'pending' AND responded_at < ?1 LIMIT ?2)`,
    params: (t) => [t - REQUEST_RETENTION_MS, BATCH_LIMIT],
  },
  {
    name: "space_direct_invites",
    sql: `DELETE FROM space_direct_invites WHERE id IN (
            SELECT id FROM space_direct_invites WHERE status <> 'pending' AND responded_at < ?1 LIMIT ?2)`,
    params: (t) => [t - REQUEST_RETENTION_MS, BATCH_LIMIT],
  },
  {
    name: "space_invites",
    sql: `DELETE FROM space_invites WHERE id IN (
            SELECT id FROM space_invites WHERE revoked_at < ?1 OR expires_at < ?1 LIMIT ?2)`,
    params: (t) => [t - INVITE_RETENTION_MS, BATCH_LIMIT],
  },
  {
    name: "status_expiry",
    sql: `UPDATE users SET status_emoji = NULL, status_text = NULL, status_expires_at = NULL
           WHERE id IN (SELECT id FROM users WHERE status_expires_at < ?1 LIMIT ?2)`,
    params: (t) => [t, BATCH_LIMIT],
  },
  {
    name: "suspension_expiry",
    sql: `UPDATE users SET suspended_at = NULL, suspended_until = NULL, suspend_reason = NULL
           WHERE id IN (SELECT id FROM users WHERE suspended_until < ?1 LIMIT ?2)`,
    params: (t) => [t, BATCH_LIMIT],
  },
  {
    name: "presence",
    sql: `UPDATE user_presence SET status = 'offline', updated_at = ?1
           WHERE user_id IN (SELECT user_id FROM user_presence WHERE status <> 'offline' AND online_until < ?1 LIMIT ?2)`,
    params: (t) => [t, BATCH_LIMIT],
  },
];

/** data-model.md 6 cleanup tasks 1..9 in order; each failure is logged and does not stop the rest. */
export async function runCleanup(env: Env): Promise<void> {
  const t = now();
  const tasks: Array<[string, () => Promise<unknown>]> = [
    ["stories", () => expireStories(env, t)],
    ["orphan_uploads", () => removeOrphanUploads(env, t)],
    ...SIMPLE_TASKS.map((task): [string, () => Promise<unknown>] => [task.name, () => run(env.DB, task.sql, ...task.params(t))]),
  ];
  for (const [name, fn] of tasks) {
    try {
      await fn();
    } catch (e) {
      console.error("cleanup task failed", { task: name, err: errFields(e) });
    }
  }
}
