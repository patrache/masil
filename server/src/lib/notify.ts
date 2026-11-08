// Inbox rows + desktop alerts (server-architecture.md 4.12, events.md 4.7, api.md 5.18).
// - notify/notifyMany: skip actor === recipient and blocks either way; INSERT OR IGNORE by dedupe_key; only rows that
//   really went in publish `notification.created` (logged) and, when the recipient's switch allows, `alert` (logged).
// - sendAlerts: alerts without an inbox row (dm, channel_message) — settings, channel prefs and blocks applied.
// - suppressed / suppressed_reason (focus, dnd) are filled by UserHub, never here.
// - fanoutMessageNotifications / notifyMessageMentions: per-message inbox rows + alerts (Task 24).
// - Every lookup over many recipients is one json_each query (D1 caps bound params at 100 per statement).
import type { Runtime } from "../env";
import type { Alert, AlertKind, Message, Notification, NotificationType } from "../types/api";
import type { ChannelRow, NotificationRow, UserSettingsRow } from "../types/rows";
import { all, batch, first, jsonList, stmt } from "./db";
import { newId } from "./ids";
import type { OutboundEvent } from "../do/user-hub";
import { deliverPerUser } from "./realtime";
import { type NotificationExtra, loadUserSummaries, serializeNotifications } from "./serialize";
import { plainSnippet } from "./text";
import { iso, now } from "./time";

export interface NotifyInput {
  userId: string; // recipient
  type: NotificationType;
  actorId: string | null;
  subject: { type: Notification["subject"]["type"]; id: string };
  preview?: string;
  context?: Partial<Notification["context"]>;
  dedupeKey?: string;
  alert?: { kind: AlertKind; title: string; body: string; target: Alert["target"] } | null; // null = no desktop alert
}

export interface AlertItem {
  userId: string;
  kind: AlertKind;
  actorId: string | null;
  title: string;
  body: string;
  target: Alert["target"];
  channelId?: string;
}

const TITLE_MAX = 80;
const BODY_MAX = 200;
const PREVIEW_MAX = 100;

/** Cut to at most n code points. */
function clip(s: string, n: number): string {
  const cps = [...s];
  return cps.length <= n ? s : cps.slice(0, n).join("");
}

type Pair = [string, string];

/** Indexes of [a, b] pairs that have a block in either direction (one query). */
async function blockedPairs(db: D1Database, pairs: Pair[]): Promise<Set<number>> {
  if (pairs.length === 0) return new Set();
  const rows = await all<{ i: number }>(
    db,
    `SELECT j.key AS i FROM json_each(?1) j
      WHERE EXISTS (SELECT 1 FROM blocks b
                     WHERE (b.blocker_id = json_extract(j.value, '$[0]') AND b.blocked_id = json_extract(j.value, '$[1]'))
                        OR (b.blocker_id = json_extract(j.value, '$[1]') AND b.blocked_id = json_extract(j.value, '$[0]')))`,
    JSON.stringify(pairs),
  );
  return new Set(rows.map((r) => Number(r.i)));
}

/** Settings switch per alert kind (events.md 4.7). channel_message: channel prefs only; focus_summary: always. */
function switchOn(kind: AlertKind, s: UserSettingsRow | undefined): boolean {
  // A missing settings row reads as the schema defaults (social off, the rest on).
  switch (kind) {
    case "dm":
      return (s?.notify_dm ?? 1) === 1;
    case "mention":
      return (s?.notify_mention ?? 1) === 1;
    case "thread_reply":
      return (s?.notify_thread_reply ?? 1) === 1;
    case "invite":
      return (s?.notify_invite ?? 1) === 1;
    case "friend_request":
      return (s?.notify_friend_request ?? 1) === 1;
    case "social":
      return (s?.notify_social ?? 0) === 1;
    case "channel_message":
    case "focus_summary":
      return true;
  }
}

/**
 * Which alert candidates survive the recipient's settings switch and channel prefs (muted=1 or notify='none' -> none;
 * no prefs row -> dm/group_dm 'all', text 'mentions'). A candidate naming a missing or deleted channel is dropped.
 * Blocks are the caller's job. Two queries for any number of candidates.
 */
