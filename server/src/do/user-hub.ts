// Per-user realtime hub (server-architecture.md 3.2, events.md 1-3, 5, 6.2).
// Task 9: upgrade, epochs + D1 liveness on connect, hello, resume -> ready, client frame validation and rate limit.
// Task 10: deliver (seq log), resume replay, device acks, close handling and the alarm (heartbeat, liveness, log trim).
// Task 11: device.tui_state to same-device notify sockets on full 0<->1 transitions (hello.local_tui_connected is set in fetch).
// Task 12: presence (events.md 6.3) recomputed on connect/close/idle/mode/focus/alarm, presence.snapshot after ready,
// alert suppression (focus/dnd) with focus_alerts, focus mode start/end (one finishFocus for early and natural end).
// Task 13: `watch` frames (TopicHub join/leave, topic events only to sockets watching them).
// Task 14: revokeSessions (session.revoked then 4001/4003).
// Task 27: `typing` frames (write access check, 2s per-connection dedupe, volatile fan-out to the channel minus me).
import { DurableObject } from "cloudflare:workers";
import type { AuthContext, Runtime } from "../env";
import { all } from "../lib/db";
import { newId, isId } from "../lib/ids";
import { getChannelAccess } from "../lib/permissions";
import { publishEvent } from "../lib/realtime";
import { loadPresenceItems } from "../lib/serialize";
import { iso } from "../lib/time";
import type { Alert, Focus, FocusSummary, PresenceState, PresenceStatus } from "../types/api";
import type { HelloData, PresenceItem, ReadyData, ReplyErrorCode, ServerEventType } from "../types/events";
import { ClientFrameSchema, CloseCode, HEARTBEAT, LIMITS, NOTIFY_ALLOWED_TYPES, type ClientFrame } from "./protocol";

/** Per-socket state kept with `ws.serializeAttachment` (server-architecture.md 3.2; JSON, <= 16,384 bytes). */
export interface ConnAttachment {
  connId: string;
  sessionId: string;
  userId: string;
  mode: "full" | "notify";
  deviceId: string;
  connectedAt: number;
  helloSeq: number; // hello.seq: the replay base for resume rules 1 and 4 (events.md 3.3)
  ready: boolean; // resume handled
  idle: boolean;
  topics: string[]; // watched topics (<= 50)
  typingAt: Record<string, number>; // channelId(+":"+threadRootId) -> last forwarded ms (<= 20 keys)
  win: { start: number; count: number }; // frame rate window
}

/** One event handed to a UserHub (server-architecture.md 3.2). */
export interface OutboundEvent {
  type: ServerEventType; // events.md 8
  data: unknown;
  ts: number; // ms
  logged: boolean; // true => seq + log
  topic?: string; // set for topic events; delivered only to sockets watching it
  onlyDevice?: string; // device.tui_state
}

export interface DeliveryBatch {
  userId: string;
  events: OutboundEvent[];
}

export type ConnMode = ConnAttachment["mode"];

export const MAX_CONNECTIONS = 10; // events.md 1.1
export const RESUME_TIMEOUT_MS = 10_000; // events.md 1.2 (4000 if no resume within 10s of hello)
export const FULL_ONLINE_MS = 10 * 60_000; // events.md 6.2 online_until = now + 10 min
export const NOTIFY_ONLINE_MS = 40 * 60_000; // events.md 6.2 notify_online_until = now + 40 min
export const FULL_ALARM_MS = 60_000; // alarm cadence with a full socket (events.md 3.4)
export const NOTIFY_ALARM_MS = 15 * 60_000; // alarm cadence with only notify sockets
export const CLOSE_GRACE_MS = 120_000; // events.md 6.2: last socket of a mode gone -> liveness = now + 120s
export const FULL_EXTEND_BELOW_MS = 5 * 60_000; // alarm re-extends online_until when less than this is left (~1 D1 write / 5 min)
// notify liveness is re-extended at most about once per 15 min whatever the alarm cadence (60s with a full socket, 15 min without):
// 40 min - 30 min = 10 min between writes with a 60s alarm; every 15-min alarm with notify sockets only.
export const NOTIFY_EXTEND_BELOW_MS = 30 * 60_000;
export const LOG_KEEP_EVENTS = 2000; // events.md 6.2: keep the last 2,000 events or 24 hours, whichever is smaller
export const LOG_KEEP_MS = 86_400_000;
const RATE_WINDOW_MS = 10_000; // events.md 2: 30 frames / 10s -> reply{rate_limited}; 60 -> 4008
const RATE_SOFT = 30;
const RATE_HARD = 60;

export const TYPING_DEDUPE_MS = 2_000; // events.md 5: duplicates per connection + channel(+thread) within 2s are dropped
export const TYPING_EXPIRES_MS = 6_000; // events.md 4: typing.expires_in_ms
export const TYPING_KEYS_MAX = 20; // ConnAttachment.typingAt keeps at most 20 keys
export const TYPING_ACCESS_CACHE_MS = 5 * 60_000; // server-architecture.md 3.2: write access may be cached for 5 min

export const WATCH_LEASE_MS = 300_000; // events.md 5: server lease 300s (clients resend `watch` every 120s)

export const FOCUS_MIN_SEC = 60; // api.md 5.1 POST /me/focus duration_sec 60..43200
export const FOCUS_MAX_SEC = 43_200;
export const FOCUS_SUMMARY_ITEMS = 20; // FocusSummary.items: newest first, <= 20

export type PresenceMode = PresenceState["mode"];

/** What the presence state was when it was last persisted/published (meta.last_state); D1's defaults for a new user. */
const INITIAL_STATE_JSON = JSON.stringify({ status: "offline", mode: "auto", focus: null } satisfies PresenceState);

/** The synchronous half of a presence recompute: what to send/write after meta was already updated. */
interface PresencePlan {
  userId: string;
  self: OutboundEvent; // presence.self (volatile)
  watchers: PresenceItem | null; // presence.updated (volatile) — only when the status changed
}

const KNOWN_CLIENT_TYPES: ReadonlySet<string> = new Set(["resume", "ack", "typing", "idle", "watch"]);
const FULL_ONLY_TYPES: ReadonlySet<string> = new Set(["typing", "idle", "watch"]);

const SCHEMA = [
  "CREATE TABLE IF NOT EXISTS meta (k TEXT PRIMARY KEY, v TEXT NOT NULL)",
  "CREATE TABLE IF NOT EXISTS events (seq INTEGER PRIMARY KEY, type TEXT NOT NULL, ts INTEGER NOT NULL, frame TEXT NOT NULL)",
  "CREATE TABLE IF NOT EXISTS device_acks (device_id TEXT NOT NULL, mode TEXT NOT NULL CHECK (mode IN ('full','notify')), epoch TEXT NOT NULL, seq INTEGER NOT NULL, updated_at INTEGER NOT NULL, PRIMARY KEY (device_id, mode))",
  "CREATE TABLE IF NOT EXISTS focus_alerts (id TEXT PRIMARY KEY, kind TEXT NOT NULL, alert TEXT NOT NULL, created_at INTEGER NOT NULL)",
];

const encoder = new TextEncoder();

