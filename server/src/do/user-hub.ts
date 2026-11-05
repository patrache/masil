// Per-user realtime hub (server-architecture.md 3.2, events.md 1-3, 5, 6.2).
// Task 9: upgrade, epochs + D1 liveness on connect, hello, resume -> ready, client frame validation and rate limit.
// deliver/ack/alarm work (Task 10), presence (Task 12), watch (Task 13) and typing (Task 27) build on this.
import { DurableObject } from "cloudflare:workers";
import type { AuthContext } from "../env";
import { newId, isId } from "../lib/ids";
import { iso } from "../lib/time";
import type { PresenceStatus } from "../types/api";
import type { HelloData, ReadyData, ReplyErrorCode, ServerEventType } from "../types/events";
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
const RATE_WINDOW_MS = 10_000; // events.md 2: 30 frames / 10s -> reply{rate_limited}; 60 -> 4008
const RATE_SOFT = 30;
const RATE_HARD = 60;

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

  /** Next alarm = min(full socket ? now+60s : notify-only ? now+15min, pending resume deadlines). Never moves an earlier alarm later. */
  protected async scheduleAlarm(): Promise<void> {
    const t = this.now();
    const sockets = this.liveSockets();
    if (sockets.length === 0) return;
    let next = this.liveSockets("mode:full").length > 0 ? t + FULL_ALARM_MS : t + NOTIFY_ALARM_MS;
    for (const ws of sockets) {
      const a = this.att(ws);
      if (a && !a.ready) {
        const deadline = a.connectedAt + RESUME_TIMEOUT_MS;
        if (deadline > t && deadline < next) next = deadline;
      }
    }
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
    for (let i = 0; i <= open.length - MAX_CONNECTIONS; i++) this.closeWs(open[i].ws, CloseCode.replaced, "replaced");

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
      return this.handleResume(ws, a, parsed.data);
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
  protected handleFrame(ws: WebSocket, a: ConnAttachment, frame: Exclude<ClientFrame, { type: "resume" }>, ref: string | null): void {
    switch (frame.type) {
      case "idle":
        if (a.idle !== frame.data.idle) {
          a.idle = frame.data.idle;
          ws.serializeAttachment(a);
        }
        break;
      case "watch":
        if (frame.data.topics.length > LIMITS.watchTopicsMax) {
          return this.reply(ws, ref, { code: "limit_exceeded", message: `at most ${LIMITS.watchTopicsMax} topics` });
        }
        break;
      case "ack":
      case "typing":
        break;
    }
    this.reply(ws, ref, null);
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
    if (a.mode === "full") this.sendFrame(ws, "presence.snapshot", { items: [] }); // Task 12 fills the items
  }

  async webSocketClose(ws: WebSocket, code: number, reason: string, _wasClean: boolean): Promise<void> {
    // With web_socket_auto_reply_to_close the runtime answers the close; this keeps older dates correct too.
    this.closeWs(ws, code === 1005 || code === 1006 ? CloseCode.normal : code, reason);
    await this.scheduleAlarm();
  }

  async webSocketError(ws: WebSocket, error: unknown): Promise<void> {
    console.error("UserHub: websocket error", error);
    this.closeWs(ws, CloseCode.internal, "socket error");
    await this.scheduleAlarm();
  }

  /** Task 9: only keeps the alarm chain going; heartbeat/resume/liveness work comes with Task 10. */
  async alarm(): Promise<void> {
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
