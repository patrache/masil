// WorkspaceHub (v0.1.3, server-architecture.md 3.1, docs/contract/workspace.md 4): one Durable Object per workspace
// share (name = share id) that relays the share's frames between the owner (seat 0) and its participants (1..8). It is
// a ciphertext relay: it routes by the 20-byte outer header and the level table only, and reads plaintext only from
// Class=Server frames (ctl_* from the owner — metadata such as levels). It never stores or logs a payload. Kept apart
// from CollabHub on purpose: a DO that stores plaintext and one that must not see it never share a class (C1.1).
//
// The Worker route (routes/workspace-shares.ts) has checked the session, RL_SHARE_WS, the share, membership and blocks;
// it passes who is connecting in X-Masil-Auth / X-Masil-Seat / X-Masil-Share, the only identity this object trusts.
//
// State: DO storage holds the share's metadata, the level table and the verified flags (plaintext metadata) and the
// ended flag. Memory only: per-connection frame counters and the latest keyframe group (ciphertext) — both start empty
// after the object wakes from hibernation (the next keyframe refills the cache).
import { DurableObject } from "cloudflare:workers";
import type { Runtime } from "../env";
import { errFields } from "../lib/log";
import {
  FLAG_GROUP_START,
  K,
  LEVELS,
  MAX_FRAME,
  MAX_SEAT,
  SEAT_ALL,
  SEAT_OWNER,
  jsonPayload,
  kindInfo,
  levelRank,
  parseHeader,
  peerAllowed,
  serverFrame,
} from "./workspace-frame";
import { endShare, markPresence, recordLevels, removeSeatFromHub, sendAttention, stillAllowed } from "../services/workspace-shares";

/** workspace.md 8 */
export const WorkspaceClose = {
  protocol: 4000,
  rateLimited: 4008,
  ended: 4100,
  kicked: 4101,
  replaced: 4102,
} as const;

/** workspace.md 4 rules 4, 6, 7 and 2 (attention) */
export const WORKSPACE_HUB_LIMITS = {
  termInputPerSec: 50,
  docOpPerSec: 30,
  propPerMin: 10,
  ownerBytesPerSec: 2 * 1024 * 1024,
  /** consecutive seconds over a limit before 4008 */
  overSeconds: 3,
  /** keyframe cache cap; past it the cache is dropped until the next group start */
  cacheBytes: 8 * 1024 * 1024,
  ownerGraceMs: 120_000,
  attentionGapMs: 30_000,
} as const;

/** What routes/workspace-shares.ts sends with the upgrade. */
export interface ShareMeta {
  share_id: string;
  owner_id: string;
  title: string;
}

/** Per-socket state (ws.serializeAttachment). */
interface HubConn {
  seat: number;
  userId: string;
  /** closed by the hub (kick / end / replace): its close event changes nothing */
  gone?: boolean;
}

interface Rate {
  sec: number;
  termInput: number;
  docOp: number;
  bytes: number;
  min: number;
  prop: number;
  overSec: number;
  overRun: number;
}

function parseJson<T>(raw: string | null): T | null {
  if (raw === null) return null;
  try {
    return JSON.parse(raw) as T;
  } catch {
    return null;
  }
}

export class WorkspaceHub extends DurableObject<Cloudflare.Env> {
  private rates = new Map<WebSocket, Rate>();
  /** the latest keyframe group the owner broadcast: frames in order (ciphertext), memory only */
  private cache: { frames: ArrayBuffer[]; bytes: number; active: boolean } = { frames: [], bytes: 0, active: false };
  private levelTable: Record<string, number> | null = null;
  private lastAttention = -Infinity;

  /** Wall clock; tests override it on the instance inside runInDurableObject. */
  protected now(): number {
    return Date.now();
  }

  private rt(): Runtime {
    return { env: this.env, waitUntil: (p) => this.ctx.waitUntil(p) };
  }

  // ---------------------------------------------------------------- storage

  private async meta(): Promise<ShareMeta | null> {
    return (await this.ctx.storage.get<ShareMeta>("meta")) ?? null;
  }

  private async ended(): Promise<boolean> {
    return (await this.ctx.storage.get<boolean>("ended")) === true;
  }

  private async levels(): Promise<Record<string, number>> {
    if (this.levelTable === null) this.levelTable = (await this.ctx.storage.get<Record<string, number>>("levels")) ?? {};
    return this.levelTable;
  }

