// Workspace shares (v0.1.3 P2, api.md 5.25, data-model.md 3.14, events.md 4.11). D1 holds metadata only — who shares
// with whom, seats, granted levels, how it ended. The share content is ciphertext the WorkspaceHub DO relays
// (docs/contract/workspace.md); nothing here ever sees a path, a file name, a command or a screen.
//
// Visibility: the owner and every member that was not removed see a share; everyone else gets 404 (existence hidden).
import type { Runtime } from "../env";
import { alertSpec } from "../lib/alerts";
import { all, batch, first, jsonList, stmt } from "../lib/db";
import { err } from "../lib/errors";
import { newId } from "../lib/ids";
import { errFields } from "../lib/log";
import { sendAlerts } from "../lib/notify";
import { publishEvent } from "../lib/realtime";
import { loadUserSummaries } from "../lib/serialize";
import { iso, isoOrNull, now } from "../lib/time";
import type { Alert, WorkspaceShare } from "../types/api";
import type { WorkspaceShareMemberRow, WorkspaceShareRow } from "../types/rows";

export const WORKSPACE_LIMITS = {
  /** live shares per owner (api.md 5.25 — 422 live_workspace_shares) */
  liveShares: 3,
  /** members per share, seats 1..8 (422 workspace_share_members) */
  members: 8,
  /** shares one user may create in `createWindowMs` (counted in D1, api.md 0.7) */
  createsPerWindow: 10,
  createWindowMs: 60 * 60_000,
  /** list page size (no cursor) */
  listMax: 50,
} as const;

export type ShareRole = "owner" | "member";
export type EndReason = "ended" | "timeout";

/** The share row and the viewer's standing on it; null when the viewer may not see it (404). */
export async function shareAccess(db: D1Database, shareId: string, userId: string): Promise<{ row: WorkspaceShareRow; role: ShareRole } | null> {
  const row = await first<WorkspaceShareRow & { is_member: number }>(
    db,
    `SELECT s.*, EXISTS (SELECT 1 FROM workspace_share_members m
                          WHERE m.share_id = s.id AND m.user_id = ?2 AND m.removed_at IS NULL) AS is_member
       FROM workspace_shares s WHERE s.id = ?1`,
    shareId,
    userId,
  );
  if (row === null) return null;
  const { is_member, ...share } = row;
  if (share.owner_id === userId) return { row: share, role: "owner" };
  return is_member === 1 ? { row: share, role: "member" } : null;
}

/** WorkspaceShare for each row (rows whose owner is gone are dropped); members in seat order, removed ones left out. */
export async function serializeShares(db: D1Database, rows: WorkspaceShareRow[]): Promise<WorkspaceShare[]> {
  if (rows.length === 0) return [];
  const members = await all<WorkspaceShareMemberRow>(
    db,
    `SELECT * FROM workspace_share_members
      WHERE share_id IN (SELECT value FROM json_each(?1)) AND removed_at IS NULL
      ORDER BY share_id, seat`,
    jsonList(rows.map((r) => r.id)),
  );
  const users = await loadUserSummaries(db, [...rows.map((r) => r.owner_id), ...members.map((m) => m.user_id)]);
  const byShare = new Map<string, WorkspaceShareMemberRow[]>();
  for (const m of members) {
    const list = byShare.get(m.share_id);
    if (list) list.push(m);
    else byShare.set(m.share_id, [m]);
  }
  return rows.flatMap((r): WorkspaceShare[] => {
    const owner = users.get(r.owner_id);
    if (!owner) return [];
    return [
      {
        id: r.id,
        owner,
        title: r.title,
        status: r.status,
        created_at: iso(r.created_at),
        ended_at: isoOrNull(r.ended_at),
        end_reason: r.end_reason,
        members: (byShare.get(r.id) ?? []).flatMap((m) => {
          const user = users.get(m.user_id);
          if (!user) return [];
          return [
            {
              user,
              seat: m.seat,
              invited_at: iso(m.invited_at),
              joined_at: isoOrNull(m.joined_at),
              left_at: isoOrNull(m.left_at),
              max_level: m.max_level,
              verified: m.verified === 1,
            },
          ];
        }),
      },
    ];
  });
}

export async function serializeShare(db: D1Database, row: WorkspaceShareRow): Promise<WorkspaceShare> {
  const [share] = await serializeShares(db, [row]);
  if (!share) throw err.notFound("workspace share");
  return share;
}

/** The share as `viewerId` may see it, or null (not the owner nor a member that was not removed). */
export async function loadShare(db: D1Database, shareId: string, viewerId: string): Promise<WorkspaceShare | null> {
  const access = await shareAccess(db, shareId, viewerId);
  if (access === null) return null;
  const [share] = await serializeShares(db, [access.row]);
  return share ?? null;
}

