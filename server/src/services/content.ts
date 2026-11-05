// Operator forced content removal (api.md 5.22 POST /v1/admin/content/remove, server-architecture.md 4.14 content.ts,
// plan 02 Task 15). Messages and posts go through their own delete services (asAdmin), the rest is handled here per
// target kind. Every removal deletes the attached images from R2 at once and emits the same events as the ordinary
// delete. The closing batch writes the admin_actions row and, when a report is given, resolves that report and every
// other open report on the same target as resolved/content_removed.
import type { Runtime } from "../env";
import { all, batch, first, stmt } from "../lib/db";
import { err } from "../lib/errors";
import { newId } from "../lib/ids";
import { publishEvent } from "../lib/realtime";
import { serializeAdminActions } from "../lib/serialize";
import { now } from "../lib/time";
import { removeUploads } from "../lib/uploads";
import type { AdminAction } from "../types/api";
import type { AdminActionRow, GuestbookRow, ReportRow, SpaceRow, StoryRow, UploadRow } from "../types/rows";
import { deleteMessage } from "./messages";
import { deletePost } from "./posts";
import { errFields } from "../lib/log";

export type RemovableType = "message" | "post" | "story" | "guestbook_entry" | "upload" | "space";

/**
 * Resolves report `reportId` and every other open report on (targetType, targetId) in one statement. `id = ?1` is the
 * anchor: for an `upload` removal the target type differs from the report's own target kind. `no_action` dismisses,
 * any other resolution resolves. Already closed reports are left as they are.
 */
export function resolveReports(
  db: D1Database,
  reportId: string,
  targetType: string,
  targetId: string,
  adminId: string,
  resolution: NonNullable<ReportRow["resolution"]>,
  note: string,
): D1PreparedStatement {
  return stmt(
    db,
    `UPDATE reports
        SET status = ?4, resolution = ?5, resolved_by = ?6, resolved_at = ?7, resolution_note = ?8
      WHERE status = 'open' AND (id = ?1 OR (target_type = ?2 AND target_id = ?3))`,
    reportId,
    targetType,
    targetId,
    resolution === "no_action" ? "dismissed" : "resolved",
    resolution,
    adminId,
    now(),
    note,
  );
}

interface Step {
  /** Writes committed together with the admin_actions row (closing batch). */
  stmts: D1PreparedStatement[];
  /** Runs after the closing batch committed, with its results for `stmts` (same order). */
  after?: (results: D1Result[]) => Promise<void>;
}

const storyDeleted = (rt: Runtime, authorId: string, storyId: string) =>
  publishEvent(rt, { followers: authorId }, "story.deleted", { user_id: authorId, story_id: storyId }, { ephemeral: true, includeSelf: authorId });

async function removeUploadsLogged(rt: Runtime, ids: string[], what: string): Promise<void> {
  try {
    await removeUploads(rt.env, ids);
  } catch (e) {
    console.error("content: media removal failed", { what, err: errFields(e) }); // rows stay non-removed; retried by cleanup
  }
}

async function removeStory(rt: Runtime, id: string): Promise<Step> {
  const db = rt.env.DB;
  // An expired story the cleanup cron has not deleted yet is still removable.
  const s = await first<StoryRow>(db, "SELECT * FROM stories WHERE id = ?1 AND deleted_at IS NULL", id);
  if (s === null) throw err.notFound("story");
  return {
    stmts: [stmt(db, "UPDATE stories SET deleted_at = ?2 WHERE id = ?1 AND deleted_at IS NULL RETURNING id", s.id, now())],
    after: async ([res]) => {
      if (res!.results.length === 0) return; // a concurrent delete won; it does the rest
      if (s.media_upload_id !== null) await removeUploadsLogged(rt, [s.media_upload_id], `story:${s.id}`);
      await storyDeleted(rt, s.author_id, s.id);
    },
  };
}

async function removeGuestbookEntry(rt: Runtime, adminId: string, id: string): Promise<Step> {
  const db = rt.env.DB;
  const g = await first<GuestbookRow>(db, "SELECT * FROM guestbook_entries WHERE id = ?1 AND deleted_at IS NULL", id);
  if (g === null) throw err.notFound("guestbook entry");
  return {
    stmts: [
      stmt(
        db,
        "UPDATE guestbook_entries SET body = '', deleted_at = ?2, deleted_by = ?3 WHERE id = ?1 AND deleted_at IS NULL",
        g.id,
        now(),
        adminId,
      ),
    ],
  };
}

/**
 * One image: R2 object deleted and the upload marked removed first (a failure leaves nothing half done), then detached
 * from where it hangs: post -> its post_media row goes; avatar -> users.avatar_upload_id = NULL; story -> a story with
 * no body is soft-deleted (the CHECK needs body or media, so media_upload_id stays), otherwise media_upload_id = NULL.
 */
