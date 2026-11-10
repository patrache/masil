// CollabHub (v0.1.3, server-architecture.md 3.1, docs/contract/collab.md): one Durable Object per room document,
// name "doc:<doc_id>". It is the document's sequencer: op frames get one seq each in arrival order and apply with
// field-level last-writer-wins (lib/collab-reducer.ts), the sender gets ack, everyone else the op. The Worker route
// (routes/docs.ts) has already checked the session, RL_WS and membership; it passes who is connecting in
// X-Masil-Doc-Auth, the only identity this object trusts.
//
// Storage (DO SQLite, data-model.md 3.13): `ops` (the sequenced frames since the snapshot, client_op_id unique for
// idempotent resends), `snapshot` (one row: the canonical state at some seq), `versions` (each version's canonical
// snapshot; its metadata row in D1 collab_versions is written by this object too), `meta`. The state is held in memory
// once loaded (snapshot + the ops after it) and rebuilt after eviction. Presence is memory only.
// The alarm mirrors head_seq / updated_at to D1 (30 s batches), makes automatic versions, applies the record-version
// retention (api.md 5.24 버전 규칙) and compacts the op log past 1,000 ops.
import { DurableObject } from "cloudflare:workers";
import { errFields } from "../lib/log";
import { DocState, applyOps, limitReason, parseSnapshot, type Change, type Op } from "../lib/collab-reducer";
import { DOC_TYPES, isDocKind, type DocKind, type DocTypeSpec } from "../lib/collab-doctypes";
import {
  COLLAB_LIMITS,
  CollabClose,
  ackFrame,
  decideOp,
  helloOpsFrame,
  helloSnapshotFrame,
  kickedFrame,
  opEntry,
  opFrame,
  parseClientFrame,
  presenceFrame,
  rejectFrame,
  utf8Bytes,
  type Presence,
} from "./collab-protocol";

export type DocKickReason = "left" | "kicked" | "banned" | "space_deleted" | "doc_deleted";
export type DocConnRole = "creator" | "admin" | "member";

/** What routes/docs.ts puts in X-Masil-Doc-Auth (collab.md 2). */
export interface DocAuth {
  userId: string;
  sessionId: string;
  role: DocConnRole;
  docId: string;
  docType: DocKind;
  locked: boolean;
}

/** collab.md 2: per-socket state (ws.serializeAttachment). */
interface DocConn {
  userId: string;
  sessionId: string;
  role: DocConnRole;
  connId: string;
  /** sent hello: gets op / presence frames from now on */
  ready: boolean;
  opSec: number;
  opCount: number;
  /** last second the op rate went over, and how many such seconds in a row */
  overSec: number;
  overRun: number;
  presSec: number;
  presCount: number;
}

/** init's answer: the head seq, or why the snapshot was refused (api.md 5.24 -> 400 fields.snapshot). */
export type InitResult = { seq: number } | { error: "invalid" | "too_long" };

/** createVersion / restoreVersion: the new version's no (its D1 collab_versions row is written), or the refusal. */
export type VersionResult = { no: number; seq: number } | { error: "limit_exceeded" | "not_found" | "invalid" };

const SCHEMA = [
  "CREATE TABLE IF NOT EXISTS meta (k TEXT PRIMARY KEY, v TEXT NOT NULL)",
  "CREATE TABLE IF NOT EXISTS ops (seq INTEGER PRIMARY KEY, author TEXT NOT NULL, ts INTEGER NOT NULL, client_op_id TEXT UNIQUE, ops TEXT NOT NULL)",
  "CREATE TABLE IF NOT EXISTS snapshot (id INTEGER PRIMARY KEY CHECK (id = 1), seq INTEGER NOT NULL, body TEXT NOT NULL)",
  // kind: named (permanent, <= 100) | auto / restore = "record" versions (30 days, newest 200 kept)
  "CREATE TABLE IF NOT EXISTS versions (no INTEGER PRIMARY KEY, seq INTEGER NOT NULL, body TEXT NOT NULL, kind TEXT NOT NULL CHECK (kind IN ('named','auto','restore')), created_at INTEGER NOT NULL)",
];

