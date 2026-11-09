// Room documents (v0.1.3 C1.4, api.md 5.24, data-model.md 3.13). D1 holds the metadata (collab_docs); a document's
// content lives in its CollabHub DO ("doc:<id>"). Every document path answers 404 to anyone who is not a member of a
// live space (docRole === null) — existence is hidden, as for spaces (api.md 3.5).
import type { Runtime } from "../env";
import { all, decodeCursor, encodeCursor, first } from "../lib/db";
import { err } from "../lib/errors";
import { errFields } from "../lib/log";
import { newId } from "../lib/ids";
import { getSpaceRole } from "../lib/permissions";
import { publishEvent } from "../lib/realtime";
import { loadUserSummaries } from "../lib/serialize";
import { iso, now } from "../lib/time";
import type { Doc, DocType } from "../types/api";
import type { CollabDocRow } from "../types/rows";
import type { DocKickReason } from "../do/collab-hub";

export type DocRole = "creator" | "admin" | "member";

/**
 * The user's standing on `doc` (api.md 5.24 권한): null when they are not a member of its space or the space is deleted
 * (soft — collab_docs rows stay, so this is the check that hides them); "creator" for the doc's author; "admin" for the
 * space owner / admin; "member" otherwise. A deleted doc is the caller's check.
 */
export async function docRole(db: D1Database, doc: CollabDocRow, userId: string): Promise<DocRole | null> {
  const role = await getSpaceRole(db, doc.space_id, userId);
  if (role === null) return null;
  if (doc.created_by === userId) return "creator";
  return role === "owner" || role === "admin" ? "admin" : "member";
}

/** creator / owner / admin may lock, unlock, delete — and edit a locked doc. */
export function canManageDoc(role: DocRole): boolean {
  return role === "creator" || role === "admin";
}

/** A live doc the user may see, with their role; 404 otherwise (missing, deleted, not a member, space deleted). */
export async function loadDocFor(db: D1Database, docId: string, userId: string): Promise<{ row: CollabDocRow; role: DocRole }> {
  const row = await first<CollabDocRow>(db, "SELECT * FROM collab_docs WHERE id = ?1 AND deleted_at IS NULL", docId);
  if (row === null) throw err.notFound("doc");
  const role = await docRole(db, row, userId);
  if (role === null) throw err.notFound("doc");
  return { row, role };
}

export async function serializeDocs(db: D1Database, rows: CollabDocRow[]): Promise<Doc[]> {
  const users = await loadUserSummaries(db, rows.map((r) => r.created_by));
  return rows.flatMap((r): Doc[] => {
    const created_by = users.get(r.created_by);
    if (!created_by) return [];
    return [
      {
        id: r.id,
        space_id: r.space_id,
        type: r.type,
        title: r.title,
        created_by,
        created_at: iso(r.created_at),
        updated_at: iso(r.updated_at),
        head_seq: r.head_seq,
        locked: r.locked === 1,
      },
    ];
  });
}

async function serializeDoc(db: D1Database, row: CollabDocRow): Promise<Doc> {
  const [doc] = await serializeDocs(db, [row]);
  if (!doc) throw err.notFound("doc");
  return doc;
}

export function collabHub(rt: Runtime, docId: string) {
  return rt.env.COLLAB_HUB.getByName(`doc:${docId}`);
}

/** Space docs, not deleted, updated_at newest first (id breaks ties); `type` null = both. 404 unless a member. */
export async function listDocs(
  db: D1Database,
  spaceId: string,
  userId: string,
  q: { type: DocType | null; cursor: string | undefined; limit: number },
): Promise<{ items: Doc[]; next_cursor: string | null }> {
  if ((await getSpaceRole(db, spaceId, userId)) === null) throw err.notFound("space");
  const cur = decodeCursor(q.cursor);
  const rows = await all<CollabDocRow>(
    db,
    `SELECT * FROM collab_docs
      WHERE space_id = ?1 AND deleted_at IS NULL AND (?2 IS NULL OR type = ?2)
        AND (?3 IS NULL OR updated_at < ?3 OR (updated_at = ?3 AND id < ?4))
      ORDER BY updated_at DESC, id DESC LIMIT ?5`,
    spaceId,
    q.type,
    cur === null ? null : Number(cur.k),
    cur === null ? "" : cur.i,
    q.limit + 1,
  );
  const page = rows.slice(0, q.limit);
  const last = page[page.length - 1];
  return {
    items: await serializeDocs(db, page),
    next_cursor: rows.length > q.limit && last ? encodeCursor({ k: last.updated_at, i: last.id }) : null,
  };
}

/**
 * POST /v1/spaces/{space_id}/docs: a member creates a doc (404 unless a member). With `snapshot` the CollabHub starts
 * from it (its own shape / limit checks — 400 fields.snapshot). After commit: doc.created to the space.
 */
