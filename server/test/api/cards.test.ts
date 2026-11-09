// v0.1.3 C1.2 / api.md 2 "카드", 5.10, 5.11: kind "card" messages — per-type schemas, server-only types, 16KB data,
// required fallback body, no edits; createCardMessage / updateCardMessage for the server-only paths.
import { env } from "cloudflare:test";
import { describe, it, expect } from "vitest";
import * as z from "zod";
import type { Runtime } from "../../src/env";
import { CARD_DATA_MAX, CARD_TYPES, mergeCards } from "../../src/lib/cards/index";
import { createCardMessage, updateCardMessage } from "../../src/services/messages";
import { api, connectWs, makeSpace, signup } from "../helpers";

const rtFor = (): Runtime => ({ env, waitUntil: () => {} });
const nonce = () => crypto.randomUUID();
const fieldsOf = (r: { body: any }) => r.body.error.details.fields;

describe("card messages", () => {
  it("a member sends a user card: 201, kind card in the response, the event and GET", async () => {
    const [o, m] = await Promise.all([signup(), signup()]);
    const { generalChannelId: ch } = await makeSpace(o, [m]);
    const wo = await connectWs(o.token);
    const card = { type: "arch.design", v: 1, data: { n: 14 } };
    const r = await api(m.token).post<any>(`/v1/channels/${ch}/messages`, { client_nonce: nonce(), kind: "card", body: "📐 결제 v3 · 노드 14", card });
    expect(r.status).toBe(201);
    expect(r.body.message).toMatchObject({ kind: "card", body: "📐 결제 v3 · 노드 14", card });
    const ev = await wo.next("message.created", (d: any) => d.message.id === r.body.message.id);
    expect(ev.data.message).toMatchObject({ kind: "card", card });
    const g = await api(o.token).get<any>(`/v1/messages/${r.body.message.id}`);
    expect(g.status).toBe(200);
    expect(g.body.message).toMatchObject({ kind: "card", card });
    const list = await api(o.token).get<any>(`/v1/channels/${ch}/messages`);
    expect(list.body.items.find((x: any) => x.id === r.body.message.id)).toMatchObject({ kind: "card", card });
    // an ordinary message has card: null
    const t = await api(m.token).post<any>(`/v1/channels/${ch}/messages`, { client_nonce: nonce(), body: "hi" });
    expect(t.body.message).toMatchObject({ kind: "text", card: null });
  });

  it("validation: server-only type, unknown type / v, data size, missing body / card", async () => {
    const o = await signup();
    const { generalChannelId: ch } = await makeSpace(o);
    const send = (j: object) => api(o.token).post<any>(`/v1/channels/${ch}/messages`, { client_nonce: nonce(), kind: "card", body: "x", ...j });
    expect(fieldsOf(await send({ card: { type: "leet.daily", v: 1, data: {} } }))).toEqual({ "card.type": "not_allowed" });
    expect(fieldsOf(await send({ card: { type: "leet.solve", v: 1, data: {} } }))).toEqual({ "card.type": "not_allowed" });
    expect(fieldsOf(await send({ card: { type: "nope.card", v: 1, data: {} } }))).toEqual({ "card.type": "invalid" });
    expect(fieldsOf(await send({ card: { type: "api.result", v: 2, data: {} } }))).toEqual({ "card.v": "invalid" });
    expect(fieldsOf(await send({ card: { type: "api.result", v: "1", data: {} } }))).toEqual({ "card.v": "invalid" });
    expect(fieldsOf(await send({ card: { type: "api.run", v: 1, data: [1, 2] } }))).toEqual({ "card.data": "invalid" });
    expect(fieldsOf(await send({}))).toEqual({ card: "required" });
    expect(fieldsOf(await send({ card: "x" }))).toEqual({ card: "invalid" });
    // JSON.stringify(data) of exactly CARD_DATA_MAX bytes passes, one more fails
    const fill = (n: number) => ({ s: "x".repeat(n - JSON.stringify({ s: "" }).length) });
    expect(JSON.stringify(fill(CARD_DATA_MAX + 1)).length).toBe(16_385);
    expect((await send({ card: { type: "arch.sim", v: 1, data: fill(CARD_DATA_MAX) } })).status).toBe(201);
    expect(fieldsOf(await send({ card: { type: "arch.sim", v: 1, data: fill(CARD_DATA_MAX + 1) } }))).toEqual({ "card.data": "too_long" });
    const nb = await api(o.token).post<any>(`/v1/channels/${ch}/messages`, { client_nonce: nonce(), kind: "card", card: { type: "api.run", v: 1, data: {} } });
    expect(nb.status).toBe(400);
    expect(fieldsOf(nb)).toEqual({ body: "required" });
  });

  it("card messages cannot be edited; thread replies may be cards", async () => {
    const o = await signup();
    const { generalChannelId: ch } = await makeSpace(o);
    const card = { type: "api.result", v: 1, data: { status: 200 } };
    const r = await api(o.token).post<any>(`/v1/channels/${ch}/messages`, { client_nonce: nonce(), kind: "card", body: "GET /x → 200", card });
    expect(r.status).toBe(201);
    const p = await api(o.token).patch<any>(`/v1/messages/${r.body.message.id}`, { body: "changed" });
    expect(p.status).toBe(400);
    expect(fieldsOf(p)).toEqual({ message_id: "not_allowed" });
    expect((await api(o.token).get<any>(`/v1/messages/${r.body.message.id}`)).body.message).toMatchObject({ body: "GET /x → 200", edited_at: null, card });

    const root = await api(o.token).post<any>(`/v1/channels/${ch}/messages`, { client_nonce: nonce(), body: "root" });
    const reply = await api(o.token).post<any>(`/v1/messages/${root.body.message.id}/thread`, { client_nonce: nonce(), kind: "card", body: "▶ 3/3", card: { type: "api.run", v: 1, data: { ok: 3 } } });
    expect(reply.status).toBe(201);
    expect(reply.body.message).toMatchObject({ kind: "card", thread_root_id: root.body.message.id, card: { type: "api.run", v: 1, data: { ok: 3 } } });
    const bad = await api(o.token).post<any>(`/v1/messages/${root.body.message.id}/thread`, { client_nonce: nonce(), kind: "card", body: "x", card: { type: "leet.solve", v: 1, data: {} } });
    expect(fieldsOf(bad)).toEqual({ "card.type": "not_allowed" });
  });

  it("a deleted card message keeps no card", async () => {
    const o = await signup();
    const { generalChannelId: ch } = await makeSpace(o);
    const r = await api(o.token).post<any>(`/v1/channels/${ch}/messages`, { client_nonce: nonce(), kind: "card", body: "x", card: { type: "arch.design", v: 1, data: {} } });
    const id = r.body.message.id;
    await api(o.token).post(`/v1/messages/${id}/thread`, { client_nonce: nonce(), body: "keep the tombstone" });
    expect((await api(o.token).del(`/v1/messages/${id}`)).status).toBe(204);
    const row = await env.DB.prepare("SELECT card_json FROM messages WHERE id = ?").bind(id).first<any>();
    expect(row.card_json).toBeNull();
    const g = await api(o.token).get<any>(`/v1/messages/${id}/thread`);
    expect(g.body.root).toMatchObject({ deleted: true, card: null });
  });

  it("createCardMessage makes a server-only root others reply under; updateCardMessage rewrites it with message.updated", async () => {
    const [o, m] = await Promise.all([signup(), signup()]);
    const { generalChannelId: ch } = await makeSpace(o, [m]);
    const wm = await connectWs(m.token);
    // leet.* data follows P1's schemas (lib/leet.ts)
    const daily = (slug: string) => ({ date: "2026-11-09", frontend_id: "1", slug, title: slug, difficulty: "Easy", tags: [], is_paid_only: false });
    const solveData = { date: "2026-11-09", frontend_id: "1", slug: "two-sum", lang: "golang", runtime_percentile: 3, code: null, spoiler: true };
    const msg = await createCardMessage(rtFor(), {
      channelId: ch,
      authorId: o.user.id,
      rootId: null,
      card: { type: "leet.daily", v: 1, data: daily("two-sum") },
      body: "https://leetcode.com/problems/two-sum/",
    });
    expect(msg).toMatchObject({ kind: "card", card: { type: "leet.daily", v: 1, data: { slug: "two-sum" } }, client_nonce: null });
    expect((await wm.next("message.created", (d: any) => d.message.id === msg.id)).data.message.kind).toBe("card");
    const reply = await api(m.token).post<any>(`/v1/messages/${msg.id}/thread`, { client_nonce: nonce(), body: "풀었다" });
    expect(reply.status).toBe(201);
    // a server-only card can also be a reply (P1 solve cards)
    const solve = await createCardMessage(rtFor(), { channelId: ch, authorId: m.user.id, rootId: msg.id, card: { type: "leet.solve", v: 1, data: solveData }, body: "solved" });
    expect(solve.thread_root_id).toBe(msg.id);

    const up = await updateCardMessage(rtFor(), msg.id, { type: "leet.daily", v: 1, data: daily("add-two-numbers") }, "https://leetcode.com/problems/add-two-numbers/");
    expect(up.card).toEqual({ type: "leet.daily", v: 1, data: daily("add-two-numbers") });
    const ev = await wm.next("message.updated", (d: any) => d.message.id === msg.id);
    expect(ev.data.message).toMatchObject({ body: "https://leetcode.com/problems/add-two-numbers/", card: { data: { slug: "add-two-numbers" } } });
    const g = await api(m.token).get<any>(`/v1/messages/${msg.id}`);
    expect(g.body.message).toMatchObject({ body: "https://leetcode.com/problems/add-two-numbers/", card: { type: "leet.daily", data: { slug: "add-two-numbers" } } });
    // body omitted keeps the body
    await updateCardMessage(rtFor(), msg.id, { type: "leet.daily", v: 1, data: daily("x") });
    expect((await api(m.token).get<any>(`/v1/messages/${msg.id}`)).body.message.body).toBe("https://leetcode.com/problems/add-two-numbers/");
    // a different type, data that fails the schema, or a non-card message -> throw
    await expect(updateCardMessage(rtFor(), msg.id, { type: "leet.solve", v: 1, data: solveData })).rejects.toThrow();
    await expect(updateCardMessage(rtFor(), msg.id, { type: "leet.daily", v: 1, data: {} })).rejects.toThrow();
    await expect(updateCardMessage(rtFor(), msg.id, { type: "leet.daily", v: 1, data: [] as unknown })).rejects.toThrow();
    await expect(updateCardMessage(rtFor(), msg.id, { type: "leet.daily", v: 9, data: {} })).rejects.toThrow();
    await expect(updateCardMessage(rtFor(), reply.body.message.id, { type: "leet.daily", v: 1, data: {} })).rejects.toThrow();
    await expect(createCardMessage(rtFor(), { channelId: ch, authorId: o.user.id, rootId: null, card: { type: "leet.daily", v: 1, data: "x" as unknown }, body: "x" })).rejects.toThrow();
  });
});

describe("card type registry", () => {
  it("has exactly the six types; leet.* are server-only", () => {
    expect(Object.keys(CARD_TYPES).sort()).toEqual(["api.result", "api.run", "arch.design", "arch.sim", "leet.daily", "leet.solve"]);
    expect(Object.entries(CARD_TYPES).filter(([, s]) => s.serverOnly).map(([t]) => t).sort()).toEqual(["leet.daily", "leet.solve"]);
  });

  it("the same type in two cards/*.ts files throws when the registry is built", () => {
    const spec = { serverOnly: false, versions: { 1: z.record(z.string(), z.unknown()) } };
    expect(() => mergeCards([{ "arch.design": spec }, { "arch.design": spec }])).toThrow(/arch\.design/);
  });
});
