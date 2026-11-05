// Row -> api.md object serializers (server-architecture.md 4.9). Every response JSON is built here so that
// fields always exist (null when absent, [] when empty). List serializers batch-load related rows with
// json_each (no N+1).
import type { Focus, Friend, FriendRequest, Me, Notification, PresenceState, Profile, Relationship, Session, Settings, UserStatus, UserSummary } from "../types/api";
import type { PresenceItem } from "../types/events";
import type { FriendRequestRow, NotificationRow, SessionRow, UserPresenceRow, UserRow, UserSettingsRow } from "../types/rows";
import { isSuspendedNow } from "./auth";
import { all, batch, jsonList, stmt } from "./db";
import { err } from "./errors";
import { canDm, canViewUserContent, presenceVisible } from "./permissions";
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

/**
 * PresenceItem (events.md 4.6, api.md 5.1 GET /presence) for existing users, in the order of `ids` (duplicates once).
 * A user without a user_presence row reads as offline / never seen. Visibility (api.md 3.6) is the caller's job.
 */
export async function loadPresenceItems(db: D1Database, ids: readonly string[]): Promise<PresenceItem[]> {
  const unique = [...new Set(ids)];
  if (unique.length === 0) return [];
  const rows = await all<{ id: string; status: PresenceItem["status"] | null; last_seen_at: number | null }>(
    db,
    `SELECT u.id, p.status, p.last_seen_at
       FROM users u LEFT JOIN user_presence p ON p.user_id = u.id
      WHERE u.id IN (SELECT value FROM json_each(?1))`,
    jsonList(unique),
  );
  const byId = new Map(rows.map((r) => [r.id, r]));
  const out: PresenceItem[] = [];
  for (const id of unique) {
    const r = byId.get(id);
    if (r) out.push({ user_id: r.id, status: r.status ?? "offline", last_seen_at: isoOrNull(r.last_seen_at) });
  }
  return out;
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

const SHARED_SPACES_MAX = 10;

interface RelationshipFactsRow {
  is_friend: number;
  fr_id: string | null;
  fr_from: string | null;
  following: "active" | "pending" | null;
  followed_by: "active" | "pending" | null;
  blocked: number;
}

/** viewer -> target Relationship (api.md 2): pending friend request, follows both ways, my block only, can_dm, can_view_posts. */
async function loadRelationship(db: D1Database, viewerId: string, targetId: string): Promise<Relationship> {
  const [[facts, spaces], dm, view] = await Promise.all([
    batch(db, [
      stmt(
        db,
        `SELECT EXISTS (SELECT 1 FROM friendships f WHERE f.user_a = MIN(?1, ?2) AND f.user_b = MAX(?1, ?2)) AS is_friend,
                (SELECT r.id FROM friend_requests r
                  WHERE r.status = 'pending'
                    AND ((r.from_user_id = ?1 AND r.to_user_id = ?2) OR (r.from_user_id = ?2 AND r.to_user_id = ?1))
                  ORDER BY r.created_at DESC, r.id DESC LIMIT 1) AS fr_id,
                (SELECT r.from_user_id FROM friend_requests r
                  WHERE r.status = 'pending'
                    AND ((r.from_user_id = ?1 AND r.to_user_id = ?2) OR (r.from_user_id = ?2 AND r.to_user_id = ?1))
                  ORDER BY r.created_at DESC, r.id DESC LIMIT 1) AS fr_from,
                (SELECT fo.status FROM follows fo WHERE fo.follower_id = ?1 AND fo.followee_id = ?2) AS following,
                (SELECT fo.status FROM follows fo WHERE fo.follower_id = ?2 AND fo.followee_id = ?1) AS followed_by,
                EXISTS (SELECT 1 FROM blocks b WHERE b.blocker_id = ?1 AND b.blocked_id = ?2) AS blocked`,
        viewerId,
        targetId,
      ),
      stmt(
        db,
        `SELECT m1.space_id
           FROM space_members m1
           JOIN space_members m2 ON m2.space_id = m1.space_id AND m2.user_id = ?2
           JOIN spaces s ON s.id = m1.space_id AND s.deleted_at IS NULL
          WHERE m1.user_id = ?1
          ORDER BY m1.space_id
          LIMIT ${SHARED_SPACES_MAX}`,
        viewerId,
        targetId,
      ),
    ]),
    canDm(db, viewerId, targetId),
    canViewUserContent(db, viewerId, targetId),
  ]);
  const f = facts.results[0] as unknown as RelationshipFactsRow;
  return {
    is_friend: f.is_friend === 1,
    friend_request: f.fr_id === null ? null : { id: f.fr_id, direction: f.fr_from === viewerId ? "outgoing" : "incoming" },
    following: f.following ?? "none",
    followed_by: f.followed_by ?? "none",
    blocked: f.blocked === 1,
    can_dm: dm,
    can_view_posts: view === "ok",
    shared_space_ids: (spaces.results as Array<{ space_id: string }>).map((r) => r.space_id),
  };
}

/**
 * Profile of `row` as seen by `viewerId`: relationship null for self; presence/last_seen_at null outside api.md 3.6.
 * A block (either way) still yields a profile — with can_dm=false and can_view_posts=false.
 */
export async function serializeProfile(db: D1Database, viewerId: string, row: UserRow): Promise<Profile> {
  const self = row.id === viewerId;
  const [visible, presence, relationship] = await Promise.all([
    self ? Promise.resolve(new Set([row.id])) : presenceVisible(db, viewerId, [row.id]),
    loadPresenceItems(db, [row.id]),
    self ? Promise.resolve(null) : loadRelationship(db, viewerId, row.id),
  ]);
  const p = visible.has(row.id) ? (presence[0] ?? null) : null;
  return {
    ...toUserSummary(row),
    bio: row.bio,
    created_at: iso(row.created_at),
    follower_count: row.follower_count,
    following_count: row.following_count,
    post_count: row.post_count,
    presence: p === null ? null : p.status,
    last_seen_at: p === null ? null : p.last_seen_at,
    relationship,
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

/** What `notifications.extra` holds (written by lib/notify.ts): the preview and the non-null context fields. */
export interface NotificationExtra {
  preview?: string;
  context?: Partial<Notification["context"]>;
}

function parseNotificationExtra(extra: string | null): NotificationExtra {
  if (extra === null) return {};
  try {
    const v: unknown = JSON.parse(extra);
    return v !== null && typeof v === "object" ? (v as NotificationExtra) : {};
  } catch {
    return {};
  }
}

/** Inbox rows -> Notification (api.md 2), in row order. Actors are batch-loaded (one query); a missing actor is null. */
export async function serializeNotifications(db: D1Database, rows: NotificationRow[]): Promise<Notification[]> {
  const actorIds = rows.flatMap((r) => (r.actor_id === null ? [] : [r.actor_id]));
  const actors = await loadUserSummaries(db, actorIds);
  return rows.map((r) => {
    const extra = parseNotificationExtra(r.extra);
    const c = extra.context ?? {};
    return {
      id: r.id,
      type: r.type,
      actor: r.actor_id === null ? null : (actors.get(r.actor_id) ?? null),
      subject: { type: r.subject_type, id: r.subject_id },
      preview: typeof extra.preview === "string" ? extra.preview : "",
      context: {
        post_id: c.post_id ?? null,
        channel_id: c.channel_id ?? null,
        message_id: c.message_id ?? null,
        thread_root_id: c.thread_root_id ?? null,
        space_id: c.space_id ?? null,
        space_name: c.space_name ?? null,
      },
      created_at: iso(r.created_at),
      read: r.read_at !== null,
    };
  });
}

// ---- friends (plan 01 Task 17 additions) ----

/** friend_requests rows -> FriendRequest (api.md 2), in row order; both ends batch-loaded in one query. A row whose
 *  user is gone is dropped. */
export async function serializeFriendRequests(db: D1Database, rows: FriendRequestRow[]): Promise<FriendRequest[]> {
  const users = await loadUserSummaries(db, rows.flatMap((r) => [r.from_user_id, r.to_user_id]));
  return rows.flatMap((r) => {
    const from = users.get(r.from_user_id);
    const to = users.get(r.to_user_id);
    if (!from || !to) return [];
    return [{ id: r.id, from, to, status: r.status, created_at: iso(r.created_at) }];
  });
}

/** Friend (api.md 2) per `{user_id, created_at}` (created_at = friendship start), in row order; users and presence are
 *  batch-loaded (two queries). Presence is not filtered: friends always see each other's presence (api.md 3.6). */
export async function serializeFriends(db: D1Database, rows: Array<{ user_id: string; created_at: number }>): Promise<Friend[]> {
  const ids = rows.map((r) => r.user_id);
  const [users, presence] = await Promise.all([loadUserSummaries(db, ids), loadPresenceItems(db, ids)]);
  const status = new Map(presence.map((p) => [p.user_id, p.status]));
  return rows.flatMap((r) => {
    const user = users.get(r.user_id);
    if (!user) return [];
    return [{ user, since: iso(r.created_at), presence: status.get(r.user_id) ?? "offline" }];
  });
}