const ALARM_BATCH_MS = 30_000;
const COMPACT_AFTER_OPS = 1000;
/** api.md 5.24 버전 규칙 */
export const VERSION_RULES = {
  namedMax: 100,
  recordMax: 200,
  recordKeepMs: 30 * 24 * 3600_000,
  autoIdleMs: 5 * 60_000,
  autoEveryOps: 200,
  autoMinGapMs: 5 * 60_000,
} as const;
const ROLES: ReadonlySet<string> = new Set(["creator", "admin", "member"]);

function parseDocAuth(header: string | null): DocAuth | null {
  if (header === null) return null;
  let v: unknown;
  try {
    v = JSON.parse(header);
  } catch {
    return null;
  }
  if (v === null || typeof v !== "object") return null;
  const a = v as Record<string, unknown>;
  if (typeof a.userId !== "string" || typeof a.sessionId !== "string" || typeof a.docId !== "string") return null;
  if (typeof a.role !== "string" || !ROLES.has(a.role) || !isDocKind(a.docType) || typeof a.locked !== "boolean") return null;
  return { userId: a.userId, sessionId: a.sessionId, role: a.role as DocConnRole, docId: a.docId, docType: a.docType, locked: a.locked };
}

export class CollabHub extends DurableObject<Cloudflare.Env> {
  /** the document state, loaded on first use (null = not loaded / dropped after a failed write) */
  private doc: DocState | null = null;
  private presence = new Map<string, Presence>();
  /** the alarm time this object last saw / set (null = unknown: ask storage) */
  private alarmAt: number | null = null;
  /** bumped by setLocked / setRole (per user): fetch keeps what an RPC set while it was reading D1 */
  private lockGen = 0;
  private roleGen = new Map<string, number>();

  constructor(ctx: DurableObjectState, env: Cloudflare.Env) {
    super(ctx, env);
    void ctx.blockConcurrencyWhile(async () => {
      for (const stmt of SCHEMA) ctx.storage.sql.exec(stmt);
    });
  }

  /** Wall clock; tests override it on the instance inside runInDurableObject. */
  protected now(): number {
    return Date.now();
  }

  // ---------------------------------------------------------------- storage

  private get sql(): SqlStorage {
    return this.ctx.storage.sql;
  }

  protected getMeta(k: string): string | null {
    const row = this.sql.exec<{ v: string }>("SELECT v FROM meta WHERE k = ?", k).toArray()[0];
    return row ? row.v : null;
  }

  protected getMetaNum(k: string): number {
    const n = Number(this.getMeta(k) ?? "0");
    return Number.isFinite(n) ? n : 0;
  }

  protected setMeta(k: string, v: string | number): void {
    this.sql.exec("INSERT INTO meta (k, v) VALUES (?, ?) ON CONFLICT (k) DO UPDATE SET v = excluded.v", k, String(v));
  }

  /** Seq of the stored snapshot (0 = none: the op log starts at 1). */
  protected snapshotSeq(): number {
    const row = this.sql.exec<{ seq: number }>("SELECT seq FROM snapshot WHERE id = 1").toArray()[0];
    return row ? row.seq : 0;
  }

  /** The state: snapshot + every op after it, in seq order. */
  protected state(): DocState {
    if (this.doc !== null) return this.doc;
    const snap = this.sql.exec<{ seq: number; body: string }>("SELECT seq, body FROM snapshot WHERE id = 1").toArray()[0];
    const st = snap ? DocState.fromText(snap.body) : new DocState();
    for (const r of this.sql.exec<{ seq: number; ops: string }>("SELECT seq, ops FROM ops WHERE seq > ? ORDER BY seq", st.seq)) {
      applyOps(st, r.seq, JSON.parse(r.ops) as Op[]);
    }
    this.doc = st;
    return st;
  }

  protected spec(): DocTypeSpec | null {
    const t = this.getMeta("doc_type");
    return isDocKind(t) ? DOC_TYPES[t] : null;
  }

  // ---------------------------------------------------------------- sockets

  protected conn(ws: WebSocket): DocConn | null {
    return (ws.deserializeAttachment() as DocConn | null) ?? null;
  }

  protected liveSockets(tag?: string): WebSocket[] {
    return this.ctx.getWebSockets(tag).filter((ws) => ws.readyState === WebSocket.OPEN);
  }

  protected send(ws: WebSocket, frame: string): void {
    try {
      ws.send(frame);
    } catch {
      // closing; its close handler cleans up
    }
  }