/** GET /v1/workspace-shares: my shares (owner) or the ones I am a member of (invited), newest first, at most 50. */
export async function listShares(db: D1Database, userId: string, role: "owner" | "invited", status: "live" | "ended"): Promise<WorkspaceShare[]> {
  const rows =
    role === "owner"
      ? await all<WorkspaceShareRow>(
          db,
          "SELECT * FROM workspace_shares WHERE owner_id = ?1 AND status = ?2 ORDER BY created_at DESC, id DESC LIMIT ?3",
          userId,
          status,
          WORKSPACE_LIMITS.listMax,
        )
      : await all<WorkspaceShareRow>(
          db,
          `SELECT s.* FROM workspace_share_members m JOIN workspace_shares s ON s.id = m.share_id
            WHERE m.user_id = ?1 AND m.removed_at IS NULL AND s.status = ?2
            ORDER BY s.created_at DESC, s.id DESC LIMIT ?3`,
          userId,
          status,
          WORKSPACE_LIMITS.listMax,
        );
  return serializeShares(db, rows);
}

/**
 * Of `userIds`, the ones `ownerId` may not invite (api.md 5.25 초대 자격), in request order: the owner, a user
 * suspended right now, a block either way, or neither friends nor members of one live space. One json_each query.
 */
async function inviteDenials(db: D1Database, ownerId: string, userIds: string[], t: number): Promise<string[]> {
  if (userIds.length === 0) return [];
  const rows = await all<{ id: string }>(
    db,
    `SELECT j.value AS id FROM json_each(?1) j LEFT JOIN users u ON u.id = j.value
      WHERE j.value = ?2
         OR u.id IS NULL
         OR (u.suspended_at IS NOT NULL AND (u.suspended_until IS NULL OR u.suspended_until > ?3))
         OR EXISTS (SELECT 1 FROM blocks b
                     WHERE (b.blocker_id = ?2 AND b.blocked_id = j.value) OR (b.blocker_id = j.value AND b.blocked_id = ?2))
         OR NOT (EXISTS (SELECT 1 FROM friendships fr
                          WHERE (fr.user_a = ?2 AND fr.user_b = j.value) OR (fr.user_a = j.value AND fr.user_b = ?2))
                 OR EXISTS (SELECT 1 FROM space_members m1
                              JOIN space_members m2 ON m2.space_id = m1.space_id AND m2.user_id = j.value
                              JOIN spaces sp ON sp.id = m1.space_id AND sp.deleted_at IS NULL
                             WHERE m1.user_id = ?2))`,
    jsonList(userIds),
    ownerId,
    t,
  );
  const denied = new Set(rows.map((r) => r.id));
  return userIds.filter((u) => denied.has(u));
}

function shareTarget(shareId: string): Alert["target"] {
  return { type: "workspace_share", id: shareId, channel_id: null, message_id: null, thread_root_id: null, post_id: null, space_id: null };
}

/** workspace_share.invited + the workspace_invite alert to each new invitee (notify.workspace, blocks applied). */
async function announceInvites(rt: Runtime, share: WorkspaceShare, invitees: string[]): Promise<void> {
  if (invitees.length === 0) return;
  await publishEvent(rt, { users: invitees }, "workspace_share.invited", { share });
  const template = { id: "workspace.invite" as const, params: { actor: share.owner.display_name, share: share.title } };
  await sendAlerts(
    rt,
    invitees.map((u) => ({ userId: u, actorId: share.owner.id, alert: alertSpec("workspace_invite", template, shareTarget(share.id)) })),
  );
}

function updated(rt: Runtime, share: WorkspaceShare, audience: string[]): Promise<void> {
  const users = [...new Set(audience)];
  if (users.length === 0) return Promise.resolve();
  return publishEvent(rt, { users }, "workspace_share.updated", { share }, { ephemeral: true });
}

/** The share's WorkspaceHub (one DO per share, name = share id). */
export function workspaceHub(rt: Runtime, shareId: string) {
  return rt.env.WORKSPACE_HUB.getByName(shareId);
}