async function alertAllowed(db: D1Database, items: Array<{ userId: string; kind: AlertKind; channelId?: string | null }>): Promise<boolean[]> {
  if (items.length === 0) return [];
  const userIds = [...new Set(items.map((i) => i.userId))];
  const withChannel: Array<{ idx: number; pair: Pair }> = [];
  items.forEach((it, idx) => {
    if (it.channelId) withChannel.push({ idx, pair: [it.userId, it.channelId] });
  });
  const [settingsRes, prefsRes] = await batch(db, [
    stmt(db, "SELECT * FROM user_settings WHERE user_id IN (SELECT value FROM json_each(?1))", jsonList(userIds)),
    stmt(
      db,
      `SELECT j.key AS i, c.kind, cp.notify, cp.muted
         FROM json_each(?1) j
         JOIN channels c ON c.id = json_extract(j.value, '$[1]') AND c.deleted_at IS NULL
         LEFT JOIN channel_prefs cp ON cp.user_id = json_extract(j.value, '$[0]') AND cp.channel_id = c.id`,
      JSON.stringify(withChannel.map((w) => w.pair)),
    ),
  ]);
  const settings = new Map((settingsRes.results as UserSettingsRow[]).map((s) => [s.user_id, s]));
  const prefs = new Map(
    (prefsRes.results as Array<{ i: number; kind: string; notify: string | null; muted: number | null }>).map((r) => [Number(r.i), r]),
  );
  const channelOk = new Map<number, boolean>();
  withChannel.forEach((w, j) => {
    const p = prefs.get(j);
    if (p === undefined) {
      channelOk.set(w.idx, false); // channel gone
      return;
    }
    const mode = p.notify ?? (p.kind === "text" ? "mentions" : "all");
    channelOk.set(w.idx, (p.muted ?? 0) !== 1 && mode !== "none");
  });
  return items.map((it, idx) => switchOn(it.kind, settings.get(it.userId)) && (channelOk.get(idx) ?? true));
}

/**
 * The one definition of "this inbox row is shown" (api.md 5.18 / 3.9) for notifications alias `n` of recipient
 * `userExpr`: hidden while its actor is blocked by the recipient or suspended right now (same boundary as
 * postVisibilitySql / isSuspendedNow: suspended_until <= `nowExpr` counts as active again, and the rows come back).
 * Rows without an actor are always shown. Used by the inbox list and by unreadCountSql.
 */
export function inboxRowVisibleSql(userExpr: string, nowExpr: string): string {
  return `(NOT ${actorHiddenSql(userExpr, "n.actor_id", nowExpr)})`;
}

/** True when `actorExpr` is suspended right now (3.9: suspended_until <= `nowExpr` counts as active again). */
function actorSuspendedSql(actorExpr: string, nowExpr: string): string {
  return `EXISTS (SELECT 1 FROM users nau
                   WHERE nau.id = ${actorExpr} AND nau.suspended_at IS NOT NULL
                     AND (nau.suspended_until IS NULL OR nau.suspended_until > ${nowExpr}))`;
}

/**
 * True when notification actor `actorExpr` is hidden from recipient `userExpr` (api.md 5.18 / 3.9): the recipient
 * blocked the actor, or the actor is suspended right now (actorSuspendedSql). A NULL actor is never hidden. The single
 * rule behind inboxRowVisibleSql and actorVisibility (UserHub replay / focus summary).
 */
function actorHiddenSql(userExpr: string, actorExpr: string, nowExpr: string): string {
  return `(EXISTS (SELECT 1 FROM blocks b WHERE b.blocker_id = ${userExpr} AND b.blocked_id = ${actorExpr})
           OR ${actorSuspendedSql(actorExpr, nowExpr)})`;
}

/** actorVisibility's answer. `suspended` is a subset of `hidden` (a suspended actor is always hidden). */
export interface ActorVisibility {
  /** hidden from the recipient by the inbox rule (actorHiddenSql): blocked by them, or suspended right now */
  hidden: Set<string>;
  /** suspended right now — their UserSummary must be masked (owner decision D1, api.md 3.9) */
  suspended: Set<string>;
}

/**
 * For `userId`, which of `ids` are hidden right now (actorHiddenSql — the inbox rule) and which of those are suspended
 * right now, in one json_each query. UserHub uses it so frames it stored earlier (the replay log, focus_alerts) never
 * re-send what the inbox hides, nor an unmasked summary of a suspended account (pending item 4-a, api.md 3.9,
 * events.md 3.3).
 */
