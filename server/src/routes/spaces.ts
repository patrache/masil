// Spaces (api.md 5.7) and the space channel list (api.md 5.9 GET /spaces/{id}/channels). requireAuth is a per-route
// argument (plan 01 Global Constraints). Task 21: spaces, members, roles, kick/ban, bans, transfer, delete; Task 23
// adds POST /spaces/:space_id/channels. Membership changes go through services/membership.ts.
import { Hono } from "hono";
import * as z from "zod";
import { rt, type AppEnv } from "../env";
import { getAuth, requireAuth, resolveUserRef } from "../lib/auth";
import { all, batch, decodeCursor, encodeCursor, first, parseLimit, stmt } from "../lib/db";
import { err } from "../lib/errors";
import { newId } from "../lib/ids";
import { requireSpaceRole } from "../lib/permissions";
import { publishEvent, publishEvents } from "../lib/realtime";
import { limit } from "../lib/ratelimit";
import {
  serializeSpace,
  serializeSpaceBans,
  serializeSpaceChannels,
  serializeSpaceDetail,
  serializeSpaceMembers,
  serializeSpaces,
} from "../lib/serialize";
import { charCount, isSingleEmoji, normalizeText } from "../lib/text";
import { now } from "../lib/time";
import { vJson, vQuery, zText, zUserRef } from "../lib/validate";
import { OWNED_SPACES_MAX, removeFromSpace } from "../services/membership";
import type { SpaceRole } from "../types/api";
import type { SpaceBanRow, SpaceMemberRow, SpaceRow } from "../types/rows";

/** api.md 0.3 / data-model.md: one emoji grapheme; the byte cap matches reactions (32 bytes). */
const ICON_EMOJI_MAX_BYTES = 32;
/** Ban reason: the client's form allows 0..200 code points (plan 04); the server enforces the same. */
const BAN_REASON_MAX = 200;
/** Default channel created with every space (api.md 3.5). */
const DEFAULT_CHANNEL_NAME = "일반";

const zIconEmoji = z
  .string()
  .transform((s) => normalizeText(s))
  .refine((s) => isSingleEmoji(s, ICON_EMOJI_MAX_BYTES), { message: "one emoji" });

const CreateBody = z.object({
  name: zText(1, 50),
  description: zText(0, 200).optional(),
  icon_emoji: zIconEmoji.nullable().optional(),
});

const PatchBody = z.object({
  name: zText(1, 50).optional(),
  description: zText(0, 200).optional(),
  icon_emoji: zIconEmoji.nullable().optional(),
});

const TransferBody = z.object({ user: zUserRef });

const RoleBody = z.object({ role: z.enum(["admin", "member"]) });

const MembersQuery = z.object({
  q: z.string().optional(),
  role: z.enum(["owner", "admin", "member"]).optional(),
  cursor: z.string().optional(),
  limit: z.string().optional(),
});

const KickQuery = z.object({
  ban: z.enum(["true", "false", "1", "0"]).optional(),
  reason: z.string().optional(),
});

const ROLE_RANK_SQL = "CASE m.role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 ELSE 2 END";

async function loadSpace(db: D1Database, spaceId: string): Promise<SpaceRow> {
  const row = await first<SpaceRow>(db, "SELECT * FROM spaces WHERE id = ?1 AND deleted_at IS NULL", spaceId);
  if (row === null) throw err.notFound("space");
  return row;
}

async function loadMember(db: D1Database, spaceId: string, userId: string): Promise<SpaceMemberRow | null> {
  return first<SpaceMemberRow>(db, "SELECT * FROM space_members WHERE space_id = ?1 AND user_id = ?2", spaceId, userId);
}

/** LIKE pattern for a substring match with `\` as the escape character. */
function likeContains(s: string): string {
  return `%${s.replace(/[\\%_]/g, (ch) => `\\${ch}`)}%`;
}

export const spaceRoutes = new Hono<AppEnv>();

/** Spaces I am a member of, in join order. */
spaceRoutes.get("/spaces", requireAuth, async (c) => {
  const me = getAuth(c).userId;
  const rows = await all<SpaceRow>(
    c.env.DB,
    `SELECT s.* FROM space_members m JOIN spaces s ON s.id = m.space_id AND s.deleted_at IS NULL
      WHERE m.user_id = ?1 ORDER BY m.joined_at, s.id`,
    me,
  );
  return c.json({ items: await serializeSpaces(c.env.DB, me, rows) });
});