  private async putLevels(table: Record<string, number>, verified?: Record<string, boolean>): Promise<void> {
    this.levelTable = table;
    await this.ctx.storage.put("levels", table);
    if (verified !== undefined) await this.ctx.storage.put("verified", verified);
  }

  // ---------------------------------------------------------------- sockets

  private conn(ws: WebSocket): HubConn | null {
    return (ws.deserializeAttachment() as HubConn | null) ?? null;
  }

  private live(tag?: string): WebSocket[] {
    return this.ctx.getWebSockets(tag).filter((ws) => ws.readyState === WebSocket.OPEN);
  }

  private send(ws: WebSocket, data: ArrayBuffer | Uint8Array): void {
    try {
      ws.send(data);
    } catch {
      // closing; its close handler cleans up
    }
  }

  private close(ws: WebSocket, code: number, reason: string, gone = true): void {
    if (gone) {
      const c = this.conn(ws);
      if (c !== null && !c.gone) {
        try {
          ws.serializeAttachment({ ...c, gone: true });
        } catch {
          // already closed
        }
      }
    }
    this.rates.delete(ws);
    try {
      ws.close(code, reason);
    } catch {
      // already closed
    }
  }

  private owners(): WebSocket[] {
    return this.live(`seat:${SEAT_OWNER}`);
  }

  private toOwner(frame: ArrayBuffer | Uint8Array): void {
    for (const ws of this.owners()) this.send(ws, frame);
  }

  private peerFrame(seat: number, userId: string, state: "joined" | "left"): Uint8Array {
    return serverFrame(K.srvPeer, SEAT_OWNER, { seat, user_id: userId, state });
  }

  /** Closes every socket 4100, marks the hub ended (later connects are refused) and drops the grace alarm. */
  private async endAll(): Promise<void> {
    await this.ctx.storage.put("ended", true);
    await this.ctx.storage.deleteAlarm();
    this.cache = { frames: [], bytes: 0, active: false };
    for (const ws of this.live()) this.close(ws, WorkspaceClose.ended, "share_ended");
  }

  /** Closes `userId`'s sockets 4101 and drops `seat` from the level table; the owner hears srv_peer left. */
  private async kickUser(userId: string, seat: number | null): Promise<void> {
    for (const ws of this.live(`u:${userId}`)) {
      const c = this.conn(ws);
      if (c === null || c.seat === SEAT_OWNER) continue;
      this.close(ws, WorkspaceClose.kicked, "kicked");
      this.toOwner(this.peerFrame(c.seat, userId, "left"));
      seat ??= c.seat;
    }
    if (seat !== null) {
      const table = { ...(await this.levels()) };
      if (String(seat) in table) {
        delete table[String(seat)];
        const verified = { ...((await this.ctx.storage.get<Record<string, boolean>>("verified")) ?? {}) };
        delete verified[String(seat)];
        await this.putLevels(table, verified);
      }
    }
  }

  // ---------------------------------------------------------------- requests

  async fetch(request: Request): Promise<Response> {
    const path = new URL(request.url).pathname;
    if (path === "/connect") return this.onConnect(request);
    if (request.method === "POST" && path === "/end") {
      await this.endAll();
      return new Response(null, { status: 204 });
    }
    if (request.method === "POST" && path === "/kick") {
      const body = parseJson<{ user_id?: unknown }>(await request.text());
      const userId = typeof body?.user_id === "string" ? body.user_id : null;
      if (userId === null) return new Response("bad kick", { status: 400 });
      const meta = await this.meta();
      let seat: number | null = null;
      if (meta !== null) {
        const r = await this.env.DB.prepare("SELECT seat FROM workspace_share_members WHERE share_id = ?1 AND user_id = ?2")
          .bind(meta.share_id, userId)
          .first<{ seat: number }>();
        seat = r?.seat ?? null;
      }
      await this.kickUser(userId, seat);
      return new Response(null, { status: 204 });
    }
    return new Response("not found", { status: 404 });
  }