/** UTF-8 byte length, without encoding when the answer is obvious (each UTF-16 unit is 1..3 bytes). */
function utf8Bytes(s: string): number {
  if (s.length * 3 <= LIMITS.maxFrameBytes) return s.length;
  return encoder.encode(s).byteLength;
}

function isAuthContext(v: unknown): v is AuthContext {
  if (v === null || typeof v !== "object") return false;
  const a = v as Record<string, unknown>;
  return typeof a.userId === "string" && typeof a.sessionId === "string" && typeof a.handle === "string" && (a.role === "user" || a.role === "admin");
}

/** The `ref` of a client frame, when it is a usable correlation id (events.md 2: 1..32 chars). */
function refOf(v: unknown): string | null {
  if (v === null || typeof v !== "object") return null;
  const ref = (v as Record<string, unknown>).ref;
  return typeof ref === "string" && ref.length >= 1 && ref.length <= 32 ? ref : null;
}

export class UserHub extends DurableObject<Cloudflare.Env> {
  constructor(ctx: DurableObjectState, env: Cloudflare.Env) {
    super(ctx, env);
    void ctx.blockConcurrencyWhile(async () => {
      for (const stmt of SCHEMA) ctx.storage.sql.exec(stmt);
    });
    // events.md 3.4: answered by the runtime without waking the object.
    ctx.setWebSocketAutoResponse(new WebSocketRequestResponsePair('{"type":"ping"}', '{"type":"pong"}'));
  }

  /** Wall clock; tests override it on the instance inside runInDurableObject. */
  protected now(): number {
    return Date.now();
  }

  // ---------------------------------------------------------------- storage helpers

  private get sql(): SqlStorage {
    return this.ctx.storage.sql;
  }

  protected getMeta(k: string): string | null {
    const row = this.sql.exec<{ v: string }>("SELECT v FROM meta WHERE k = ?", k).toArray()[0];
    return row ? row.v : null;
  }

  protected getMetaNum(k: string): number {
    const v = this.getMeta(k);
    const n = v === null || v === "" ? 0 : Number(v);
    return Number.isFinite(n) ? n : 0;
  }

  protected setMeta(k: string, v: string | number): void {
    this.sql.exec("INSERT INTO meta (k, v) VALUES (?, ?) ON CONFLICT (k) DO UPDATE SET v = excluded.v", k, String(v));
  }

  protected currentSeq(): number {
    return this.getMetaNum("seq");
  }

  /** Oldest seq still in the log; seq + 1 when the log is empty (events.md 3.2). */
  protected oldestSeq(): number {
    const row = this.sql.exec<{ s: number | null }>("SELECT MIN(seq) AS s FROM events").toArray()[0];
    return row && row.s !== null ? row.s : this.currentSeq() + 1;
  }

  protected epochOf(mode: ConnMode): string {
    return this.getMeta(mode === "full" ? "epoch_full" : "epoch_notify") ?? "";
  }

  // ---------------------------------------------------------------- socket helpers

  protected att(ws: WebSocket): ConnAttachment | null {
    return (ws.deserializeAttachment() as ConnAttachment | null) ?? null;
  }

  /** Open sockets (optionally by tag), excluding ones we already started closing. */
  protected liveSockets(tag?: string): WebSocket[] {
    return this.ctx.getWebSockets(tag).filter((ws) => ws.readyState === WebSocket.OPEN);
  }

  protected sendFrame(ws: WebSocket, type: ServerEventType, data: unknown, seq: number | null = null, ts = this.now()): void {
    try {
      ws.send(JSON.stringify({ type, seq, ts: iso(ts), data }));
    } catch {
      // closing socket — ignore
    }
  }

  protected reply(ws: WebSocket, ref: string | null, error: { code: ReplyErrorCode; message: string } | null): void {
    if (ref === null) return; // no ref: success is silent and failures are ignored (events.md 2)
    this.sendFrame(ws, "reply", { ref, ok: error === null, error });
  }

  protected closeWs(ws: WebSocket, code: number, reason: string): void {
    try {
      ws.close(code, reason);
    } catch {
      // already closed
    }
  }

  /**
   * Next alarm = min(full socket ? now+60s : notify-only ? now+15min, pending resume deadlines, focus end).
   * Never moves an earlier alarm later. With no socket and no focus nothing is scheduled.
   */
  protected async scheduleAlarm(): Promise<void> {
    const t = this.now();
    const sockets = this.liveSockets();
    let next = Number.POSITIVE_INFINITY;
    if (sockets.length > 0) {
      next = this.liveSockets("mode:full").length > 0 ? t + FULL_ALARM_MS : t + NOTIFY_ALARM_MS;
      for (const ws of sockets) {
        const a = this.att(ws);
        if (a && !a.ready) {
          const deadline = a.connectedAt + RESUME_TIMEOUT_MS;
          if (deadline > t && deadline < next) next = deadline;
        }
      }
    }
    const focusEnds = this.getMetaNum("focus_ends_at");
    if (focusEnds > 0) next = Math.min(next, Math.max(focusEnds, t)); // an overdue focus end runs at once
    if (!Number.isFinite(next)) return;
    const current = await this.ctx.storage.getAlarm();
    if (current === null || current > next) await this.ctx.storage.setAlarm(next);
  }

  // ---------------------------------------------------------------- upgrade

