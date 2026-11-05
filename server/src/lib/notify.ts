// Inbox rows + desktop alerts (server-architecture.md 4.12, events.md 4.7, api.md 5.18).
// - notify/notifyMany: skip actor === recipient and blocks either way; INSERT OR IGNORE by dedupe_key; only rows that
//   really went in publish `notification.created` (logged) and, when the recipient's switch allows, `alert` (logged).
// - sendAlerts: alerts without an inbox row (dm, channel_message) — settings, channel prefs and blocks applied.
// - suppressed / suppressed_reason (focus, dnd) are filled by UserHub, never here.
// - Every lookup over many recipients is one json_each query (D1 caps bound params at 100 per statement).
import type { Runtime } from "../env";
import type { Alert, AlertKind, Notification, NotificationType } from "../types/api";
import type { NotificationRow, UserSettingsRow } from "../types/rows";
import { all, batch, jsonList, stmt } from "./db";
import { newId } from "./ids";
import { publishEvents, resolveOnlineAudience } from "./realtime";
import { type NotificationExtra, loadUserSummaries, serializeNotifications } from "./serialize";
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

/** RPC fan-out of per-recipient batches runs this many recipients at a time. */
const PUBLISH_CHUNK = 50;

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

/** Unread inbox count per recipient, hiding rows whose actor the recipient blocked (same rule as the inbox list). */
async function unreadCounts(db: D1Database, userIds: string[]): Promise<Map<string, number>> {
  if (userIds.length === 0) return new Map();
  const rows = await all<{ user_id: string; n: number }>(
    db,
    `SELECT r.value AS user_id,
            (SELECT COUNT(*) FROM notifications n
              WHERE n.user_id = r.value AND n.read_at IS NULL
                AND NOT EXISTS (SELECT 1 FROM blocks b WHERE b.blocker_id = r.value AND b.blocked_id = n.actor_id)) AS n
       FROM json_each(?1) r`,
    jsonList([...new Set(userIds)]),
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

/** One publishEvents (one UserHub RPC) per online recipient; offline recipients cost nothing past one query. */
async function publishPerRecipient(rt: Runtime, byUser: Map<string, Outbound[]>): Promise<void> {
  if (byUser.size === 0) return;
  const online = await resolveOnlineAudience(rt.env, { users: [...byUser.keys()] }, { notifyOk: true });
  for (let i = 0; i < online.length; i += PUBLISH_CHUNK) {
    await Promise.all(online.slice(i, i + PUBLISH_CHUNK).map((u) => publishEvents(rt, { users: [u] }, byUser.get(u) ?? [])));
  }
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
