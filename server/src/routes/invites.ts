// Invite links and direct (friend) invites (api.md 5.8). requireAuth is a per-route argument (plan 01 Global
// Constraints). Joining goes through services/membership.ts joinSpace, which records the invite use in the same batch.
import { Hono } from "hono";
import * as z from "zod";
import { rt, type AppEnv } from "../env";
import { getAuth, isSuspendedNow, requireAuth, resolveUserRef } from "../lib/auth";
import { all, first, stmt } from "../lib/db";
import { err } from "../lib/errors";
import { isId, newId, newInviteCode } from "../lib/ids";
import { notify } from "../lib/notify";
import { areFriends, getSpaceRole, isBlockedEither, requireSpaceRole } from "../lib/permissions";
import { limit } from "../lib/ratelimit";
import { publishEvent } from "../lib/realtime";
import { loadUserSummaries, serializeDirectInvites, serializeInvitePreview, serializeInvites } from "../lib/serialize";
import { now } from "../lib/time";
import { vJson, zUserRef } from "../lib/validate";
import {
  D_INVITE_ACCEPTABLE,
  insertDirectInvite,
  inviteInvalidReason,
  joinSpace,
  linkIssuerCanInvite,
  type InviteCheckRow,
} from "../services/membership";
import type { SpaceDirectInviteRow, SpaceInviteRow, SpaceRow } from "../types/rows";

/** api.md 5.8: expires_in_sec omitted -> 7 days. */
const DEFAULT_EXPIRES_IN_SEC = 604_800;
/** Attempts at a fresh 8-char code before giving up (56^8 codes: a clash is already very unlikely). */
const CODE_ATTEMPTS = 5;

const CreateInviteBody = z.object({
  max_uses: z.number().int().min(1).max(1000).nullable().optional(),
  expires_in_sec: z.number().int().min(300).max(2_592_000).nullable().optional(),
});

const DirectInviteBody = z.object({ user: zUserRef });

function isUniqueViolation(e: unknown): boolean {
  return e instanceof Error && /UNIQUE constraint failed/i.test(e.message);
}

/** The invite link `code` of a live space with its issuer check; 404 when missing or the space is deleted. */
async function loadInvite(db: D1Database, code: string): Promise<InviteCheckRow> {
  const row = await first<InviteCheckRow>(
    db,
    `SELECT i.*, ${linkIssuerCanInvite("i")} AS _issuer_ok
       FROM space_invites i JOIN spaces s ON s.id = i.space_id AND s.deleted_at IS NULL WHERE i.code = ?1`,
    code,
  );
  if (row === null) throw err.notFound("invite");
  return row;
}

/** A direct invite in a live space; a malformed id or a missing row is 404. */
async function loadDirectInvite(db: D1Database, id: string): Promise<SpaceDirectInviteRow> {
  if (!isId(id)) throw err.notFound("invite");
  const row = await first<SpaceDirectInviteRow>(
    db,
    "SELECT d.* FROM space_direct_invites d JOIN spaces s ON s.id = d.space_id AND s.deleted_at IS NULL WHERE d.id = ?1",
    id,
  );
  if (row === null) throw err.notFound("invite");
  return row;
}

export const inviteRoutes = new Hono<AppEnv>();

// ---- invite links ----

/** owner·admin; every link not revoked (expired / used up included), newest first. A link whose creator lost owner·admin
 *  counts as revoked (api.md 3.5) and is left out. */
inviteRoutes.get("/spaces/:space_id/invites", requireAuth, async (c) => {
  const me = getAuth(c).userId;
  const db = c.env.DB;
  const spaceId = c.req.param("space_id");
  await requireSpaceRole(db, spaceId, me, "admin");
  const rows = await all<SpaceInviteRow>(
    db,
    `SELECT * FROM space_invites
      WHERE space_id = ?1 AND revoked_at IS NULL AND ${linkIssuerCanInvite("space_invites")}
      ORDER BY created_at DESC, id DESC`,
    spaceId,
  );
  return c.json({ items: await serializeInvites(db, rows) });
});

/**
 * owner·admin; max_uses 1..1000 | null (unlimited), expires_in_sec 300..2592000 | null (never), omitted = 7 days.
 * The INSERT itself re-checks the role: a demotion or removal that commits after requireSpaceRole has nothing to revoke
 * yet, so without the gate the link would land live for a plain member / non-member (pending item 9). Refused there:
 * the role check again gives 404 (no longer a member) / 403 (no longer owner·admin).
 */