  /** WebSocket upgrade only. The Worker has already authenticated; it passes AuthContext in header "X-Masil-Auth" (JSON). */
  async fetch(request: Request): Promise<Response> {
    if ((request.headers.get("Upgrade") ?? "").toLowerCase() !== "websocket") {
      return new Response("expected websocket upgrade", { status: 400 });
    }
    // 1. auth (set by routes/ws.ts only), mode, device_id
    let auth: unknown = null;
    try {
      auth = JSON.parse(request.headers.get("X-Masil-Auth") ?? "null");
    } catch {
      auth = null;
    }
    if (!isAuthContext(auth)) return new Response("missing auth", { status: 401 });
    const url = new URL(request.url);
    const modeRaw = url.searchParams.get("mode") ?? "full";
    const deviceId = url.searchParams.get("device_id") ?? "";
    if ((modeRaw !== "full" && modeRaw !== "notify") || !isId(deviceId)) return new Response("invalid query", { status: 400 });
    const mode: ConnMode = modeRaw;
    if (this.getMeta("user_id") === null) this.setMeta("user_id", auth.userId);

    // 2. at the cap: replace the oldest connection
    const open = this.liveSockets()
      .map((ws) => ({ ws, a: this.att(ws) }))
      .sort((x, y) => (x.a?.connId ?? "").localeCompare(y.a?.connId ?? ""));
    const replacedFullDevices: string[] = [];
    for (let i = 0; i <= open.length - MAX_CONNECTIONS; i++) {
      this.closeWs(open[i].ws, CloseCode.replaced, "replaced");
      if (open[i].a?.mode === "full") replacedFullDevices.push(open[i].a!.deviceId);
    }

    // 3-4. epoch check + D1 liveness write, serialized so concurrent connects don't rotate an epoch twice.
    const accepted = await this.ctx.blockConcurrencyWhile(async () => {
      const t = this.now();
      const fullUntil = this.getMetaNum("full_until");
      const notifyUntil = this.getMetaNum("notify_until");
      if (this.epochOf("full") === "" || t >= fullUntil) this.setMeta("epoch_full", newId());
      if (this.epochOf("notify") === "" || t >= Math.max(fullUntil, notifyUntil)) this.setMeta("epoch_notify", newId());
      try {
        const until = await this.extendLiveness(auth.userId, mode, t);
        this.setMeta(mode === "full" ? "full_until" : "notify_until", until); // raise only after D1 succeeded
        this.setMeta("zero_since", ""); // a socket is connected from here on
        return true;
      } catch (e) {
        console.error("UserHub: presence write failed on connect", e);
        return false; // never throw inside blockConcurrencyWhile (it would reset the object)
      }
    });

    const pair = new WebSocketPair();
    const [client, server] = Object.values(pair) as [WebSocket, WebSocket];
    if (!accepted) {
      server.accept();
      server.close(CloseCode.internal, "presence unavailable");
      return new Response(null, { status: 101, webSocket: client });
    }

    // 5. accept, attachment, hello, alarm
    const t = this.now();
    const localTui = this.liveSockets(`dev:${deviceId}`).some((ws) => this.att(ws)?.mode === "full");
    const seq = this.currentSeq();
    const a: ConnAttachment = {
      connId: newId(),
      sessionId: auth.sessionId,
      userId: auth.userId,
      mode,
      deviceId,
      connectedAt: t,
      helloSeq: seq,
      ready: false,
      idle: false,
      topics: [],
      typingAt: {},
      win: { start: t, count: 0 },
    };
    this.ctx.acceptWebSocket(server, [`mode:${mode}`, `dev:${deviceId}`, `sess:${auth.sessionId}`]);
    server.serializeAttachment(a);
    const hello: HelloData = {
      connection_id: a.connId,
      session_id: a.sessionId,
      user_id: a.userId,
      mode,
      device_id: deviceId,
      epoch: this.epochOf(mode),
      seq,
      oldest_seq: this.oldestSeq(),
      heartbeat_interval_ms: HEARTBEAT.intervalMs,
      heartbeat_timeout_ms: HEARTBEAT.timeoutMs,
      local_tui_connected: localTui,
      server_time: iso(t),
      limits: { max_frame_bytes: LIMITS.maxFrameBytes, watch_topics_max: LIMITS.watchTopicsMax, typing_min_interval_ms: LIMITS.typingMinIntervalMs },
    };
    this.sendFrame(server, "hello", hello, null, t);
    // full 0->1 on this device: tell its daemons (events.md 7). Replaced full sockets may have been a device's last one.
    if (mode === "full" && !localTui) this.sendTuiState(deviceId, true);
    for (const dev of new Set(replacedFullDevices)) this.signalTuiGone(dev);
    await this.recomputePresence();
    await this.scheduleAlarm();
    return new Response(null, { status: 101, webSocket: client });
  }

  /** D1 liveness for one mode: never shortens (MAX); returns the stored value (the new watermark). Throws on D1 failure. */
  protected async extendLiveness(userId: string, mode: ConnMode, t: number): Promise<number> {
    const col = mode === "full" ? "online_until" : "notify_online_until";
    const target = t + (mode === "full" ? FULL_ONLINE_MS : NOTIFY_ONLINE_MS);
    const row = await this.env.DB.prepare(
      `INSERT INTO user_presence (user_id, ${col}, updated_at) VALUES (?1, ?2, ?3)
       ON CONFLICT (user_id) DO UPDATE SET ${col} = MAX(${col}, excluded.${col}), updated_at = excluded.updated_at
       RETURNING ${col} AS until`,
    )
      .bind(userId, target, t)
      .first<{ until: number }>();
    if (row === null) throw new Error("user_presence upsert returned no row");
    return row.until;
  }

  // ---------------------------------------------------------------- delivery

  /**
   * Deliver events to this user's sockets (server-architecture.md 3.2 deliver 1-4). Logged events get seq + go to the log.
   * Fully synchronous after the call starts, so it never interleaves with a resume replay: an event is either in the
   * replayed range or sent live after `ready`, never both and never neither.
   */
  async deliver(batch: DeliveryBatch): Promise<void> {
    if (batch.events.length === 0) return;
    // 1-3 in one SQL transaction: user_id, seq + frame + log row per logged event
    const out = this.ctx.storage.transactionSync(() => {
      if (this.getMeta("user_id") === null) this.setMeta("user_id", batch.userId);
      let seq = this.currentSeq();
      const frames: { ev: OutboundEvent; frame: string }[] = [];
      for (const ev of batch.events) {
        const data = this.prepareEventData(ev);
        if (ev.logged) {
          seq++;
          const frame = JSON.stringify({ type: ev.type, seq, ts: iso(ev.ts), data });
          this.sql.exec("INSERT INTO events (seq, type, ts, frame) VALUES (?, ?, ?, ?)", seq, ev.type, ev.ts, frame);
          frames.push({ ev, frame });
        } else {
          frames.push({ ev, frame: JSON.stringify({ type: ev.type, seq: null, ts: iso(ev.ts), data }) });
        }
      }
      if (seq !== this.currentSeq()) this.setMeta("seq", seq);
      return frames;
    });

    // 4. ready sockets only; notify sockets get the events.md 7 types only; topic events only where watched
    const targets = this.liveSockets()
      .map((ws) => ({ ws, a: this.att(ws) }))
      .filter((x): x is { ws: WebSocket; a: ConnAttachment } => x.a !== null && x.a.ready);
    for (const { ev, frame } of out) {
      for (const { ws, a } of targets) {
        if (a.mode === "notify" && !NOTIFY_ALLOWED_TYPES.has(ev.type)) continue;
        if (ev.topic !== undefined && !a.topics.includes(ev.topic)) continue;
        if (ev.onlyDevice !== undefined && a.deviceId !== ev.onlyDevice) continue;
        try {
          ws.send(frame);
        } catch {
          // closing socket — ignore
        }
      }
    }
  }

  /**
   * deliver step 2 (server-architecture.md 3.2, events.md 4.7): an alert is framed with suppressed/suppressed_reason from
   * this hub's state — focus active -> "focus" + a focus_alerts row (summary), manual dnd -> "dnd" (not accumulated).
   * focus_summary is never suppressed ("항상"). Runs inside deliver's SQL transaction.
   */
  protected prepareEventData(ev: OutboundEvent): unknown {
    if (ev.type !== "alert") return ev.data;
    const data = ev.data as { alert?: Alert } | null;
    const alert = data?.alert;
    if (alert === undefined || alert === null || typeof alert !== "object") return ev.data;
    let out: Alert;
    if (alert.kind === "focus_summary") {
      out = { ...alert, suppressed: false, suppressed_reason: null };
    } else if (this.getMetaNum("focus_ends_at") > this.now()) {
      out = { ...alert, suppressed: true, suppressed_reason: "focus" };
      this.sql.exec(
        "INSERT OR IGNORE INTO focus_alerts (id, kind, alert, created_at) VALUES (?, ?, ?, ?)",
        out.id,
        out.kind,
        JSON.stringify(out),
        ev.ts,
      );
    } else if (this.manualMode() === "dnd") {
      out = { ...alert, suppressed: true, suppressed_reason: "dnd" };
    } else {
      out = { ...alert, suppressed: false, suppressed_reason: null };
    }
    return { ...data, alert: out };
  }

