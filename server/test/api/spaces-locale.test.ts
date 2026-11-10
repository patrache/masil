// P5 Task 20 / api.md 5.7: the default channel made with a space is named in the request's Accept-Language
// (ko 일반 / en general / zh-Hans 综合, anything else ko). After that it is user data: no later rename.
import { describe, it, expect } from "vitest";
import { api, signup } from "../helpers";

async function createSpace(token: string, lang: string | null): Promise<any> {
  const headers: Record<string, string> = { "Content-Type": "application/json" };
  if (lang !== null) headers["Accept-Language"] = lang;
  const res = await api(token).raw("/v1/spaces", { method: "POST", headers, body: JSON.stringify({ name: "locale room" }) });
  expect(res.status).toBe(201);
  return ((await res.json()) as any).space;
}

async function defaultChannelName(token: string, space: any, lang: string | null): Promise<string> {
  const headers: Record<string, string> = {};
  if (lang !== null) headers["Accept-Language"] = lang;
  const res = await api(token).raw(`/v1/spaces/${space.id}`, { method: "GET", headers });
  expect(res.status).toBe(200);
  const detail = ((await res.json()) as any).space;
  const ch = detail.channels.find((c: any) => c.id === detail.default_channel_id);
  return ch.name;
}

describe("spaces: default channel name per request language", () => {
  it("default channel follows Accept-Language", async () => {
    const u = await signup();
    const cases: [string | null, string][] = [
      ["en", "general"],
      ["zh-Hans", "综合"],
      ["ko", "일반"],
      [null, "일반"],
      ["fr", "일반"],
      ["en-US", "일반"],
    ];
    for (const [lang, want] of cases) {
      const space = await createSpace(u.token, lang);
      const ch = space.channels.find((c: any) => c.id === space.default_channel_id);
      expect(ch?.name, String(lang)).toBe(want);
    }
  });

  it("language change later does not rename", async () => {
    const u = await signup();
    const space = await createSpace(u.token, "en");
    expect(await defaultChannelName(u.token, space, "ko")).toBe("general");
    expect(await defaultChannelName(u.token, space, null)).toBe("general");
  });
});
