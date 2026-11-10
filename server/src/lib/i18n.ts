// Server-side user-facing text (i18n design 7.2-7.3, 7.6; api.md 0.1 and 2 "alert 템플릿"). The server builds no
// user-facing sentences anywhere else (test/unit/hangul-ratchet.test.ts): an alert's meaning is `template = {id,
// params}` (ids, params and body kind fixed in docs/contract/i18n/alert-templates.json), and its title / body are
// fallback text rendered here in the connection's / request's Accept-Language. The language is never stored.
// en / zh-Hans are first drafts (the P5 review ledger checks them later).

export type Lang = "ko" | "en" | "zh-Hans";
export const LANGS: readonly Lang[] = ["ko", "en", "zh-Hans"];

/** api.md 0.1: exactly "ko", "en" or "zh-Hans", anything else (en-US, zh, "", missing) -> "ko". */
export function langOf(header: string | null | undefined): Lang {
  return header === "en" || header === "zh-Hans" || header === "ko" ? header : "ko";
}

// ---- P5 (Task 20) ----

const DEFAULT_CHANNEL_NAMES: Record<Lang, string> = { ko: "일반", en: "general", "zh-Hans": "综合" };

/**
 * api.md 5.7 POST /v1/spaces: the name of the default channel made with a new space, in the request language
 * (langOf). Each is a valid channel name (api.md 0.3). Once made, the name is user data — never renamed later.
 */
export function defaultChannelName(l: Lang): string {
  return DEFAULT_CHANNEL_NAMES[l];
}

/** What an alert's body is: none = "", verbatim = the user's own text (any language), template = rendered here. */
export type BodyKind = "none" | "verbatim" | "template";

interface TemplateText {
  title: string;
  body?: string; // only for body kind "template"
}

interface TemplateDef {
  body: BodyKind;
  text: Record<Lang, TemplateText>;
}