  // ---------------------------------------------------------------- device.tui_state

  /** Volatile `device.tui_state` to every open notify socket of `deviceId` (ready or not: it is a state signal, idempotent). */
  protected sendTuiState(deviceId: string, connected: boolean): void {
    for (const ws of this.liveSockets(`dev:${deviceId}`)) {
      if (this.att(ws)?.mode !== "notify") continue;
      this.sendFrame(ws, "device.tui_state", { device_id: deviceId, tui_connected: connected });
    }
  }

  /** A full socket of `deviceId` went away (closed by either side): if none is left open there, signal 1->0. */
  protected signalTuiGone(deviceId: string, goneConnId?: string): void {
    const anyFull = this.liveSockets(`dev:${deviceId}`).some((ws) => {
      const a = this.att(ws);
      return a !== null && a.mode === "full" && a.connId !== goneConnId;
    });
    if (!anyFull) this.sendTuiState(deviceId, false);
  }

  // ---------------------------------------------------------------- client frames

  async webSocketMessage(ws: WebSocket, message: string | ArrayBuffer): Promise<void> {
    if (ws.readyState !== WebSocket.OPEN) return; // we already closed it
    const a = this.att(ws);
    if (a === null) return this.closeWs(ws, CloseCode.internal, "lost connection state");
    // events.md 2: text frames only, <= 8KB
    if (typeof message !== "string") return this.closeWs(ws, CloseCode.protocol, "binary frames are not allowed");
    if (utf8Bytes(message) > LIMITS.maxFrameBytes) return this.closeWs(ws, CloseCode.tooBig, "frame too big");

    let raw: unknown;
    let parsedJson = true;
    try {
      raw = JSON.parse(message);
    } catch {
      parsedJson = false;
    }
    const ref = parsedJson ? refOf(raw) : null;

    // frame rate: 30 / 10s -> reply{rate_limited} (frame dropped), > 60 -> 4008. `ping` never gets here.
    const t = this.now();
    if (t - a.win.start >= RATE_WINDOW_MS) a.win = { start: t, count: 0 };
    a.win.count++;
    ws.serializeAttachment(a);
    if (a.win.count > RATE_HARD) return this.closeWs(ws, CloseCode.rateLimited, "too many frames");
    if (a.win.count > RATE_SOFT) return this.reply(ws, ref, { code: "rate_limited", message: "too many frames" });

    const type = parsedJson && raw !== null && typeof raw === "object" ? (raw as Record<string, unknown>).type : undefined;
    const parsed = parsedJson ? ClientFrameSchema.safeParse(raw) : null;

    // Before ready only a valid resume is acceptable; after ready a second resume is a protocol error.
    if (!a.ready) {
      if (!parsed?.success || parsed.data.type !== "resume") return this.closeWs(ws, CloseCode.protocol, "expected resume");
      this.handleResume(ws, a, parsed.data);
      if (a.ready && a.mode === "full") await this.sendPresenceSnapshot(ws, a.userId);
      return;
    }
    if (!parsedJson) return; // not JSON: no ref to answer
    if (typeof type !== "string" || !KNOWN_CLIENT_TYPES.has(type)) {
      return this.reply(ws, ref, { code: "unknown_type", message: `unknown frame type ${JSON.stringify(type ?? null)}` });
    }
    if (type === "resume") return this.closeWs(ws, CloseCode.protocol, "resume already handled");
    if (!parsed?.success) return this.reply(ws, ref, { code: "invalid_frame", message: `invalid ${type} frame` });
    const frame = parsed.data;
    if (frame.type === "resume") return; // handled above (narrowing only)
    if (a.mode === "notify" && FULL_ONLY_TYPES.has(frame.type)) {
      return this.reply(ws, ref, { code: "not_allowed_in_mode", message: `${frame.type} is not allowed in notify mode` });
    }
    return this.handleFrame(ws, a, frame, ref);
  }

  /** ack / typing / idle / watch after ready. Handlers are filled in by later tasks (10: ack, 12: idle, 13: watch, 27: typing). */
  protected async handleFrame(ws: WebSocket, a: ConnAttachment, frame: Exclude<ClientFrame, { type: "resume" }>, ref: string | null): Promise<void> {
    switch (frame.type) {
      case "idle":
        if (a.idle !== frame.data.idle) {
          a.idle = frame.data.idle;
          ws.serializeAttachment(a);
          await this.recomputePresence(); // events.md 6.3 rule 5 (mode auto)
        }
        break;
      case "watch":
        if (frame.data.topics.length > LIMITS.watchTopicsMax) {
          return this.reply(ws, ref, { code: "limit_exceeded", message: `at most ${LIMITS.watchTopicsMax} topics` });
        }
        await this.handleWatch(ws, frame.data.topics);
        break;
      case "ack":
        // events.md 3.5: keyed by (device_id, this connection's mode), stamped with that mode's current epoch.
        this.sql.exec(
          `INSERT INTO device_acks (device_id, mode, epoch, seq, updated_at) VALUES (?, ?, ?, ?, ?)
           ON CONFLICT (device_id, mode) DO UPDATE SET epoch = excluded.epoch, seq = excluded.seq, updated_at = excluded.updated_at`,
          a.deviceId,
          a.mode,
          this.epochOf(a.mode),
          frame.data.seq,
          this.now(),
        );
        break;
      case "typing":
        return this.handleTyping(ws, a, frame.data, ref);
    }
    this.reply(ws, ref, null);
  }

  /** channelId -> until (ms): positive write-access results of this hub's user (in memory; lost on hibernation). */
  private typingAccess = new Map<string, number>();

  protected async canTypeIn(userId: string, channelId: string): Promise<boolean> {
    const t = this.now();
    const until = this.typingAccess.get(channelId);
    if (until !== undefined && until > t) return true;
    const access = await getChannelAccess(this.env.DB, channelId, userId);
    if (access === null || !access.canWrite) {
      this.typingAccess.delete(channelId);
      return false;
    }
    if (this.typingAccess.size >= 100) this.typingAccess.clear(); // bound the cache; it refills on demand
    this.typingAccess.set(channelId, t + TYPING_ACCESS_CACHE_MS);
    return true;
  }

