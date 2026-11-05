import { env } from "cloudflare:test";
import { describe, it, expect } from "vitest";

describe("schema", () => {
  it("applies 0001 and 0002", async () => {
    const { results } = await env.DB.prepare("SELECT name FROM sqlite_master WHERE type IN ('table') ORDER BY name").all<{ name: string }>();
    const names = results.map((r) => r.name);
    for (const t of ["users", "sessions", "channels", "messages", "message_mentions", "user_presence", "messages_fts", "posts_fts", "admin_actions"]) expect(names).toContain(t);
  });
  it("trigram FTS matches a 3-char Korean substring", async () => {
    const id = "01JTESTFTS" + crypto.randomUUID().replace(/-/g, "").slice(0, 16).toUpperCase();
    await env.DB.batch([
      env.DB.prepare("INSERT INTO users (id, handle, display_name, password_hash, created_at, updated_at) VALUES (?1, ?2, 'x', 'x', 0, 0)").bind(id, "fts_" + id.slice(-8).toLowerCase()),
      env.DB.prepare("INSERT INTO channels (id, kind, created_by, created_at, updated_at) VALUES (?1, 'group_dm', ?1, 0, 0)").bind(id),
      env.DB.prepare("INSERT INTO messages (id, channel_id, author_id, body, created_at) VALUES (?1, ?1, ?1, '오늘 배포는 몇 시?', 0)").bind(id),
    ]);
    const row = await env.DB.prepare("SELECT m.id FROM messages_fts f JOIN messages m ON m.rowid = f.rowid WHERE messages_fts MATCH ?1 AND m.id = ?2").bind('"배포는"', id).first();
    expect(row).not.toBeNull();
  });
});