export async function actorVisibility(db: D1Database, userId: string, ids: string[], at: number): Promise<ActorVisibility> {
  const unique = [...new Set(ids)];
  const out: ActorVisibility = { hidden: new Set(), suspended: new Set() };
  if (unique.length === 0) return out;
  const rows = await all<{ id: string; suspended: number }>(
    db,
    `SELECT j.value AS id, ${actorSuspendedSql("j.value", "?3")} AS suspended
       FROM json_each(?1) j WHERE ${actorHiddenSql("?2", "j.value", "?3")}`,
    jsonList(unique),
    userId,
    at,
  );
  for (const r of rows) {
    out.hidden.add(r.id);
    if (r.suspended === 1) out.suspended.add(r.id);
  }
  return out;
}

/** Which of `actorIds` are hidden from `userId` right now (actorVisibility().hidden). */
export async function hiddenActors(db: D1Database, userId: string, actorIds: string[], at: number): Promise<Set<string>> {
  return (await actorVisibility(db, userId, actorIds, at)).hidden;
}

/**
 * The one definition of "unread inbox count" (api.md 5.18): unread rows of `userExpr` that pass inboxRowVisibleSql
 * (same rule as the inbox list). Rides idx_notifications_unread. Both unreadNotificationCount and the per-recipient
 * batch below are built from this, so the badge never differs between events and REST.
 */
function unreadCountSql(userExpr: string, nowExpr: string): string {
  return `SELECT COUNT(*) FROM notifications n
           WHERE n.user_id = ${userExpr} AND n.read_at IS NULL
             AND ${inboxRowVisibleSql(userExpr, nowExpr)}`;
}

/** Unread inbox count of one user (single source for the inbox routes, notification.created, notifications.read). */
export async function unreadNotificationCount(db: D1Database, userId: string): Promise<number> {
  const row = await first<{ n: number }>(db, `SELECT (${unreadCountSql("?1", "?2")}) AS n`, userId, now());
  return row?.n ?? 0;
}

/** unreadNotificationCount for many recipients in one json_each query (same SQL). */
async function unreadCounts(db: D1Database, userIds: string[]): Promise<Map<string, number>> {
  const ids = [...new Set(userIds)];
  if (ids.length === 0) return new Map();
  if (ids.length === 1) return new Map([[ids[0]!, await unreadNotificationCount(db, ids[0]!)]]);
  const rows = await all<{ user_id: string; n: number }>(
    db,
    `SELECT r.value AS user_id, (${unreadCountSql("r.value", "?2")}) AS n FROM json_each(?1) r`,
    jsonList(ids),
    now(),
  );
  return new Map(rows.map((r) => [r.user_id, r.n]));
}

function buildAlert(
  a: { kind: AlertKind; title: string; body: string; target: Alert["target"] },
  actor: Alert["actor"],
  createdAt: number,
): Alert {
  return {
    id: newId(),
    kind: a.kind,
    title: clip(a.title, TITLE_MAX),
    body: clip(a.body, BODY_MAX),
    actor,
    target: a.target,
    created_at: iso(createdAt),
    suppressed: false, // UserHub fills focus/dnd
    suppressed_reason: null,
  };
}

type Outbound = { type: "notification.created" | "alert"; data: unknown };

/** One scheduled fan-out for all recipients: one online query, one UserHub RPC per online recipient, 50 at a time. */
function publishPerRecipient(rt: Runtime, byUser: Map<string, Outbound[]>): Promise<void> {
  const ts = Date.now();
  const out = new Map<string, OutboundEvent[]>();
  for (const [uid, evs] of byUser) out.set(uid, evs.map((e) => ({ type: e.type, data: e.data, ts, logged: true })));
  return deliverPerUser(rt, out);
}

function push(byUser: Map<string, Outbound[]>, userId: string, e: Outbound): void {
  const list = byUser.get(userId);
  if (list) list.push(e);
  else byUser.set(userId, [e]);
}

/** Skips if actor === recipient or blocked either way. INSERT OR IGNORE by dedupe_key.
 *  Publishes notification.created (logged) and, if settings allow, alert (logged). */
export function notify(rt: Runtime, input: NotifyInput): Promise<void> {
  return notifyMany(rt, [input]);
}

