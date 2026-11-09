// Room documents (v0.1.3 C1.4, api.md 5.24, data-model.md 3.13). D1 holds the metadata (collab_docs); a document's
// content lives in its CollabHub DO ("doc:<id>"), and so do the version bodies (D1 collab_versions = their metadata). Every document path answers 404 to anyone who is not a member of a
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
import type { Doc, DocType, DocVersion } from "../types/api";
import type { CollabDocRow, CollabVersionRow } from "../types/rows";
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
 * PATCH /v1/docs/{doc_id}: title by any member (creator / owner / admin while locked); locked by creator / owner / admin (else 403 forbidden). A change
 * publishes doc.updated{deleted:false} to the space and, for `locked`, tells the CollabHub.
 */
export async function patchDoc(rt: Runtime, docId: string, userId: string, input: { title?: string; locked?: boolean }): Promise<Doc> {
  const db = rt.env.DB;
  const { row, role } = await loadDocFor(db, docId, userId);
  if (input.locked !== undefined && !canManageDoc(role)) throw err.forbidden("forbidden", "only the creator or a space owner / admin can lock");
  // a locked doc is edited (title included) only by its creator / owner / admin (api.md 5.24)
  if (input.title !== undefined && row.locked === 1 && !canManageDoc(role)) throw err.forbidden("forbidden", "document is locked");
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

// ---------------------------------------------------------------- versions (api.md 5.24 버전 규칙, P0 Task 28)

async function serializeVersions(db: D1Database, rows: CollabVersionRow[]): Promise<DocVersion[]> {
  const users = await loadUserSummaries(db, rows.flatMap((r) => (r.created_by === null ? [] : [r.created_by])));
  return rows.map((r) => ({
    no: r.no,
    seq: r.seq,
    name: r.name,
    auto: r.auto === 1,
    restored_from: r.restored_from,
    created_by: r.created_by === null ? null : (users.get(r.created_by) ?? null),
    created_at: iso(r.created_at),
  }));
}

async function versionRow(db: D1Database, docId: string, no: number): Promise<CollabVersionRow> {
  const row = await first<CollabVersionRow>(db, "SELECT * FROM collab_versions WHERE doc_id = ?1 AND no = ?2", docId, no);
  if (row === null) throw err.notFound("version");
  return row;
}

/** GET /v1/docs/{doc_id}/versions: newest first; named ones plus the record versions retention left (<= 300). */
export async function listVersions(db: D1Database, docId: string, userId: string): Promise<DocVersion[]> {
  await loadDocFor(db, docId, userId);
  const rows = await all<CollabVersionRow>(db, "SELECT * FROM collab_versions WHERE doc_id = ?1 ORDER BY no DESC", docId);
  return serializeVersions(db, rows);
}

/** GET /v1/docs/{doc_id}/versions/{no}: the version and its snapshot; unknown or pruned -> 404. */
export async function getVersion(rt: Runtime, docId: string, userId: string, no: number): Promise<{ version: DocVersion; snapshot: unknown }> {
  const db = rt.env.DB;
  await loadDocFor(db, docId, userId);
  const row = await versionRow(db, docId, no);
  const body = await collabHub(rt, docId).versionBody(no);
  if (body === null) throw err.notFound("version");
  const [version] = await serializeVersions(db, [row]);
  return { version: version!, snapshot: JSON.parse(body) };
}

/** POST /v1/docs/{doc_id}/versions {name}: a named version of the head; the 101st -> 422 limit_exceeded named_versions. */
export async function createVersion(rt: Runtime, docId: string, userId: string, name: string): Promise<DocVersion> {
  const db = rt.env.DB;
  const { row } = await loadDocFor(db, docId, userId);
  const r = await collabHub(rt, docId).createVersion(docId, row.type, name, userId);
  if ("error" in r) throw err.limit("named_versions", 100);
  const [version] = await serializeVersions(db, [await versionRow(db, docId, r.no)]);
  return version!;
}

/**
 * POST /v1/docs/{doc_id}/versions/{no}/restore: a locked doc only by creator / owner / admin (403). The hub sequences a
 * reset to every socket and records a restore version (restored_from = no); D1 head_seq follows right away.
 */
export async function restoreVersion(rt: Runtime, docId: string, userId: string, no: number): Promise<{ doc: Doc; version: DocVersion }> {
  const db = rt.env.DB;
  const { row, role } = await loadDocFor(db, docId, userId);
  if (row.locked === 1 && !canManageDoc(role)) throw err.forbidden("forbidden", "the document is locked");
  await versionRow(db, docId, no);
  const r = await collabHub(rt, docId).restoreVersion(docId, row.type, no, userId);
  if ("error" in r) {
    if (r.error === "not_found") throw err.notFound("version");
    throw new Error(`restore: hub refused version ${no}: ${r.error}`);
  }
  const t = Math.max(now(), row.updated_at);
  const updated = await first<CollabDocRow>(
    db,
    "UPDATE collab_docs SET head_seq = MAX(head_seq, ?2), updated_at = MAX(updated_at, ?3) WHERE id = ?1 AND deleted_at IS NULL RETURNING *",
    docId,
    r.seq,
    t,
  );
  const [version] = await serializeVersions(db, [await versionRow(db, docId, r.no)]);
  return { doc: await serializeDoc(db, updated ?? row), version: version! };
}

// ---------------------------------------------------------------- open sockets (collab.md 4.1, P0 Task 28)

async function eachHub(rt: Runtime, docIds: string[], what: string, call: (hub: ReturnType<typeof collabHub>) => Promise<void>): Promise<void> {
  await Promise.all(
    docIds.map(async (id) => {
      try {
        await call(collabHub(rt, id));
      } catch (e) {
        console.error({ msg: "docs: CollabHub call failed", call: what, doc_id: id, err: errFields(e) }); // D1 is the truth (collab.md 4.1)
      }
    }),
  );
}

async function liveDocIdsOfSpace(db: D1Database, spaceId: string): Promise<string[]> {
  return (await all<{ id: string }>(db, "SELECT id FROM collab_docs WHERE space_id = ?1 AND deleted_at IS NULL", spaceId)).map((r) => r.id);
}

/**
 * After a removal / space delete committed (callers wrap it in rt.waitUntil): `kicked{reason}` + 4100 on the user's
 * sockets of every live doc in the space — or on every socket (userId null, space_deleted; those hubs then refuse new
 * connections).
 */
export async function kickFromSpaceDocs(rt: Runtime, spaceId: string, userId: string | null, reason: "left" | "kicked" | "banned" | "space_deleted"): Promise<void> {
  const ids = await liveDocIdsOfSpace(rt.env.DB, spaceId);
  await eachHub(rt, ids, "kick", (hub) => (userId === null ? hub.closeAll(reason) : hub.kick(userId, reason)));
}

/**
 * After services/users.ts revokeSessions (callers wrap it in rt.waitUntil): closes the user's document sockets of
 * those sessions with `code` (4001, 4003 for a suspension) in every live doc of the spaces they are a member of.
 */
export async function closeDocSessions(rt: Runtime, userId: string, sessionIds: string[] | "all", code: 4001 | 4003): Promise<void> {
  const ids = (
    await all<{ id: string }>(
      rt.env.DB,
      "SELECT d.id FROM collab_docs d JOIN space_members m ON m.space_id = d.space_id AND m.user_id = ?1 WHERE d.deleted_at IS NULL",
      userId,
    )
  ).map((r) => r.id);
  await eachHub(rt, ids, "revokeSession", (hub) => hub.revokeSession(userId, sessionIds, code));
}

/** After a space role change committed (member role / ownership transfer): the user's doc sockets in that space follow. */
export async function setDocRole(rt: Runtime, spaceId: string, userId: string, role: "admin" | "member"): Promise<void> {
  const ids = await liveDocIdsOfSpace(rt.env.DB, spaceId);
  await eachHub(rt, ids, "setRole", (hub) => hub.setRole(userId, role));
}
