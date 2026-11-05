// Polls (api.md 5.19): GET /polls/:poll_id, PUT|DELETE /polls/:poll_id/vote (RL_REACT), POST /polls/:poll_id/close
// (plan 02 Task 11). Polls are created with their message / post; read access = that message's / post's read access.
import { Hono } from "hono";
import * as z from "zod";
import { rt, type AppEnv } from "../env";
import { getAuth, requireAuth } from "../lib/auth";
import { limit } from "../lib/ratelimit";
import { vJson } from "../lib/validate";
import { closePoll, getPoll, vote } from "../services/polls";

// Range, size (single / multi) and duplicates are checked against the poll in services/polls.ts (vote).
const VoteBody = z.object({ option_idxs: z.array(z.number().int()) });

export const pollRoutes = new Hono<AppEnv>();
pollRoutes.use("/polls/*", requireAuth);

pollRoutes.get("/polls/:poll_id", async (c) => {
  return c.json({ poll: await getPoll(rt(c), c.req.param("poll_id"), getAuth(c).userId) });
});

pollRoutes.put("/polls/:poll_id/vote", limit("RL_REACT"), vJson(VoteBody), async (c) => {
  const { option_idxs } = c.req.valid("json");
  return c.json({ poll: await vote(rt(c), c.req.param("poll_id"), getAuth(c).userId, option_idxs) });
});

pollRoutes.delete("/polls/:poll_id/vote", limit("RL_REACT"), async (c) => {
  return c.json({ poll: await vote(rt(c), c.req.param("poll_id"), getAuth(c).userId, null) });
});

pollRoutes.post("/polls/:poll_id/close", async (c) => {
  return c.json({ poll: await closePoll(rt(c), c.req.param("poll_id"), getAuth(c).userId) });
});