  /**
   * `typing` (events.md 5, server-architecture.md 3.2; full only, checked by the caller). Duplicates for the same
   * channel(+thread) on this connection within 2s are dropped (stamped before any await so back-to-back frames dedupe).
   * No write access -> reply{not_found} (and the stamp is withdrawn). Otherwise a volatile `typing` goes to the
   * channel audience minus me (all my connections included).
   */
  protected async handleTyping(
    ws: WebSocket,
    a: ConnAttachment,
    data: { channel_id: string; thread_root_id: string | null },
    ref: string | null,
  ): Promise<void> {
    const key = data.thread_root_id === null ? data.channel_id : `${data.channel_id}:${data.thread_root_id}`;
    const t = this.now();
    const last = a.typingAt[key];
    if (last !== undefined && t - last < TYPING_DEDUPE_MS) return this.reply(ws, ref, null);
    const typingAt: Record<string, number> = {};
    for (const [k, v] of Object.entries(a.typingAt)) if (t - v < TYPING_DEDUPE_MS) typingAt[k] = v; // expired stamps are useless
    typingAt[key] = t;
    const keys = Object.keys(typingAt);
    if (keys.length > TYPING_KEYS_MAX) {
      keys.sort((x, y) => typingAt[x]! - typingAt[y]!);
      for (const k of keys.slice(0, keys.length - TYPING_KEYS_MAX)) delete typingAt[k];
    }
    a.typingAt = typingAt;
    ws.serializeAttachment(a);

    let ok: boolean;
    try {
      ok = await this.canTypeIn(a.userId, data.channel_id);
    } catch (e) {
      console.error("UserHub: typing access check failed", e);
      ok = false;
    }
    if (!ok) {
      const cur = this.att(ws); // re-read: other frames may have updated the attachment meanwhile
      if (cur !== null && cur.typingAt[key] === t) {
        delete cur.typingAt[key];
        ws.serializeAttachment(cur);
      }
      return this.reply(ws, ref, { code: "not_found", message: "channel not found" });
    }
    await publishEvent(
      this.runtime(),
      { channel: data.channel_id },
      "typing",
      { channel_id: data.channel_id, thread_root_id: data.thread_root_id, user_id: a.userId, expires_in_ms: TYPING_EXPIRES_MS },
      { ephemeral: true, exclude: [a.userId] },
    );
    this.reply(ws, ref, null);
  }

  /** Watch frames of this hub run one after another, so join/leave RPCs for one topic never race each other. */
  private watchQueue: Promise<void> = Promise.resolve();

  /**
   * `watch` (events.md 5, server-architecture.md 3.2): full replacement of this socket's topics. The attachment is
   * updated first (deliver filters on it), then topics no other socket of mine still watches are left and every topic
   * of the new list is joined (lease renewal included). TopicHub failures are logged only: leases expire on their own.
   */
  protected handleWatch(ws: WebSocket, topics: string[]): Promise<void> {
    const run = async (): Promise<void> => {
      const a = this.att(ws);
      if (a === null || ws.readyState !== WebSocket.OPEN) return;
      const next = [...new Set(topics)];
      const prev = a.topics;
      a.topics = next;
      ws.serializeAttachment(a);

      const others = new Set<string>();
      for (const s of this.liveSockets()) {
        const o = this.att(s);
        if (o !== null && o.connId !== a.connId) for (const t of o.topics) others.add(t);
      }
      const keep = new Set(next);
      const toLeave = prev.filter((t) => !keep.has(t) && !others.has(t));
      const calls = [
        ...toLeave.map((t) => ({ t, op: "leave", p: this.env.TOPIC_HUB.getByName(t).leave(a.userId) })),
        ...next.map((t) => ({ t, op: "join", p: this.env.TOPIC_HUB.getByName(t).join(a.userId, WATCH_LEASE_MS) })),
      ];
      const results = await Promise.allSettled(calls.map((c) => c.p));
      results.forEach((r, i) => {
        if (r.status === "rejected") console.error(`UserHub: TopicHub ${calls[i].op} ${calls[i].t} failed`, r.reason);
      });
    };
    const p = this.watchQueue.then(run, run);
    this.watchQueue = p.catch(() => {});
    return p;
  }

  /** events.md 3.3 rules 1-4, then ready (+ presence.snapshot for full). Synchronous: nothing can interleave. */
  protected handleResume(ws: WebSocket, a: ConnAttachment, frame: Extract<ClientFrame, { type: "resume" }>): void {
    const epoch = this.epochOf(a.mode);
    const seq = this.currentSeq();
    const oldest = this.oldestSeq();

    let lastSeq: number | null = null; // null => rules 1/4 (base = hello.seq)
    let resync = false;
    const d = frame.data;
    if ("use_device_ack" in d) {
      const row = this.sql
        .exec<{ epoch: string; seq: number }>("SELECT epoch, seq FROM device_acks WHERE device_id = ? AND mode = ?", a.deviceId, a.mode)
        .toArray()[0];
      if (row && row.epoch === epoch) lastSeq = row.seq;
      else resync = true;
    } else if (d.epoch !== null) {
      if (d.epoch === epoch && d.last_seq !== null) lastSeq = d.last_seq;
      else resync = true;
    }
    if (lastSeq !== null && (lastSeq < oldest - 1 || lastSeq > seq)) {
      lastSeq = null;
      resync = true;
    }

    const base = lastSeq ?? a.helloSeq;
    let replayed = 0;
    for (const row of this.sql.exec<{ type: string; frame: string }>("SELECT type, frame FROM events WHERE seq > ? ORDER BY seq", base)) {
      if (a.mode === "notify" && !NOTIFY_ALLOWED_TYPES.has(row.type as ServerEventType)) continue;
      try {
        ws.send(row.frame);
        replayed++;
      } catch {
        return; // socket went away mid-replay
      }
    }

    a.ready = true;
    ws.serializeAttachment(a);
    const ready: ReadyData = { resumed: lastSeq !== null, resync_required: resync, replayed, seq, epoch };
    this.sendFrame(ws, "ready", ready);
    // presence.snapshot (full only) needs D1, so webSocketMessage sends it right after this returns.
  }

  async webSocketClose(ws: WebSocket, code: number, reason: string, _wasClean: boolean): Promise<void> {
    // With web_socket_auto_reply_to_close the runtime answers the close; this keeps older dates correct too.
    this.closeWs(ws, code === 1005 || code === 1006 ? CloseCode.normal : code, reason);
    await this.onSocketGone(ws);
    await this.recomputePresence();
    await this.scheduleAlarm();
  }

  async webSocketError(ws: WebSocket, error: unknown): Promise<void> {
    console.error("UserHub: websocket error", error);
    this.closeWs(ws, CloseCode.internal, "socket error");
    await this.onSocketGone(ws);
    await this.recomputePresence();
    await this.scheduleAlarm();
  }

  /**
   * server-architecture.md 3.2 webSocketClose/webSocketError: last full socket gone -> full_until/online_until = now+120s,
   * last notify socket gone -> notify_until/notify_online_until likewise, no socket left -> zero_since, last_seen_at.
   * Runs under blockConcurrencyWhile so a connect arriving meanwhile cannot have its fresh liveness shortened by this write.
   */
  protected async onSocketGone(ws: WebSocket): Promise<void> {
    const gone = this.att(ws);
    if (gone === null) return;
    if (gone.mode === "full") this.signalTuiGone(gone.deviceId, gone.connId);
    await this.ctx.blockConcurrencyWhile(async () => {
      try {
        const others = this.liveSockets()
          .map((s) => this.att(s))
          .filter((x): x is ConnAttachment => x !== null && x.connId !== gone.connId);
        await this.markDisconnected(gone.userId, this.now(), {
          full: gone.mode === "full" && !others.some((x) => x.mode === "full"),
          notify: gone.mode === "notify" && !others.some((x) => x.mode === "notify"),
          zero: others.length === 0 && (this.getMeta("zero_since") ?? "") === "",
        });
      } catch (e) {
        console.error("UserHub: close handling failed", e); // never throw inside blockConcurrencyWhile
      }
    });
  }

