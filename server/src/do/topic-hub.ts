// Per-topic watcher hub (server-architecture.md 3.3, events.md 4.8, 5 `watch`, 6.1 `topic:` audience).
// One instance per topic string (`post:<post_id>`). Watchers hold a lease (UserHub renews it on every `watch`,
// clients resend `watch` every 120s, lease 300s); publish fans out to the UserHubs of unexpired watchers.
import { DurableObject } from "cloudflare:workers";
import { all, jsonList } from "../lib/db";
import type { VisibilitySpec } from "../lib/realtime";
import type { OutboundEvent } from "./user-hub";
import { errFields } from "../lib/log";

const SCHEMA = "CREATE TABLE IF NOT EXISTS watchers (user_id TEXT PRIMARY KEY, expires_at INTEGER NOT NULL)";

/** UserHub RPCs run this many at a time (server-architecture.md 3.3: 50 in parallel). */
const RPC_CHUNK = 50;

export class TopicHub extends DurableObject<Cloudflare.Env> {
  /** False until the table exists; reset after `deleteAll()` drops it. */
  private schemaReady = false;

  /** Wall clock; tests override it on the instance inside runInDurableObject. */
  protected now(): number {
    return Date.now();
  }

  private get sql(): SqlStorage {
    if (!this.schemaReady) {
      this.ctx.storage.sql.exec(SCHEMA);
      this.schemaReady = true;
    }
    return this.ctx.storage.sql;
  }

  /** Upsert the lease (expires_at = now + ttlMs); schedule the purge alarm when none is pending (or it is later). */
  async join(userId: string, ttlMs: number): Promise<void> {
    const expires = this.now() + ttlMs;
    this.sql.exec(
      "INSERT INTO watchers (user_id, expires_at) VALUES (?, ?) ON CONFLICT (user_id) DO UPDATE SET expires_at = excluded.expires_at",
      userId,
      expires,
    );
    const current = await this.ctx.storage.getAlarm();
    if (current === null || current > expires) await this.ctx.storage.setAlarm(expires);
  }

  async leave(userId: string): Promise<void> {
    this.sql.exec("DELETE FROM watchers WHERE user_id = ?", userId);
  }

  /**
   * Deliver `events` (tagged with `topic`) to every unexpired watcher's UserHub. With `visibleTo`, watchers are first
   * filtered by one D1 query (events.md 4.8; watcher ids as a single jsonList parameter). Returns delivered user count.
   */
  async publish(topic: string, events: OutboundEvent[], opts: { visibleTo?: VisibilitySpec } = {}): Promise<number> {
    if (events.length === 0) return 0;
    let ids = this.sql
      .exec<{ user_id: string }>("SELECT user_id FROM watchers WHERE expires_at > ?", this.now())
      .toArray()
      .map((r) => r.user_id);
    if (ids.length === 0) return 0;
    if (opts.visibleTo) ids = await this.visibleWatchers(ids, opts.visibleTo);
    if (ids.length === 0) return 0;

    const tagged = events.map((e) => ({ ...e, topic }));
    let delivered = 0;
    for (let i = 0; i < ids.length; i += RPC_CHUNK) {
      const chunk = ids.slice(i, i + RPC_CHUNK);
      const results = await Promise.allSettled(chunk.map((userId) => this.env.USER_HUB.getByName(userId).deliver({ userId, events: tagged })));
      results.forEach((r, j) => {
        if (r.status === "fulfilled") delivered++;
        else console.error("TopicHub deliver failed", { topic, target: chunk[j], err: errFields(r.reason) });
      });
    }
    return delivered;
  }

  /** server-architecture.md 4.11 visibleTo SQL over the watcher list: no block either way with any author, and an
   *  active follower (or the author) of every private author. */
  protected async visibleWatchers(ids: string[], v: VisibilitySpec): Promise<string[]> {
    const rows = await all<{ user_id: string }>(
      this.env.DB,
      `SELECT r.value AS user_id FROM json_each(?1) r
        WHERE NOT EXISTS (SELECT 1 FROM blocks b, json_each(?2) a
                           WHERE (b.blocker_id = r.value AND b.blocked_id = a.value)
                              OR (b.blocker_id = a.value AND b.blocked_id = r.value))
          AND NOT EXISTS (SELECT 1 FROM json_each(?3) pa
                           WHERE pa.value <> r.value
                             AND NOT EXISTS (SELECT 1 FROM follows f
                                              WHERE f.follower_id = r.value AND f.followee_id = pa.value AND f.status = 'active'))`,
      jsonList(ids),
      jsonList(v.authors),
      jsonList(v.privateAuthors),
    );
    return rows.map((r) => r.user_id);
  }

  /** Test/debug only: unexpired watcher count. */
  async inspect(): Promise<{ watchers: number }> {
    const row = this.sql.exec<{ n: number }>("SELECT COUNT(*) AS n FROM watchers WHERE expires_at > ?", this.now()).toArray()[0];
    return { watchers: row ? row.n : 0 };
  }

  /** Purge expired leases; when none is left free the object's storage, otherwise wake at the next expiry. */
  async alarm(): Promise<void> {
    const t = this.now();
    this.sql.exec("DELETE FROM watchers WHERE expires_at <= ?", t);
    const row = this.sql.exec<{ next: number | null }>("SELECT MIN(expires_at) AS next FROM watchers").toArray()[0];
    const next = row ? row.next : null;
    if (next === null) {
      await this.ctx.storage.deleteAll();
      this.schemaReady = false;
      return;
    }
    await this.ctx.storage.setAlarm(next);
  }
}