export async function notifyMany(rt: Runtime, inputs: NotifyInput[]): Promise<void> {
  const db = rt.env.DB;
  const candidates = inputs.filter((i) => i.actorId === null || i.actorId !== i.userId);
  if (candidates.length === 0) return;
  const withActor = candidates.flatMap((inp, idx) => (inp.actorId === null ? [] : [{ idx, pair: [inp.userId, inp.actorId] as Pair }]));
  const blocked = await blockedPairs(db, withActor.map((w) => w.pair));
  const blockedIdx = new Set(withActor.filter((_, j) => blocked.has(j)).map((w) => w.idx));
  const keep = candidates.filter((_, idx) => !blockedIdx.has(idx));
  if (keep.length === 0) return;

  // One statement per row (9 params each), all in one batch: never a multi-row VALUES past the bind limit.
  const t = now();
  const rows: NotificationRow[] = keep.map((inp) => {
    const extra: NotificationExtra = { preview: clip(inp.preview ?? "", PREVIEW_MAX), context: inp.context ?? {} };
    return {
      id: newId(t),
      user_id: inp.userId,
      type: inp.type,
      actor_id: inp.actorId,
      subject_type: inp.subject.type,
      subject_id: inp.subject.id,
      extra: JSON.stringify(extra),
      dedupe_key: inp.dedupeKey ?? null,
      created_at: t,
      read_at: null,
    };
  });
  const results = await batch(
    db,
    rows.map((r) =>
      stmt(
        db,
        `INSERT OR IGNORE INTO notifications (id, user_id, type, actor_id, subject_type, subject_id, extra, dedupe_key, created_at)
         VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9)`,
        r.id,
        r.user_id,
        r.type,
        r.actor_id,
        r.subject_type,
        r.subject_id,
        r.extra,
        r.dedupe_key,
        r.created_at,
      ),
    ),
  );
  const inserted = rows.map((r, i) => ({ row: r, input: keep[i]! })).filter((_, i) => results[i]!.meta.changes > 0);
  if (inserted.length === 0) return;

  const recipients = inserted.map((x) => x.row.user_id);
  const alertIdx = inserted.flatMap((x, i) => (x.input.alert ? [i] : []));
  const [notifications, counts, allowed, actors] = await Promise.all([
    serializeNotifications(db, inserted.map((x) => x.row)),
    unreadCounts(db, recipients),
    alertAllowed(
      db,
      alertIdx.map((i) => ({ userId: inserted[i]!.row.user_id, kind: inserted[i]!.input.alert!.kind, channelId: inserted[i]!.input.context?.channel_id })),
    ),
    loadUserSummaries(db, inserted.flatMap((x) => (x.input.actorId === null ? [] : [x.input.actorId]))),
  ]);
  const alertOk = new Set(alertIdx.filter((_, j) => allowed[j]));

  const byUser = new Map<string, Outbound[]>();
  inserted.forEach((x, i) => {
    const uid = x.row.user_id;
    push(byUser, uid, { type: "notification.created", data: { notification: notifications[i]!, unread_count: counts.get(uid) ?? 0 } });
    if (alertOk.has(i)) {
      const actor = x.input.actorId === null ? null : (actors.get(x.input.actorId) ?? null);
      push(byUser, uid, { type: "alert", data: { alert: buildAlert(x.input.alert!, actor, t) } });
    }
  });
  await publishPerRecipient(rt, byUser);
}

/** Alert without an inbox row (DMs, channel_message). Applies settings, channel prefs, blocks. */
export async function sendAlerts(rt: Runtime, items: AlertItem[]): Promise<void> {
  const db = rt.env.DB;
  const candidates = items.filter((i) => i.actorId === null || i.actorId !== i.userId);
  if (candidates.length === 0) return;
  const withActor = candidates.flatMap((it, idx) => (it.actorId === null ? [] : [{ idx, pair: [it.userId, it.actorId] as Pair }]));
  const blocked = await blockedPairs(db, withActor.map((w) => w.pair));
  const blockedIdx = new Set(withActor.filter((_, j) => blocked.has(j)).map((w) => w.idx));
  const keep = candidates.filter((_, idx) => !blockedIdx.has(idx));
  if (keep.length === 0) return;
  const allowed = await alertAllowed(db, keep);
  const send = keep.filter((_, i) => allowed[i]);
  if (send.length === 0) return;
  const actors = await loadUserSummaries(db, send.flatMap((s) => (s.actorId === null ? [] : [s.actorId])));
  const t = now();
  const byUser = new Map<string, Outbound[]>();
  for (const s of send) {
    const actor = s.actorId === null ? null : (actors.get(s.actorId) ?? null);
    push(byUser, s.userId, { type: "alert", data: { alert: buildAlert(s, actor, t) } });
  }
  await publishPerRecipient(rt, byUser);
}