inviteRoutes.post("/spaces/:space_id/invites", requireAuth, limit("RL_SOCIAL"), vJson(CreateInviteBody), async (c) => {
  const me = getAuth(c).userId;
  const db = c.env.DB;
  const spaceId = c.req.param("space_id");
  await requireSpaceRole(db, spaceId, me, "admin");
  const b = c.req.valid("json");
  const t = now();
  const expiresIn = b.expires_in_sec === undefined ? DEFAULT_EXPIRES_IN_SEC : b.expires_in_sec;
  const expiresAt = expiresIn === null ? null : t + expiresIn * 1000;
  let row: SpaceInviteRow | null = null;
  for (let attempt = 0; ; attempt++) {
    try {
      row = await first<SpaceInviteRow>(
        db,
        `INSERT INTO space_invites (id, space_id, code, created_by, max_uses, use_count, expires_at, revoked_at, created_at)
         SELECT ?1, ?2, ?3, ?4, ?5, 0, ?6, NULL, ?7
          WHERE EXISTS (SELECT 1 FROM space_members WHERE space_id = ?2 AND user_id = ?4 AND role IN ('owner', 'admin'))
         RETURNING *`,
        newId(t),
        spaceId,
        newInviteCode(),
        me,
        b.max_uses ?? null,
        expiresAt,
        t,
      );
      break;
    } catch (e) {
      if (!isUniqueViolation(e) || attempt + 1 >= CODE_ATTEMPTS) throw e;
    }
  }
  if (row === null) {
    await requireSpaceRole(db, spaceId, me, "admin"); // 404 / 403 for what changed
    throw err.forbidden("forbidden", "requires admin role");
  }
  const [invite] = await serializeInvites(db, [row]);
  if (!invite) throw err.notFound("space");
  return c.json({ invite }, 201);
});

/** owner·admin of that space (404 for non-members); revoking an already revoked link is a no-op 204. */
inviteRoutes.delete("/invites/:code", requireAuth, async (c) => {
  const me = getAuth(c).userId;
  const db = c.env.DB;
  const inv = await loadInvite(db, c.req.param("code"));
  await requireSpaceRole(db, inv.space_id, me, "admin");
  await stmt(db, "UPDATE space_invites SET revoked_at = ?2 WHERE id = ?1 AND revoked_at IS NULL", inv.id, now()).run();
  return c.body(null, 204);
});

/** Anyone logged in: 404 unknown code, 410 invite_invalid{reason} when it can no longer be used (creator no longer
 *  owner·admin = revoked, api.md 3.5). */
inviteRoutes.get("/invites/:code", requireAuth, async (c) => {
  const me = getAuth(c).userId;
  const db = c.env.DB;
  const inv = await loadInvite(db, c.req.param("code"));
  const reason = inviteInvalidReason(inv);
  if (reason !== null) throw err.inviteInvalid(reason);
  return c.json({ invite: await serializeInvitePreview(db, me, inv) });
});

/** Join through a link: 410 -> 403 banned_from_space -> 409 already_member -> 422 (joinSpace). */
inviteRoutes.post("/invites/:code/accept", requireAuth, async (c) => {
  const me = getAuth(c).userId;
  const inv = await loadInvite(c.env.DB, c.req.param("code"));
  const space = await joinSpace(rt(c), inv.space_id, me, { inviteCode: inv.code });
  return c.json({ space });
});

// ---- direct invites ----

/**
 * Any member invites one of their own friends (api.md 3.5, any role). 403 action_not_allowed (not friends / blocked
 * either way / invitee currently suspended), 403 banned_from_space, 409 already_member, 409 already_exists (a pending invite to this space).
 * After the insert: space.invite_received{invite} to the invitee + inbox space_invite + alert invite (Task 15 table).
 * One batch (insertDirectInvite, pending item 9): a stale pending invite to the same person that can no longer be
 * accepted (its inviter is no longer a member, or no longer their friend / a block either way — D3) becomes canceled
 * (space.invite_removed canceled after commit), then the INSERT — both gated on the inviter still being a member, so an inviter removed after requireSpaceRole writes nothing and gets 404, and on the relation
 * rules above (pending item 10), so an unfriend / block / suspension landing after the pre-checks writes nothing and
 * gets 403 action_not_allowed.
 * And on the invitee still being outside the space (9-b): a join by another route landing after the pre-checks
 * writes nothing and gets 409 already_member.
 */