  /**
   * Shortens the liveness of the modes that lost their last socket to now+120s (watermark first, regardless of the D1
   * outcome; D1 with MIN so it never extends) and records zero_since/last_seen_at. Call under blockConcurrencyWhile.
   */
  protected async markDisconnected(userId: string, t: number, what: { full: boolean; notify: boolean; zero: boolean }): Promise<void> {
    const grace = t + CLOSE_GRACE_MS;
    const sets: string[] = [];
    const binds: unknown[] = [];
    if (what.full) {
      this.setMeta("full_until", Math.min(this.getMetaNum("full_until"), grace));
      sets.push("online_until = MIN(online_until, ?)");
      binds.push(grace);
    }
    if (what.notify) {
      this.setMeta("notify_until", Math.min(this.getMetaNum("notify_until"), grace));
      sets.push("notify_online_until = MIN(notify_online_until, ?)");
      binds.push(grace);
    }
    if (what.zero) {
      this.setMeta("zero_since", t);
      this.setMeta("last_seen_at", t);
      sets.push("last_seen_at = ?");
      binds.push(t);
    }
    if (sets.length === 0) return;
    try {
      await this.env.DB.prepare(`UPDATE user_presence SET ${sets.join(", ")}, updated_at = ? WHERE user_id = ?`)
        .bind(...binds, t, this.getMeta("user_id") ?? userId)
        .run();
    } catch (e) {
      console.error("UserHub: presence write failed on disconnect", e);
    }
  }

  /**
   * server-architecture.md 3.2 alarm: resume deadline (4000), heartbeat (4002), liveness extension (full: <5 min left ->
   * +10 min; notify: +40 min), extension failure (1011), missed close handling, log trim. Reschedules itself while sockets exist.
   * The body runs under blockConcurrencyWhile: liveness writes must not interleave with connect/close writes.
   */
  async alarm(): Promise<void> {
    await this.ctx.blockConcurrencyWhile(async () => {
      try {
        await this.runAlarm();
      } catch (e) {
        console.error("UserHub: alarm failed", e); // never throw inside blockConcurrencyWhile
      }
    });
    // Outside blockConcurrencyWhile: these publish (RPCs to other hubs). Natural focus end, then whatever the
    // sockets closed above did to the status.
    try {
      await this.catchUpFocus();
      await this.recomputePresence();
    } catch (e) {
      console.error("UserHub: alarm presence/focus failed", e);
    }
    await this.scheduleAlarm();
  }

  protected async runAlarm(): Promise<void> {
    const t = this.now();
    const userId = this.getMeta("user_id") ?? "";

    // full sockets this alarm closes: their devices may lose their last TUI (webSocketClose may never run for them)
    const closedFullDevices = new Set<string>();
    const closeFull = (ws: WebSocket, a: ConnAttachment | null, code: number, reason: string): void => {
      this.closeWs(ws, code, reason);
      if (a?.mode === "full") closedFullDevices.add(a.deviceId);
    };

    // 1. no resume within 10s of hello -> 4000; no ping for 120s -> 4002
    for (const ws of this.liveSockets()) {
      const a = this.att(ws);
      if (a === null) {
        this.closeWs(ws, CloseCode.internal, "lost connection state");
        continue;
      }
      if (!a.ready) {
        if (t >= a.connectedAt + RESUME_TIMEOUT_MS) closeFull(ws, a, CloseCode.protocol, "resume timeout");
        continue;
      }
      const pingAt = this.ctx.getWebSocketAutoResponseTimestamp(ws)?.getTime() ?? 0;
      if (t - Math.max(pingAt, a.connectedAt) > HEARTBEAT.timeoutMs) closeFull(ws, a, CloseCode.heartbeat, "heartbeat timeout");
    }

    // 2. extend D1 liveness for the modes that still have sockets; the watermark follows only a successful write
    const modes = (): { full: number; notify: number } => {
      let full = 0;
      let notify = 0;
      for (const ws of this.liveSockets()) {
        const m = this.att(ws)?.mode;
        if (m === "full") full++;
        else if (m === "notify") notify++;
      }
      return { full, notify };
    };
    let live = modes();
    if (userId !== "") {
      if (live.full > 0 && this.getMetaNum("full_until") - t < FULL_EXTEND_BELOW_MS) await this.tryExtend(userId, "full", t);
      if (live.notify > 0 && this.getMetaNum("notify_until") - t < NOTIFY_EXTEND_BELOW_MS) await this.tryExtend(userId, "notify", t);
    }

    // 3. extension kept failing and the watermark ran out: publishers no longer target these sockets -> 1011 (reconnect resyncs)
    const fullUntil = this.getMetaNum("full_until");
    const notifyUntil = this.getMetaNum("notify_until");
    if (live.full > 0 && t >= fullUntil) {
      for (const ws of this.liveSockets("mode:full")) closeFull(ws, this.att(ws), CloseCode.internal, "presence expired");
    }
    if (live.notify > 0 && t >= Math.max(fullUntil, notifyUntil)) {
      for (const ws of this.liveSockets("mode:notify")) this.closeWs(ws, CloseCode.internal, "presence expired");
    }

    for (const dev of closedFullDevices) this.signalTuiGone(dev);

    // 4. missed close handling (also covers the sockets closed above): no socket of a mode but its watermark still runs past the grace
    live = modes();
    const missed = {
      full: live.full === 0 && this.getMetaNum("full_until") > t + CLOSE_GRACE_MS,
      notify: live.notify === 0 && this.getMetaNum("notify_until") > t + CLOSE_GRACE_MS,
      zero: live.full + live.notify === 0 && (this.getMeta("zero_since") ?? "") === "",
    };
    if (userId !== "") await this.markDisconnected(userId, t, missed);

    // 5. log retention (events.md 6.2) and acks of rotated epochs (useless to use_device_ack)
    this.sql.exec(
      `DELETE FROM events WHERE seq <= (SELECT MAX(seq) - ${LOG_KEEP_EVENTS} FROM events) OR ts < ?`,
      t - LOG_KEEP_MS,
    );
    this.sql.exec(
      "DELETE FROM device_acks WHERE (mode = 'full' AND epoch <> ?) OR (mode = 'notify' AND epoch <> ?)",
      this.epochOf("full"),
      this.epochOf("notify"),
    );
  }

  /** One liveness extension; failures are logged and leave the watermark alone (step 3 of the alarm handles expiry). */
  protected async tryExtend(userId: string, mode: ConnMode, t: number): Promise<void> {
    try {
      const until = await this.extendLiveness(userId, mode, t);
      this.setMeta(mode === "full" ? "full_until" : "notify_until", until);
    } catch (e) {
      console.error(`UserHub: ${mode} liveness extension failed`, e);
    }
  }

  // ---------------------------------------------------------------- presence (events.md 6.3) and focus mode (RPC: server-architecture.md 3.2)