export async function createDoc(rt: Runtime, spaceId: string, userId: string, input: { type: DocType; title: string; snapshot?: unknown }): Promise<Doc> {
  const db = rt.env.DB;
  if ((await getSpaceRole(db, spaceId, userId)) === null) throw err.notFound("space");
  const t = now();
  const row: CollabDocRow = {
    id: newId(t),
    space_id: spaceId,
    type: input.type,
    title: input.title,
    created_by: userId,
    created_at: t,
    updated_at: t,
    head_seq: 0,
    locked: 0,
    deleted_at: null,
  };
  if (input.snapshot !== undefined && input.snapshot !== null) {
    // the DO validates and stores the first state before the row exists, so a refused snapshot leaves nothing behind
    const r = await collabHub(rt, row.id).init(row.type, input.snapshot);
    if ("error" in r) throw err.invalid({ snapshot: r.error });
    row.head_seq = r.seq;
  }
  const res = await db
    .prepare(
      `INSERT INTO collab_docs (id, space_id, type, title, created_by, created_at, updated_at, head_seq, locked)
       SELECT ?1, ?2, ?3, ?4, ?5, ?6, ?6, ?7, 0
        WHERE EXISTS (SELECT 1 FROM space_members m JOIN spaces s ON s.id = m.space_id AND s.deleted_at IS NULL
                       WHERE m.space_id = ?2 AND m.user_id = ?5)`,
    )
    .bind(row.id, row.space_id, row.type, row.title, row.created_by, t, row.head_seq)
    .run();
  if (res.meta.changes === 0) throw err.notFound("space"); // left / space deleted since the check
  const doc = await serializeDoc(db, row);
  await publishEvent(rt, { space: spaceId }, "doc.created", { doc });
  return doc;
}

/**
 * PATCH /v1/docs/{doc_id}: title by any member; locked by creator / owner / admin (else 403 forbidden). A change
 * publishes doc.updated{deleted:false} to the space and, for `locked`, tells the CollabHub.
 */
export async function patchDoc(rt: Runtime, docId: string, userId: string, input: { title?: string; locked?: boolean }): Promise<Doc> {
  const db = rt.env.DB;
  const { row, role } = await loadDocFor(db, docId, userId);
  if (input.locked !== undefined && !canManageDoc(role)) throw err.forbidden("forbidden", "only the creator or a space owner / admin can lock");
  const next: CollabDocRow = { ...row };
  if (input.title !== undefined) next.title = input.title;
  if (input.locked !== undefined) next.locked = input.locked ? 1 : 0;
  if (next.title === row.title && next.locked === row.locked) return serializeDoc(db, row);
  next.updated_at = Math.max(now(), row.updated_at);
  const res = await db
    .prepare("UPDATE collab_docs SET title = ?2, locked = ?3, updated_at = ?4 WHERE id = ?1 AND deleted_at IS NULL")
    .bind(docId, next.title, next.locked, next.updated_at)
    .run();
  if (res.meta.changes === 0) throw err.notFound("doc");
  if (next.locked !== row.locked) {
    try {
      await collabHub(rt, docId).setLocked(next.locked === 1);
    } catch (e) {
      // D1 is the truth: the next document connection carries the lock to the hub again (collab.md 4.1)
      console.error({ msg: "docs: CollabHub setLocked failed", doc_id: docId, err: errFields(e) });
    }
  }
  const doc = await serializeDoc(db, next);
  await publishEvent(rt, { space: row.space_id }, "doc.updated", { doc, deleted: false });
  return doc;
}

/** DELETE /v1/docs/{doc_id}: creator / owner / admin (else 403). Soft delete, doc.updated{deleted:true}, sockets closed. */
export async function deleteDoc(rt: Runtime, docId: string, userId: string): Promise<void> {
  const db = rt.env.DB;
  const { row, role } = await loadDocFor(db, docId, userId);
  if (!canManageDoc(role)) throw err.forbidden("forbidden", "only the creator or a space owner / admin can delete");
  const res = await db.prepare("UPDATE collab_docs SET deleted_at = ?2 WHERE id = ?1 AND deleted_at IS NULL").bind(docId, now()).run();
  if (res.meta.changes === 0) throw err.notFound("doc"); // a concurrent delete won; it publishes
  const doc = await serializeDoc(db, row);
  await publishEvent(rt, { space: row.space_id }, "doc.updated", { doc, deleted: true });
  closeDocSockets(rt, [docId], "doc_deleted");
}

/** After a cleanup batch committed: `kicked{reason}` + 4100 on every open socket of these docs (background). */
export function closeDocSockets(rt: Runtime, docIds: string[], kickReason: DocKickReason, userId: string | null = null): void {
  if (docIds.length === 0) return;
  const work = Promise.all(
    docIds.map(async (id) => {
      try {
        const hub = collabHub(rt, id);
        if (userId === null) await hub.closeAll(kickReason);
        else await hub.kick(userId, kickReason);
      } catch (e) {
        console.error({ msg: "docs: CollabHub kick failed", doc_id: id, kick_reason: kickReason, err: errFields(e) });
      }
    }),
  );
  rt.waitUntil(work);
}