  protected closeWs(ws: WebSocket, code: number, reason: string): void {
    try {
      ws.close(code, reason);
    } catch {
      // already closed
    }
  }

  /** Ready sockets other than `except` (and, with `exceptUser`, other than that user's). */
  protected others(except: WebSocket, exceptUser: string | null = null): WebSocket[] {
    return this.liveSockets().filter((s) => {
      if (s === except) return false;
      const c = this.conn(s);
      return c !== null && c.ready && (exceptUser === null || c.userId !== exceptUser);
    });
  }

  /**
   * collab.md 2 / 3.4: the upgrade, from routes/docs.ts only. The header's lock, role and membership come from a D1
   * read the Worker made before this request arrived; a lock / role / membership / session change can commit in
   * between and its RPC (setLocked, setRole, kick, revokeSession) find no socket yet. So the socket is accepted first
   * (from here every such RPC sees it), then D1 is read again: no longer a member of a live space, or the doc gone ->
   * 404; the session revoked -> 401; otherwise the lock and role come from this read, unless an RPC arrived during it
   * (that RPC is newer, keep what it set).
   */
  async fetch(request: Request): Promise<Response> {
    const auth = parseDocAuth(request.headers.get("X-Masil-Doc-Auth"));
    if (auth === null) return new Response("missing doc auth", { status: 401 });
    if ((request.headers.get("Upgrade") ?? "").toLowerCase() !== "websocket") return new Response("expected websocket upgrade", { status: 400 });
    if (this.getMeta("closed") !== null) return new Response("not found", { status: 404 }); // doc / space deleted (closeAll)
    if (this.getMeta("doc_id") === null) this.setMeta("doc_id", auth.docId);
    if (this.getMeta("doc_type") === null) this.setMeta("doc_type", auth.docType);
    const pair = new WebSocketPair();
    const [client, server] = [pair[0], pair[1]];
    const c: DocConn = {
      userId: auth.userId,
      sessionId: auth.sessionId,
      role: auth.role,
      connId: crypto.randomUUID(),
      ready: false,
      opSec: 0,
      opCount: 0,
      overSec: 0,
      overRun: 0,
      presSec: 0,
      presCount: 0,
    };
    this.ctx.acceptWebSocket(server, [`u:${auth.userId}`, `s:${auth.sessionId}`]);
    server.serializeAttachment(c);
    const lockGen = this.lockGen;
    const roleGen = this.roleGen.get(auth.userId) ?? 0;
    const refuse = (status: number, why: string): Response => {
      this.closeWs(server, CollabClose.normal, why);
      return new Response(why, { status });
    };
    type Check = { locked: number; created_by: string; space_role: string | null; session_ok: number };
    let row: Check | null;
    try {
      row = await this.env.DB.prepare(
        `SELECT d.locked, d.created_by,
                (SELECT m.role FROM space_members m JOIN spaces sp ON sp.id = m.space_id AND sp.deleted_at IS NULL
                  WHERE m.space_id = d.space_id AND m.user_id = ?2) AS space_role,
                EXISTS (SELECT 1 FROM sessions s
                         WHERE s.id = ?3 AND s.user_id = ?2 AND s.revoked_at IS NULL AND s.expires_at > ?4) AS session_ok
           FROM collab_docs d WHERE d.id = ?1 AND d.deleted_at IS NULL`,
      )
        .bind(auth.docId, auth.userId, auth.sessionId, Date.now())
        .first<Check>();
    } catch (e) {
      console.error({ msg: "CollabHub: connect check failed", doc_id: auth.docId, err: errFields(e) });
      return refuse(503, "try again");
    }
    if (row === null || row.space_role === null) return refuse(404, "not found");
    if (row.session_ok !== 1) return refuse(401, "session revoked");
    // kicked / revoked / closeAll while D1 was read: the socket is already closed
    const cur = this.conn(server);
    if (server.readyState !== WebSocket.OPEN || cur === null || this.getMeta("closed") !== null) return refuse(404, "not found");
    if (this.lockGen === lockGen) this.setMeta("locked", row.locked === 1 ? 1 : 0);
    if ((this.roleGen.get(auth.userId) ?? 0) === roleGen) {
      cur.role = row.created_by === auth.userId ? "creator" : row.space_role === "owner" || row.space_role === "admin" ? "admin" : "member";
      server.serializeAttachment(cur);
    }
    return new Response(null, { status: 101, webSocket: client });
  }

