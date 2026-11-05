// Channels (api.md 5.9) and channel membership (api.md 5.6). requireAuth is a per-route argument (plan 01 Global
// Constraints). Task 19: GET /channels/:channel_id and the group_dm member routes; Task 23 adds text channel
// management, private text members and prefs; Task 27 adds PUT /channels/:channel_id/read.
import { Hono } from "hono";
import * as z from "zod";
import { rt, type AppEnv } from "../env";
import { getAuth, requireAuth, resolveUserRef } from "../lib/auth";
import { requireChannelRead } from "../lib/permissions";
import { serializeChannels } from "../lib/serialize";
import { vJson, zUserRef } from "../lib/validate";
import { addChannelMembers, GROUP_DM_MAX_MEMBERS, removeChannelMember } from "../services/membership";

const AddMembersBody = z.object({ users: z.array(zUserRef).min(1).max(GROUP_DM_MAX_MEMBERS - 1) });

export const channelRoutes = new Hono<AppEnv>();

channelRoutes.get("/channels/:channel_id", requireAuth, async (c) => {
  const me = getAuth(c).userId;
  const access = await requireChannelRead(c.env.DB, c.req.param("channel_id"), me);
  const [channel] = await serializeChannels(c.env.DB, me, [access.channel]);
  return c.json({ channel });
});

channelRoutes.post("/channels/:channel_id/members", requireAuth, vJson(AddMembersBody), async (c) => {
  const auth = getAuth(c);
  const db = c.env.DB;
  // 404 for a channel I cannot read comes before resolving the users (no existence leak through the body).
  await requireChannelRead(db, c.req.param("channel_id"), auth.userId);
  const users = await Promise.all(c.req.valid("json").users.map((ref) => resolveUserRef(db, ref, auth)));
  const channel = await addChannelMembers(rt(c), c.req.param("channel_id"), auth.userId, users.map((u) => u.id));
  return c.json({ channel });
});

channelRoutes.delete("/channels/:channel_id/members/:user", requireAuth, async (c) => {
  const auth = getAuth(c);
  const db = c.env.DB;
  await requireChannelRead(db, c.req.param("channel_id"), auth.userId);
  const target = await resolveUserRef(db, c.req.param("user"), auth);
  await removeChannelMember(rt(c), c.req.param("channel_id"), auth.userId, target.id);
  return c.body(null, 204);
});