  /**
   * workspace.md 1 / 4: the upgrade, from routes/workspace-shares.ts only. The socket is accepted first (from here a
   * /kick or /end sees it), then D1 is read again: a kick or end that committed after the Worker's read refuses it.
   */
  private async onConnect(request: Request): Promise<Response> {
    const auth = parseJson<{ userId?: unknown }>(request.headers.get("X-Masil-Auth"));
    const share = parseJson<ShareMeta>(request.headers.get("X-Masil-Share"));
    const seat = Number(request.headers.get("X-Masil-Seat") ?? "NaN");
    if (typeof auth?.userId !== "string" || share === null || typeof share.share_id !== "string" || !Number.isInteger(seat) || seat < 0 || seat > MAX_SEAT) {
      return new Response("missing share auth", { status: 401 });
    }
    if ((request.headers.get("Upgrade") ?? "").toLowerCase() !== "websocket") return new Response("expected websocket upgrade", { status: 400 });
    if (await this.ended()) return new Response("not found", { status: 404 });
    if ((await this.meta()) === null) {
      await this.ctx.storage.put("meta", { share_id: share.share_id, owner_id: share.owner_id, title: share.title } satisfies ShareMeta);
    }
    const userId = auth.userId;
    for (const old of this.live(`u:${userId}`)) this.close(old, WorkspaceClose.replaced, "replaced");

    const pair = new WebSocketPair();
    const [client, server] = [pair[0], pair[1]];
    this.ctx.acceptWebSocket(server, [`seat:${seat}`, `u:${userId}`]);
    server.serializeAttachment({ seat, userId } satisfies HubConn);
    let ok: boolean;
    try {
      ok = await stillAllowed(this.env.DB, share.share_id, userId);
    } catch (e) {
      console.error({ msg: "WorkspaceHub: connect check failed", share_id: share.share_id, err: errFields(e) });
      this.close(server, 1011, "try again");
      return new Response("try again", { status: 503 });
    }
    if (!ok || server.readyState !== WebSocket.OPEN || (await this.ended())) {
      this.close(server, 1000, "not found");
      return new Response("not found", { status: 404 });
    }
    const t = this.now();
    if (seat === SEAT_OWNER) await this.ctx.storage.deleteAlarm();
    else await markPresence(this.env.DB, share.share_id, userId, "joined_at", t);

    const seats = new Map<number, string>();
    for (const ws of this.live()) {
      const c = this.conn(ws);
      if (c !== null && !c.gone) seats.set(c.seat, c.userId);
    }
    this.send(
      server,
      serverFrame(K.srvHello, seat, {
        share_id: share.share_id,
        seat,
        owner_id: share.owner_id,
        seats: [...seats].sort((a, b) => a[0] - b[0]).map(([s, u]) => ({ seat: s, user_id: u })),
        max_frame: MAX_FRAME,
      }),
    );
    if (seat !== SEAT_OWNER) this.toOwner(this.peerFrame(seat, userId, "joined"));
    return new Response(null, { status: 101, webSocket: client });
  }

  // ---------------------------------------------------------------- frames

  async webSocketMessage(ws: WebSocket, message: string | ArrayBuffer): Promise<void> {
    if (ws.readyState !== WebSocket.OPEN) return;
    const c = this.conn(ws);
    if (c === null || c.gone) return this.close(ws, WorkspaceClose.protocol, "lost connection state");
    if (typeof message === "string") return this.close(ws, WorkspaceClose.protocol, "binary frames only");
    if (message.byteLength > MAX_FRAME) return this.close(ws, WorkspaceClose.protocol, "frame too big");
    const h = parseHeader(message);
    if (h === null) return this.close(ws, WorkspaceClose.protocol, "bad header");
    if (h.from !== c.seat) return this.close(ws, WorkspaceClose.protocol, "from is not your seat");
    if (!this.withinLimits(ws, c.seat, h.kind, message.byteLength)) return;
    const info = kindInfo(h.kind);
    if (info === undefined) return; // unknown kind: dropped (a newer client talking to an older hub)
    if (info.cls === "Server") {
      if (c.seat === SEAT_OWNER) await this.onControl(h.kind, message);
      return;
    }
    if (c.seat === SEAT_OWNER) return this.fromOwner(h.kind, h.flags, h.to, message);
    return this.fromPeer(c.seat, h.kind, h.to, message);
  }

