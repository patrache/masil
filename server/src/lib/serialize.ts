// Row -> api.md object serializers (server-architecture.md 4.9). Every response JSON is built here so that
// fields always exist (null when absent, [] when empty). List serializers batch-load related rows with
// json_each (no N+1).
import type {
  Channel,
  ChannelMyState,
  Focus,
  Friend,
  FriendRequest,
  Me,
  Notification,
  PresenceState,
  Profile,
  Relationship,
  Session,
  Settings,
  UnreadState,
  UserStatus,
  UserSummary,
} from "../types/api";
import type { PresenceItem } from "../types/events";
import type { ChannelPrefsRow, ChannelRow, FriendRequestRow, NotificationRow, SessionRow, UserPresenceRow, UserRow, UserSettingsRow } from "../types/rows";
import { isSuspendedNow } from "./auth";
import { all, batch, jsonList, stmt } from "./db";
import { err } from "./errors";
import { idFloor } from "./ids";
import { canDm, canDmMany, canViewUserContent, presenceVisible } from "./permissions";
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

// ---- channels (plan 01 Task 19) ----

/** Badge counts stop at this value (api.md ChannelMyState); each count scans at most UNREAD_SCAN_MAX rows. */
const UNREAD_CAP = 999;
const UNREAD_SCAN_MAX = 1000;

interface UnreadBaseRow {
  id: string;
  last_message_id: string | null;
  read_id: string | null;
  joined_at: number | null;
}

/**
 * UnreadState per channel id (api.md 2, data-model.md 3.4) for `userId`; one entry for every existing channel in
 * `channelIds` (counts 0 when nothing is unread). Access is the caller's job.
 * Read pointer = channel_reads.last_read_message_id, else idFloor(joined_at) (channel_members for dm/group_dm/private
 * text, space_members for public text); counting never goes below idFloor(joined_at), so messages from before a
 * (re)join are never unread. Cost rule: only channels whose last_message_id is past the pointer are counted, each
 * count is a LIMIT 1000 range scan (idx_messages_channel_top / idx_message_mentions_user), capped at 999.
 * unread = top-level, non-system, not deleted, by someone else; mention = top-level message_mentions(in_thread=0) of me.
 */
export async function loadUnreadStates(db: D1Database, userId: string, channelIds: string[]): Promise<Map<string, UnreadState>> {
  const out = new Map<string, UnreadState>();
  const ids = [...new Set(channelIds)];
  if (ids.length === 0) return out;
  const bases = await all<UnreadBaseRow>(
    db,
    `SELECT c.id, c.last_message_id,
            (SELECT r.last_read_message_id FROM channel_reads r WHERE r.user_id = ?2 AND r.channel_id = c.id) AS read_id,
            CASE WHEN c.kind = 'text' AND c.is_private = 0
                 THEN (SELECT sm.joined_at FROM space_members sm WHERE sm.space_id = c.space_id AND sm.user_id = ?2)
                 ELSE (SELECT cm.joined_at FROM channel_members cm WHERE cm.channel_id = c.id AND cm.user_id = ?2) END AS joined_at
       FROM channels c
      WHERE c.id IN (SELECT value FROM json_each(?1))`,
    jsonList(ids),
    userId,
  );
  const toCount: Array<{ c: string; b: string }> = [];
  for (const r of bases) {
    out.set(r.id, { channel_id: r.id, unread_count: 0, mention_count: 0, last_message_id: r.last_message_id, last_read_message_id: r.read_id });
    if (r.joined_at === null) continue; // not a member: nothing counts
    const floor = idFloor(r.joined_at);
    const base = r.read_id !== null && r.read_id > floor ? r.read_id : floor;
    if (r.last_message_id !== null && r.last_message_id > base) toCount.push({ c: r.id, b: base });
  }
  if (toCount.length === 0) return out;
  const counts = await all<{ c: string; unread: number; mentions: number }>(
    db,
    `WITH t AS (SELECT json_extract(value, '$.c') AS c, json_extract(value, '$.b') AS b FROM json_each(?1))
     SELECT t.c,
            (SELECT COUNT(*) FROM (SELECT 1 FROM messages m
                                    WHERE m.channel_id = t.c AND m.thread_root_id IS NULL AND m.id > t.b
                                      AND m.kind <> 'system' AND m.author_id <> ?2 AND m.deleted_at IS NULL
                                    LIMIT ${UNREAD_SCAN_MAX})) AS unread,
            (SELECT COUNT(*) FROM (SELECT 1 FROM message_mentions mm
                                     JOIN messages m ON m.id = mm.message_id AND m.deleted_at IS NULL
                                    WHERE mm.user_id = ?2 AND mm.channel_id = t.c AND mm.in_thread = 0 AND mm.message_id > t.b
                                    LIMIT ${UNREAD_SCAN_MAX})) AS mentions
       FROM t`,
    JSON.stringify(toCount),
    userId,
  );
  for (const r of counts) {
    const st = out.get(r.c);
    if (!st) continue;
    st.unread_count = Math.min(r.unread, UNREAD_CAP);
    st.mention_count = Math.min(r.mentions, UNREAD_CAP);
  }
  return out;
}

/** data-model.md 3.4: no channel_prefs row -> dm/group_dm notify 'all', text 'mentions', muted false. */
function defaultNotify(kind: ChannelRow["kind"]): ChannelMyState["notify"] {
  return kind === "text" ? "mentions" : "all";
}