  async webSocketMessage(ws: WebSocket, message: string | ArrayBuffer): Promise<void> {
    if (ws.readyState !== WebSocket.OPEN) return;
    const c = this.conn(ws);
    if (c === null) return this.closeWs(ws, CollabClose.internal, "lost connection state");
    if (typeof message !== "string") return this.closeWs(ws, CollabClose.unsupported, "text frames only");
    if (message.length > COLLAB_LIMITS.clientFrameBytes || (message.length * 3 > COLLAB_LIMITS.clientFrameBytes && utf8Bytes(message) > COLLAB_LIMITS.clientFrameBytes)) {
      return this.closeWs(ws, CollabClose.tooBig, "frame too big");
    }
    const f = parseClientFrame(message);
    switch (f.t) {
      case "hello":
        return this.onHello(ws, c, f.since);
      case "op":
        return this.onOp(ws, c, f.clientOpId, f.ops);
      case "presence":
        return this.onPresence(ws, c, f.patch);
      default:
        return;
    }
  }

  async webSocketClose(ws: WebSocket, code: number, reason: string, _wasClean: boolean): Promise<void> {
    this.closeWs(ws, code === 1005 || code === 1006 ? CollabClose.normal : code, reason);
    const c = this.conn(ws);
    if (c !== null) this.presence.delete(c.connId);
  }

  async webSocketError(ws: WebSocket, error: unknown): Promise<void> {
    console.error({ msg: "CollabHub: websocket error", err: errFields(error) });
    this.closeWs(ws, CollabClose.internal, "socket error");
    const c = this.conn(ws);
    if (c !== null) this.presence.delete(c.connId);
  }

  // ---------------------------------------------------------------- frames

  /**
   * collab.md 3.2 hello: snapshot for null / a compacted range / an unknown seq, else the ops after `since`. Answered
   * once per connection (3.4): a repeat is dropped, or a 30-byte frame could ask for a full snapshot each time.
   */
  protected onHello(ws: WebSocket, c: DocConn, since: number | null): void {
    if (c.ready) return;
    const st = this.state();
    const head = st.seq;
    let frame: string | null = null;
    if (since !== null && since <= head && since >= this.snapshotSeq()) {
      const max = this.spec()?.maxDocBytes ?? 0;
      const entries: string[] = [];
      let bytes = 0;
      for (const r of this.sql.exec<{ seq: number; author: string; ops: string }>("SELECT seq, author, ops FROM ops WHERE seq > ? ORDER BY seq", since)) {
        const e = opEntry(r.seq, r.author, r.ops);
        bytes += utf8Bytes(e) + 1;
        if (bytes > max) break;
        entries.push(e);
      }
      if (bytes <= max) frame = helloOpsFrame(head, entries);
    }
    this.send(ws, frame ?? helloSnapshotFrame(head, st.snapshotText()));
    c.ready = true;
    ws.serializeAttachment(c);
  }