  /** Rule 3, owner side: To=255 -> every participant with a level, To=1..8 -> that seat whatever its level. */
  private async fromOwner(kind: number, flags: number, to: number, frame: ArrayBuffer): Promise<void> {
    if (to === SEAT_ALL) {
      this.cacheFrame(kind, flags, frame);
      const table = await this.levels();
      for (const ws of this.live()) {
        const c = this.conn(ws);
        if (c !== null && !c.gone && c.seat !== SEAT_OWNER && (table[String(c.seat)] ?? 0) > 0) this.send(ws, frame);
      }
      return;
    }
    if (to >= 1 && to <= MAX_SEAT) for (const ws of this.live(`seat:${to}`)) this.send(ws, frame);
  }

  /** Rule 3, participant side: To=0 only, and only kinds the seat's level allows (MinLevel). */
  private async fromPeer(seat: number, kind: number, to: number, frame: ArrayBuffer): Promise<void> {
    if (to !== SEAT_OWNER) return;
    const rank = (await this.levels())[String(seat)] ?? 0;
    if (!peerAllowed(kind, rank)) return;
    this.toOwner(frame);
  }

  /** Rule 4: term_key with FlagGroupStart starts a new group; later term_key / term_out join it (8 MiB cap). */
  private cacheFrame(kind: number, flags: number, frame: ArrayBuffer): void {
    if (kind === K.termKey && (flags & FLAG_GROUP_START) !== 0) {
      this.cache = { frames: [frame], bytes: frame.byteLength, active: true };
    } else if ((kind === K.termKey || kind === K.termOut) && this.cache.active) {
      this.cache.frames.push(frame);
      this.cache.bytes += frame.byteLength;
    } else return;
    if (this.cache.bytes > WORKSPACE_HUB_LIMITS.cacheBytes) this.cache = { frames: [], bytes: 0, active: false };
  }

  /** Rule 2: ctl_* frames, owner only, consumed here and never forwarded. */
  private async onControl(kind: number, frame: ArrayBuffer): Promise<void> {
    const meta = await this.meta();
    if (meta === null) return;
    const body = jsonPayload(frame);
    try {
      if (kind === K.ctlLevels && body !== null) await this.onLevels(meta, body);
      else if (kind === K.ctlKick && body !== null) {
        const seat = body.seat;
        if (typeof seat !== "number" || !Number.isInteger(seat) || seat < 1 || seat > MAX_SEAT) return;
        const userId = await removeSeatFromHub(this.rt(), meta.share_id, seat);
        if (userId !== null) await this.kickUser(userId, seat);
        else for (const ws of this.live(`seat:${seat}`)) this.close(ws, WorkspaceClose.kicked, "kicked");
      } else if (kind === K.ctlEnd) {
        await endShare(this.rt(), meta.share_id, "ended", { fromHub: true });
        await this.endAll();
      } else if (kind === K.ctlAttention) {
        const t = this.now();
        if (t - this.lastAttention < WORKSPACE_HUB_LIMITS.attentionGapMs) return;
        this.lastAttention = t;
        await sendAttention(this.rt(), meta.share_id, meta.owner_id, meta.title);
      }
    } catch (e) {
      console.error({ msg: "WorkspaceHub: control frame failed", share_id: meta.share_id, kind, err: errFields(e) });
    }
  }

  /**
   * ctl_levels {levels:{"<seat>": name}, verified:{"<seat>": bool}} — the whole table (a seat left out has no level).
   * Stored, mirrored to D1 (max_level / verified), and every seat that had no level before gets the cached keyframe
   * group so its screen starts at once.
   */
  private async onLevels(meta: ShareMeta, body: Record<string, unknown>): Promise<void> {
    const rawLevels = body.levels;
    if (rawLevels === null || typeof rawLevels !== "object" || Array.isArray(rawLevels)) return;
    const rawVerified = body.verified !== null && typeof body.verified === "object" && !Array.isArray(body.verified) ? (body.verified as Record<string, unknown>) : {};
    const table: Record<string, number> = {};
    const verified: Record<string, boolean> = {};
    const names = new Map<number, string>();
    const checks = new Map<number, boolean>();
    for (const [k, v] of Object.entries(rawLevels as Record<string, unknown>)) {
      const seat = Number(k);
      const rank = levelRank(v);
      if (!/^[1-8]$/.test(k) || rank < 0) return; // malformed table: ignore the whole frame
      if (rank === 0) continue;
      table[String(seat)] = rank;
      names.set(seat, LEVELS[rank]!);
    }
    for (const [k, v] of Object.entries(rawVerified)) {
      if (!/^[1-8]$/.test(k) || typeof v !== "boolean") return;
      verified[k] = v;
      checks.set(Number(k), v);
    }
    const before = await this.levels();
    await this.putLevels(table, verified);
    const fresh = Object.keys(table).filter((s) => (before[s] ?? 0) === 0);
    if (fresh.length > 0 && this.cache.active) {
      for (const s of fresh) for (const ws of this.live(`seat:${s}`)) for (const f of this.cache.frames) this.send(ws, f);
    }
    await recordLevels(this.rt(), meta.share_id, names, checks);
  }