/** POST to a hub's internal path (`/end`, `/kick`); a failure is logged, never surfaced (D1 already committed). */
async function tellHub(rt: Runtime, shareId: string, path: "/end" | "/kick", body: unknown): Promise<void> {
  try {
    const res = await workspaceHub(rt, shareId).fetch(`https://hub${path}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    await res.body?.cancel();
  } catch (e) {
    console.error({ msg: "workspace hub call failed", share_id: shareId, path, err: errFields(e) });
  }
}

/**
 * POST /v1/workspace-shares: a live share owned by `ownerId` with `userIds` on seats 1..n (request order, duplicates
 * counted once). 403 action_not_allowed {user_ids} for anyone who may not be invited; 422 live_workspace_shares at 3
 * live shares; 429 RL_SHARE at 10 shares created in the last 60 minutes (D1 window, api.md 0.7). The caps are checked
 * in the INSERT itself, so two concurrent creates cannot both pass them.
 */
export async function createShare(rt: Runtime, ownerId: string, title: string, userIds: string[]): Promise<WorkspaceShare> {
  const db = rt.env.DB;
  const ids = [...new Set(userIds)];
  const t = now();
  const denied = await inviteDenials(db, ownerId, ids, t);
  if (denied.length > 0) throw err.forbidden("action_not_allowed", "action not allowed", { user_ids: denied });
  const id = newId(t);
  const [ins] = await batch(db, [
    stmt(
      db,
      `INSERT INTO workspace_shares (id, owner_id, title, status, created_at)
       SELECT ?1, ?2, ?3, 'live', ?4
        WHERE (SELECT COUNT(*) FROM workspace_shares WHERE owner_id = ?2 AND status = 'live') < ?5
          AND (SELECT COUNT(*) FROM workspace_shares WHERE owner_id = ?2 AND created_at > ?6) < ?7`,
      id,
      ownerId,
      title,
      t,
      WORKSPACE_LIMITS.liveShares,
      t - WORKSPACE_LIMITS.createWindowMs,
      WORKSPACE_LIMITS.createsPerWindow,
    ),
    ...ids.map((u, i) =>
      stmt(
        db,
        `INSERT INTO workspace_share_members (share_id, user_id, seat, invited_at)
         SELECT ?1, ?2, ?3, ?4 WHERE EXISTS (SELECT 1 FROM workspace_shares WHERE id = ?1)`,
        id,
        u,
        i + 1,
        t,
      ),
    ),
  ]);
  if (ins!.meta.changes === 0) {
    const live = await first<{ n: number }>(db, "SELECT COUNT(*) AS n FROM workspace_shares WHERE owner_id = ?1 AND status = 'live'", ownerId);
    if ((live?.n ?? 0) >= WORKSPACE_LIMITS.liveShares) throw err.limit("live_workspace_shares", WORKSPACE_LIMITS.liveShares);
    throw err.rateLimited("RL_SHARE", 60);
  }
  const row = await first<WorkspaceShareRow>(db, "SELECT * FROM workspace_shares WHERE id = ?1", id);
  if (row === null) throw err.notFound("workspace share");
  const share = await serializeShare(db, row);
  await announceInvites(rt, share, ids);
  await updated(rt, share, [ownerId]); // the owner's other devices list it under "my shares"
  return share;
}

/**
 * POST /v1/workspace-shares/{id}/invites (owner only — a member gets 403 forbidden, anyone else 404; an ended share
 * 409 conflict {reason:"ended"}). Members already in are skipped; a removed member comes back on their seat; new
 * people take the lowest free seats, reclaiming the seat of the longest-removed row when none is free. 422
 * workspace_share_members past 8 members that were not removed.
 */
export async function addInvites(rt: Runtime, shareId: string, ownerId: string, userIds: string[]): Promise<WorkspaceShare> {
  const db = rt.env.DB;
  const access = await shareAccess(db, shareId, ownerId);
  if (access === null) throw err.notFound("workspace share");
  if (access.role !== "owner") throw err.forbidden();
  if (access.row.status !== "live") throw err.conflict("conflict", "ended");
  const t = now();
  const rows = await all<WorkspaceShareMemberRow>(db, "SELECT * FROM workspace_share_members WHERE share_id = ?1", shareId);
  const byUser = new Map(rows.map((r) => [r.user_id, r]));
  const active = rows.filter((r) => r.removed_at === null);
  // no row yet (undefined) or removed (a time): to add; removed_at null = already in, skipped
  const toAdd = [...new Set(userIds)].filter((u) => byUser.get(u)?.removed_at !== null);
  if (toAdd.length === 0) return serializeShare(db, access.row);
  const denied = await inviteDenials(db, ownerId, toAdd, t);
  if (denied.length > 0) throw err.forbidden("action_not_allowed", "action not allowed", { user_ids: denied });
  if (active.length + toAdd.length > WORKSPACE_LIMITS.members) throw err.limit("workspace_share_members", WORKSPACE_LIMITS.members);

  const rejoin = toAdd.filter((u) => byUser.has(u));
  const brandNew = toAdd.filter((u) => !byUser.has(u));
  const taken = new Set(rows.map((r) => r.seat));
  const free = Array.from({ length: WORKSPACE_LIMITS.members }, (_, i) => i + 1).filter((s) => !taken.has(s));
  // removed rows not coming back, longest removed first: their seats can be reclaimed
  const reclaimable = rows
    .filter((r) => r.removed_at !== null && !rejoin.includes(r.user_id))
    .sort((a, b) => a.removed_at! - b.removed_at! || a.seat - b.seat);
  const drop: WorkspaceShareMemberRow[] = [];
  while (free.length < brandNew.length) {
    const r = reclaimable.shift();
    if (!r) throw err.limit("workspace_share_members", WORKSPACE_LIMITS.members); // unreachable: count checked above
    drop.push(r);
    free.push(r.seat);
  }
  free.sort((a, b) => a - b);
  const live = "EXISTS (SELECT 1 FROM workspace_shares WHERE id = ?1 AND status = 'live')";
  await batch(db, [
    ...drop.map((r) => stmt(db, "DELETE FROM workspace_share_members WHERE share_id = ?1 AND user_id = ?2 AND removed_at IS NOT NULL", shareId, r.user_id)),
    ...rejoin.map((u) =>
      stmt(
        db,
        `UPDATE workspace_share_members
            SET removed_at = NULL, invited_at = ?3, joined_at = NULL, left_at = NULL, max_level = NULL, verified = 0
          WHERE share_id = ?1 AND user_id = ?2 AND removed_at IS NOT NULL AND ${live}`,
        shareId,
        u,
        t,
      ),
    ),
    ...brandNew.map((u, i) =>
      stmt(db, `INSERT INTO workspace_share_members (share_id, user_id, seat, invited_at) SELECT ?1, ?2, ?3, ?4 WHERE ${live}`, shareId, u, free[i]!, t),
    ),
  ]);
  const row = await first<WorkspaceShareRow>(db, "SELECT * FROM workspace_shares WHERE id = ?1", shareId);
  if (row === null) throw err.notFound("workspace share");
  if (row.status !== "live") throw err.conflict("conflict", "ended"); // ended while we wrote: the guarded writes did nothing
  const share = await serializeShare(db, row);
  await announceInvites(rt, share, toAdd);
  await updated(rt, share, [ownerId, ...active.map((r) => r.user_id)]);
  return share;
}

/**
 * DELETE /v1/workspace-shares/{id}/participants/{user}: the owner removes a member, or a member removes themselves.
 * Anyone else 404; a member naming someone else, or the owner naming themselves, 403 forbidden; a target that is not
 * a member (or was already removed) 404. Sets removed_at, closes their share socket (4101) and tells the owner, the
 * remaining members and the removed person (workspace_share.updated without them in `members`).
 */
export async function removeMember(rt: Runtime, shareId: string, actorId: string, targetId: string): Promise<void> {
  const db = rt.env.DB;
  const access = await shareAccess(db, shareId, actorId);
  if (access === null) throw err.notFound("workspace share");
  if (access.role === "owner" ? targetId === actorId : targetId !== actorId) throw err.forbidden();
  const res = await batch(db, [
    stmt(db, "UPDATE workspace_share_members SET removed_at = ?3 WHERE share_id = ?1 AND user_id = ?2 AND removed_at IS NULL", shareId, targetId, now()),
  ]);
  if (res[0]!.meta.changes === 0) throw err.notFound("user");
  await tellHub(rt, shareId, "/kick", { user_id: targetId });
  const share = await serializeShare(db, access.row);
  await updated(rt, share, [access.row.owner_id, ...share.members.map((m) => m.user.id), targetId]);
}

/**
 * Ends a live share (DELETE by the owner -> "ended", the hub's owner-gone alarm -> "timeout"): status / ended_at /
 * end_reason, workspace_share.ended to the owner and the members, then the hub closes every socket 4100 — unless the
 * hub itself is the caller (`fromHub`), which closes its own sockets. Already ended: nothing, false.
 */
export async function endShare(rt: Runtime, shareId: string, reason: EndReason, opts: { fromHub?: boolean } = {}): Promise<boolean> {
  const db = rt.env.DB;
  const res = await batch(db, [
    stmt(
      db,
      "UPDATE workspace_shares SET status = 'ended', ended_at = ?2, end_reason = ?3 WHERE id = ?1 AND status = 'live'",
      shareId,
      now(),
      reason,
    ),
  ]);
  if (res[0]!.meta.changes === 0) return false;
  const audience = await all<{ user_id: string }>(
    db,
    `SELECT owner_id AS user_id FROM workspace_shares WHERE id = ?1
     UNION SELECT user_id FROM workspace_share_members WHERE share_id = ?1 AND removed_at IS NULL`,
    shareId,
  );
  await publishEvent(rt, { users: audience.map((r) => r.user_id) }, "workspace_share.ended", { share_id: shareId, reason });
  if (!opts.fromHub) await tellHub(rt, shareId, "/end", { reason });
  return true;
}
