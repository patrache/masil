// i18n 7.2-7.3 / api.md 2 "alert 템플릿": docs/contract/i18n/alert-templates.json is the one list of template ids,
// their params and body kind; server/src/lib/i18n.ts must carry exactly those ids, with ko / en / zh-Hans text each,
// and the params the texts use must be the declared ones.
import { describe, it, expect } from "vitest";
import { LANGS, TEMPLATES, langOf, renderAlert, type AlertTemplateId } from "../../src/lib/i18n";

const raw = import.meta.glob("../../../docs/contract/i18n/alert-templates.json", { query: "?raw", import: "default", eager: true }) as Record<string, string>;
const json = JSON.parse(Object.values(raw)[0]!) as {
  version: number;
  templates: Record<string, { params: string[]; body: "none" | "verbatim" | "template" }>;
};

const PARAM_RE = /\{([A-Za-z0-9_]+)\}/g;
function used(s: string | undefined): string[] {
  return s === undefined ? [] : [...s.matchAll(PARAM_RE)].map((m) => m[1]!);
}

describe("alert templates: contract json == lib/i18n.ts", () => {
  it("has the same id set", () => {
    expect(json.version).toBe(1);
    expect(Object.keys(TEMPLATES).sort()).toEqual(Object.keys(json.templates).sort());
  });

  it("every id has text in ko, en and zh-Hans, the declared body kind, and only declared params", () => {
    for (const [id, decl] of Object.entries(json.templates)) {
      const t = TEMPLATES[id as AlertTemplateId] as { body: string; text: Record<string, { title: string; body?: string } | undefined> };
      expect(t.body, id).toBe(decl.body);
      const union = new Set<string>();
      for (const lang of LANGS) {
        const text = t.text[lang]!;
        expect(text, `${id} ${lang}`).toBeDefined();
        expect(text.title.length, `${id} ${lang} title`).toBeGreaterThan(0);
        if (decl.body === "template") expect(text.body, `${id} ${lang} body`).toBeTruthy();
        else expect(text.body, `${id} ${lang} body`).toBeUndefined();
        for (const p of [...used(text.title), ...used(text.body)]) {
          expect(decl.params, `${id} ${lang} uses {${p}}`).toContain(p);
          union.add(p);
        }
      }
      // every declared param is used by some language's text (ko may leave a context param out — doc.comment_mention)
      expect([...union].sort(), id).toEqual([...decl.params].sort());
    }
  });
});

describe("langOf (api.md 0.1)", () => {
  it("is exactly ko, en or zh-Hans, else ko", () => {
    expect(langOf("en")).toBe("en");
    expect(langOf("zh-Hans")).toBe("zh-Hans");
    expect(langOf("ko")).toBe("ko");
    for (const v of ["en-US", "zh", "", "EN", " en", "ko-KR", null, undefined]) expect(langOf(v), String(v)).toBe("ko");
  });
});

describe("renderAlert", () => {
  it("renders the 0.1.2 Korean strings byte for byte", () => {
    const r = (id: AlertTemplateId, params: Record<string, string | number>, body = "") => renderAlert({ id, params }, "ko", body);
    expect(r("social.friend_request", { actor: "하나" }).title).toBe("하나님의 친구 요청");
    expect(r("social.follow", { actor: "하나" }).title).toBe("하나님이 팔로우했어요");
    expect(r("social.follow_request", { actor: "하나" }).title).toBe("하나님의 팔로우 요청");
    expect(r("social.follow_accepted", { actor: "하나" }).title).toBe("하나님이 팔로우 요청을 수락했어요");
    expect(r("social.guestbook", { actor: "하나" }, "놀러왔어요")).toEqual({ title: "하나님의 방명록", body: "놀러왔어요" });
    expect(r("invite.space", { actor: "하나", space: "노바" }).title).toBe("하나님이 노바 방에 초대했어요");
    expect(r("post.reply", { actor: "하나" }).title).toBe("하나님의 답글");
    expect(r("post.quote", { actor: "하나" }).title).toBe("하나님이 글을 인용했어요");
    expect(r("post.mention", { actor: "하나" }).title).toBe("하나님이 글에서 멘션했어요");
    expect(r("post.like", { actor: "하나" }).title).toBe("하나님이 글을 좋아해요");
    expect(r("post.repost", { actor: "하나" }).title).toBe("하나님이 글을 리포스트했어요");
    expect(r("message.channel", { actor: "sam", space: "노바 개발방", channel: "개발" }).title).toBe("sam · 노바 개발방 #개발");
    expect(r("message.dm", { actor: "sam" }).title).toBe("sam");
    expect(r("message.group", { actor: "sam", group: "a, b" }).title).toBe("sam · a, b");
    expect(r("focus.summary", { mention: 3, dm: 2, thread_reply: 1, invite: 0, friend_request: 4, other: 5 })).toEqual({
      title: "집중 모드 끝 — 멘션 3, DM 2",
      body: "스레드 답글 1 · 초대 0 · 친구 요청 4 · 기타 5",
    });
  });

  it("body: none -> empty, verbatim -> the given text, clipped to 80 / 200 code points", () => {
    expect(renderAlert({ id: "social.follow", params: { actor: "x" } }, "en", "ignored").body).toBe("");
    expect(renderAlert({ id: "post.like", params: { actor: "x" } }, "en", "hello").body).toBe("hello");
    const long = renderAlert({ id: "message.dm", params: { actor: "가".repeat(300) } }, "ko", "나".repeat(300));
    expect([...long.title].length).toBe(80);
    expect([...long.body].length).toBe(200);
  });

  it("substitutes once: a param value that looks like a placeholder stays literal", () => {
    expect(renderAlert({ id: "invite.space", params: { actor: "{space}", space: "$&" } }, "ko", "").title).toBe("{space}님이 $& 방에 초대했어요");
  });
});