  /**
   * Rule 6 (per connection, 1-second windows): participants term_input 50/s, doc_op 30/s, prop_submit 10/min; the
   * owner 2 MiB/s. A frame over the limit is dropped (false); the third second in a row with a drop closes 4008.
   */
  private withinLimits(ws: WebSocket, seat: number, kind: number, bytes: number): boolean {
    const t = this.now();
    const sec = Math.floor(t / 1000);
    const min = Math.floor(t / 60_000);
    let r = this.rates.get(ws);
    if (r === undefined) {
      r = { sec, termInput: 0, docOp: 0, bytes: 0, min, prop: 0, overSec: -1, overRun: 0 };
      this.rates.set(ws, r);
    }
    if (r.sec !== sec) Object.assign(r, { sec, termInput: 0, docOp: 0, bytes: 0 });
    if (r.min !== min) Object.assign(r, { min, prop: 0 });
    const L = WORKSPACE_HUB_LIMITS;
    let over = false;
    if (seat === SEAT_OWNER) over = (r.bytes += bytes) > L.ownerBytesPerSec;
    else if (kind === K.termInput) over = ++r.termInput > L.termInputPerSec;
    else if (kind === K.docOp) over = ++r.docOp > L.docOpPerSec;
    else if (kind === K.propSubmit) over = ++r.prop > L.propPerMin;
    if (!over) return true;
    if (r.overSec !== sec) {
      r.overRun = r.overSec === sec - 1 ? r.overRun + 1 : 1;
      r.overSec = sec;
    }
    if (r.overRun >= L.overSeconds) this.close(ws, WorkspaceClose.rateLimited, "rate_limited");
    return false;
  }

  // ---------------------------------------------------------------- close / alarm

  async webSocketClose(ws: WebSocket, code: number, reason: string, _wasClean: boolean): Promise<void> {
    const c = this.conn(ws);
    this.rates.delete(ws);
    try {
      ws.close(code === 1005 || code === 1006 ? 1000 : code, reason);
    } catch {
      // already closed
    }
    if (c === null || c.gone) return;
    await this.departed(ws, c);
  }

  async webSocketError(ws: WebSocket, error: unknown): Promise<void> {
    console.error({ msg: "WorkspaceHub: websocket error", err: errFields(error) });
    const c = this.conn(ws);
    this.close(ws, 1011, "socket error");
    if (c !== null && !c.gone) await this.departed(ws, c);
  }

  /** Rule 7 / 8: the owner's last socket arms the 2-minute alarm; a participant's last socket records left_at. */
  private async departed(ws: WebSocket, c: HubConn): Promise<void> {
    if (this.live(`u:${c.userId}`).some((s) => s !== ws)) return; // another socket of theirs is still here
    if (await this.ended()) return;
    const t = this.now();
    if (c.seat === SEAT_OWNER) {
      if (this.owners().some((s) => s !== ws)) return;
      await this.ctx.storage.setAlarm(t + WORKSPACE_HUB_LIMITS.ownerGraceMs);
      return;
    }
    this.toOwner(this.peerFrame(c.seat, c.userId, "left"));
    const meta = await this.meta();
    if (meta !== null) {
      try {
        await markPresence(this.env.DB, meta.share_id, c.userId, "left_at", t);
      } catch (e) {
        console.error({ msg: "WorkspaceHub: left_at failed", share_id: meta.share_id, err: errFields(e) });
      }
    }
  }

  /** Rule 7: the owner stayed away for the whole grace -> the share ends ("timeout") and every socket closes 4100. */
  async alarm(): Promise<void> {
    if (await this.ended()) return;
    if (this.owners().length > 0) return;
    const meta = await this.meta();
    if (meta !== null) await endShare(this.rt(), meta.share_id, "timeout", { fromHub: true });
    await this.endAll();
  }
}

