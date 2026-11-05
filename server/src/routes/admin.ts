// Global operator routes (api.md 5.22, plan 02 Task 15-16). Every /admin/* request needs role='admin' (403 not_admin).
// `/admin` is a prefix only this module uses, so the auth middleware is attached with .use (plan 02 Global Constraints).
import { Hono } from "hono";
import * as z from "zod";
import { rt, type AppEnv } from "../env";
import { getAuth, requireAdmin, requireAuth } from "../lib/auth";
import { vJson, zId, zText } from "../lib/validate";
import { removeContent } from "../services/content";

/** Operator-entered reason / note length (no api.md limit; kept bounded). */
export const ADMIN_REASON_MAX = 1000;

const RemoveBody = z.object({
  target_type: z.enum(["message", "post", "story", "guestbook_entry", "upload", "space"]),
  target_id: zId,
  reason: zText(0, ADMIN_REASON_MAX),
  report_id: zId.optional(),
});

export const adminRoutes = new Hono<AppEnv>();
adminRoutes.use("/admin/*", requireAuth, requireAdmin);

/** 200 { action: AdminAction }; the removal emits the ordinary delete events. Missing target / report -> 404. */
adminRoutes.post("/admin/content/remove", vJson(RemoveBody), async (c) => {
  const b = c.req.valid("json");
  const action = await removeContent(rt(c), getAuth(c).userId, { type: b.target_type, id: b.target_id }, b.reason, b.report_id);
  return c.json({ action });
});
