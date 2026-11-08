// Pending item 4-a follow-up: 3cc82b8 put one D1 query (actorVisibility / hiddenActors, notify.ts) inside two UserHub
// paths that used to be synchronous, so other work of the same hub can now run while that query is in flight.
//
// 1. finishFocus — the alarm's natural end, DELETE /v1/me/focus and the catchUpFocus of GET/PUT presence and POST focus
//    may all be inside finishFocus at once. After the await it re-checks the focus (still the one it started with, not
//    yet finished); without that a second finisher logs and sends a second focus.ended + focus_summary alert (with
//    started_at 1970 and zero counts, the focus meta and focus_alerts being cleared by the first), or ends a focus
//    started meanwhile.
// 2. resume — events.md 3.3: the lookup and the replay are one block (blockConcurrencyWhile): an event goes either into
//    the replayed range (seq <= ready.seq, before ready) or live after ready, exactly once. A second resume frame, a
//    deliver and the alarm's log trim all wait for it.
//
// The hub's actor lookup is slowed with a D1 stub so the interleaving is certain, not a matter of timing luck.
import { env, runDurableObjectAlarm, runInDurableObject } from "cloudflare:test";
import { describe, expect, it } from "vitest";
import { CloseCode } from "../../src/do/protocol";
import { LOG_KEEP_MS } from "../../src/do/user-hub";
import { newId } from "../../src/lib/ids";
import type { Alert, FocusSummary, UserSummary } from "../../src/types/api";
import { api, connectWs, signup, waitFor, type TestUser, type WsClient } from "../helpers";

const hub = (id: string) => env.USER_HUB.getByName(id);
const settle = (ms = 150) => new Promise<void>((r) => setTimeout(r, ms));
const LOOKUP_DELAY_MS = 500;

function summaryOf(t: TestUser): UserSummary {
  return { id: t.user.id, handle: t.handle, display_name: t.handle, avatar_url: null, status: null, is_private: false, suspended: false };
}

function alertFrom(actor: UserSummary | null, kind: Alert["kind"] = "mention"): Alert {
  return {
    id: newId(),
    kind,
    title: "t",
    body: "b",
    actor,
    target: { type: "user", id: newId(), channel_id: null, message_id: null, thread_root_id: null, post_id: null, space_id: null },
    created_at: new Date().toISOString(),
    suppressed: false,
    suppressed_reason: null,
  };
}

/**
 * From now on the hub's notification-actor lookup (the actorVisibility query of notify.ts, used by resume and
 * finishFocus) answers `LOOKUP_DELAY_MS` late — every call, or only the first one. Everything else goes to the real D1.
 * `lookups(userId)` reads how many such lookups the hub made since.
 */
async function slowActorLookup(userId: string, opts: { onlyFirst?: boolean } = {}): Promise<void> {
  await runInDurableObject(hub(userId), (inst) => {
    const real = (inst as any).env;
    const self = inst as any;
    self.__lookups = 0;
    const db = new Proxy(real.DB, {
      get(target, prop) {
        if (prop !== "prepare") {
          const v = Reflect.get(target, prop);
          return typeof v === "function" ? v.bind(target) : v;
        }
        return (sql: string) => {
          const st = target.prepare(sql);
          if (!(sql.includes("FROM json_each(?1) j WHERE") && sql.includes("AS suspended"))) return st;
          return {
            bind: (...params: unknown[]) => {
              const bound = st.bind(...params);
              return {
                all: async () => {
                  const n = ++self.__lookups;
                  if (!opts.onlyFirst || n === 1) await new Promise((z) => setTimeout(z, LOOKUP_DELAY_MS));
                  return bound.all();
                },
              };
            },
          };
        };
      },
    });
    self.env = { ...real, DB: db };
  });
}

const lookups = (userId: string) => runInDurableObject(hub(userId), (inst) => (inst as any).__lookups as number);