async function removeUpload(rt: Runtime, id: string): Promise<Step> {
  const db = rt.env.DB;
  const u = await first<UploadRow>(db, "SELECT * FROM uploads WHERE id = ?1 AND status <> 'removed'", id);
  if (u === null) throw err.notFound("upload");
  await removeUploads(rt.env, [u.id]);

  const sep = u.attached_to?.indexOf(":") ?? -1;
  const kind = sep < 0 ? null : u.attached_to!.slice(0, sep);
  const ref = sep < 0 ? "" : u.attached_to!.slice(sep + 1);
  switch (kind) {
    case "post":
      return { stmts: [stmt(db, "DELETE FROM post_media WHERE upload_id = ?1", u.id)] };
    case "avatar":
      return { stmts: [stmt(db, "UPDATE users SET avatar_upload_id = NULL WHERE id = ?1 AND avatar_upload_id = ?2", ref, u.id)] };
    case "story": {
      const t = now();
      return {
        stmts: [
          stmt(
            db,
            `UPDATE stories SET deleted_at = ?3
              WHERE id = ?1 AND media_upload_id = ?2 AND body = '' AND deleted_at IS NULL
              RETURNING id, author_id`,
            ref,
            u.id,
            t,
          ),
          stmt(db, "UPDATE stories SET media_upload_id = NULL WHERE id = ?1 AND media_upload_id = ?2 AND body <> ''", ref, u.id),
        ],
        after: async ([deleted]) => {
          const row = (deleted!.results as Array<{ id: string; author_id: string }>)[0];
          if (row !== undefined) await storyDeleted(rt, row.author_id, row.id);
        },
      };
    }
    default:
      return { stmts: [] }; // still pending: nothing to detach
  }
}

/**
 * Space and its channels soft-deleted in the closing batch. The {space} audience drops deleted spaces, so the member
 * ids are read first and space.deleted goes to {users} after the commit (same as DELETE /spaces/{id}).
 */
async function removeSpace(rt: Runtime, id: string): Promise<Step> {
  const db = rt.env.DB;
  const s = await first<SpaceRow>(db, "SELECT * FROM spaces WHERE id = ?1 AND deleted_at IS NULL", id);
  if (s === null) throw err.notFound("space");
  const memberIds = (await all<{ user_id: string }>(db, "SELECT user_id FROM space_members WHERE space_id = ?1", s.id)).map((r) => r.user_id);
  const t = now();
  return {
    // channels must directly follow spaces: changes() is the row count of the previous statement
    stmts: [
      stmt(db, "UPDATE spaces SET deleted_at = ?2, updated_at = ?2 WHERE id = ?1 AND deleted_at IS NULL", s.id, t),
      stmt(db, "UPDATE channels SET deleted_at = ?2, updated_at = ?2 WHERE space_id = ?1 AND deleted_at IS NULL AND changes() > 0", s.id, t),
    ],
    after: async ([res]) => {
      if ((res!.meta.changes ?? 0) === 0) return; // a concurrent delete won; it published
      await publishEvent(rt, { users: memberIds }, "space.deleted", { space_id: s.id });
    },
  };
}

/** Forced removal by a global operator. Missing / already removed target or unknown report -> 404. */
export async function removeContent(
  rt: Runtime,
  adminId: string,
  target: { type: RemovableType; id: string },
  reason: string,
  reportId?: string,
): Promise<AdminAction> {
  const db = rt.env.DB;
  // Checked before anything is removed: admin_actions.report_id references reports(id).
  if (reportId !== undefined && (await first(db, "SELECT 1 AS x FROM reports WHERE id = ?1", reportId)) === null) {
    throw err.notFound("report");
  }

  let step: Step;
  switch (target.type) {
    case "message":
      await deleteMessage(rt, target.id, adminId, { asAdmin: true });
      step = { stmts: [] };
      break;
    case "post":
      await deletePost(rt, target.id, adminId, { asAdmin: true });
      step = { stmts: [] };
      break;
    case "story":
      step = await removeStory(rt, target.id);
      break;
    case "guestbook_entry":
      step = await removeGuestbookEntry(rt, adminId, target.id);
      break;
    case "upload":
      step = await removeUpload(rt, target.id);
      break;
    case "space":
      step = await removeSpace(rt, target.id);
      break;
  }

  const t = now();
  const row: AdminActionRow = {
    id: newId(t),
    admin_id: adminId,
    action: "remove_content",
    target_type: target.type,
    target_id: target.id,
    report_id: reportId ?? null,
    reason,
    created_at: t,
  };
  const closing = [
    ...step.stmts,
    stmt(
      db,
      `INSERT INTO admin_actions (id, admin_id, action, target_type, target_id, report_id, reason, created_at)
       VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8)`,
      row.id,
      row.admin_id,
      row.action,
      row.target_type,
      row.target_id,
      row.report_id,
      row.reason,
      row.created_at,
    ),
  ];
  if (reportId !== undefined) closing.push(resolveReports(db, reportId, target.type, target.id, adminId, "content_removed", reason));
  const results = await batch(db, closing);
  if (step.after) await step.after(results.slice(0, step.stmts.length));

  const [action] = await serializeAdminActions(db, [row]);
  return action!;
}
