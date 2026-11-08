// Pending item 5 (server side, defence in depth): a path ID or invite code that is not in the form the server issues
// (ULID / 8 chars of the invite alphabet) is 404 before any lookup, so a value a URL normaliser or a client bug could
// produce never reaches a query. The rows below exist under malformed keys, so only the format check can answer 404.
import { env } from "cloudflare:test";
import { describe, expect, it } from "vitest";
import { newId } from "../../src/lib/ids";
import { api, makeSpace, signup } from "../helpers";

describe("malformed path ids and invite codes are 404 before any lookup", () => {
  it("space, channel, message and invite code", async () => {
    const owner = await signup();
    const { space, generalChannelId } = await makeSpace(owner);
    const t = Date.now();
    const badSpace = "space-not-a-ulid-000000000";
    const badChannel = "chan.not.a.ulid";
    const badMessage = "msg..not..a..ulid";
    const badCode = "bad-code"; // 8 chars, '-' is not in the invite alphabet
    await env.DB.batch([
      env.DB.prepare("INSERT INTO spaces (id, name, owner_id, member_count, created_at, updated_at) VALUES (?1, 'bad', ?2, 1, ?3, ?3)").bind(
        badSpace,
        owner.user.id,
        t,
      ),
      env.DB.prepare("INSERT INTO space_members (space_id, user_id, role, joined_at) VALUES (?1, ?2, 'owner', ?3)").bind(badSpace, owner.user.id, t),
      env.DB.prepare(
        "INSERT INTO channels (id, kind, space_id, name, is_private, created_by, member_count, created_at, updated_at) VALUES (?1, 'text', ?2, 'bad', 0, ?3, 0, ?4, ?4)",
      ).bind(badChannel, space.id, owner.user.id, t),
      env.DB.prepare("INSERT INTO messages (id, channel_id, author_id, kind, body, created_at) VALUES (?1, ?2, ?3, 'text', 'x', ?4)").bind(
        badMessage,
        generalChannelId,
        owner.user.id,
        t,
      ),
      env.DB.prepare("INSERT INTO space_invites (id, space_id, code, created_by, created_at) VALUES (?1, ?2, ?3, ?4, ?5)").bind(
        newId(),
        space.id,
        badCode,
        owner.user.id,
        t,
      ),
    ]);
    const a = api(owner.token);
    const notFound = async (what: string, res: Promise<{ status: number; body: unknown }>) => {
      const r = await res;
      expect(r.status, `${what}: ${JSON.stringify(r.body)}`).toBe(404);
    };
    await notFound("GET space", a.get(`/v1/spaces/${badSpace}`));
    await notFound("GET space members", a.get(`/v1/spaces/${badSpace}/members`));
    await notFound("GET space channels", a.get(`/v1/spaces/${badSpace}/channels`));
    await notFound("GET space invites", a.get(`/v1/spaces/${badSpace}/invites`));
    await notFound("GET channel", a.get(`/v1/channels/${badChannel}`));
    await notFound("GET channel messages", a.get(`/v1/channels/${badChannel}/messages`));
    await notFound("GET message", a.get(`/v1/messages/${badMessage}`));
    await notFound("DELETE message", a.del(`/v1/messages/${badMessage}`));
    await notFound("GET invite", a.get(`/v1/invites/${badCode}`));
    await notFound("DELETE invite", a.del(`/v1/invites/${badCode}`));
    // Authentication still answers first.
    expect((await api().get(`/v1/spaces/${badSpace}`)).status).toBe(401);
    expect((await api().get(`/v1/invites/${badCode}`)).status).toBe(401);
    // Well-formed ids still work.
    expect((await a.get(`/v1/spaces/${space.id}`)).status).toBe(200);
    expect((await a.get(`/v1/channels/${generalChannelId}`)).status).toBe(200);
  });
});