const focusEnded = (c: WsClient) => c.frames.filter((f) => f.type === "focus.ended");
const summaryAlerts = (c: WsClient) => c.frames.filter((f) => f.type === "alert" && f.data.alert.kind === "focus_summary");

/** r (connected as `c`) focuses for 60s and collects one suppressed mention alert from v. */
async function focusWithAlert(): Promise<{ r: TestUser; c: WsClient; focus: { started_at: string; ends_at: string }; alert: Alert }> {
  const [r, v] = await Promise.all([signup(), signup()]);
  const c = await connectWs(r.token);
  const f = await api(r.token).post<any>("/v1/me/focus", { duration_sec: 60 });
  expect(f.status).toBe(200);
  await c.next("focus.started");
  const alert = alertFrom(summaryOf(v));
  await hub(r.user.id).deliver({ userId: r.user.id, events: [{ type: "alert", data: { alert }, ts: Date.now(), logged: true }] });
  expect((await c.next("alert", (d) => d.alert.id === alert.id)).data.alert.suppressed_reason).toBe("focus");
  return { r, c, focus: f.body.focus, alert };
}

/**
 * Waits (bounded) until focus.ended and the focus_summary alert have arrived — they are published after the call the
 * test awaited returns — then a little longer, so a wrong second summary would be seen too (v0.1.1 S3).
 */
async function settleOneSummary(c: WsClient): Promise<void> {
  await waitFor(() => focusEnded(c).length > 0 && summaryAlerts(c).length > 0, (ok) => ok);
  await settle(300);
}

function expectOneSummary(c: WsClient, focus: { started_at: string }): void {
  const ended = focusEnded(c);
  expect(ended).toHaveLength(1);
  expect(summaryAlerts(c)).toHaveLength(1);
  const s = ended[0]!.data.summary as FocusSummary;
  expect(s.started_at).toBe(focus.started_at);
  expect(s.counts.mention).toBe(1);
}

describe("finishFocus stays exactly-once while its actor lookup is in flight (4-a follow-up)", () => {
  it("two finishFocus calls at once: one summary, the other returns null", async () => {
    const { r, c, focus } = await focusWithAlert();
    await slowActorLookup(r.user.id);
    const res = await runInDurableObject(hub(r.user.id), async (inst) => {
      const t = Date.now();
      return Promise.all([(inst as any).finishFocus(t), (inst as any).finishFocus(t)]) as Promise<Array<FocusSummary | null>>;
    });
    expect(res.filter((x) => x !== null)).toHaveLength(1);
    expect(await lookups(r.user.id)).toBe(2); // both really waited on the lookup at the same time
    await settleOneSummary(c);
    expectOneSummary(c, focus);
  });

  it("the alarm's natural end and DELETE /v1/me/focus overlap: one focus.ended, one focus_summary alert", async () => {
    const { r, c, focus } = await focusWithAlert();
    await slowActorLookup(r.user.id);
    await runInDurableObject(hub(r.user.id), (inst) => {
      (inst as any).now = () => Date.now() + 61_000; // the focus is over (< 120s heartbeat timeout: the socket stays)
    });
    const alarm = runDurableObjectAlarm(hub(r.user.id));
    await settle(100);
    const del = api(r.token).del<any>("/v1/me/focus");
    expect(await alarm).toBe(true);
    expect((await del).status).toBe(409); // overdue: finished naturally, DELETE itself has no summary to return
    expect(await lookups(r.user.id)).toBe(2);
    await settleOneSummary(c);
    expectOneSummary(c, focus);
    expect((await api(r.token).get<any>("/v1/me/focus")).body.focus).toBeNull();
  });

  it("GET /v1/me/focus and PUT /v1/me/presence catch up an overdue focus at once: one summary", async () => {
    const { r, c, focus } = await focusWithAlert();
    await slowActorLookup(r.user.id);
    await runInDurableObject(hub(r.user.id), (inst) => {
      (inst as any).now = () => Date.now() + 61_000;
    });
    const [g, p] = await Promise.all([api(r.token).get<any>("/v1/me/focus"), api(r.token).put<any>("/v1/me/presence", { mode: "auto" })]);
    expect([g.status, p.status]).toEqual([200, 200]);
    expect(await lookups(r.user.id)).toBe(2);
    await settleOneSummary(c);
    expectOneSummary(c, focus);
  });

  it("a focus started while an older finisher waits is not ended by that finisher", async () => {
    const { r, c, focus } = await focusWithAlert();
    await slowActorLookup(r.user.id, { onlyFirst: true });
    const slow = runInDurableObject(hub(r.user.id), (inst) => (inst as any).finishFocus(Date.now()) as Promise<FocusSummary | null>);
    await settle(100);
    const end = await api(r.token).del<any>("/v1/me/focus"); // the second lookup is not delayed: this one wins
    expect(end.status).toBe(200);
    expect(end.body.summary.started_at).toBe(focus.started_at);
    const again = await api(r.token).post<any>("/v1/me/focus", { duration_sec: 60 });
    expect(again.status).toBe(200);
    expect(again.body.focus.started_at).not.toBe(focus.started_at);
    expect(await slow).toBeNull();
    await settleOneSummary(c);
    expectOneSummary(c, focus);
    expect((await api(r.token).get<any>("/v1/me/focus")).body.focus).toEqual(again.body.focus);
  });
});

