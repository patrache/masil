// Room documents (v0.1.3 C1.4, api.md 5.24): list / create in a space, read / rename / lock / delete, and the
// document socket GET /v1/docs/{doc_id}/ws (docs/contract/collab.md 2) handed to the doc's CollabHub. Writes take
// RL_DOC; the socket upgrade takes RL_WS. Not a member of a live space -> 404 everywhere.
import { Hono } from "hono";
import * as z from "zod";
import { rt, type AppEnv } from "../env";
import { getAuth, requireAuth } from "../lib/auth";
import { parseLimit } from "../lib/db";
import { err } from "../lib/errors";
import { idParam } from "../lib/params";
import { limit } from "../lib/ratelimit";
import { vJson, vQuery, zText } from "../lib/validate";
import {
  collabHub,
  createComment,
  createDoc,
  deleteComment,
  createVersion,
  deleteDoc,
  getVersion,
  listComments,
  listDocs,
  listVersions,
  loadDocFor,
  patchComment,
  patchDoc,
  restoreVersion,
  serializeDocs,
} from "../services/docs";

const DocTypeSchema = z.enum(["arch", "apicol"]);

const ListQuery = z.object({
  type: DocTypeSchema.optional(),
  cursor: z.string().optional(),
  limit: z.string().optional(),
});

const CreateBody = z.object({
  type: DocTypeSchema,
  title: zText(1, 80),
  snapshot: z.unknown().optional(), // collab.md Snapshot; shape and limits are checked by the CollabHub
});

const PatchBody = z.object({
  title: zText(1, 80).optional(),
  locked: z.boolean().optional(),
});

const VersionBody = z.object({ name: zText(1, 60) });

const CommentListQuery = z.object({ cursor: z.string().optional(), limit: z.string().optional() });

const CommentBody = z.object({
  anchor: zText(1, 128),
  body: zText(1, 2000),
  root_id: z.string().nullable().optional(),
});

const CommentPatch = z.object({
  body: zText(1, 2000).optional(),
  resolved: z.boolean().optional(),
});

/** A version number path segment: a positive integer, else 404 (as for an unknown version). */
function versionNo(raw: string): number {
  const n = /^[1-9][0-9]{0,8}$/.test(raw) ? Number(raw) : NaN;
  if (!Number.isSafeInteger(n)) throw err.notFound("version");
  return n;
}

export const docRoutes = new Hono<AppEnv>();

docRoutes.get("/spaces/:space_id/docs", requireAuth, vQuery(ListQuery), async (c) => {
  const q = c.req.valid("query");
  const page = await listDocs(c.env.DB, idParam(c, "space_id", "space"), getAuth(c).userId, {
    type: q.type ?? null,
    cursor: q.cursor,
    limit: parseLimit(q.limit),
  });
  return c.json(page);
});

docRoutes.post("/spaces/:space_id/docs", requireAuth, limit("RL_DOC"), vJson(CreateBody), async (c) => {
  const doc = await createDoc(rt(c), idParam(c, "space_id", "space"), getAuth(c).userId, c.req.valid("json"));
  return c.json({ doc }, 201);
});

docRoutes.get("/docs/:doc_id", requireAuth, async (c) => {
  const { row } = await loadDocFor(c.env.DB, idParam(c, "doc_id", "doc"), getAuth(c).userId);
  const [doc] = await serializeDocs(c.env.DB, [row]);
  if (!doc) throw err.notFound("doc");
  return c.json({ doc });
});

docRoutes.patch("/docs/:doc_id", requireAuth, limit("RL_DOC"), vJson(PatchBody), async (c) => {
  const doc = await patchDoc(rt(c), idParam(c, "doc_id", "doc"), getAuth(c).userId, c.req.valid("json"));
  return c.json({ doc });
});

docRoutes.delete("/docs/:doc_id", requireAuth, limit("RL_DOC"), async (c) => {
  await deleteDoc(rt(c), idParam(c, "doc_id", "doc"), getAuth(c).userId);
  return c.body(null, 204);
});