export const TEMPLATES = {
  "social.friend_request": {
    body: "none",
    text: { ko: { title: "{actor}님의 친구 요청" }, en: { title: "{actor} sent you a friend request" }, "zh-Hans": { title: "{actor} 向你发送了好友请求" } },
  },
  "social.follow": {
    body: "none",
    text: { ko: { title: "{actor}님이 팔로우했어요" }, en: { title: "{actor} followed you" }, "zh-Hans": { title: "{actor} 关注了你" } },
  },
  "social.follow_request": {
    body: "none",
    text: { ko: { title: "{actor}님의 팔로우 요청" }, en: { title: "{actor} asked to follow you" }, "zh-Hans": { title: "{actor} 请求关注你" } },
  },
  "social.follow_accepted": {
    body: "none",
    text: {
      ko: { title: "{actor}님이 팔로우 요청을 수락했어요" },
      en: { title: "{actor} accepted your follow request" },
      "zh-Hans": { title: "{actor} 接受了你的关注请求" },
    },
  },
  "social.guestbook": {
    body: "verbatim",
    text: { ko: { title: "{actor}님의 방명록" }, en: { title: "{actor} signed your guestbook" }, "zh-Hans": { title: "{actor} 在你的留言板留言" } },
  },
  "invite.space": {
    body: "none",
    text: {
      ko: { title: "{actor}님이 {space} 방에 초대했어요" },
      en: { title: "{actor} invited you to {space}" },
      "zh-Hans": { title: "{actor} 邀请你加入 {space}" },
    },
  },
  "post.reply": {
    body: "verbatim",
    text: { ko: { title: "{actor}님의 답글" }, en: { title: "{actor} replied to your post" }, "zh-Hans": { title: "{actor} 回复了你的帖子" } },
  },
  "post.quote": {
    body: "verbatim",
    text: { ko: { title: "{actor}님이 글을 인용했어요" }, en: { title: "{actor} quoted your post" }, "zh-Hans": { title: "{actor} 引用了你的帖子" } },
  },
  "post.mention": {
    body: "verbatim",
    text: { ko: { title: "{actor}님이 글에서 멘션했어요" }, en: { title: "{actor} mentioned you in a post" }, "zh-Hans": { title: "{actor} 在帖子中提到了你" } },
  },
  "post.like": {
    body: "verbatim",
    text: { ko: { title: "{actor}님이 글을 좋아해요" }, en: { title: "{actor} liked your post" }, "zh-Hans": { title: "{actor} 赞了你的帖子" } },
  },
  "post.repost": {
    body: "verbatim",
    text: { ko: { title: "{actor}님이 글을 리포스트했어요" }, en: { title: "{actor} reposted your post" }, "zh-Hans": { title: "{actor} 转发了你的帖子" } },
  },
  "message.channel": {
    body: "verbatim",
    text: { ko: { title: "{actor} · {space} #{channel}" }, en: { title: "{actor} · {space} #{channel}" }, "zh-Hans": { title: "{actor} · {space} #{channel}" } },
  },
  "message.dm": {
    body: "verbatim",
    text: { ko: { title: "{actor}" }, en: { title: "{actor}" }, "zh-Hans": { title: "{actor}" } },
  },
  "message.group": {
    body: "verbatim",
    text: { ko: { title: "{actor} · {group}" }, en: { title: "{actor} · {group}" }, "zh-Hans": { title: "{actor} · {group}" } },
  },
  "focus.summary": {
    body: "template",
    text: {
      ko: {
        title: "집중 모드 끝 — 멘션 {mention}, DM {dm}",
        body: "스레드 답글 {thread_reply} · 초대 {invite} · 친구 요청 {friend_request} · 기타 {other}",
      },
      en: {
        title: "Focus ended — mentions {mention}, DMs {dm}",
        body: "Thread replies {thread_reply} · Invites {invite} · Friend requests {friend_request} · Other {other}",
      },
      "zh-Hans": {
        title: "专注模式结束 — 提及 {mention}，私信 {dm}",
        body: "话题回复 {thread_reply} · 邀请 {invite} · 好友请求 {friend_request} · 其他 {other}",
      },
    },
  },
  // Task 29 sends it. ko leaves the space out (the client catalog's wording); en / zh-Hans name it.
  "doc.comment_mention": {
    body: "verbatim",
    text: {
      ko: { title: "{actor}님이 {doc} 댓글에서 멘션했어요" },
      en: { title: "{actor} mentioned you in a comment on {doc} · {space}" },
      "zh-Hans": { title: "{actor} 在 {space} · {doc} 的评论中提到了你" },
    },
  },
  // v0.1.3 P2 (api.md 5.26): workspace share invite / "a program in my shared workspace is waiting for a confirmation".
  // zh-Hans copies en until the P5 review ledger checks it.
  "workspace.invite": {
    body: "template",
    text: {
      ko: { title: "{actor}님이 작업공간을 공유했어요", body: "{share}" },
      en: { title: "{actor} shared a workspace with you", body: "{share}" },
      "zh-Hans": { title: "{actor} shared a workspace with you", body: "{share}" },
    },
  },
  "workspace.attention": {
    body: "template",
    text: {
      ko: { title: "{share}", body: "공유 중인 작업공간에서 확인이 필요해요" },
      en: { title: "{share}", body: "Your shared workspace needs a confirmation" },
      "zh-Hans": { title: "{share}", body: "Your shared workspace needs a confirmation" },
    },
  },
} as const satisfies Record<string, TemplateDef>;

export type AlertTemplateId = keyof typeof TEMPLATES;

export interface AlertTemplate {
  id: AlertTemplateId;
  params: Record<string, string | number>;
}

export const ALERT_TITLE_MAX = 80; // api.md 2 Alert.title, code points
export const ALERT_BODY_MAX = 200; // api.md 2 Alert.body, code points

/** Cut to at most n code points. */
export function clipCodePoints(s: string, n: number): string {
  const cps = [...s];
  return cps.length <= n ? s : cps.slice(0, n).join("");
}

export function isAlertTemplateId(id: unknown): id is AlertTemplateId {
  return typeof id === "string" && Object.prototype.hasOwnProperty.call(TEMPLATES, id);
}

/** One pass over `{name}` placeholders; a value is inserted literally (never re-scanned, `$&` is not special). */
function fill(text: string, params: Record<string, string | number>): string {
  return text.replace(/\{([A-Za-z0-9_]+)\}/g, (whole, name: string) =>
    Object.prototype.hasOwnProperty.call(params, name) ? String(params[name]) : whole,
  );
}

/**
 * Fallback title (<= 80 code points) and body (<= 200) of `t` in `lang`. `verbatimBody` is the user's own text for
 * body kind "verbatim" (language-independent — the stored alert's body), ignored otherwise.
 */
export function renderAlert(t: AlertTemplate, lang: Lang, verbatimBody: string): { title: string; body: string } {
  const def: TemplateDef = TEMPLATES[t.id];
  const text = def.text[lang];
  const body = def.body === "verbatim" ? verbatimBody : def.body === "template" ? fill(text.body ?? "", t.params) : "";
  return { title: clipCodePoints(fill(text.title, t.params), ALERT_TITLE_MAX), body: clipCodePoints(body, ALERT_BODY_MAX) };
}