// ---- resume: lookup + replay are one block (events.md 3.3) ----

interface RawSocket {
  ws: WebSocket;
  got: Array<{ type: string; seq: number | null; data: any }>;
  closed: Promise<number>;
  resume(epoch: string, lastSeq: number): void;
}

/**
 * r was online (epoch/seq of its ready), got one logged alert from v (ts `ts`), and went away. A new full socket of r
 * has received hello and not sent resume yet.
 */
async function alertThenReconnect(ts = Date.now()): Promise<{ r: TestUser; alert: Alert; alertSeq: number; epoch: string; seq: number; sock: RawSocket }> {
  const [r, v] = await Promise.all([signup(), signup()]);
  const c = await connectWs(r.token);
  const ready = c.frames.find((f) => f.type === "ready")!;
  const alert = alertFrom(summaryOf(v));
  await hub(r.user.id).deliver({ userId: r.user.id, events: [{ type: "alert", data: { alert }, ts, logged: true }] });
  const live = await c.next("alert", (d) => d.alert.id === alert.id);
  c.close();
  await settle();

  const res = await api(r.token).raw(`/v1/ws?mode=full&device_id=${newId()}`, { headers: { Upgrade: "websocket" } });
  const ws = res.webSocket!;
  const got: RawSocket["got"] = [];
  const closed = new Promise<number>((resolve) => ws.addEventListener("close", (e) => resolve(e.code), { once: true }));
  const hello = new Promise<void>((resolve) =>
    ws.addEventListener("message", (e) => {
      got.push(JSON.parse(e.data as string));
      resolve();
    }),
  );
  ws.accept();
  await hello;
  const sock: RawSocket = {
    ws,
    got,
    closed,
    resume: (epoch, lastSeq) => ws.send(JSON.stringify({ type: "resume", data: { epoch, last_seq: lastSeq } })),
  };
  return { r, alert, alertSeq: live.seq!, epoch: ready.data.epoch, seq: ready.data.seq, sock };
}

async function until(cond: () => boolean, what: string, ms = 3000): Promise<void> {
  for (let t = 0; t < ms && !cond(); t += 25) await settle(25);
  if (!cond()) throw new Error(`timed out waiting for ${what}`);
}

/**
 * The replayed range is (base, ready.seq], all of it before ready and counted in `replayed`; logged frames after ready
 * are newer than ready.seq. Returns the logged frames before / after ready.
 */
