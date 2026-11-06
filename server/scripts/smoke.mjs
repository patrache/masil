#!/usr/bin/env node
// Local end-to-end smoke test against a running `npx wrangler dev --test-scheduled` (plan 02 Task 18).
//
//   cd server
//   npx wrangler d1 migrations apply DB --local
//   npx wrangler dev --test-scheduled &
//   npm run smoke                      # MASIL_SMOKE_BASE=http://localhost:8787 by default
//
// One ESM file, no dependencies: Node 22+ global fetch / WebSocket. Every run uses fresh handles, so it can be
// repeated, but RL_SIGNUP (5 per IP per 60s) means consecutive runs need ~60s between them.
// The admin designation (step 13) edits the LOCAL D1 only (`wrangler d1 execute DB --local`); never --remote.
import { execFileSync } from "node:child_process";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const BASE = (process.env.MASIL_SMOKE_BASE ?? "http://localhost:8787").replace(/\/+$/, "");
const SERVER_DIR = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const UA = "masil/0.1.0 (smoke; node)";
const WAIT_MS = 5000;

// A valid 1x1 PNG (same bytes as test/helpers.ts PNG_1X1).
const PNG_1X1 = Uint8Array.from(
  Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==", "base64"),
);

// ---------------------------------------------------------------- helpers

const CROCKFORD = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";

/** ULID: 48-bit ms timestamp (10 chars) + 80-bit randomness (16 chars), Crockford base32. */
function ulid() {
  let t = Date.now();
  let time = "";
  for (let i = 0; i < 10; i++) {
    time = CROCKFORD[t % 32] + time;
    t = Math.floor(t / 32);
  }
  let r = 0n;
  for (const b of crypto.getRandomValues(new Uint8Array(10))) r = (r << 8n) | BigInt(b);
  let rand = "";
  for (let i = 0; i < 16; i++) {
    rand = CROCKFORD[Number(r & 31n)] + rand;
    r >>= 5n;
  }
  return time + rand;
}

function randomBase36(len) {
  const alphabet = "0123456789abcdefghijklmnopqrstuvwxyz";
  let out = "";
  for (const b of crypto.getRandomValues(new Uint8Array(len))) out += alphabet[b % 36];
  return out;
}

class CheckFailed extends Error {}

function short(v) {
  const s = typeof v === "string" ? v : JSON.stringify(v);
  return s === undefined ? String(v) : s.length > 2000 ? `${s.slice(0, 2000)}…` : s;
}

/** Asserts one condition of the current step; on failure aborts the run with the step name and detail. */
function check(name, cond, detail) {
  if (!cond) throw new CheckFailed(`${name}: ${short(typeof detail === "function" ? detail() : detail)}`);
}

let currentStep = "";

/** Runs one scenario step; prints "✓ <name>" when every check inside passed. */
async function step(name, fn) {
  currentStep = name;
  await fn();
  console.log(`✓ ${name}`);
}

/** HTTP call: JSON body when `json` is given, raw `body` otherwise. Returns status, parsed body, headers, bytes. */
async function call(method, path, { token, json, body, headers = {} } = {}) {
  const h = new Headers(headers);
  if (!h.has("User-Agent")) h.set("User-Agent", UA);
  if (token) h.set("Authorization", `Bearer ${token}`);
  let reqBody = body;
  if (json !== undefined) {
    h.set("Content-Type", "application/json");
    reqBody = JSON.stringify(json);
  }
  const res = await fetch(BASE + path, { method, headers: h, body: reqBody });
  const bytes = new Uint8Array(await res.arrayBuffer());
  const text = new TextDecoder().decode(bytes);
  let parsed = null;
  if (text !== "") {
    try {
      parsed = JSON.parse(text);
    } catch {
      parsed = text;
    }
  }
  return { status: res.status, body: parsed, headers: res.headers, bytes };
}

/** `call` that must answer `status`; returns the parsed body. */
async function expectStatus(name, status, method, path, opts) {
  const r = await call(method, path, opts);
  check(name, r.status === status, () => `${method} ${path} -> ${r.status} (want ${status}) ${short(r.body)}`);
  return r.body;
}