/** One batch in data-model.md 3.8 order: spaces (with the pre-made default_channel_id) -> channels -> space_members. */
spaceRoutes.post("/spaces", requireAuth, limit("RL_SOCIAL"), vJson(CreateBody), async (c) => {
  const me = getAuth(c).userId;
  const db = c.env.DB;
  const b = c.req.valid("json");
  const owned = await first<{ n: number }>(db, "SELECT COUNT(*) AS n FROM spaces WHERE owner_id = ?1 AND deleted_at IS NULL", me);
  if ((owned?.n ?? 0) >= OWNED_SPACES_MAX) throw err.limit("owned_spaces", OWNED_SPACES_MAX);

  const t = now();
  const spaceId = newId(t);
  const channelId = newId(t);
  await batch(db, [
    stmt(
      db,
      `INSERT INTO spaces (id, name, description, icon_emoji, owner_id, default_channel_id, member_count, created_at, updated_at)
       VALUES (?1, ?2, ?3, ?4, ?5, ?6, 1, ?7, ?7)`,
      spaceId,
      b.name,
      b.description ?? "",
      b.icon_emoji ?? null,
      me,
      channelId,
      t,
    ),
    stmt(
      db,
      `INSERT INTO channels (id, kind, space_id, name, topic, is_private, position, created_by, created_at, updated_at)
       VALUES (?1, 'text', ?2, ?3, '', 0, 0, ?4, ?5, ?5)`,
      channelId,
      spaceId,
      DEFAULT_CHANNEL_NAME,
      me,
      t,
    ),
    stmt(db, "INSERT INTO space_members (space_id, user_id, role, joined_at) VALUES (?1, ?2, 'owner', ?3)", spaceId, me, t),
  ]);

  const space = await serializeSpaceDetail(db, me, await loadSpace(db, spaceId));
  await publishEvent(rt(c), { users: [me] }, "space.joined", { space });
  return c.json({ space }, 201);
});

spaceRoutes.get("/spaces/:space_id", requireAuth, async (c) => {
  const me = getAuth(c).userId;
  const db = c.env.DB;
  await requireSpaceRole(db, c.req.param("space_id"), me, "member");
  return c.json({ space: await serializeSpaceDetail(db, me, await loadSpace(db, c.req.param("space_id"))) });
});

/** owner·admin; space.updated{space} (event defaults) to the members. */
spaceRoutes.patch("/spaces/:space_id", requireAuth, vJson(PatchBody), async (c) => {
  const me = getAuth(c).userId;
  const db = c.env.DB;
  const spaceId = c.req.param("space_id");
  await requireSpaceRole(db, spaceId, me, "admin");
  const b = c.req.valid("json");
  const sets: string[] = [];
  const params: unknown[] = [spaceId];
  const set = (col: string, v: unknown) => {
    params.push(v);
    sets.push(`${col} = ?${params.length}`);
  };
  if (b.name !== undefined) set("name", b.name);
  if (b.description !== undefined) set("description", b.description);
  if (b.icon_emoji !== undefined) set("icon_emoji", b.icon_emoji);
  if (sets.length === 0) return c.json({ space: await serializeSpace(db, me, await loadSpace(db, spaceId)) });
  set("updated_at", now());
  const row = await first<SpaceRow>(db, `UPDATE spaces SET ${sets.join(", ")} WHERE id = ?1 AND deleted_at IS NULL RETURNING *`, ...params);
  if (row === null) throw err.notFound("space");
  const [forEvent, forMe] = await Promise.all([serializeSpace(db, null, row), serializeSpace(db, me, row)]);
  await publishEvent(rt(c), { space: spaceId }, "space.updated", { space: forEvent });
  return c.json({ space: forMe });
});

/**
 * owner; soft-deletes the space and its channels in one batch. The {space} audience drops deleted spaces, so the member
 * ids are read before the batch and space.deleted goes to {users} after the commit (plan 01 Global Constraints).
 */
spaceRoutes.delete("/spaces/:space_id", requireAuth, async (c) => {
  const me = getAuth(c).userId;
  const db = c.env.DB;
  const spaceId = c.req.param("space_id");
  await requireSpaceRole(db, spaceId, me, "owner");
  const memberIds = (await all<{ user_id: string }>(db, "SELECT user_id FROM space_members WHERE space_id = ?1", spaceId)).map((r) => r.user_id);
  const t = now();
  const [res] = await batch(db, [
    stmt(db, "UPDATE spaces SET deleted_at = ?2, updated_at = ?2 WHERE id = ?1 AND owner_id = ?3 AND deleted_at IS NULL", spaceId, t, me),
    stmt(db, "UPDATE channels SET deleted_at = ?2, updated_at = ?2 WHERE space_id = ?1 AND deleted_at IS NULL AND changes() > 0", spaceId, t),
  ]);
  if ((res!.meta.changes ?? 0) === 0) throw err.notFound("space");
  await publishEvent(rt(c), { users: memberIds }, "space.deleted", { space_id: spaceId });
  return c.body(null, 204);
});