  /** collab.md 3.3, one op frame. Everything up to the sends is synchronous: frames are sequenced in arrival order. */
  protected async onOp(ws: WebSocket, c: DocConn, clientOpId: string, ops: Op[] | null): Promise<void> {
    // rate (collab.md 4): 20 op frames per second per connection; a third second over in a row closes 4008
    const t = this.now();
    const sec = Math.floor(t / 1000);
    if (c.opSec !== sec) {
      c.opSec = sec;
      c.opCount = 0;
    }
    c.opCount++;
    if (c.opCount === COLLAB_LIMITS.opsPerSecond + 1) {
      c.overRun = c.overSec === sec - 1 ? c.overRun + 1 : 1;
      c.overSec = sec;
    }
    ws.serializeAttachment(c);
    if (c.opCount > COLLAB_LIMITS.opsPerSecond) {
      if (c.overRun >= COLLAB_LIMITS.rateStrikes) return this.closeWs(ws, CollabClose.rateLimited, "too many op frames");
      return this.send(ws, rejectFrame(clientOpId, "rate_limited"));
    }
    if (ops === null) return this.send(ws, rejectFrame(clientOpId, "invalid"));
    // idempotent resend: the first seq again, nothing else
    const prior = this.sql.exec<{ seq: number }>("SELECT seq FROM ops WHERE client_op_id = ?", clientOpId).toArray()[0];
    if (prior) return this.send(ws, ackFrame(clientOpId, prior.seq));
    if (this.getMeta("locked") === "1" && c.role !== "creator" && c.role !== "admin") return this.send(ws, rejectFrame(clientOpId, "locked"));
    const spec = this.spec();
    if (spec === null) return this.closeWs(ws, CollabClose.internal, "unknown document type");

    const st = this.state();
    const seq = st.seq + 1;
    const r = decideOp(st, spec, seq, clientOpId, ops);
    if (!r.ok) return this.send(ws, r.frame);
    try {
      this.ctx.storage.transactionSync(() => {
        this.sql.exec("INSERT INTO ops (seq, author, ts, client_op_id, ops) VALUES (?, ?, ?, ?, ?)", seq, c.userId, t, clientOpId, r.opsText);
        this.setMeta("seq", seq);
        this.setMeta("last_op_at", t);
        if (this.getMetaNum("mirror_at") === 0) this.setMeta("mirror_at", t + ALARM_BATCH_MS);
      });
    } catch (e) {
      this.doc = null; // memory ran ahead of storage: rebuild from what was written
      console.error({ msg: "CollabHub: op write failed", err: errFields(e) });
      return this.closeWs(ws, CollabClose.internal, "write failed");
    }
    this.send(ws, ackFrame(clientOpId, seq));
    const frame = opFrame(seq, c.userId, clientOpId, r.opsText);
    for (const s of this.others(ws)) this.send(s, frame);
    await this.armAlarm(t);
  }

  /** collab.md 3.1-3.2 presence: merged per connection, memory only, to other people, 10 per second (rest dropped). */
  protected onPresence(ws: WebSocket, c: DocConn, patch: Partial<Presence>): void {
    const sec = Math.floor(this.now() / 1000);
    if (c.presSec !== sec) {
      c.presSec = sec;
      c.presCount = 0;
    }
    c.presCount++;
    ws.serializeAttachment(c);
    if (c.presCount > COLLAB_LIMITS.presencePerSecond) return;
    const prev = this.presence.get(c.connId) ?? { cursor: null, selection: [], dragging: null };
    const next: Presence = { ...prev, ...patch };
    this.presence.set(c.connId, next);
    const frame = presenceFrame(c.userId, next);
    for (const s of this.others(ws, c.userId)) this.send(s, frame);
  }

  // ---------------------------------------------------------------- alarm

  /**
   * The earliest thing due: the D1 mirror (30 s after the first unmirrored op), the next automatic version (api.md
   * 5.24: edits idle 5 minutes, or 200 seqs since the last version — never within 5 minutes of the last automatic one)
   * and the oldest record version's 30-day expiry. Never moves a set alarm later; the alarm re-arms itself.
   */
  protected async armAlarm(t = this.now()): Promise<void> {
    const due: number[] = [];
    const seq = this.getMetaNum("seq");
    if (seq > this.getMetaNum("d1_seq")) due.push(this.getMetaNum("mirror_at") || t + ALARM_BATCH_MS);
    const lastVersionSeq = this.getMetaNum("last_version_seq");
    if (seq > lastVersionSeq) {
      const base = seq - lastVersionSeq >= VERSION_RULES.autoEveryOps ? t : this.getMetaNum("last_op_at") + VERSION_RULES.autoIdleMs;
      due.push(Math.max(base, this.getMetaNum("last_auto_at") + VERSION_RULES.autoMinGapMs));
    }
    const oldest = this.sql.exec<{ c: number | null }>("SELECT MIN(created_at) AS c FROM versions WHERE kind <> 'named'").toArray()[0]?.c ?? null;
    if (oldest !== null) due.push(oldest + VERSION_RULES.recordKeepMs + 1);
    if (due.length === 0) return;
    const next = Math.min(...due);
    if (this.alarmAt !== null && this.alarmAt <= next) return;
    const cur = await this.ctx.storage.getAlarm();
    if (cur === null || cur > next) {
      await this.ctx.storage.setAlarm(next);
      this.alarmAt = next;
    } else this.alarmAt = cur;
  }

  async alarm(): Promise<void> {
    this.alarmAt = null;
    const t = this.now();
    await this.mirrorHead(t);
    await this.autoVersion(t);
    await this.pruneVersions(t);
    this.compact();
    await this.armAlarm(t);
  }