/**
 * Opens /v1/ws with the token in the Authorization header (events.md 1.1), waits for hello, sends
 * resume {epoch:null,last_seq:null} and waits for ready. Frames are buffered; next() consumes them in order.
 */
async function wsConnect(token, mode, deviceId) {
  const url = `${BASE.replace(/^http/, "ws")}/v1/ws?mode=${mode}&device_id=${encodeURIComponent(deviceId)}`;
  const ws = new WebSocket(url, { headers: { Authorization: `Bearer ${token}`, "User-Agent": UA } });
  const frames = [];
  const taken = new Set();
  const waiters = new Set();
  let closed = null; // { code, reason }
  const wake = () => {
    for (const w of [...waiters]) w();
  };
  ws.addEventListener("message", (e) => {
    if (typeof e.data !== "string") return;
    try {
      frames.push(JSON.parse(e.data));
    } catch {
      frames.push({ type: "<unparseable>", data: e.data });
    }
    wake();
  });
  ws.addEventListener("close", (e) => {
    closed = { code: e.code, reason: e.reason };
    wake();
  });
  ws.addEventListener("error", () => wake());

  const waitUntil = (probe, describe, ms) =>
    new Promise((res, rej) => {
      const attempt = () => {
        const v = probe();
        if (v === undefined) return false;
        res(v);
        return true;
      };
      if (attempt()) return;
      const waiter = () => {
        if (attempt()) {
          waiters.delete(waiter);
          clearTimeout(timer);
        }
      };
      const timer = setTimeout(() => {
        waiters.delete(waiter);
        rej(new CheckFailed(`${mode} ws: timed out waiting for ${describe}; got [${frames.map((f) => f.type).join(", ")}]` +
          (closed ? ` closed=${closed.code}` : "")));
      }, ms);
      waiters.add(waiter);
    });

  const client = {
    ws,
    frames,
    get closed() {
      return closed;
    },
    send(frame) {
      ws.send(JSON.stringify(frame));
    },
    /** First not-yet-returned frame of `type` whose data matches `pred` (already buffered or arriving within ms). */
    next(type, pred = () => true, ms = WAIT_MS) {
      return waitUntil(
        () => {
          const i = frames.findIndex((f, idx) => !taken.has(idx) && f.type === type && pred(f.data));
          if (i < 0) return undefined;
          taken.add(i);
          return frames[i];
        },
        type,
        ms,
      );
    },
    /** Resolves with the close event { code, reason }. */
    waitClose(ms = WAIT_MS) {
      return waitUntil(() => (closed ? closed : undefined), "close", ms);
    },
    close() {
      if (!closed) ws.close(1000);
    },
  };

  const hello = await client.next("hello");
  client.send({ type: "resume", data: { epoch: null, last_seq: null } });
  const ready = await client.next("ready");
  return { client, hello, ready };
}

async function signup(handle, displayName) {
  const password = `pw-${randomBase36(16)}`;
  const body = await expectStatus(`signup ${handle}`, 201, "POST", "/v1/auth/signup", {
    json: { handle, password, display_name: displayName, client: "other", device_name: "smoke" },
  });
  return { token: body.token, user: body.user, handle: body.user.handle, password };
}

function bytesEqual(a, b) {
  return a.length === b.length && a.every((v, i) => v === b[i]);
}

function isLocalBase() {
  try {
    const h = new URL(BASE).hostname;
    return h === "localhost" || h === "127.0.0.1" || h === "::1" || h === "[::1]";
  } catch {
    return false;
  }
}

/** Collects every item of a cursor-paginated list endpoint (stops after `maxPages`). */
async function listAll(name, path, token, maxPages = 20) {
  const items = [];
  let cursor = null;
  for (let i = 0; i < maxPages; i++) {
    const sep = path.includes("?") ? "&" : "?";
    const body = await expectStatus(name, 200, "GET", cursor ? `${path}${sep}cursor=${encodeURIComponent(cursor)}` : path, { token });
    items.push(...body.items);
    cursor = body.next_cursor;
    if (!cursor) break;
  }
  return items;
}

// ---------------------------------------------------------------- scenario

