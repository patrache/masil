// Global operator routes (api.md 5.22, plan 02 Task 15-16). Every /admin/* request needs role='admin' (403 not_admin).
// `/admin` is a prefix only this module uses, so the auth middleware is attached with .use (plan 02 Global Constraints).
import { Hono } from "hono";
import * as z from "zod";
import { rt, type AppEnv } from "../env";
import { getAuth, requireAdmin, requireAuth, resolveUserRef } from "../lib/auth";
import { all, batch, decodeCursor, encodeCursor, first, parseLimit, stmt } from "../lib/db";
import { err } from "../lib/errors";
import { isId, newId } from "../lib/ids";
import { serializeAdminActions, serializeAdminUsers, serializeReports } from "../lib/serialize";
import { now } from "../lib/time";
import { vJson, vQuery, zId, zText } from "../lib/validate";
import { removeContent, resolveReports } from "../services/content";
import { suspendUser, unsuspendUser } from "../services/users";
import type { Report } from "../types/api";
import type { AdminActionRow, ReportRow, UserRow } from "../types/rows";

/** Operator-entered reason / note length (no api.md limit; kept bounded). */
export const ADMIN_REASON_MAX = 1000;

const RemoveBody = z.object({
  target_type: z.enum(["message", "post", "story", "guestbook_entry", "upload", "space"]),
  target_id: zId,
  reason: zText(0, ADMIN_REASON_MAX),
  report_id: zId.optional(),
});

const PageQuery = { cursor: z.string().optional(), limit: z.string().optional() };

const ReportsQuery = z.object({
  ...PageQuery,
  status: z.enum(["open", "resolved", "dismissed"]).optional(),
  target_type: z.enum(["user", "message", "post", "story", "guestbook_entry", "space"]).optional(),
});

const ResolveBody = z.object({
  resolution: z.enum(["content_removed", "user_suspended", "no_action"]),
  note: zText(0, ADMIN_REASON_MAX).optional(),
});

/** Longest timed suspension. "Forever" is null; an unbounded value would push suspended_until past the Date range. */
const SUSPEND_MAX_SEC = 10 * 365 * 86_400;

/** api.md 5.22: duration_sec null / omitted = indefinite. */
const SuspendBody = z.object({
  reason: zText(0, ADMIN_REASON_MAX),
  duration_sec: z.number().int().min(1).max(SUSPEND_MAX_SEC).nullable().optional(),
  report_id: zId.optional(),
});

const UsersQuery = z.object({
  ...PageQuery,
  q: z.string().optional(),
  suspended: z.enum(["true", "false"]).optional(),
});

const ActionsQuery = z.object({
  ...PageQuery,
  target_type: z.string().min(1).optional(),
  target_id: zId.optional(),
});

/** ID-keyed cursor {k: id, i: id}; anything else is invalid. */
function idCursor(raw: string | undefined): string | null {
  const c = decodeCursor(raw);
  if (c === null) return null;
  if (typeof c.k !== "string" || c.k !== c.i || !isId(c.i)) throw err.invalid({ cursor: "invalid" });
  return c.i;
}

function nextIdCursor<T extends { id: string }>(rows: T[], n: number): string | null {
  const last = rows[n - 1];
  return rows.length > n && last !== undefined ? encodeCursor({ k: last.id, i: last.id }) : null;
}

/** Escapes LIKE wildcards for `... LIKE ? ESCAPE '\'`. */
function likeEscape(s: string): string {
  return s.replace(/[\\%_]/g, (ch) => `\\${ch}`);
}

async function loadReport(db: D1Database, id: string): Promise<ReportRow> {
  const row = isId(id) ? await first<ReportRow>(db, "SELECT * FROM reports WHERE id = ?1", id) : null;
  if (row === null) throw err.notFound("report");
  return row;
}

async function reportView(db: D1Database, row: ReportRow): Promise<Report> {
  const [report] = await serializeReports(db, [row]);
  if (report === undefined) throw err.notFound("report"); // reporter gone
  return report;
}

export const adminRoutes = new Hono<AppEnv>();
adminRoutes.use("/admin/*", requireAuth, requireAdmin);

/** 200 { action: AdminAction }; the removal emits the ordinary delete events. Missing target / report -> 404. */
adminRoutes.post("/admin/content/remove", vJson(RemoveBody), async (c) => {
  const b = c.req.valid("json");
  const action = await removeContent(rt(c), getAuth(c).userId, { type: b.target_type, id: b.target_id }, b.reason, b.report_id);
  return c.json({ action });
});

/** Oldest first (ID ascending, idx_reports_status); status defaults to open; optional target_type filter. */
adminRoutes.get("/admin/reports", vQuery(ReportsQuery), async (c) => {
  const db = c.env.DB;
  const qy = c.req.valid("query");
  const n = parseLimit(qy.limit, 30, 100);
  const after = idCursor(qy.cursor);
  const rows = await all<ReportRow>(
    db,
    `SELECT * FROM reports
      WHERE status = ?1 AND (?2 IS NULL OR target_type = ?2) AND (?3 IS NULL OR id > ?3)
      ORDER BY id ASC
      LIMIT ${n + 1}`,
    qy.status ?? "open",
    qy.target_type ?? null,
    after,
  );
  const page = rows.slice(0, n);
  return c.json({ items: await serializeReports(db, page), next_cursor: nextIdCursor(rows, n) });
});