/**
 * channels rows -> Channel (api.md 2), in row order. Everything related is batch-loaded (no N+1):
 * dm/group_dm members (all, joined order), the space member_count of public text channels, and — when `viewerId` is
 * set — the viewer's prefs, unread state and the 1:1 dm read-only state (api.md 3.1). With `viewerId === null`
 * (event payloads) the per-viewer fields are the events.md 4.4 defaults.
 */
export async function serializeChannels(db: D1Database, viewerId: string | null, rows: ChannelRow[]): Promise<Channel[]> {
  if (rows.length === 0) return [];
  const convIds = rows.filter((r) => r.kind !== "text").map((r) => r.id);
  const publicSpaceIds = [...new Set(rows.flatMap((r) => (r.kind === "text" && r.is_private === 0 && r.space_id !== null ? [r.space_id] : [])))];
  const allIds = rows.map((r) => r.id);

  const [memberRows, spaceCounts, prefRows, unread] = await Promise.all([
    convIds.length === 0
      ? Promise.resolve([] as Array<{ channel_id: string; user_id: string }>)
      : all<{ channel_id: string; user_id: string }>(
          db,
          `SELECT channel_id, user_id FROM channel_members
            WHERE channel_id IN (SELECT value FROM json_each(?1))
            ORDER BY channel_id, joined_at, user_id`,
          jsonList(convIds),
        ),
    publicSpaceIds.length === 0
      ? Promise.resolve([] as Array<{ id: string; member_count: number }>)
      : all<{ id: string; member_count: number }>(db, "SELECT id, member_count FROM spaces WHERE id IN (SELECT value FROM json_each(?1))", jsonList(publicSpaceIds)),
    viewerId === null
      ? Promise.resolve([] as ChannelPrefsRow[])
      : all<ChannelPrefsRow>(db, "SELECT * FROM channel_prefs WHERE user_id = ?1 AND channel_id IN (SELECT value FROM json_each(?2))", viewerId, jsonList(allIds)),
    viewerId === null ? Promise.resolve(new Map<string, UnreadState>()) : loadUnreadStates(db, viewerId, allIds),
  ]);

  const membersOf = new Map<string, string[]>();
  for (const m of memberRows) {
    const list = membersOf.get(m.channel_id);
    if (list) list.push(m.user_id);
    else membersOf.set(m.channel_id, [m.user_id]);
  }
  const users = await loadUserSummaries(db, memberRows.map((m) => m.user_id));
  const spaceCount = new Map(spaceCounts.map((s) => [s.id, s.member_count]));
  const prefs = new Map(prefRows.map((p) => [p.channel_id, p]));

  // 1:1 dm read-only state for the viewer: one canDmMany over every partner, then my blocks among the denied ones.
  const partnerOf = new Map<string, string>();
  if (viewerId !== null) {
    for (const r of rows) {
      if (r.kind !== "dm") continue;
      const other = (membersOf.get(r.id) ?? []).find((u) => u !== viewerId);
      if (other !== undefined) partnerOf.set(r.id, other);
    }
  }
  let writable = new Set<string>();
  let iBlocked = new Set<string>();
  if (viewerId !== null && partnerOf.size > 0) {
    const { ok, denied } = await canDmMany(db, viewerId, [...partnerOf.values()]);
    writable = new Set(ok);
    if (denied.length > 0) {
      const blocked = await all<{ blocked_id: string }>(
        db,
        "SELECT blocked_id FROM blocks WHERE blocker_id = ?1 AND blocked_id IN (SELECT value FROM json_each(?2))",
        viewerId,
        jsonList(denied),
      );
      iBlocked = new Set(blocked.map((b) => b.blocked_id));
    }
  }

  return rows.map((r) => {
    const memberIds = membersOf.get(r.id) ?? [];
    let readOnly = false;
    let readOnlyReason: Channel["read_only_reason"] = null;
    if (viewerId !== null && r.kind === "dm") {
      const other = partnerOf.get(r.id);
      if (other === undefined || !writable.has(other)) {
        readOnly = true;
        readOnlyReason = other !== undefined && iBlocked.has(other) ? "blocked" : "not_eligible";
      }
    }
    const p = prefs.get(r.id);
    const st = unread.get(r.id);
    return {
      id: r.id,
      kind: r.kind,
      space_id: r.space_id,
      name: r.name,
      topic: r.topic,
      is_private: r.kind === "text" && r.is_private === 1,
      position: r.position,
      owner_id: r.owner_id,
      member_count: r.kind === "text" && r.is_private === 0 && r.space_id !== null ? (spaceCount.get(r.space_id) ?? 0) : r.member_count,
      members: r.kind === "text" ? null : memberIds.flatMap((id) => users.get(id) ?? []),
      last_message_id: r.last_message_id,
      last_message_at: isoOrNull(r.last_message_at),
      created_at: iso(r.created_at),
      read_only: readOnly,
      read_only_reason: readOnlyReason,
      my: {
        notify: p?.notify ?? defaultNotify(r.kind),
        muted: (p?.muted ?? 0) === 1,
        last_read_message_id: st?.last_read_message_id ?? null,
        unread_count: st?.unread_count ?? 0,
        mention_count: st?.mention_count ?? 0,
      },
    };
  });
}