  /** D1 presence writes run one after another; each writes the state current when it runs, so the last one wins. */
  private presenceWrites: Promise<void> = Promise.resolve();

  protected runtime(): Runtime {
    return { env: this.env, waitUntil: (p) => this.ctx.waitUntil(p) };
  }

  protected ensureUser(userId: string): void {
    if (this.getMeta("user_id") === null) this.setMeta("user_id", userId);
  }

  protected manualMode(): PresenceMode {
    const m = this.getMeta("mode");
    return m === "away" || m === "dnd" ? m : "auto";
  }

  /** The active focus, or null (none, or its end time passed and the alarm has not finished it yet). */
  protected currentFocus(t = this.now()): Focus | null {
    const ends = this.getMetaNum("focus_ends_at");
    if (ends <= t) return null;
    return { started_at: iso(this.getMetaNum("focus_started_at")), ends_at: iso(ends) };
  }

  protected presenceState(): PresenceState {
    return { status: this.computeStatus(), mode: this.manualMode(), focus: this.currentFocus() };
  }

  /**
   * Synchronous half of a recompute: compares the state with the last persisted one (meta.last_state/last_status) and,
   * when it changed, records the new one at once (so concurrent recomputes never announce the same change twice).
   * Returns null when nothing changed. `offline` stamps last_seen_at (events.md 6.3).
   */
  protected planPresence(): PresencePlan | null {
    const userId = this.getMeta("user_id");
    if (userId === null) return null;
    const t = this.now();
    const state = this.presenceState();
    const stateJson = JSON.stringify(state);
    const prevStatus = this.getMeta("last_status") ?? "offline";
    if (stateJson === (this.getMeta("last_state") ?? INITIAL_STATE_JSON) && state.status === prevStatus) return null;
    const statusChanged = state.status !== prevStatus;
    this.setMeta("last_state", stateJson);
    this.setMeta("last_status", state.status);
    if (statusChanged && state.status === "offline") this.setMeta("last_seen_at", t);
    const lastSeen = this.getMetaNum("last_seen_at");
    return {
      userId,
      self: { type: "presence.self", data: { presence: state }, ts: t, logged: false },
      watchers: statusChanged ? { user_id: userId, status: state.status, last_seen_at: lastSeen > 0 ? iso(lastSeen) : null } : null,
    };
  }

  /** Async half: D1 user_presence(status, mode, focus_ends_at, last_seen_at), then presence.updated to presenceWatchers. */
  protected async finishPresence(plan: PresencePlan | null): Promise<void> {
    if (plan === null) return;
    const write = this.presenceWrites.then(() => this.writePresence(plan.userId));
    this.presenceWrites = write.catch(() => {});
    try {
      await write;
    } catch (e) {
      console.error("UserHub: presence write failed", e);
    }
    if (plan.watchers !== null) {
      await publishEvent(this.runtime(), { presenceWatchers: plan.userId }, "presence.updated", plan.watchers, { ephemeral: true });
    }
  }

  protected async writePresence(userId: string): Promise<void> {
    const t = this.now();
    const ends = this.getMetaNum("focus_ends_at");
    const lastSeen = this.getMetaNum("last_seen_at");
    await this.env.DB.prepare(
      `INSERT INTO user_presence (user_id, status, mode, focus_ends_at, last_seen_at, updated_at) VALUES (?1, ?2, ?3, ?4, ?5, ?6)
       ON CONFLICT (user_id) DO UPDATE SET status = excluded.status, mode = excluded.mode, focus_ends_at = excluded.focus_ends_at,
         last_seen_at = COALESCE(excluded.last_seen_at, user_presence.last_seen_at), updated_at = excluded.updated_at`,
    )
      .bind(userId, this.getMeta("last_status") ?? "offline", this.manualMode(), ends > t ? ends : null, lastSeen > 0 ? lastSeen : null, t)
      .run();
  }

  /** Recompute; on change: presence.self to my sockets (local), D1, presence.updated to my presence watchers. */
  protected async recomputePresence(): Promise<void> {
    const plan = this.planPresence();
    if (plan === null) return;
    await this.deliver({ userId: plan.userId, events: [plan.self] });
    await this.finishPresence(plan);
  }

  /** `presence.snapshot` (events.md 4.6): friends + dm/group_dm co-members, to this connection only, right after ready. */
  protected async sendPresenceSnapshot(ws: WebSocket, userId: string): Promise<void> {
    let items: PresenceItem[] = [];
    try {
      const rows = await all<{ id: string }>(
        this.env.DB,
        `SELECT CASE WHEN fr.user_a = ?1 THEN fr.user_b ELSE fr.user_a END AS id
           FROM friendships fr WHERE fr.user_a = ?1 OR fr.user_b = ?1
         UNION
         SELECT cm2.user_id AS id FROM channel_members cm1
           JOIN channels c ON c.id = cm1.channel_id AND c.kind IN ('dm', 'group_dm') AND c.deleted_at IS NULL
           JOIN channel_members cm2 ON cm2.channel_id = cm1.channel_id AND cm2.user_id <> ?1
          WHERE cm1.user_id = ?1
         ORDER BY id`,
        userId,
      );
      items = await loadPresenceItems(
        this.env.DB,
        rows.map((r) => r.id),
      );
    } catch (e) {
      console.error("UserHub: presence snapshot failed", e); // still send one: the client waits for it after ready
    }
    if (ws.readyState === WebSocket.OPEN) this.sendFrame(ws, "presence.snapshot", { items });
  }

  /** A focus whose end time passed but was not finished yet (alarm late or not run) is finished now, at its end time. */
  protected async catchUpFocus(): Promise<void> {
    const ends = this.getMetaNum("focus_ends_at");
    if (ends > 0 && ends <= this.now()) await this.finishFocus(ends);
  }

  /**
   * The one way a focus ends (early via endFocus, naturally via the alarm). Synchronously: summary from focus_alerts,
   * focus meta cleared, focus_alerts emptied — so a second caller (or the later alarm) finds nothing and the summary
   * alert goes out exactly once. Then presence recompute, and focus.ended + alert{focus_summary} (both logged) in one
   * delivery. The summary alert is never suppressed (events.md 4.7 "항상").
   */
  protected async finishFocus(endedAt: number): Promise<FocusSummary | null> {
    const endsRaw = this.getMeta("focus_ends_at");
    const userId = this.getMeta("user_id");
    if (endsRaw === null || endsRaw === "" || userId === null) return null;
    const startedAt = this.getMetaNum("focus_started_at");
    const summary = this.ctx.storage.transactionSync((): FocusSummary => {
      const counts: FocusSummary["counts"] = { dm: 0, mention: 0, thread_reply: 0, invite: 0, friend_request: 0, other: 0 };
      for (const r of this.sql.exec<{ kind: string; n: number }>("SELECT kind, COUNT(*) AS n FROM focus_alerts GROUP BY kind")) {
        const k = r.kind;
        if (k === "dm" || k === "mention" || k === "thread_reply" || k === "invite" || k === "friend_request") counts[k] += r.n;
        else counts.other += r.n;
      }
      const items = this.sql
        .exec<{ alert: string }>(`SELECT alert FROM focus_alerts ORDER BY created_at DESC, rowid DESC LIMIT ${FOCUS_SUMMARY_ITEMS}`)
        .toArray()
        .map((r) => JSON.parse(r.alert) as Alert);
      this.setMeta("focus_started_at", "");
      this.setMeta("focus_ends_at", "");
      this.sql.exec("DELETE FROM focus_alerts");
      return { started_at: iso(startedAt), ended_at: iso(endedAt), counts, items };
    });
    const plan = this.planPresence();
    const t = this.now();
    const c = summary.counts;
    const alert: Alert = {
      id: newId(),
      kind: "focus_summary",
      title: `집중 모드 끝 — 멘션 ${c.mention}, DM ${c.dm}`,
      body: `스레드 답글 ${c.thread_reply} · 초대 ${c.invite} · 친구 요청 ${c.friend_request} · 기타 ${c.other}`,
      actor: null,
      target: { type: "user", id: userId, channel_id: null, message_id: null, thread_root_id: null, post_id: null, space_id: null },
      created_at: iso(t),
      suppressed: false,
      suppressed_reason: null,
    };
    await this.deliver({
      userId,
      events: [
        ...(plan !== null ? [plan.self] : []),
        { type: "focus.ended", data: { summary }, ts: t, logged: true },
        { type: "alert", data: { alert }, ts: t, logged: true },
      ],
    });
    await this.finishPresence(plan);
    return summary;
  }