// ---------------------------------------------------------------------------
// message notifications (Task 24; table in plan 01 Task 15)

const GROUP_TITLE_MEMBERS = 3;

/** Of `userIds`, those who can read `channel` now (api.md 3.7), one json_each query. */
async function channelReadersAmong(db: D1Database, channel: ChannelRow, userIds: string[]): Promise<Set<string>> {
  const ids = [...new Set(userIds)];
  if (ids.length === 0) return new Set();
  const cond =
    channel.kind === "text"
      ? `EXISTS (SELECT 1 FROM space_members sm JOIN spaces s ON s.id = sm.space_id AND s.deleted_at IS NULL
                  WHERE sm.space_id = ?3 AND sm.user_id = j.value)
         AND (?4 = 0 OR EXISTS (SELECT 1 FROM channel_members cm WHERE cm.channel_id = ?2 AND cm.user_id = j.value))`
      : "EXISTS (SELECT 1 FROM channel_members cm WHERE cm.channel_id = ?2 AND cm.user_id = j.value)";
  const rows = await all<{ user_id: string }>(
    db,
    `SELECT j.value AS user_id FROM json_each(?1) j WHERE ${cond}`,
    jsonList(ids),
    channel.id,
    channel.space_id,
    channel.is_private,
  );
  return new Set(rows.map((r) => r.user_id));
}

interface MessageAlertParts {
  title: string;
  body: string;
  preview: string;
  target: Alert["target"];
  context: Partial<Notification["context"]>;
  /** dm / group_dm members other than the author, in joined order. */
  convMembers: string[];
}

/**
 * Alert title / body / target for a message (plan 01 Task 24): text "<name> · <space> #<channel>", dm "<name>",
 * group_dm "<name> · <group name or 3 member names>"; body = plainSnippet(body, 200) (a poll uses its question).
 */
async function messageAlertParts(db: D1Database, message: Message, channel: ChannelRow): Promise<MessageAlertParts> {
  const authorId = message.author.id;
  const name = message.author.display_name;
  const text = message.kind === "poll" ? (message.poll?.question ?? "") : message.body;
  let title = name;
  let convMembers: string[] = [];
  if (channel.kind === "text") {
    const space = channel.space_id === null ? null : await first<{ name: string }>(db, "SELECT name FROM spaces WHERE id = ?1", channel.space_id);
    title = `${name} · ${space?.name ?? ""} #${channel.name ?? ""}`;
  } else {
    const rows = await all<{ user_id: string }>(db, "SELECT user_id FROM channel_members WHERE channel_id = ?1 ORDER BY joined_at, user_id", channel.id);
    convMembers = rows.map((r) => r.user_id).filter((u) => u !== authorId);
    if (channel.kind === "group_dm") {
      let group = (channel.name ?? "").trim();
      if (group === "") {
        const shown = convMembers.slice(0, GROUP_TITLE_MEMBERS);
        const users = await loadUserSummaries(db, shown);
        group = shown.flatMap((u) => (users.has(u) ? [users.get(u)!.display_name] : [])).join(", ");
      }
      title = group === "" ? name : `${name} · ${group}`;
    }
  }
  const spaceId = channel.kind === "text" ? channel.space_id : null;
  return {
    title,
    body: plainSnippet(text, BODY_MAX),
    preview: plainSnippet(text, PREVIEW_MAX),
    target: {
      type: "message",
      id: message.id,
      channel_id: channel.id,
      message_id: message.id,
      thread_root_id: message.thread_root_id,
      post_id: null,
      space_id: spaceId,
    },
    context: { channel_id: channel.id, message_id: message.id, thread_root_id: message.thread_root_id, space_id: spaceId },
    convMembers,
  };
}

function mentionInput(message: Message, parts: MessageAlertParts, userId: string, withAlert: boolean): NotifyInput {
  return {
    userId,
    type: "message_mention",
    actorId: message.author.id,
    subject: { type: "message", id: message.id },
    preview: parts.preview,
    context: parts.context,
    dedupeKey: `message_mention:${message.id}:${userId}`,
    alert: withAlert ? { kind: "mention", title: parts.title, body: parts.body, target: parts.target } : null,
  };
}