/** owner -> another member becomes owner, the previous owner becomes admin. */
spaceRoutes.post("/spaces/:space_id/transfer", requireAuth, vJson(TransferBody), async (c) => {
  const auth = getAuth(c);
  const me = auth.userId;
  const db = c.env.DB;
  const spaceId = c.req.param("space_id");
  await requireSpaceRole(db, spaceId, me, "owner");
  const target = await resolveUserRef(db, c.req.valid("json").user, auth);
  if (target.id === me) throw err.conflict("conflict", "self");
  if ((await loadMember(db, spaceId, target.id)) === null) throw err.notFound("member");
  const owned = await first<{ n: number }>(db, "SELECT COUNT(*) AS n FROM spaces WHERE owner_id = ?1 AND deleted_at IS NULL", target.id);
  if ((owned?.n ?? 0) >= OWNED_SPACES_MAX) throw err.limit("owned_spaces", OWNED_SPACES_MAX);

  const t = now();
  const ownerIsTarget = "(SELECT owner_id FROM spaces WHERE id = ?1) = ?3";
  const [res] = await batch(db, [
    stmt(
      db,
      `UPDATE spaces SET owner_id = ?3, updated_at = ?4
        WHERE id = ?1 AND owner_id = ?2 AND deleted_at IS NULL
          AND EXISTS (SELECT 1 FROM space_members WHERE space_id = ?1 AND user_id = ?3)`,
      spaceId,
      me,
      target.id,
      t,
    ),
    stmt(db, `UPDATE space_members SET role = 'admin' WHERE space_id = ?1 AND user_id = ?2 AND ${ownerIsTarget}`, spaceId, me, target.id),
    stmt(db, `UPDATE space_members SET role = 'owner' WHERE space_id = ?1 AND user_id = ?3 AND ${ownerIsTarget}`, spaceId, me, target.id),
  ]);
  if ((res!.meta.changes ?? 0) === 0) throw err.conflict("conflict", "transfer_failed");

  const row = await loadSpace(db, spaceId);
  const memberRows = await all<SpaceMemberRow>(
    db,
    "SELECT * FROM space_members WHERE space_id = ?1 AND user_id IN (?2, ?3) ORDER BY CASE user_id WHEN ?2 THEN 0 ELSE 1 END",
    spaceId,
    me,
    target.id,
  );
  const [members, forEvent, forMe] = await Promise.all([serializeSpaceMembers(db, memberRows), serializeSpace(db, null, row), serializeSpace(db, me, row)]);
  await publishEvents(rt(c), { space: spaceId }, [
    ...members.map((member) => ({ type: "space.member_updated" as const, data: { space_id: spaceId, member } })),
    { type: "space.updated" as const, data: { space: forEvent } },
  ]);
  return c.json({ space: forMe });
});

/** members only; role order (owner -> admin -> member), then display_name, then user id. q = handle/display_name substring. */
spaceRoutes.get("/spaces/:space_id/members", requireAuth, vQuery(MembersQuery), async (c) => {
  const me = getAuth(c).userId;
  const db = c.env.DB;
  const spaceId = c.req.param("space_id");
  await requireSpaceRole(db, spaceId, me, "member");
  const qy = c.req.valid("query");
  const n = parseLimit(qy.limit);
  const cursor = decodeCursor(qy.cursor);

  const where = ["m.space_id = ?1"];
  const params: unknown[] = [spaceId];
  const p = (v: unknown) => {
    params.push(v);
    return `?${params.length}`;
  };
  if (qy.q !== undefined) {
    const q = normalizeText(qy.q);
    const len = charCount(q);
    if (len < 1) throw err.invalid({ q: "too_short" });
    if (len > 100) throw err.invalid({ q: "too_long" });
    const pat = p(likeContains(q));
    where.push(`(u.handle LIKE ${pat} ESCAPE '\\' OR u.display_name LIKE ${pat} ESCAPE '\\')`);
  }
  if (qy.role !== undefined) where.push(`m.role = ${p(qy.role)}`);
  if (cursor !== null) {
    // k = "<rank>|<display_name>", i = user id
    const k = String(cursor.k);
    const sep = k.indexOf("|");
    const rank = Number(k.slice(0, sep));
    if (sep < 1 || !Number.isInteger(rank)) throw err.invalid({ cursor: "invalid" });
    const r = p(rank);
    const d = p(k.slice(sep + 1));
    const i = p(cursor.i);
    where.push(`(${ROLE_RANK_SQL} > ${r} OR (${ROLE_RANK_SQL} = ${r} AND (u.display_name > ${d} OR (u.display_name = ${d} AND m.user_id > ${i}))))`);
  }
  const rows = await all<SpaceMemberRow & { _rank: number; _name: string }>(
    db,
    `SELECT m.*, ${ROLE_RANK_SQL} AS _rank, u.display_name AS _name
       FROM space_members m JOIN users u ON u.id = m.user_id
      WHERE ${where.join(" AND ")}
      ORDER BY _rank, u.display_name, m.user_id
      LIMIT ${n + 1}`,
    ...params,
  );
  const page = rows.slice(0, n);
  const last = page.at(-1);
  const next = rows.length > n && last ? encodeCursor({ k: `${last._rank}|${last._name}`, i: last.user_id }) : null;
  const items = await serializeSpaceMembers(
    db,
    page.map(({ _rank, _name, ...m }) => m),
  );
  return c.json({ items, next_cursor: next });
});