async function main() {
  console.log(`masil smoke -> ${BASE}`);
  console.log("note: RL_SIGNUP allows 5 signups per IP per 60s; wait ~60s between consecutive runs.");

  const run = randomBase36(6);
  const tag = `스모크${run}`;
  let alice, bob, admin, aliceFull, aliceNotify, post, upload;

  // 1
  await step("health", async () => {
    const body = await expectStatus("health", 200, "GET", "/v1/health");
    check("health", body && body.ok === true, body);
  });

  // 2
  await step("signup alice, bob, admin", async () => {
    alice = await signup(`smk_${run}_alice`, "alice");
    bob = await signup(`smk_${run}_bob`, "bob");
    admin = await signup(`smk_${run}_admin`, "admin");
  });

  // 3
  await step("alice ws full + notify ready", async () => {
    const deviceId = ulid();
    const full = await wsConnect(alice.token, "full", deviceId);
    check("ws full hello", full.hello.data.user_id === alice.user.id && full.hello.data.mode === "full", full.hello);
    check("ws full ready", full.ready.data.resumed === false, full.ready);
    aliceFull = full.client;
    const notify = await wsConnect(alice.token, "notify", deviceId);
    check("ws notify hello", notify.hello.data.mode === "notify" && notify.hello.data.local_tui_connected === true, notify.hello);
    aliceNotify = notify.client;
  });

  // 4
  await step("bob follows alice (active)", async () => {
    const body = await expectStatus("follow", 200, "PUT", `/v1/users/@${alice.handle}/follow`, { token: bob.token });
    check("follow state", body.state === "active", body);
  });

  // 5
  await step("upload + post + following timeline + media/ETag", async () => {
    const up = await expectStatus("upload", 201, "POST", "/v1/uploads?purpose=post", {
      token: alice.token,
      headers: { "Content-Type": "image/png" },
      body: PNG_1X1,
    });
    upload = up.upload;
    check("upload", upload && upload.status === "pending" && upload.size === PNG_1X1.length, up);
    const created = await expectStatus("post", 201, "POST", "/v1/posts", {
      token: alice.token,
      json: { client_nonce: ulid(), body: `스모크 글 #${tag}`, media_upload_ids: [upload.id] },
    });
    post = created.post;
    check("post", post.media.length === 1 && post.media[0].id === upload.id && post.hashtags.includes(tag), post);

    const tl = await expectStatus("timeline", 200, "GET", "/v1/timeline/following", { token: bob.token });
    check("timeline first item", tl.items[0] && tl.items[0].type === "post" && tl.items[0].post.id === post.id, tl.items[0]);

    const media = await call("GET", `/v1/media/${upload.id}`, { token: bob.token });
    check("media 200", media.status === 200, () => `${media.status} ${short(media.body)}`);
    check("media bytes", bytesEqual(media.bytes, PNG_1X1), `got ${media.bytes.length} bytes`);
    const etag = media.headers.get("ETag");
    check("media etag", !!etag, "no ETag header");
    const again = await call("GET", `/v1/media/${upload.id}`, { token: bob.token, headers: { "If-None-Match": etag } });
    check("media 304", again.status === 304, () => `${again.status} ${short(again.body)}`);
  });

  // 6
  await step("bob likes -> alice notify ws notification.created(post_like)", async () => {
    const body = await expectStatus("like", 200, "PUT", `/v1/posts/${post.id}/like`, { token: bob.token });
    check("like", body.liked === true && body.like_count === 1, body);
    const f = await aliceNotify.next(
      "notification.created",
      (d) => d.notification.type === "post_like" && d.notification.context.post_id === post.id,
    );
    check("post_like actor", f.data.notification.actor && f.data.notification.actor.id === bob.user.id, f.data);
  });

  // 7 (reply and quote carry the run's tag too, so the tag has 2 authors in step 12's trends)
  await step("reply + replies list, quote -> quote_count 1", async () => {
    await expectStatus("reply", 201, "POST", "/v1/posts", {
      token: bob.token,
      json: { client_nonce: ulid(), body: `답글 #${tag}`, reply_to_id: post.id },
    });
    const replies = await expectStatus("replies", 200, "GET", `/v1/posts/${post.id}/replies`, { token: bob.token });
    check("replies count", replies.items.length === 1, replies);
    await expectStatus("quote", 201, "POST", "/v1/posts", {
      token: bob.token,
      json: { client_nonce: ulid(), body: `인용 #${tag}`, quote_of_id: post.id },
    });
    const got = await expectStatus("get post", 200, "GET", `/v1/posts/${post.id}`, { token: bob.token });
    check("quote_count", got.post.quote_count === 1 && got.post.reply_count === 1, got.post);
  });

  // 8
  await step("story feed has_unseen -> view -> seen", async () => {
    const created = await expectStatus("story", 201, "POST", "/v1/stories", {
      token: alice.token,
      json: { body: "스모크 스토리", bg: "accent" },
    });
    const story = created.story;
    const feed = await expectStatus("story feed", 200, "GET", "/v1/stories/feed", { token: bob.token });
    const g = feed.items[0];
    check("story feed first group", g && g.user.id === alice.user.id && g.has_unseen === true, feed.items);
    check("story in group", g.stories.some((s) => s.id === story.id), g);
    await expectStatus("story view", 204, "POST", `/v1/stories/${story.id}/view`, { token: bob.token });
    const feed2 = await expectStatus("story feed again", 200, "GET", "/v1/stories/feed", { token: bob.token });
    const g2 = feed2.items.find((x) => x.user.id === alice.user.id);
    check("story seen", g2 && g2.has_unseen === false, feed2.items);
  });

  // 9
  await step("guestbook -> alice inbox guestbook_entry", async () => {
    await expectStatus("guestbook", 201, "POST", `/v1/users/@${alice.handle}/guestbook`, {
      token: bob.token,
      json: { body: "스모크 방명록" },
    });
    const inbox = await expectStatus("notifications", 200, "GET", "/v1/notifications", { token: alice.token });
    check(
      "guestbook_entry notification",
      inbox.items.some((n) => n.type === "guestbook_entry" && n.actor && n.actor.id === bob.user.id),
      inbox.items.map((n) => n.type),
    );
  });

  // 10
  await step("poll post -> bob votes -> voter_count 1", async () => {
    const created = await expectStatus("poll post", 201, "POST", "/v1/posts", {
      token: alice.token,
      json: { client_nonce: ulid(), body: "", poll: { question: "스모크 투표?", options: ["네", "아니요"] } },
    });
    const poll = created.post.poll;
    check("poll", poll && poll.options.length === 2, created.post);
    const voted = await expectStatus("vote", 200, "PUT", `/v1/polls/${poll.id}/vote`, {
      token: bob.token,
      json: { option_idxs: [0] },
    });
    check("voter_count", voted.poll.voter_count === 1 && voted.poll.options[0].vote_count === 1, voted.poll);
  });

  // 11
  await step("status + focus -> focus.ended + focus_summary alert (full and notify)", async () => {
    const text = `스모크 상태 ${run}`;
    const st = await expectStatus("status", 200, "PUT", "/v1/me/status", { token: alice.token, json: { emoji: "🎧", text } });
    check("status", st.status.text === text, st);
    const prof = await expectStatus("profile", 200, "GET", `/v1/users/@${alice.handle}`, { token: bob.token });
    check("profile status", prof.user.status && prof.user.status.text === text, prof.user);

    const started = await expectStatus("focus start", 200, "POST", "/v1/me/focus", { token: alice.token, json: { duration_sec: 60 } });
    check("focus", started.focus && started.focus.ends_at, started);
    const ended = await expectStatus("focus end", 200, "DELETE", "/v1/me/focus", { token: alice.token });
    check("focus summary", ended.summary && ended.summary.started_at && ended.summary.ended_at, ended);

    await aliceFull.next("focus.ended");
    const a = await aliceFull.next("alert", (d) => d.alert.kind === "focus_summary");
    check("full focus_summary", a.data.alert.suppressed === false, a.data);
    const n = await aliceNotify.next("alert", (d) => d.alert.kind === "focus_summary");
    check("notify focus_summary same id", n.data.alert.id === a.data.alert.id, { full: a.data.alert.id, notify: n.data.alert.id });
  });

  // 12
  await step("cron: trends include tag, cleanup runs", async () => {
    const t = await call("GET", `/__scheduled?cron=${"*/5 * * * *".replaceAll(" ", "+")}`);
    check("trends cron", t.status === 200, () => `${t.status} ${short(t.body)} (is wrangler dev running with --test-scheduled?)`);
    const trends = await expectStatus("trends", 200, "GET", "/v1/trends", { token: bob.token });
    check("trend tag", trends.items.some((x) => x.tag === tag), { want: tag, items: trends.items });
    const c = await call("GET", `/__scheduled?cron=${"17 * * * *".replaceAll(" ", "+")}`);
    check("cleanup cron", c.status === 200, () => `${c.status} ${short(c.body)}`);
  });

  // 13
  await step("report -> admin remove -> post and media 404", async () => {
    const rep = await expectStatus("report", 201, "POST", "/v1/reports", {
      token: bob.token,
      json: { target_type: "post", target_id: post.id, reason: "spam", note: "smoke" },
    });
    check("report id", typeof rep.report_id === "string", rep);

    check("local D1 only", isLocalBase(), `admin designation edits the local D1 only; ${BASE} is not local`);
    try {
      execFileSync(
        "npx",
        ["wrangler", "d1", "execute", "DB", "--local", "--command", `UPDATE users SET role='admin' WHERE handle='${admin.handle}'`],
        { cwd: SERVER_DIR, stdio: "pipe" },
      );
    } catch (e) {
      throw new CheckFailed(`wrangler d1 execute --local failed: ${short(String(e.stderr ?? e.message))}`);
    }

    const reports = await listAll("admin reports", "/v1/admin/reports?target_type=post&limit=100", admin.token);
    check("report listed", reports.some((r) => r.id === rep.report_id), `report ${rep.report_id} not in ${reports.length} open reports`);
    const removed = await expectStatus("content remove", 200, "POST", "/v1/admin/content/remove", {
      token: admin.token,
      json: { target_type: "post", target_id: post.id, reason: "smoke", report_id: rep.report_id },
    });
    check("remove action", removed.action && removed.action.action === "remove_content", removed);
    const gone = await call("GET", `/v1/posts/${post.id}`, { token: bob.token });
    check("post 404", gone.status === 404, () => `${gone.status} ${short(gone.body)}`);
    const goneMedia = await call("GET", `/v1/media/${upload.id}`, { token: bob.token });
    check("media 404", goneMedia.status === 404, () => `${goneMedia.status} ${short(goneMedia.body)}`);
  });

  // 14
  await step("suspend alice -> ws 4003, login 403 -> unsuspend -> login 200", async () => {
    await expectStatus("suspend", 200, "POST", `/v1/admin/users/@${alice.handle}/suspend`, {
      token: admin.token,
      json: { reason: "smoke" },
    });
    const fc = await aliceFull.waitClose();
    check("full ws 4003", fc.code === 4003, fc);
    const nc = await aliceNotify.waitClose();
    check("notify ws 4003", nc.code === 4003, nc);
    const denied = await call("POST", "/v1/auth/login", { json: { handle: alice.handle, password: alice.password } });
    check("login suspended", denied.status === 403 && denied.body?.error?.code === "account_suspended", () => `${denied.status} ${short(denied.body)}`);
    await expectStatus("unsuspend", 200, "POST", `/v1/admin/users/@${alice.handle}/unsuspend`, { token: admin.token });
    const ok = await expectStatus("login", 200, "POST", "/v1/auth/login", { json: { handle: alice.handle, password: alice.password } });
    check("login token", typeof ok.token === "string", ok);
  });

  console.log("SMOKE OK");
}

main().then(
  () => process.exit(0),
  (e) => {
    const detail = e instanceof CheckFailed ? e.message : `${e?.message ?? e}${e?.cause ? ` (${e.cause.code ?? e.cause.message ?? e.cause})` : ""}`;
    console.error(`✗ ${currentStep || "setup"} — ${detail}`);
    process.exit(1);
  },
);
