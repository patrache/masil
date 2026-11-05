// User-level services (server-architecture.md 4.5 users.ts). Plan 01 owns `revokeSessions`; plan 02 appends
// suspendUser / unsuspendUser.
import type { Runtime } from "../env";
import { all, jsonList } from "../lib/db";
import { now } from "../lib/time";

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
    console.error("revokeSessions: UserHub call failed", e);
  }
}