function splitAtReady(got: RawSocket["got"]): { before: RawSocket["got"]; ready: any; after: RawSocket["got"] } {
  const i = got.findIndex((f) => f.type === "ready");
  expect(i).toBeGreaterThan(0);
  const ready = got[i]!.data;
  const before = got.slice(0, i).filter((f) => f.seq !== null);
  const after = got.slice(i + 1).filter((f) => f.seq !== null);
  expect(ready.replayed).toBe(before.length);
  for (const f of before) expect(f.seq!).toBeLessThanOrEqual(ready.seq);
  for (const f of after) expect(f.seq!).toBeGreaterThan(ready.seq);
  return { before, ready, after };
}

describe("resume: the actor lookup and the replay are one block (events.md 3.3, 4-a follow-up)", () => {
  it("a second resume sent while the lookup waits: one replay, one ready, then 4000 for the extra resume", async () => {
    const { r, alert, epoch, seq, sock } = await alertThenReconnect();
    await slowActorLookup(r.user.id);
    sock.resume(epoch, seq);
    await settle(50);
    sock.resume(epoch, seq);
    const code = await Promise.race([sock.closed, settle(2500).then(() => null)]);
    expect(code, `frames: ${JSON.stringify(sock.got.map((f) => f.type))}`).toBe(CloseCode.protocol); // "resume already handled"
    expect(sock.got.filter((f) => f.type === "ready")).toHaveLength(1);
    expect(sock.got.filter((f) => f.type === "alert").map((f) => f.data.alert.id)).toEqual([alert.id]);
    const { ready } = splitAtReady(sock.got);
    expect(ready).toEqual(expect.objectContaining({ resumed: true, resync_required: false, replayed: 1 }));
    expect(await lookups(r.user.id)).toBe(1);
  });

  it("a deliver arriving while the lookup waits goes out exactly once, live after ready", async () => {
    const { r, alert, alertSeq, epoch, seq, sock } = await alertThenReconnect();
    await slowActorLookup(r.user.id);
    sock.resume(epoch, seq);
    await settle(100);
    const later = alertFrom(null, "invite");
    const delivered = hub(r.user.id).deliver({ userId: r.user.id, events: [{ type: "alert", data: { alert: later }, ts: Date.now(), logged: true }] });
    await delivered;
    await until(() => sock.got.some((f) => f.type === "alert" && f.data.alert.id === later.id), "the later alert");
    await settle(100);
    expect(sock.got.filter((f) => f.type === "alert" && f.data.alert.id === later.id)).toHaveLength(1);
    const { before, ready, after } = splitAtReady(sock.got);
    expect(before.map((f) => f.seq)).toEqual([alertSeq]);
    expect(before[0]!.data.alert.id).toBe(alert.id);
    expect(ready.seq).toBe(alertSeq);
    expect(after.filter((f) => f.type === "alert").map((f) => f.data.alert.id)).toEqual([later.id]);
    expect(await lookups(r.user.id)).toBe(1);
    sock.ws.close(1000);
  });

  it("the alarm's log trim waits: an event it trims is still replayed to the resume already looking it up", async () => {
    // the alert is older than the log's 24h window: the next alarm trims it (events.md 6.2)
    const { r, alert, alertSeq, epoch, seq, sock } = await alertThenReconnect(Date.now() - LOG_KEEP_MS - 60_000);
    await slowActorLookup(r.user.id);
    sock.resume(epoch, seq);
    await settle(100);
    const alarm = runDurableObjectAlarm(hub(r.user.id));
    expect(await alarm).toBe(true);
    await until(() => sock.got.some((f) => f.type === "ready"), "ready");
    const { before, ready } = splitAtReady(sock.got);
    expect(ready).toEqual(expect.objectContaining({ resumed: true, resync_required: false, replayed: 1 }));
    expect(before.map((f) => [f.seq, f.data.alert.id])).toEqual([[alertSeq, alert.id]]);
    // and the trim did run, right after
    const s = await hub(r.user.id).inspect();
    expect(s.oldestSeq).toBeGreaterThan(alertSeq);
    sock.ws.close(1000);
  });
});