// ---- versions (api.md 5.24 버전 규칙) ----

docRoutes.get("/docs/:doc_id/versions", requireAuth, async (c) => {
  const items = await listVersions(c.env.DB, idParam(c, "doc_id", "doc"), getAuth(c).userId);
  return c.json({ items });
});

docRoutes.post("/docs/:doc_id/versions", requireAuth, limit("RL_DOC"), vJson(VersionBody), async (c) => {
  const version = await createVersion(rt(c), idParam(c, "doc_id", "doc"), getAuth(c).userId, c.req.valid("json").name);
  return c.json({ version }, 201);
});

docRoutes.get("/docs/:doc_id/versions/:no", requireAuth, async (c) => {
  const docId = idParam(c, "doc_id", "doc");
  return c.json(await getVersion(rt(c), docId, getAuth(c).userId, versionNo(c.req.param("no"))));
});

docRoutes.post("/docs/:doc_id/versions/:no/restore", requireAuth, limit("RL_DOC"), async (c) => {
  const docId = idParam(c, "doc_id", "doc");
  return c.json(await restoreVersion(rt(c), docId, getAuth(c).userId, versionNo(c.req.param("no"))));
});

// ---- comments (api.md 5.24, P0 Task 29) ----

docRoutes.get("/docs/:doc_id/comments", requireAuth, vQuery(CommentListQuery), async (c) => {
  const q = c.req.valid("query");
  return c.json(await listComments(c.env.DB, idParam(c, "doc_id", "doc"), getAuth(c).userId, { cursor: q.cursor, limit: parseLimit(q.limit) }));
});

docRoutes.post("/docs/:doc_id/comments", requireAuth, limit("RL_DOC"), vJson(CommentBody), async (c) => {
  const comment = await createComment(rt(c), idParam(c, "doc_id", "doc"), getAuth(c).userId, c.req.valid("json"));
  return c.json({ comment }, 201);
});

docRoutes.patch("/docs/:doc_id/comments/:comment_id", requireAuth, limit("RL_DOC"), vJson(CommentPatch), async (c) => {
  const docId = idParam(c, "doc_id", "doc");
  const comment = await patchComment(rt(c), docId, idParam(c, "comment_id", "comment"), getAuth(c).userId, c.req.valid("json"));
  return c.json({ comment });
});

docRoutes.delete("/docs/:doc_id/comments/:comment_id", requireAuth, limit("RL_DOC"), async (c) => {
  await deleteComment(rt(c), idParam(c, "doc_id", "doc"), idParam(c, "comment_id", "comment"), getAuth(c).userId);
  return c.body(null, 204);
});

/**
 * collab.md 2: requireAuth -> RL_WS -> member of the live space (404 before the upgrade otherwise) -> the hub gets the
 * upgrade with X-Masil-Doc-Auth set by us (the global middleware already dropped any client copy).
 */
docRoutes.get("/docs/:doc_id/ws", requireAuth, limit("RL_WS"), async (c) => {
  const upgrade = c.req.header("Upgrade");
  if ((upgrade ?? "").toLowerCase() !== "websocket") throw err.invalid({ upgrade: upgrade === undefined ? "required" : "invalid" });
  const auth = getAuth(c);
  const docId = idParam(c, "doc_id", "doc");
  const { row, role } = await loadDocFor(c.env.DB, docId, auth.userId);
  const headers = new Headers(c.req.raw.headers);
  headers.delete("X-Masil-Doc-Auth");
  headers.set(
    "X-Masil-Doc-Auth",
    JSON.stringify({ userId: auth.userId, sessionId: auth.sessionId, role, docId, docType: row.type, locked: row.locked === 1 }),
  );
  return collabHub(rt(c), docId).fetch(new Request(c.req.raw.url, { method: "GET", headers }));
});
