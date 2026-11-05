// Row -> api.md object serializers (server-architecture.md 4.9). Every response JSON is built here so that
// fields always exist (null when absent, [] when empty). List serializers batch-load related rows with
// json_each (no N+1).
import type { Focus, Me, PresenceState, Session, Settings, UserStatus, UserSummary } from "../types/api";
import type { SessionRow, UserPresenceRow, UserRow, UserSettingsRow } from "../types/rows";
import { isSuspendedNow } from "./auth";
import { all, batch, jsonList, stmt } from "./db";
import { err } from "./errors";
import { iso, isoOrNull, now } from "./time";

function toUserStatus(row: UserRow, nowMs: number): UserStatus | null {
  if (row.status_text === null && row.status_emoji === null) return null;
  if (row.status_expires_at !== null && row.status_expires_at <= nowMs) return null;
  return { emoji: row.status_emoji, text: row.status_text ?? "", expires_at: isoOrNull(row.status_expires_at) };
}

/** Expired status -> null; avatar_url "/v1/media/<id>"; suspended = suspended right now. */
export function toUserSummary(row: UserRow, nowMs: number = now()): UserSummary {
  return {
    id: row.id,
    handle: row.handle,
    display_name: row.display_name,
    avatar_url: row.avatar_upload_id === null ? null : `/v1/media/${row.avatar_upload_id}`,
    status: toUserStatus(row, nowMs),
    is_private: row.is_private === 1,
    suspended: isSuspendedNow(row, nowMs),
  };
}

/** One query for any number of ids; missing ids are simply absent from the map. */
export async function loadUserSummaries(db: D1Database, ids: readonly string[]): Promise<Map<string, UserSummary>> {
  const out = new Map<string, UserSummary>();
  const unique = [...new Set(ids)];
  if (unique.length === 0) return out;
  const rows = await all<UserRow>(db, "SELECT * FROM users WHERE id IN (SELECT value FROM json_each(?1))", jsonList(unique));
  const t = now();
  for (const row of rows) out.set(row.id, toUserSummary(row, t));
  return out;
}

function toSettings(user: UserRow, s: UserSettingsRow | null): Settings {
  // A user without a settings row (should not happen: signup creates it) reads as the schema defaults.
  return {
    dm_policy: s?.dm_policy ?? "friends_and_spaces",
    is_private: user.is_private === 1,
    notify: {
      dm: (s?.notify_dm ?? 1) === 1,
      mention: (s?.notify_mention ?? 1) === 1,
      invite: (s?.notify_invite ?? 1) === 1,
      friend_request: (s?.notify_friend_request ?? 1) === 1,
      thread_reply: (s?.notify_thread_reply ?? 1) === 1,
      social: (s?.notify_social ?? 0) === 1,
    },
    focus_default_min: s?.focus_default_min ?? 25,
  };
}

function toPresenceState(p: UserPresenceRow | null, nowMs: number): PresenceState {
  if (p === null) return { status: "offline", mode: "auto", focus: null };
  let focus: Focus | null = null;
  if (p.focus_ends_at !== null && p.focus_ends_at > nowMs) {
    // D1 keeps only the end time; the start lives in UserHub storage. updated_at is the row's last write,
    // which is the focus start unless presence changed since (then it is later than the true start).
    focus = { started_at: iso(Math.min(p.updated_at, p.focus_ends_at)), ends_at: iso(p.focus_ends_at) };
  }
  return { status: p.status, mode: p.mode, focus };
}

/** Me = UserSummary + profile counters + settings + presence (user_presence row). 404 if the user is gone. */
export async function serializeMe(db: D1Database, userId: string): Promise<Me> {
  const [u, s, p] = await batch(db, [
    stmt(db, "SELECT * FROM users WHERE id = ?1", userId),
    stmt(db, "SELECT * FROM user_settings WHERE user_id = ?1", userId),
    stmt(db, "SELECT * FROM user_presence WHERE user_id = ?1", userId),
  ]);
  const user = (u.results[0] as UserRow | undefined) ?? null;
  if (user === null) throw err.notFound("user");
  const settings = (s.results[0] as UserSettingsRow | undefined) ?? null;
  const presence = (p.results[0] as UserPresenceRow | undefined) ?? null;
  const t = now();
  return {
    ...toUserSummary(user, t),
    bio: user.bio,
    created_at: iso(user.created_at),
    role: user.role,
    follower_count: user.follower_count,
    following_count: user.following_count,
    post_count: user.post_count,
    settings: toSettings(user, settings),
    presence: toPresenceState(presence, t),
  };
}

export function serializeSession(row: SessionRow, currentSessionId: string): Session {
  return {
    id: row.id,
    client: row.client,
    device_name: row.device_name,
    created_at: iso(row.created_at),
    last_used_at: iso(row.last_used_at),
    expires_at: iso(row.expires_at),
    current: row.id === currentSessionId,
  };
}