  /** head_seq / updated_at to D1 collab_docs (MAX: never back). A failure is retried by the next alarm. */
  protected async mirrorHead(t: number): Promise<void> {
    const docId = this.getMeta("doc_id");
    const seq = this.getMetaNum("seq");
    if (docId === null || seq <= this.getMetaNum("d1_seq")) return;
    try {
      await this.env.DB.prepare("UPDATE collab_docs SET head_seq = MAX(head_seq, ?2), updated_at = MAX(updated_at, ?3) WHERE id = ?1")
        .bind(docId, seq, this.getMetaNum("last_op_at"))
        .run();
      this.setMeta("d1_seq", seq);
      this.setMeta("mirror_at", 0);
    } catch (e) {
      this.setMeta("mirror_at", t + ALARM_BATCH_MS);
      console.error({ msg: "CollabHub: D1 mirror failed", doc_id: docId, err: errFields(e) });
    }
  }

  /** api.md 5.24: an automatic version when one is due (see armAlarm). */
  protected async autoVersion(t: number): Promise<void> {
    const seq = this.getMetaNum("seq");
    const lastVersionSeq = this.getMetaNum("last_version_seq");
    if (seq <= lastVersionSeq || this.getMeta("doc_id") === null) return;
    const idle = t - this.getMetaNum("last_op_at") >= VERSION_RULES.autoIdleMs;
    const many = seq - lastVersionSeq >= VERSION_RULES.autoEveryOps;
    if (!(idle || many) || t - this.getMetaNum("last_auto_at") < VERSION_RULES.autoMinGapMs) return;
    try {
      await this.addVersion("auto", null, null, null, t);
    } catch (e) {
      console.error({ msg: "CollabHub: auto version failed", doc_id: this.getMeta("doc_id"), err: errFields(e) });
    }
  }

  /**
   * Stores a version of the current state (DO body first, then the D1 metadata row; a D1 failure takes the body back
   * out and throws). `no` comes from a counter, so concurrent calls never collide.
   */
  protected async addVersion(kind: "named" | "auto" | "restore", name: string | null, createdBy: string | null, restoredFrom: number | null, t: number): Promise<{ no: number; seq: number }> {
    const docId = this.getMeta("doc_id");
    if (docId === null) throw new Error("CollabHub: version before the doc id is known");
    const st = this.state();
    const no = this.getMetaNum("version_no") + 1;
    const seq = st.seq;
    this.ctx.storage.transactionSync(() => {
      this.setMeta("version_no", no);
      this.sql.exec("INSERT INTO versions (no, seq, body, kind, created_at) VALUES (?, ?, ?, ?, ?)", no, seq, st.snapshotText(), kind, t);
      this.setMeta("last_version_seq", Math.max(seq, this.getMetaNum("last_version_seq")));
      if (kind === "auto") this.setMeta("last_auto_at", t);
    });
    try {
      await this.env.DB.prepare(
        `INSERT INTO collab_versions (doc_id, no, seq, name, auto, restored_from, created_by, created_at)
         VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8)`,
      )
        .bind(docId, no, seq, name, kind === "auto" ? 1 : 0, restoredFrom, createdBy, t)
        .run();
    } catch (e) {
      // no trace of the failed version: last_version_seq / last_auto_at again follow the versions that exist, so the
      // next alarm still sees the automatic version as due (collab.md 4.2). Recomputed rather than restored, because
      // another addVersion may have run during the await. version_no stays: a skipped no is harmless, a reused one is not.
      this.ctx.storage.transactionSync(() => {
        this.sql.exec("DELETE FROM versions WHERE no = ?", no);
        this.setMeta("last_version_seq", this.sql.exec<{ s: number | null }>("SELECT MAX(seq) AS s FROM versions").toArray()[0]?.s ?? 0);
        this.setMeta("last_auto_at", this.sql.exec<{ a: number | null }>("SELECT MAX(created_at) AS a FROM versions WHERE kind = 'auto'").toArray()[0]?.a ?? 0);
      });
      throw e;
    }
    return { no, seq };
  }