  /** PUT /me/presence: the manual mode; returns the resulting state. */
  async setPresenceMode(userId: string, mode: PresenceMode): Promise<PresenceState> {
    this.ensureUser(userId);
    await this.catchUpFocus();
    this.setMeta("mode", mode);
    await this.recomputePresence();
    return this.presenceState();
  }

  async getPresenceState(userId: string): Promise<PresenceState> {
    this.ensureUser(userId);
    await this.catchUpFocus();
    return this.presenceState();
  }

  /** POST /me/focus: starts a focus, or (already focusing) replaces its end time — that is not an end, no summary. */
  async startFocus(userId: string, durationSec: number): Promise<Focus> {
    if (!Number.isInteger(durationSec) || durationSec < FOCUS_MIN_SEC || durationSec > FOCUS_MAX_SEC) {
      throw new RangeError(`durationSec must be an integer in ${FOCUS_MIN_SEC}..${FOCUS_MAX_SEC}`);
    }
    this.ensureUser(userId);
    await this.catchUpFocus();
    const t = this.now();
    const active = this.getMetaNum("focus_ends_at") > t;
    const startedAt = active ? this.getMetaNum("focus_started_at") : t;
    const endsAt = t + durationSec * 1000;
    if (!active) this.sql.exec("DELETE FROM focus_alerts");
    this.setMeta("focus_started_at", startedAt);
    this.setMeta("focus_ends_at", endsAt);
    const focus: Focus = { started_at: iso(startedAt), ends_at: iso(endsAt) };
    const plan = this.planPresence();
    await this.deliver({
      userId,
      events: [{ type: "focus.started", data: { focus }, ts: t, logged: true }, ...(plan !== null ? [plan.self] : [])],
    });
    await this.scheduleAlarm();
    await this.finishPresence(plan);
    return focus;
  }

  /** DELETE /me/focus: early end -> summary; null when not focusing (an overdue focus is finished naturally first). */
  async endFocus(userId: string): Promise<FocusSummary | null> {
    this.ensureUser(userId);
    const ends = this.getMetaNum("focus_ends_at");
    if (ends === 0) return null;
    const t = this.now();
    if (ends <= t) {
      await this.finishFocus(ends);
      return null;
    }
    return this.finishFocus(t);
  }

  // ---------------------------------------------------------------- sessions

  /**
   * Called by services/users.ts revokeSessions after D1 revoked the sessions: each target socket (all of them for "all",
   * otherwise those tagged `sess:<id>`) gets `session.revoked{session_id, reason}` (volatile, also before `ready` and on
   * notify sockets) and is then closed with 4001, or 4003 for "suspended".
   */
  async revokeSessions(userId: string, sessionIds: string[] | "all", reason: "logout" | "revoked" | "password_changed" | "suspended"): Promise<void> {
    this.ensureUser(userId);
    const code = reason === "suspended" ? CloseCode.suspended : CloseCode.unauthorized;
    const targets = sessionIds === "all" ? this.liveSockets() : [...new Set(sessionIds)].flatMap((id) => this.liveSockets(`sess:${id}`));
    if (targets.length === 0) return;
    const t = this.now();
    const closedFullDevices = new Set<string>();
    for (const ws of targets) {
      const a = this.att(ws);
      if (a !== null) this.sendFrame(ws, "session.revoked", { session_id: a.sessionId, reason }, null, t);
      this.closeWs(ws, code, reason);
      if (a?.mode === "full") closedFullDevices.add(a.deviceId);
    }
    for (const dev of closedFullDevices) this.signalTuiGone(dev);
    // Same bookkeeping as webSocketClose, without waiting for the peer's close (the alarm would catch it otherwise).
    await this.ctx.blockConcurrencyWhile(async () => {
      try {
        const now = this.now();
        const live = this.liveSockets().map((s) => this.att(s));
        const full = live.some((x) => x?.mode === "full");
        const notify = live.some((x) => x?.mode === "notify");
        await this.markDisconnected(userId, now, {
          full: !full && this.getMetaNum("full_until") > now + CLOSE_GRACE_MS,
          notify: !notify && this.getMetaNum("notify_until") > now + CLOSE_GRACE_MS,
          zero: live.length === 0 && (this.getMeta("zero_since") ?? "") === "",
        });
      } catch (e) {
        console.error("UserHub: revoke bookkeeping failed", e); // never throw inside blockConcurrencyWhile
      }
    });
    await this.recomputePresence();
    await this.scheduleAlarm();
  }

  // ---------------------------------------------------------------- debug

  /** Test/debug only. */
  async inspect(): Promise<{
    userId: string | null;
    epochFull: string;
    epochNotify: string;
    seq: number;
    oldestSeq: number;
    connections: number;
    fullUntil: number;
    notifyUntil: number;
    status: PresenceStatus;
  }> {
    return {
      userId: this.getMeta("user_id"),
      epochFull: this.epochOf("full"),
      epochNotify: this.epochOf("notify"),
      seq: this.currentSeq(),
      oldestSeq: this.oldestSeq(),
      connections: this.liveSockets().length,
      fullUntil: this.getMetaNum("full_until"),
      notifyUntil: this.getMetaNum("notify_until"),
      status: this.computeStatus(),
    };
  }

  /** events.md 6.3 (focus/manual mode read from meta; Task 12 adds publishing on change). */
  protected computeStatus(): PresenceStatus {
    const t = this.now();
    const focusEnds = this.getMetaNum("focus_ends_at");
    if (focusEnds > t) return "dnd";
    const mode = this.getMeta("mode") ?? "auto";
    if (mode === "dnd") return "dnd";
    const full = this.liveSockets("mode:full").map((ws) => this.att(ws));
    if (full.length === 0) return "offline";
    if (mode === "away") return "away";
    if (full.every((a) => a?.idle === true)) return "away";
    return "online";
  }
}
