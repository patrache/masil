// Reports (api.md 5.21, plan 02 Task 14): POST /reports.
// The reporter must be able to see the target (else 404, existence hidden). The server copies the target's content
// at report time into `reports.snapshot`; `target_user_id` is the author/owner (or the user themself). The reporter
// gets no event or notification about the report or its outcome.
import { Hono } from "hono";
import * as z from "zod";
import type { AppEnv } from "../env";
import { getAuth, requireAuth } from "../lib/auth";
import { all, first, run } from "../lib/db";
import { ApiError, err } from "../lib/errors";
import { newId } from "../lib/ids";
import { canViewPost, canViewUserContent, getSpaceRole, requireChannelRead } from "../lib/permissions";
import { limit } from "../lib/ratelimit";
import { now } from "../lib/time";
import { vJson, zId, zText } from "../lib/validate";
import type { GuestbookRow, MessageRow, PostRow, ReportRow, SpaceRow, StoryRow, UserRow } from "../types/rows";

/** api.md 0.3: report note 0..1000 code points. */
export const REPORT_NOTE_MAX = 1000;

const CreateBody = z.object({
  target_type: z.enum(["user", "message", "post", "story", "guestbook_entry", "space"]),
  target_id: zId,
  reason: z.enum(["spam", "harassment", "illegal", "sexual", "violence", "other"]),
  note: zText(0, REPORT_NOTE_MAX).optional(),
});

type TargetType = ReportRow["target_type"];

interface ResolvedTarget {
  targetUserId: string;
  snapshot: Record<string, unknown>;
}

const mediaUrl = (uploadId: string) => `/v1/media/${uploadId}`;

function isUniqueViolation(e: unknown): boolean {
  return e instanceof Error && /UNIQUE constraint failed/i.test(e.message);
}

/** Loads the target as the reporter may see it; 404 when it is missing or not visible to the reporter. */
async function resolveTarget(db: D1Database, me: string, type: TargetType, id: string): Promise<ResolvedTarget> {
  switch (type) {
    case "user": {
      // A profile answers 200 even across a block (api.md 5.2), so existence is enough.
      const u = await first<UserRow>(db, "SELECT * FROM users WHERE id = ?1", id);
      if (u === null) throw err.notFound("user");
      return {
        targetUserId: u.id,
        snapshot: {
          handle: u.handle,
          display_name: u.display_name,
          bio: u.bio,
          avatar_url: u.avatar_upload_id === null ? null : mediaUrl(u.avatar_upload_id),
        },
      };
    }
    case "message": {
      const m = await first<MessageRow>(db, "SELECT * FROM messages WHERE id = ?1 AND deleted_at IS NULL", id);
      if (m === null) throw err.notFound("message");
      try {
        await requireChannelRead(db, m.channel_id, me);
      } catch (e) {
        if (e instanceof ApiError && e.status === 404) throw err.notFound("message");
        throw e;
      }
      return { targetUserId: m.author_id, snapshot: { body: m.body, author_id: m.author_id, channel_id: m.channel_id, kind: m.kind } };
    }
    case "post": {
      const p = await first<PostRow>(db, "SELECT * FROM posts WHERE id = ?1 AND deleted_at IS NULL", id);
      if (p === null || !(await canViewPost(db, me, p))) throw err.notFound("post");
      const media = await all<{ upload_id: string }>(
        db,
        "SELECT upload_id FROM post_media WHERE post_id = ?1 ORDER BY position",
        p.id,
      );
      return {
        targetUserId: p.author_id,
        snapshot: { body: p.body, author_id: p.author_id, media_urls: media.map((r) => mediaUrl(r.upload_id)) },
      };
    }
    case "story": {
      const s = await first<StoryRow>(
        db,
        "SELECT * FROM stories WHERE id = ?1 AND deleted_at IS NULL AND expires_at > ?2",
        id,
        now(),
      );
      if (s === null || (await canViewUserContent(db, me, s.author_id)) !== "ok") throw err.notFound("story");
      return {
        targetUserId: s.author_id,
        snapshot: { body: s.body, author_id: s.author_id, media_urls: s.media_upload_id === null ? [] : [mediaUrl(s.media_upload_id)] },
      };
    }
    case "guestbook_entry": {
      const g = await first<GuestbookRow>(db, "SELECT * FROM guestbook_entries WHERE id = ?1 AND deleted_at IS NULL", id);
      if (g === null || (await canViewUserContent(db, me, g.owner_id)) !== "ok") throw err.notFound("guestbook entry");
      if (g.is_secret === 1 && me !== g.owner_id && me !== g.author_id) throw err.notFound("guestbook entry");
      return { targetUserId: g.author_id, snapshot: { body: g.body, author_id: g.author_id, owner_id: g.owner_id } };
    }
    case "space": {
      // getSpaceRole is null for non-members and for deleted spaces.
      if ((await getSpaceRole(db, id, me)) === null) throw err.notFound("space");
      const s = await first<SpaceRow>(db, "SELECT * FROM spaces WHERE id = ?1 AND deleted_at IS NULL", id);
      if (s === null) throw err.notFound("space");
      return { targetUserId: s.owner_id, snapshot: { name: s.name, description: s.description, owner_id: s.owner_id } };
    }
  }
}

export const reportRoutes = new Hono<AppEnv>();
reportRoutes.use("/reports", requireAuth);

/** 201 { report_id }; 409 already_exists when I already have an open report on the same target. */
reportRoutes.post("/reports", limit("RL_SOCIAL"), vJson(CreateBody), async (c) => {
  const me = getAuth(c).userId;
  const db = c.env.DB;
  const b = c.req.valid("json");
  const target = await resolveTarget(db, me, b.target_type, b.target_id);

  const open = await first(
    db,
    "SELECT 1 AS x FROM reports WHERE reporter_id = ?1 AND target_type = ?2 AND target_id = ?3 AND status = 'open'",
    me,
    b.target_type,
    b.target_id,
  );
  if (open !== null) throw err.conflict("already_exists");

  const t = now();
  const id = newId(t);
  try {
    await run(
      db,
      `INSERT INTO reports (id, reporter_id, target_type, target_id, target_user_id, reason, note, snapshot, status, created_at)
       VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, 'open', ?9)`,
      id,
      me,
      b.target_type,
      b.target_id,
      target.targetUserId,
      b.reason,
      b.note ?? "",
      JSON.stringify(target.snapshot),
      t,
    );
  } catch (e) {
    // uq_reports_open: a concurrent duplicate won the race.
    if (isUniqueViolation(e)) throw err.conflict("already_exists");
    throw e;
  }
  return c.json({ report_id: id }, 201);
});
