// User-level services (server-architecture.md 4.5 users.ts). Plan 01 owns `revokeSessions`; plan 02 appends
// suspendUser / unsuspendUser.
import type { Runtime } from "../env";
import { all, batch, first, jsonList, stmt } from "../lib/db";
import { err } from "../lib/errors";
import { newId } from "../lib/ids";
import { publishEvent } from "../lib/realtime";
import { loadPresenceItems, serializeAdminUsers, toUserSummary } from "../lib/serialize";
import { now } from "../lib/time";
import type { AdminUserView } from "../types/api";
import type { UserRow } from "../types/rows";
import { resolveReports } from "./content";
import { errFields } from "../lib/log";

export type RevokeReason = "logout" | "revoked" | "password_changed" | "suspended";

/**
 * Revokes sessions in D1 (`revoked_at = now`, `revoke_reason`) first, then asks the user's UserHub to send
 * `session.revoked` to those sessions' sockets and close them (4001, or 4003 for "suspended").
 * `"all"` = every active session of the user, minus `exceptSessionId` when given. Only the user's own, not yet revoked
 * sessions are touched. A UserHub failure is logged only (D1 is the source of truth: the token is already dead).
 */
export async function revokeSessions(
  rt: Runtime,
  userId: string,
  sessionIds: string[] | "all",
  reason: RevokeReason,
  exceptSessionId?: string,
): Promise<void> {
  if (sessionIds !== "all" && sessionIds.length === 0) return;
  const t = now();
  const rows = await all<{ id: string }>(
    rt.env.DB,
    `UPDATE sessions SET revoked_at = ?1, revoke_reason = ?2
      WHERE user_id = ?3 AND revoked_at IS NULL
        AND (?4 IS NULL OR id IN (SELECT value FROM json_each(?4)))
        AND (?5 IS NULL OR id <> ?5)
      RETURNING id`,
    t,
    reason,
    userId,
    sessionIds === "all" ? null : jsonList(sessionIds),
    exceptSessionId ?? null,
  );
  // "all" without an exception closes every socket (also ones whose session had merely expired); otherwise exactly
  // the sessions this call revoked.
  const targets: string[] | "all" = sessionIds === "all" && exceptSessionId === undefined ? "all" : rows.map((r) => r.id);
  if (targets !== "all" && targets.length === 0) return;
  try {
    await rt.env.USER_HUB.getByName(userId).revokeSessions(userId, targets, reason);
  } catch (e) {
    console.error({ msg: "revokeSessions: UserHub call failed", err: errFields(e) });
  }
}

/**
 * After suspend / unsuspend (api.md 3.9), so open views follow without a refetch. The account itself has no live
 * session then.
 * - user.updated{user} (logged, events.md 4.5) to its summaryWatchers — friends, DM / group DM co-members and members
 *   of its spaces, whose member lists and message authors hold its UserSummary: suspended=true with status and
 *   avatar_url null, or back. Being logged, a member who resumes replays it after any older event that carried the
 *   unmasked summary.
 * - presence.updated (ephemeral, events.md 4.6) to its presenceWatchers: offline / null, or the real item back. The
 *   4003 close only announces a status change, and a manual dnd or a focus holds with zero sockets (events.md 6.3), so
 *   without this the watchers would keep showing it as dnd. An online account gets the same offline item twice
 *   (here and from the close) — harmless, the item is the same.
 * A suspension that simply runs out (suspended_until) sends nothing; clients see it on their next fetch.
 */
async function publishSuspensionChange(rt: Runtime, userId: string): Promise<void> {
  const row = await first<UserRow>(rt.env.DB, "SELECT * FROM users WHERE id = ?1", userId);
  if (row === null) return;
  await publishEvent(rt, { summaryWatchers: userId }, "user.updated", { user: toUserSummary(row) });
  const [item] = await loadPresenceItems(rt.env.DB, [userId]);
  if (item !== undefined) await publishEvent(rt, { presenceWatchers: userId }, "presence.updated", item, { ephemeral: true });
}

function adminActionStmt(
  db: D1Database,
  t: number,
  adminId: string,
  action: "suspend_user" | "unsuspend_user",
  userId: string,
  reportId: string | null,
  reason: string,
): D1PreparedStatement {
  return stmt(
    db,
    `INSERT INTO admin_actions (id, admin_id, action, target_type, target_id, report_id, reason, created_at)
     VALUES (?1, ?2, ?3, 'user', ?4, ?5, ?6, ?7)`,
    newId(t),
    adminId,
    action,
    userId,
    reportId,
    reason,
    t,
  );
}

async function adminUserView(db: D1Database, userId: string): Promise<AdminUserView> {
  const row = await first<UserRow>(db, "SELECT * FROM users WHERE id = ?1", userId);
  if (row === null) throw err.notFound("user");
  const [view] = await serializeAdminUsers(db, [row]);
  return view!;
}

/**
 * Suspends `userId` (api.md 5.22 / 3.9): suspended_at = now, suspended_until = now + durationSec (null = indefinite),
 * suspend_reason; an admin_actions row; and, with `reportId`, that report plus every other open report on the user
 * resolved as user_suspended — all in one batch. After the commit every session is revoked (UserHub sends
 * session.revoked and closes the sockets with 4003). Suspending an operator -> 409 conflict; unknown report -> 404.
 * Re-suspending an already suspended user overwrites the period and reason.
 */
export async function suspendUser(
  rt: Runtime,
  adminId: string,
  userId: string,
  reason: string,
  durationSec: number | null,
  reportId?: string,
): Promise<AdminUserView> {
  const db = rt.env.DB;
  const target = await first<Pick<UserRow, "id" | "role">>(db, "SELECT id, role FROM users WHERE id = ?1", userId);
  if (target === null) throw err.notFound("user");
  if (target.role === "admin") throw err.conflict("conflict", "admin");
  // admin_actions.report_id references reports(id)
  if (reportId !== undefined && (await first(db, "SELECT 1 AS x FROM reports WHERE id = ?1", reportId)) === null) {
    throw err.notFound("report");
  }

  const t = now();
  const stmts = [
    stmt(
      db,
      "UPDATE users SET suspended_at = ?2, suspended_until = ?3, suspend_reason = ?4, updated_at = ?2 WHERE id = ?1 AND role <> 'admin'",
      userId,
      t,
      durationSec === null ? null : t + durationSec * 1000,
      reason,
    ),
    adminActionStmt(db, t, adminId, "suspend_user", userId, reportId ?? null, reason),
  ];
  if (reportId !== undefined) stmts.push(resolveReports(db, reportId, "user", userId, adminId, "user_suspended", reason));
  await batch(db, stmts);

  await revokeSessions(rt, userId, "all", "suspended");
  await publishSuspensionChange(rt, userId);
  return adminUserView(db, userId);
}

/** Lifts a suspension (all three columns NULL) and records unsuspend_user. Unknown user -> 404. */
export async function unsuspendUser(rt: Runtime, adminId: string, userId: string): Promise<AdminUserView> {
  const db = rt.env.DB;
  if ((await first(db, "SELECT 1 AS x FROM users WHERE id = ?1", userId)) === null) throw err.notFound("user");
  const t = now();
  await batch(db, [
    stmt(
      db,
      "UPDATE users SET suspended_at = NULL, suspended_until = NULL, suspend_reason = NULL, updated_at = ?2 WHERE id = ?1",
      userId,
      t,
    ),
    adminActionStmt(db, t, adminId, "unsuspend_user", userId, null, ""),
  ]);
  await publishSuspensionChange(rt, userId);
  return adminUserView(db, userId);
}
