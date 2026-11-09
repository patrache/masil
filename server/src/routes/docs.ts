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
import { collabHub, createDoc, deleteDoc, listDocs, loadDocFor, patchDoc, serializeDocs } from "../services/docs";

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
