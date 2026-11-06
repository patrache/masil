// test/api/search-long-terms.test.ts
// Regression: D1/workerd rejects LIKE patterns over 50 bytes ("LIKE or GLOB pattern too complex"), so every substring
// search must work for any valid q (1..100 code points): 17 Hangul syllables = 51 UTF-8 bytes, 60 ASCII characters.
import { describe, expect, it } from "vitest";
import { api, createPost, makeAdmin, makeSpace, signup, uniqueHandle } from "../helpers";

/** `n` random Hangul syllables (3 UTF-8 bytes each), unique enough to isolate one test's rows. */
function hangul(n: number): string {
  const r = crypto.getRandomValues(new Uint16Array(n));
  return Array.from(r, (x) => String.fromCharCode(0xac00 + (x % 11172))).join("");
}

/** `n` ASCII letters/digits: a unique tag padded with "x". */
function ascii(n: number): string {
  return uniqueHandle("lt").replace(/_/g, "").padEnd(n, "x").slice(0, n);
}

const enc = encodeURIComponent;

describe("long search terms (LIKE pattern length)", () => {
  it("message search: 2-syllable word + 17-syllable word (LIKE mode), long URL, case-folded ASCII, literal _ and %", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    const { generalChannelId: ch } = await makeSpace(a, [b]);
    const ko = hangul(17);
    const url = `https://github.com/Masil/${ascii(60)}`;
    const send = async (body: string) => {
      const r = await api(a.token).post<any>(`/v1/channels/${ch}/messages`, { client_nonce: crypto.randomUUID().slice(0, 12), body });
      expect(r.status).toBe(201);
      return r.body.message.id as string;
    };
    const m1 = await send(`안녕 ${ko} 반가워`);
    const m2 = await send(`링크 ${url}`);
    const m3 = await send(`a_c ${ko}`);
    const m4 = await send(`50% ${ko}`);
    const search = async (q: string) => {
      const r = await api(b.token).get<any>(`/v1/search/messages?channel_id=${ch}&q=${enc(q)}`);
      expect(r.status).toBe(200);
      return r.body.items.map((i: any) => i.message.id);
    };
    expect(await search(`안녕 ${ko}`)).toEqual([m1]);
    expect(await search(`링크 ${url}`)).toEqual([m2]);
    expect(await search(`링크 ${url.toUpperCase()}`)).toEqual([m2]); // ASCII case folding, as LIKE did
    expect(await search(`가 ${ascii(60)}`)).toEqual([]);
    expect(await search(`${"%".repeat(30)} ${ko}`)).toEqual([]); // wildcards are literal, even many of them
    expect(await search(`a_c ${ko}`)).toEqual([m3]);
    expect(await search(`ab ${ko}`)).toEqual([]);
    expect(await search(`% ${ko}`)).toEqual([m4]);
  });

  it("post search: 17-syllable term in LIKE mode and a 60-character ASCII term", async () => {
    const [a, v] = await Promise.all([signup(), signup()]);
    const ko = hangul(17);
    const long = ascii(60);
    const p1 = (await createPost(a, `안녕 ${ko}`)).id;
    const p2 = (await createPost(a, `글 ${long.toUpperCase()}`)).id;
    const search = async (q: string) => {
      const r = await api(v.token).get<any>(`/v1/search/posts?q=${enc(q)}`);
      expect(r.status).toBe(200);
      return r.body.items.map((i: any) => i.post.id);
    };
    expect(await search(`안녕 ${ko}`)).toEqual([p1]);
    expect(await search(`글 ${long}`)).toEqual([p2]);
    expect(await search(`글 ${ko}`)).toEqual([]);
  });

  it("user search: 17 syllables and 60 ASCII characters; handle prefix still works", async () => {
    const ko = hangul(17);
    const upper = ascii(30).toUpperCase();
    const [me, k, u] = await Promise.all([signup(), signup({ displayName: `${ko}님` }), signup({ displayName: upper })]);
    const search = async (q: string) => {
      const r = await api(me.token).get<any>(`/v1/users/search?q=${enc(q)}`);
      expect(r.status).toBe(200);
      return r.body.items.map((i: any) => i.id);
    };
    expect(await search(ko)).toEqual([k.user.id]);
    expect(await search(upper.toLowerCase())).toEqual([u.user.id]);
    expect(await search(ascii(60))).toEqual([]);
    expect(await search(`@${k.handle.toUpperCase()}`)).toEqual([k.user.id]); // exact handle, case-insensitive
    expect(await search(k.handle.slice(0, 12))).toEqual([k.user.id]); // handle prefix
    expect(await search(k.handle.slice(2, 12))).toEqual([]); // handle match is a prefix, not a substring
  });

  it("space member q and admin user q: 17 syllables and 60 ASCII characters", async () => {
    const ko = hangul(17);
    const [o, m, adm] = await Promise.all([signup(), signup({ displayName: `${ko}멤버` }), signup()]);
    await makeAdmin(adm);
    const { space } = await makeSpace(o, [m]);
    const members = async (q: string) => {
      const r = await api(o.token).get<any>(`/v1/spaces/${space.id}/members?q=${enc(q)}`);
      expect(r.status).toBe(200);
      return r.body.items.map((i: any) => i.user.id);
    };
    expect(await members(ko)).toEqual([m.user.id]);
    expect(await members(ascii(60))).toEqual([]);
    expect(await members(m.handle.slice(3, 15).toUpperCase())).toEqual([m.user.id]); // substring, case-insensitive
    const admin = async (q: string) => {
      const r = await api(adm.token).get<any>(`/v1/admin/users?q=${enc(q)}`);
      expect(r.status).toBe(200);
      return r.body.items.map((i: any) => i.user?.id ?? i.id);
    };
    expect(await admin(ko)).toEqual([m.user.id]);
    expect(await admin(ascii(60))).toEqual([]);
    expect(await admin(`@${m.handle.toUpperCase()}`)).toEqual([m.user.id]);
  });
});