  /** Record versions (auto / restore): older than 30 days, and beyond the newest 200, go — D1 rows first. */
  protected async pruneVersions(t: number): Promise<void> {
    const docId = this.getMeta("doc_id");
    if (docId === null) return;
    const doomed = this.sql
      .exec<{ no: number }>(
        `SELECT no FROM versions WHERE kind <> 'named'
          AND (created_at < ? OR no NOT IN (SELECT no FROM versions WHERE kind <> 'named' ORDER BY no DESC LIMIT ?))`,
        t - VERSION_RULES.recordKeepMs,
        VERSION_RULES.recordMax,
      )
      .toArray()
      .map((r) => r.no);
    if (doomed.length === 0) return;
    try {
      await this.env.DB.prepare("DELETE FROM collab_versions WHERE doc_id = ?1 AND no IN (SELECT value FROM json_each(?2))").bind(docId, JSON.stringify(doomed)).run();
      this.sql.exec(`DELETE FROM versions WHERE no IN (${doomed.map(() => "?").join(",")})`, ...doomed);
    } catch (e) {
      console.error({ msg: "CollabHub: version prune failed", doc_id: docId, err: errFields(e) });
    }
  }

  /** Past 1,000 ops after the snapshot (any, with `force`): snapshot at head, older ops dropped (collab.md 3.2). */
  protected compact(force = false): void {
    const since = this.snapshotSeq();
    const n = this.sql.exec<{ n: number }>("SELECT COUNT(*) AS n FROM ops WHERE seq > ?", since).toArray()[0]?.n ?? 0;
    if (n === 0 || (!force && n <= COMPACT_AFTER_OPS)) return;
    const st = this.state();
    this.ctx.storage.transactionSync(() => {
      this.sql.exec("INSERT INTO snapshot (id, seq, body) VALUES (1, ?, ?) ON CONFLICT (id) DO UPDATE SET seq = excluded.seq, body = excluded.body", st.seq, st.snapshotText());
      this.sql.exec("DELETE FROM ops WHERE seq <= ?", st.seq);
    });
  }

  // ---------------------------------------------------------------- RPC (collab.md 4.1)

  /**
   * First state of a new document (POST /v1/spaces/{id}/docs `snapshot`): shape and name rules, the type's validateOp
   * (seeing it as a reset) and limits. A non-empty snapshot becomes seq 1. Returns the head seq or the refusal.
   */
  async init(docType: DocKind, raw: unknown): Promise<InitResult> {
    if (this.getMetaNum("seq") !== 0 || this.snapshotSeq() !== 0) throw new Error("CollabHub.init on a document that has content");
    const spec = DOC_TYPES[docType];
    const snap = parseSnapshot(raw);
    if (snap === null) return { error: "invalid" };
    const seq = snap.objects.length > 0 ? 1 : 0;
    const empty = new DocState();
    if (spec.validateOp({ op: "reset", snapshot: snap }, empty) !== null) return { error: "invalid" };
    const st = DocState.fromSnapshot({ seq, objects: snap.objects });
    const changes: Change[] = snap.objects.flatMap((o) => Object.keys(o.fields).map((field) => ({ id: o.id, field, removed: o.deleted === true })));
    if (limitReason(st, spec, changes) !== null) return { error: "too_long" };
    this.ctx.storage.transactionSync(() => {
      this.setMeta("doc_type", docType);
      if (seq > 0) {
        this.sql.exec("INSERT INTO snapshot (id, seq, body) VALUES (1, ?, ?)", seq, st.snapshotText());
        this.setMeta("seq", seq);
        this.setMeta("d1_seq", seq); // createDoc writes head_seq with the row
      }
    });
    this.doc = st;
    return { seq };
  }

  /** The doc id / type a REST call names, for an object that has had no socket yet. */
  private bindDoc(docId: string, docType: DocKind): void {
    if (this.getMeta("doc_id") === null) this.setMeta("doc_id", docId);
    if (this.getMeta("doc_type") === null) this.setMeta("doc_type", docType);
  }

  /** POST /v1/docs/{id}/versions: a named version of the head (api.md 5.24; the 101st -> limit_exceeded). */
  async createVersion(docId: string, docType: DocKind, name: string, userId: string): Promise<VersionResult> {
    this.bindDoc(docId, docType);
    const named = this.sql.exec<{ n: number }>("SELECT COUNT(*) AS n FROM versions WHERE kind = 'named'").toArray()[0]?.n ?? 0;
    if (named >= VERSION_RULES.namedMax) return { error: "limit_exceeded" };
    const t = this.now();
    const r = await this.addVersion("named", name, userId, null, t);
    await this.armAlarm(t);
    return r;
  }