/**
 * Per-recipient alerts for a new message (one each, dm > mention > thread_reply > channel_message) and inbox rows:
 * message_mention to every message_mentions row (explicit + @here, already filtered by api.md 2.1; none in a 1:1 dm),
 * thread_reply to the root author + thread participants who still read the channel and were not mentioned.
 * channel_message: text channel, top-level only, members whose prefs say notify 'all'. The author never gets anything;
 * blocks, settings switches and channel prefs are applied by notifyMany / sendAlerts.
 */
export async function fanoutMessageNotifications(rt: Runtime, message: Message, channel: ChannelRow): Promise<void> {
  const db = rt.env.DB;
  if (message.kind === "system" || message.deleted) return;
  const authorId = message.author.id;
  const rootId = message.thread_root_id;

  const [parts, mentionRows, threadRows, prefsRows] = await Promise.all([
    messageAlertParts(db, message, channel),
    all<{ user_id: string }>(db, "SELECT user_id FROM message_mentions WHERE message_id = ?1 ORDER BY rowid", message.id),
    rootId === null
      ? Promise.resolve([] as Array<{ user_id: string }>)
      : all<{ user_id: string }>(
          db,
          "SELECT user_id FROM thread_participants WHERE root_id = ?1 UNION SELECT author_id AS user_id FROM messages WHERE id = ?1",
          rootId,
        ),
    channel.kind !== "text" || rootId !== null
      ? Promise.resolve([] as Array<{ user_id: string }>)
      : all<{ user_id: string }>(db, "SELECT user_id FROM channel_prefs WHERE channel_id = ?1 AND notify = 'all' AND muted = 0", channel.id),
  ]);

  const dmTo = new Set(parts.convMembers); // dm / group_dm: everyone else gets the dm alert
  const mentioned = mentionRows.map((r) => r.user_id).filter((u) => u !== authorId);
  const mentionedSet = new Set(mentioned);
  const readers = await channelReadersAmong(db, channel, [...threadRows, ...prefsRows].map((r) => r.user_id));
  const threadTo = [...new Set(threadRows.map((r) => r.user_id))].filter((u) => u !== authorId && !mentionedSet.has(u) && readers.has(u));
  const threadSet = new Set(threadTo);
  const channelTo = prefsRows.map((r) => r.user_id).filter((u) => u !== authorId && !mentionedSet.has(u) && !threadSet.has(u) && readers.has(u));

  const inbox: NotifyInput[] = [];
  if (channel.kind !== "dm") for (const u of mentioned) inbox.push(mentionInput(message, parts, u, !dmTo.has(u)));
  for (const u of threadTo) {
    inbox.push({
      userId: u,
      type: "thread_reply",
      actorId: authorId,
      subject: { type: "message", id: message.id },
      preview: parts.preview,
      context: parts.context,
      dedupeKey: `thread_reply:${message.id}:${u}`,
      alert: dmTo.has(u) ? null : { kind: "thread_reply", title: parts.title, body: parts.body, target: parts.target },
    });
  }
  const alerts: AlertItem[] = [
    ...[...dmTo].map((u) => ({ userId: u, kind: "dm" as const })),
    ...channelTo.map((u) => ({ userId: u, kind: "channel_message" as const })),
  ].map((a) => ({ ...a, actorId: authorId, title: parts.title, body: parts.body, target: parts.target, channelId: channel.id }));

  await Promise.all([notifyMany(rt, inbox), sendAlerts(rt, alerts)]);
}

/**
 * message_mention inbox rows (+ mention alert) for mentions an edit newly added (api.md 5.10 PATCH). Not in a 1:1 dm;
 * the dedupe key keeps it to one row per person per message however many times the message is edited.
 */
export async function notifyMessageMentions(rt: Runtime, message: Message, channel: ChannelRow, userIds: string[]): Promise<void> {
  if (channel.kind === "dm" || message.deleted) return;
  const targets = [...new Set(userIds)].filter((u) => u !== message.author.id);
  if (targets.length === 0) return;
  const parts = await messageAlertParts(rt.env.DB, message, channel);
  await notifyMany(
    rt,
    targets.map((u) => mentionInput(message, parts, u, true)),
  );
}