adminRoutes.get("/admin/reports/:report_id", async (c) => {
  const db = c.env.DB;
  return c.json({ report: await reportView(db, await loadReport(db, c.req.param("report_id"))) });
});

/**
 * no_action -> dismissed + dismiss_report, otherwise resolved + resolve_report; every other open report on the same
 * target gets the same outcome. Only marks reports: removal / suspension are their own endpoints. A report that is
 * no longer open -> 409 conflict.
 */
adminRoutes.post("/admin/reports/:report_id/resolve", vJson(ResolveBody), async (c) => {
  const db = c.env.DB;
  const adminId = getAuth(c).userId;
  const b = c.req.valid("json");
  const report = await loadReport(db, c.req.param("report_id"));
  if (report.status !== "open") throw err.conflict("conflict", "report already closed");
  const note = b.note ?? "";
  const t = now();
  await batch(db, [
    resolveReports(db, report.id, report.target_type, report.target_id, adminId, b.resolution, note),
    stmt(
      db,
      `INSERT INTO admin_actions (id, admin_id, action, target_type, target_id, report_id, reason, created_at)
       VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8)`,
      newId(t),
      adminId,
      b.resolution === "no_action" ? "dismiss_report" : "resolve_report",
      report.target_type,
      report.target_id,
      report.id,
      note,
      t,
    ),
  ]);
  return c.json({ report: await reportView(db, await loadReport(db, report.id)) });
});

/** 200 { user: AdminUserView }; operator target -> 409 conflict. Sessions revoked, sockets closed with 4003. */
adminRoutes.post("/admin/users/:user/suspend", vJson(SuspendBody), async (c) => {
  const auth = getAuth(c);
  const b = c.req.valid("json");
  const target = await resolveUserRef(c.env.DB, c.req.param("user"), auth);
  const user = await suspendUser(rt(c), auth.userId, target.id, b.reason, b.duration_sec ?? null, b.report_id);
  return c.json({ user });
});

adminRoutes.post("/admin/users/:user/unsuspend", async (c) => {
  const auth = getAuth(c);
  const target = await resolveUserRef(c.env.DB, c.req.param("user"), auth);
  return c.json({ user: await unsuspendUser(rt(c), auth.userId, target.id) });
});

/**
 * q = handle prefix (a leading "@" is ignored, case-insensitive) or display_name substring; suspended=true|false =
 * suspended right now or not. Newest first (ID descending).
 */
adminRoutes.get("/admin/users", vQuery(UsersQuery), async (c) => {
  const db = c.env.DB;
  const qy = c.req.valid("query");
  const n = parseLimit(qy.limit, 30, 100);
  const before = idCursor(qy.cursor);
  const q = (qy.q ?? "").trim();
  const handleQ = (q.startsWith("@") ? q.slice(1) : q).toLowerCase();
  const suspended = qy.suspended === undefined ? null : qy.suspended === "true" ? 1 : 0;
  const rows = await all<UserRow>(
    db,
    `SELECT * FROM users u
      WHERE (?1 = '' OR u.handle LIKE ?2 ESCAPE '\\' OR u.display_name LIKE ?3 ESCAPE '\\')
        AND (?4 IS NULL OR ?4 = (CASE WHEN u.suspended_at IS NOT NULL AND (u.suspended_until IS NULL OR u.suspended_until > ?5)
                                      THEN 1 ELSE 0 END))
        AND (?6 IS NULL OR u.id < ?6)
      ORDER BY u.id DESC
      LIMIT ${n + 1}`,
    q,
    `${likeEscape(handleQ)}%`,
    `%${likeEscape(q)}%`,
    suspended,
    now(),
    before,
  );
  const page = rows.slice(0, n);
  return c.json({ items: await serializeAdminUsers(db, page), next_cursor: nextIdCursor(rows, n) });
});

/** Audit log, newest first (ID descending); optional target_type / target_id filters. */
adminRoutes.get("/admin/actions", vQuery(ActionsQuery), async (c) => {
  const db = c.env.DB;
  const qy = c.req.valid("query");
  const n = parseLimit(qy.limit, 30, 100);
  const before = idCursor(qy.cursor);
  const rows = await all<AdminActionRow>(
    db,
    `SELECT * FROM admin_actions
      WHERE (?1 IS NULL OR target_type = ?1) AND (?2 IS NULL OR target_id = ?2) AND (?3 IS NULL OR id < ?3)
      ORDER BY id DESC
      LIMIT ${n + 1}`,
    qy.target_type ?? null,
    qy.target_id ?? null,
    before,
  );
  const page = rows.slice(0, n);
  return c.json({ items: await serializeAdminActions(db, page), next_cursor: nextIdCursor(rows, n) });
});