  /**
   * POST /v1/docs/{id}/versions/{no}/restore (permission checks are the Worker's): one seq whose frame is
   * `[{op:"reset", snapshot}]` by `userId`, sent to every socket (collab.md 1.1, 3.2), then a restore version
   * (restored_from = no) of the result. History is kept.
   */
  async restoreVersion(docId: string, docType: DocKind, no: number, userId: string): Promise<VersionResult> {
    this.bindDoc(docId, docType);
    const row = this.sql.exec<{ body: string }>("SELECT body FROM versions WHERE no = ?", no).toArray()[0];
    if (!row) return { error: "not_found" };
    const spec = this.spec();
    if (spec === null) return { error: "invalid" };
    const snapshot = parseSnapshot(JSON.parse(row.body));
    if (snapshot === null) return { error: "invalid" };
    const ops: Op[] = [{ op: "reset", snapshot }];
    const st = this.state();
    const seq = st.seq + 1;
    const t = this.now();
    const r = decideOp(st, spec, seq, "", ops);
    if (!r.ok) return { error: "invalid" };
    try {
      this.ctx.storage.transactionSync(() => {
        this.sql.exec("INSERT INTO ops (seq, author, ts, client_op_id, ops) VALUES (?, ?, ?, NULL, ?)", seq, userId, t, r.opsText);
        this.setMeta("seq", seq);
        this.setMeta("last_op_at", t);
        if (this.getMetaNum("mirror_at") === 0) this.setMeta("mirror_at", t + ALARM_BATCH_MS);
      });
    } catch (e) {
      this.doc = null;
      throw e;
    }
    const frame = opFrame(seq, userId, null, r.opsText);
    for (const s of this.liveSockets()) if (this.conn(s)?.ready) this.send(s, frame);
    const v = await this.addVersion("restore", null, userId, no, t);
    await this.pruneVersions(t);
    await this.armAlarm(t);
    return v;
  }

  /** A version's canonical snapshot text, or null (never made / pruned). */
  async versionBody(no: number): Promise<string | null> {
    return this.sql.exec<{ body: string }>("SELECT body FROM versions WHERE no = ?", no).toArray()[0]?.body ?? null;
  }

  /** kicked{reason} then 4100 on every socket of the user. */
  async kick(userId: string, reason: DocKickReason): Promise<void> {
    for (const ws of this.liveSockets(`u:${userId}`)) this.kickWs(ws, reason);
  }

  /** kicked{reason} then 4100 on every socket; new connections are refused from now on (doc / space deleted). */
  async closeAll(reason: DocKickReason): Promise<void> {
    this.setMeta("closed", reason);
    for (const ws of this.liveSockets()) this.kickWs(ws, reason);
  }

  private kickWs(ws: WebSocket, reason: DocKickReason): void {
    this.send(ws, kickedFrame(reason));
    this.closeWs(ws, CollabClose.kicked, reason);
  }

  async setLocked(locked: boolean): Promise<void> {
    this.lockGen++;
    this.setMeta("locked", locked ? 1 : 0);
  }

  async head(): Promise<number> {
    return this.getMetaNum("seq");
  }

  /** Closes the user's sockets of these sessions ("all" = every one) with `code`, without a frame. */
  async revokeSession(userId: string, sessionIds: string[] | "all", code: 4001 | 4003): Promise<void> {
    const ids = sessionIds === "all" ? null : new Set(sessionIds);
    for (const ws of this.liveSockets(`u:${userId}`)) {
      const c = this.conn(ws);
      if (ids === null || (c !== null && ids.has(c.sessionId))) this.closeWs(ws, code, code === 4003 ? "account suspended" : "session revoked");
    }
  }

  /** The user's new space role on their open sockets (the doc's creator stays "creator"); the next op sees it. */
  async setRole(userId: string, role: "admin" | "member"): Promise<void> {
    this.roleGen.set(userId, (this.roleGen.get(userId) ?? 0) + 1);
    for (const ws of this.liveSockets(`u:${userId}`)) {
      const c = this.conn(ws);
      if (c === null || c.role === "creator" || c.role === role) continue;
      c.role = role;
      ws.serializeAttachment(c);
    }
  }
}