inviteRoutes.post("/spaces/:space_id/direct-invites", requireAuth, limit("RL_SOCIAL"), vJson(DirectInviteBody), async (c) => {
  const auth = getAuth(c);
  const me = auth.userId;
  const db = c.env.DB;
  const spaceId = c.req.param("space_id");
  await requireSpaceRole(db, spaceId, me, "member");
  const target = await resolveUserRef(db, c.req.valid("json").user, auth);
  if (target.id === me) throw err.conflict("already_member");
  if (isSuspendedNow(target, now())) throw err.forbidden("action_not_allowed"); // api.md 3.9: no invites to a suspended user
  const [friends, blocked] = await Promise.all([areFriends(db, me, target.id), isBlockedEither(db, me, target.id)]);
  if (!friends || blocked) throw err.forbidden("action_not_allowed");
  const facts = await first<{ banned: number; member: number; pending: number }>(
    db,
    `SELECT EXISTS (SELECT 1 FROM space_bans WHERE space_id = ?1 AND user_id = ?2) AS banned,
            EXISTS (SELECT 1 FROM space_members WHERE space_id = ?1 AND user_id = ?2) AS member,
            EXISTS (SELECT 1 FROM space_direct_invites d WHERE d.space_id = ?1 AND d.invitee_id = ?2 AND d.status = 'pending'
                       AND ${D_INVITE_ACCEPTABLE}) AS pending`,
    spaceId,
    target.id,
  );
  if (facts?.banned) throw err.forbidden("banned_from_space");
  if (facts?.member) throw err.conflict("already_member");
  if (facts?.pending) throw err.conflict("already_exists");

  const t = now();
  const { row, stale } = await insertDirectInvite(db, spaceId, me, target.id, t, newId(t));
  for (const old of stale) {
    await publishEvent(rt(c), { users: [target.id, old.inviter_id] }, "space.invite_removed", { invite_id: old.id, reason: "canceled" });
  }
  const [[invite], space, users] = await Promise.all([
    serializeDirectInvites(db, [row]),
    first<SpaceRow>(db, "SELECT * FROM spaces WHERE id = ?1", spaceId),
    loadUserSummaries(db, [me]),
  ]);
  if (!invite || space === null) throw err.notFound("space");
  const name = users.get(me)?.display_name ?? auth.handle;
  await Promise.all([
    publishEvent(rt(c), { users: [target.id] }, "space.invite_received", { invite }),
    notify(rt(c), {
      userId: target.id,
      type: "space_invite",
      actorId: me,
      subject: { type: "space_direct_invite", id: row.id },
      context: { space_id: spaceId, space_name: space.name },
      preview: "",
      dedupeKey: `space_invite:${row.id}`,
      alert: {
        kind: "invite",
        title: `${name} 님이 ${space.name} 방에 초대했어요`,
        body: "",
        target: { type: "space", id: spaceId, channel_id: null, message_id: null, thread_root_id: null, post_id: null, space_id: spaceId },
      },
    }),
  ]);
  return c.json({ invite }, 201);
});

/**
 * Pending invites to me in live spaces I am not in yet (9-b) from inviters who are still members and still my friends
 * with no block either way (D3; others cannot be accepted), newest first.
 */
inviteRoutes.get("/me/direct-invites", requireAuth, async (c) => {
  const me = getAuth(c).userId;
  const db = c.env.DB;
  const rows = await all<SpaceDirectInviteRow>(
    db,
    `SELECT d.* FROM space_direct_invites d JOIN spaces s ON s.id = d.space_id AND s.deleted_at IS NULL
      WHERE d.invitee_id = ?1 AND d.status = 'pending' AND ${D_INVITE_ACCEPTABLE}
      ORDER BY d.created_at DESC, d.id DESC`,
    me,
  );
  return c.json({ items: await serializeDirectInvites(db, rows) });
});

/** The invitee joins (joinSpace's directInviteId batch); anyone else or a non-pending invite: 404. */
inviteRoutes.post("/direct-invites/:invite_id/accept", requireAuth, async (c) => {
  const me = getAuth(c).userId;
  const inv = await loadDirectInvite(c.env.DB, c.req.param("invite_id"));
  if (inv.invitee_id !== me || inv.status !== "pending") throw err.notFound("invite");
  const space = await joinSpace(rt(c), inv.space_id, me, { directInviteId: inv.id });
  return c.json({ space });
});

/** The invitee only (anyone else: 404); a non-pending invite: 404. space.invite_removed to invitee + inviter. */
inviteRoutes.post("/direct-invites/:invite_id/decline", requireAuth, async (c) => {
  const me = getAuth(c).userId;
  const db = c.env.DB;
  const inv = await loadDirectInvite(db, c.req.param("invite_id"));
  if (inv.invitee_id !== me) throw err.notFound("invite");
  const res = await stmt(
    db,
    "UPDATE space_direct_invites SET status = 'declined', responded_at = ?3 WHERE id = ?1 AND invitee_id = ?2 AND status = 'pending'",
    inv.id,
    me,
    now(),
  ).run();
  if ((res.meta.changes ?? 0) === 0) throw err.notFound("invite");
  await publishEvent(rt(c), { users: [inv.invitee_id, inv.inviter_id] }, "space.invite_removed", { invite_id: inv.id, reason: "declined" });
  return c.body(null, 204);
});

/**
 * The inviter or an owner·admin of that space cancels. Not a member of the space: 404; a plain member who is not the
 * inviter: 403 forbidden; a non-pending invite: 404. space.invite_removed to invitee + inviter.
 */
inviteRoutes.delete("/direct-invites/:invite_id", requireAuth, async (c) => {
  const me = getAuth(c).userId;
  const db = c.env.DB;
  const inv = await loadDirectInvite(db, c.req.param("invite_id"));
  const role = await getSpaceRole(db, inv.space_id, me);
  if (role === null) throw err.notFound("invite");
  if (inv.inviter_id !== me && role === "member") throw err.forbidden("forbidden", "only the inviter or an admin can cancel");
  const res = await stmt(
    db,
    "UPDATE space_direct_invites SET status = 'canceled', responded_at = ?2 WHERE id = ?1 AND status = 'pending'",
    inv.id,
    now(),
  ).run();
  if ((res.meta.changes ?? 0) === 0) throw err.notFound("invite");
  await publishEvent(rt(c), { users: [inv.invitee_id, inv.inviter_id] }, "space.invite_removed", { invite_id: inv.id, reason: "canceled" });
  return c.body(null, 204);
});
