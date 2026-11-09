// v0.1.3 A10.3 (P1 Task 2): PUT /v1/spaces/{space_id}/leet — owner/admin turn #daily on (the public channel `daily`,
// made with the ordinary channel rules and events, or an existing public channel) and off (the channel stays).
import { describe, expect, it } from "vitest";
import { api, connectWs, makeSpace, signup } from "../helpers";

const put = (token: string, spaceId: string, body: unknown) => api(token).put<any>(`/v1/spaces/${spaceId}/leet`, body);
const channels = async (token: string, spaceId: string) => (await api(token).get<any>(`/v1/spaces/${spaceId}/channels`)).body.items as any[];

describe("PUT /v1/spaces/{space_id}/leet", () => {
  it("owner turns #daily on: creates `daily` once (channel.created, system message), twice is still one channel", async () => {
    const [o, m] = await Promise.all([signup(), signup()]);
    const { space } = await makeSpace(o, [m]);
    const wm = await connectWs(m.token);
    const r = await put(o.token, space.id, { enabled: true });
    expect(r.status).toBe(200);
    expect(r.body.settings).toMatchObject({ space_id: space.id, enabled: true, enabled_by: o.user.id });
    expect(typeof r.body.settings.enabled_at).toBe("string");
    const chId = r.body.settings.channel_id;
    const ev = await wm.next("channel.created", (d: any) => d.channel.id === chId);
    expect(ev.data.channel).toMatchObject({ name: "daily", is_private: false, space_id: space.id });
    const msgs = await api(m.token).get<any>(`/v1/channels/${chId}/messages`);
    expect(msgs.body.items.at(-1)).toMatchObject({ kind: "system", system: { event: "channel_created", data: { name: "daily" } } });

    const again = await put(o.token, space.id, { enabled: true });
    expect(again.status).toBe(200);
    expect(again.body.settings.channel_id).toBe(chId);
    expect((await channels(o.token, space.id)).filter((c) => c.name === "daily")).toHaveLength(1);
  });

  it("an existing public `daily` channel is reused", async () => {
    const o = await signup();
    const { space } = await makeSpace(o);
    const made = await api(o.token).post<any>(`/v1/spaces/${space.id}/channels`, { name: "daily" });
    expect(made.status).toBe(201);
    const r = await put(o.token, space.id, { enabled: true });
    expect(r.status).toBe(200);
    expect(r.body.settings.channel_id).toBe(made.body.channel.id);
    expect((await channels(o.token, space.id)).filter((c) => c.name === "daily")).toHaveLength(1);
  });

  it("channel_id picks an existing public channel; private or another space's channel -> 404", async () => {
    const [o, a] = await Promise.all([signup(), signup()]);
    const { space, generalChannelId } = await makeSpace(o);
    const { generalChannelId: otherCh } = await makeSpace(a);
    const priv = await api(o.token).post<any>(`/v1/spaces/${space.id}/channels`, { name: "secret", is_private: true });
    expect(priv.status).toBe(201);
    const r = await put(o.token, space.id, { enabled: true, channel_id: generalChannelId });
    expect(r.status).toBe(200);
    expect(r.body.settings.channel_id).toBe(generalChannelId);
    expect((await put(o.token, space.id, { enabled: true, channel_id: priv.body.channel.id })).status).toBe(404);
    expect((await put(o.token, space.id, { enabled: true, channel_id: otherCh })).status).toBe(404);
    // nothing changed by the refused calls
    expect((await put(o.token, space.id, { enabled: true })).body.settings.channel_id).toBe(generalChannelId);
  });

  it("an admin may; a member gets 403, a non-member 404; bad body 400", async () => {
    const [o, ad, m, x] = await Promise.all([signup(), signup(), signup(), signup()]);
    const { space } = await makeSpace(o, [ad, m]);
    expect((await api(o.token).patch(`/v1/spaces/${space.id}/members/${ad.user.id}`, { role: "admin" })).status).toBe(200);
    expect((await put(ad.token, space.id, { enabled: true })).status).toBe(200);
    const mr = await put(m.token, space.id, { enabled: false });
    expect(mr.status).toBe(403);
    expect(mr.body.error.code).toBe("forbidden");
    expect((await put(x.token, space.id, { enabled: true })).status).toBe(404);
    const bad = await put(o.token, space.id, { enabled: "yes" });
    expect(bad.status).toBe(400);
    expect(bad.body.error.details.fields).toEqual({ enabled: "invalid" });
    expect((await put(o.token, space.id, {})).body.error.details.fields).toEqual({ enabled: "required" });
  });

  it("enabled:false turns it off and keeps the channel", async () => {
    const o = await signup();
    const { space } = await makeSpace(o);
    const on = await put(o.token, space.id, { enabled: true });
    const chId = on.body.settings.channel_id;
    const off = await put(o.token, space.id, { enabled: false });
    expect(off.status).toBe(200);
    expect(off.body.settings).toMatchObject({ space_id: space.id, enabled: false });
    expect((await api(o.token).get<any>(`/v1/channels/${chId}`)).status).toBe(200);
    // turning it on again reuses the same channel
    expect((await put(o.token, space.id, { enabled: true })).body.settings.channel_id).toBe(chId);
  });

  it("off before ever on: enabled false, no channel", async () => {
    const o = await signup();
    const { space } = await makeSpace(o);
    const r = await put(o.token, space.id, { enabled: false });
    expect(r.status).toBe(200);
    expect(r.body.settings).toEqual({ space_id: space.id, enabled: false, channel_id: null, enabled_by: null, enabled_at: null });
    expect((await channels(o.token, space.id)).map((c) => c.name)).not.toContain("daily");
  });

  it("the remembered channel was deleted: turning on makes a fresh `daily`", async () => {
    const o = await signup();
    const { space } = await makeSpace(o);
    const first = (await put(o.token, space.id, { enabled: true })).body.settings.channel_id;
    expect((await api(o.token).del(`/v1/channels/${first}`)).status).toBe(204);
    const r = await put(o.token, space.id, { enabled: true });
    expect(r.status).toBe(200);
    expect(r.body.settings.channel_id).not.toBe(first);
    expect((await channels(o.token, space.id)).filter((c) => c.name === "daily")).toHaveLength(1);
  });
});