/** owner; role admin|member (owner only through transfer). */
spaceRoutes.patch("/spaces/:space_id/members/:user", requireAuth, vJson(RoleBody), async (c) => {
  const auth = getAuth(c);
  const db = c.env.DB;
  const spaceId = c.req.param("space_id");
  await requireSpaceRole(db, spaceId, auth.userId, "owner");
  const target = await resolveUserRef(db, c.req.param("user"), auth);
  if (target.id === auth.userId) throw err.conflict("conflict", "self");
  const role = c.req.valid("json").role;
  const row = await first<SpaceMemberRow>(
    db,
    "UPDATE space_members SET role = ?3 WHERE space_id = ?1 AND user_id = ?2 AND role <> 'owner' RETURNING *",
    spaceId,
    target.id,
    role,
  );
  if (row === null) throw err.notFound("member");
  const [member] = await serializeSpaceMembers(db, [row]);
  await publishEvent(rt(c), { space: spaceId }, "space.member_updated", { space_id: spaceId, member: member! });
  return c.json({ member });
});

/** @me = leave (owner: 409 owner_cannot_leave); others = kick / ban (api.md 3.5: owner anyone but self, admin members only). */
spaceRoutes.delete("/spaces/:space_id/members/:user", requireAuth, vQuery(KickQuery), async (c) => {
  const auth = getAuth(c);
  const me = auth.userId;
  const db = c.env.DB;
  const spaceId = c.req.param("space_id");
  const myRole = await requireSpaceRole(db, spaceId, me, "member");
  const qy = c.req.valid("query");
  const ban = qy.ban === "true" || qy.ban === "1";
  const reasonText = normalizeText(qy.reason ?? "");
  if (charCount(reasonText) > BAN_REASON_MAX) throw err.invalid({ reason: "too_long" });
  const target = await resolveUserRef(db, c.req.param("user"), auth);

  if (target.id === me) {
    if (myRole === "owner") throw err.conflict("conflict", "owner_cannot_leave");
    await removeFromSpace(rt(c), spaceId, me, me, "left");
    return c.body(null, 204);
  }
  if (myRole === "member") throw err.forbidden("forbidden", "requires admin role");
  const targetRow = await loadMember(db, spaceId, target.id);
  if (targetRow === null) throw err.notFound("member");
  const allowed: Record<SpaceRole, SpaceRole[]> = { owner: ["admin", "member"], admin: ["member"], member: [] };
  if (!allowed[myRole].includes(targetRow.role)) throw err.forbidden("forbidden", "cannot remove this member");
  await removeFromSpace(rt(c), spaceId, target.id, me, ban ? "banned" : "kicked", ban ? reasonText : undefined);
  return c.body(null, 204);
});

/** owner·admin; newest first. */
spaceRoutes.get("/spaces/:space_id/bans", requireAuth, async (c) => {
  const me = getAuth(c).userId;
  const db = c.env.DB;
  const spaceId = c.req.param("space_id");
  await requireSpaceRole(db, spaceId, me, "admin");
  const rows = await all<SpaceBanRow>(db, "SELECT * FROM space_bans WHERE space_id = ?1 ORDER BY created_at DESC, user_id DESC", spaceId);
  return c.json({ items: await serializeSpaceBans(db, rows) });
});

/** owner·admin; 404 when the user is not banned. */
spaceRoutes.delete("/spaces/:space_id/bans/:user", requireAuth, async (c) => {
  const auth = getAuth(c);
  const db = c.env.DB;
  const spaceId = c.req.param("space_id");
  await requireSpaceRole(db, spaceId, auth.userId, "admin");
  const target = await resolveUserRef(db, c.req.param("user"), auth);
  const res = await stmt(db, "DELETE FROM space_bans WHERE space_id = ?1 AND user_id = ?2", spaceId, target.id).run();
  if ((res.meta.changes ?? 0) === 0) throw err.notFound("ban");
  return c.body(null, 204);
});

/** members only; text channels I can see, position order. */
spaceRoutes.get("/spaces/:space_id/channels", requireAuth, async (c) => {
  const me = getAuth(c).userId;
  const spaceId = c.req.param("space_id");
  await requireSpaceRole(c.env.DB, spaceId, me, "member");
  return c.json({ items: await serializeSpaceChannels(c.env.DB, me, spaceId) });
});
