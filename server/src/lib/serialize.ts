// Row -> api.md object serializers (server-architecture.md 4.9). Every response JSON is built here so that
// fields always exist (null when absent, [] when empty). List serializers batch-load related rows with
// json_each (no N+1).
import type {
  AdminAction,
  AdminUserView,
  Channel,
  ChannelMyState,
  ChannelRef,
  DirectInvite,
  Focus,
  Friend,
  FriendRequest,
  GuestbookEntry,
  Invite,
  InvitePreview,
  Me,
  Media,
  MentionRef,
  Message,
  Notification,
  Poll,
  Post,
  PresenceState,
  Profile,
  ReactionSummary,
  Relationship,
  Report,
  RollResult,
  Session,
  Settings,
  Space,
  SpaceDetail,
  SpaceMember,
  SpacePreview,
  SpaceRole,
  Story,
  SystemEvent,
  Card,
  TimelineItem,
  UnreadState,
  Upload,
  UserStatus,
  UserSummary,
} from "../types/api";
import type { Env } from "../env";
import type { PresenceItem } from "../types/events";
import type {
  AdminActionRow,
  ChannelPrefsRow,
  ChannelRow,
  FriendRequestRow,
  GuestbookRow,
  MessageRow,
  NotificationRow,
  PollOptionRow,
  PollRow,
  PostRow,
  ReportRow,
  SessionRow,
  SpaceBanRow,
  SpaceDirectInviteRow,
  SpaceInviteRow,
  SpaceMemberRow,
  SpaceRow,
  StoryRow,
  TimelineRow,
  UploadRow,
  UserPresenceRow,
  UserRow,
  UserSettingsRow,
} from "../types/rows";
import { isSuspendedNow } from "./auth";
import { all, batch, jsonList, stmt } from "./db";
import { err } from "./errors";
import { idFloor } from "./ids";
import { canDm, canDmMany, canViewUserContent, presenceVisible } from "./permissions";
import { iso, isoOrNull, now } from "./time";
import { errFields } from "./log";

function toUserStatus(row: UserRow, nowMs: number): UserStatus | null {
  if (row.status_text === null && row.status_emoji === null) return null;
  if (row.status_expires_at !== null && row.status_expires_at <= nowMs) return null;
  return { emoji: row.status_emoji, text: row.status_text ?? "", expires_at: isoOrNull(row.status_expires_at) };
}

/**
 * `reveal`: the summary is for the account's owner or for an operator view (/v1/admin/*), so a suspension masks nothing.
 * Everywhere else a suspended account shows only handle, display name and suspended=true (owner decision D1, api.md 3.9).
 */
export interface SummaryOpts {
  reveal?: boolean;
}

/** Expired status -> null; avatar_url "/v1/media/<id>"; suspended = suspended right now — and then (unless `reveal`)
 *  status and avatar_url are null (D1, api.md 3.9). */
export function toUserSummary(row: UserRow, nowMs: number = now(), opts: SummaryOpts = {}): UserSummary {
  const suspended = isSuspendedNow(row, nowMs);
  const masked = suspended && opts.reveal !== true;
  return {
    id: row.id,
    handle: row.handle,
    display_name: row.display_name,
    avatar_url: masked || row.avatar_upload_id === null ? null : `/v1/media/${row.avatar_upload_id}`,
    status: masked ? null : toUserStatus(row, nowMs),
    is_private: row.is_private === 1,
    suspended,
  };
}

/** One query for any number of ids; missing ids are simply absent from the map. Suspended accounts are masked as in
 *  toUserSummary unless `opts.reveal`. */
export async function loadUserSummaries(db: D1Database, ids: readonly string[], opts: SummaryOpts = {}): Promise<Map<string, UserSummary>> {
  const out = new Map<string, UserSummary>();
  const unique = [...new Set(ids)];
  if (unique.length === 0) return out;
  const rows = await all<UserRow>(db, "SELECT * FROM users WHERE id IN (SELECT value FROM json_each(?1))", jsonList(unique));
  const t = now();
  for (const row of rows) out.set(row.id, toUserSummary(row, t, opts));
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
      workspace: (s?.notify_workspace ?? 1) === 1,
    },
    focus_default_min: s?.focus_default_min ?? 25,
    leet_share_with_friends: (s?.leet_share_with_friends ?? 1) === 1,
  };
}

function toPresenceState(p: UserPresenceRow | null, nowMs: number): PresenceState {
  if (p === null) return { status: "offline", mode: "auto", focus: null };
  let focus: Focus | null = null;
  if (p.focus_ends_at !== null && p.focus_ends_at > nowMs) {
    // UserHub writes focus_started_at and focus_ends_at together (0003_focus_started_at.sql), so the start is exact.
    // A NULL start beside a live end can only be a row written before that migration; updated_at (the row's last
    // write) is then the only stand-in left. Routes returning Me still prefer serializeMeLive (the hub's state, whose
    // status can be ahead of this mirror); this read serves signup and the hub-unreachable fallback.
    const startedAt = p.focus_started_at ?? Math.min(p.updated_at, p.focus_ends_at);
    focus = { started_at: iso(startedAt), ends_at: iso(p.focus_ends_at) };
  }
  return { status: p.status, mode: p.mode, focus };
}

/** The PresenceItem anyone but the owner gets for a suspended account (api.md 3.9): no status, no last_seen_at. */
export function suspendedPresenceItem(userId: string): PresenceItem {
  return { user_id: userId, status: "offline", last_seen_at: null };
}

/**
 * PresenceItem (events.md 4.6, api.md 5.1 GET /presence) for existing users, in the order of `ids` (duplicates once).
 * A user without a user_presence row reads as offline / never seen. A user suspended right now reads as
 * suspendedPresenceItem (api.md 3.9: its last_seen_at would be, in effect, the suspension time); callers are never the
 * suspended user (no session). Visibility (api.md 3.6) is the caller's job.
 */
export async function loadPresenceItems(db: D1Database, ids: readonly string[]): Promise<PresenceItem[]> {
  const unique = [...new Set(ids)];
  if (unique.length === 0) return [];
  const rows = await all<{
    id: string;
    status: PresenceItem["status"] | null;
    last_seen_at: number | null;
    suspended_at: number | null;
    suspended_until: number | null;
  }>(
    db,
    `SELECT u.id, p.status, p.last_seen_at, u.suspended_at, u.suspended_until
       FROM users u LEFT JOIN user_presence p ON p.user_id = u.id
      WHERE u.id IN (SELECT value FROM json_each(?1))`,
    jsonList(unique),
  );
  const t = now();
  const byId = new Map(rows.map((r) => [r.id, r]));
  const out: PresenceItem[] = [];
  for (const id of unique) {
    const r = byId.get(id);
    if (!r) continue;
    out.push(
      isSuspendedNow(r, t)
        ? suspendedPresenceItem(r.id)
        : { user_id: r.id, status: r.status ?? "offline", last_seen_at: isoOrNull(r.last_seen_at) },
    );
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
    ...toUserSummary(user, t, { reveal: true }), // my own account
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

/**
 * serializeMe with `presence` from the authoritative UserHub state (the one GET /me/focus and focus.started report):
 * the D1 mirror's status can lag the hub. Falls back to the D1 mirror (focus start included) when the hub RPC fails.
 */
export async function serializeMeLive(env: Env, userId: string): Promise<Me> {
  const [me, live] = await Promise.all([
    serializeMe(env.DB, userId),
    env.USER_HUB.getByName(userId)
      .getPresenceState(userId)
      .then(
        (p): PresenceState | null => p,
        (e: unknown) => {
          console.error({ msg: "serializeMeLive: UserHub presence read failed, using the D1 mirror", err: errFields(e) });
          return null;
        },
      ),
  ]);
  return live === null ? me : { ...me, presence: live };
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
 * A suspended account seen by anyone but its owner (owner decision D1, api.md 3.9 / 5.2): only handle, display name
 * and suspended=true — bio "", status/avatar_url/presence/last_seen_at null, counts 0 (relationship stays: it is the
 * viewer's own state, e.g. whether they blocked the account).
 */
export async function serializeProfile(db: D1Database, viewerId: string, row: UserRow): Promise<Profile> {
  const self = row.id === viewerId;
  const t = now();
  const masked = !self && isSuspendedNow(row, t);
  const [visible, presence, relationship] = await Promise.all([
    self ? Promise.resolve(new Set([row.id])) : masked ? Promise.resolve(new Set<string>()) : presenceVisible(db, viewerId, [row.id]),
    masked ? Promise.resolve([]) : loadPresenceItems(db, [row.id]),
    self ? Promise.resolve(null) : loadRelationship(db, viewerId, row.id),
  ]);
  const p = visible.has(row.id) ? (presence[0] ?? null) : null;
  return {
    ...toUserSummary(row, t, { reveal: self }),
    bio: masked ? "" : row.bio,
    created_at: iso(row.created_at),
    follower_count: masked ? 0 : row.follower_count,
    following_count: masked ? 0 : row.following_count,
    post_count: masked ? 0 : row.post_count,
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

/**
 * channel IDs -> ChannelRef (api.md 2) in one query (json_each). name: text = channel name, group_dm = its optional
 * name, dm = null; space_id/space_name only for text. Missing IDs are absent from the map. Access is the caller's job.
 */
export async function loadChannelRefs(db: D1Database, channelIds: string[]): Promise<Map<string, ChannelRef>> {
  const out = new Map<string, ChannelRef>();
  const ids = [...new Set(channelIds)];
  if (ids.length === 0) return out;
  const rows = await all<{ id: string; kind: ChannelRef["kind"]; name: string | null; space_id: string | null; space_name: string | null }>(
    db,
    `SELECT c.id, c.kind, c.name, c.space_id, s.name AS space_name
       FROM channels c LEFT JOIN spaces s ON s.id = c.space_id
      WHERE c.id IN (SELECT value FROM json_each(?1))`,
    jsonList(ids),
  );
  for (const r of rows) {
    out.set(r.id, { id: r.id, kind: r.kind, name: r.kind === "dm" ? null : r.name, space_id: r.space_id, space_name: r.space_name });
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

// ---- messages and polls (plan 01 Task 20) ----

const THREAD_PARTICIPANTS_MAX = 5;

/** Poll is closed when closed manually or its closes_at has passed (api.md Poll.closed). */
function pollClosed(row: PollRow, nowMs: number): boolean {
  return row.closed_at !== null || (row.closes_at !== null && row.closes_at <= nowMs);
}

/**
 * Poll per id (api.md 2), loaded in three queries at most (polls, options, the viewer's votes). Missing ids are absent.
 * my_votes = the viewer's option idx list, ascending; [] when viewerId is null (event payloads). Access is the caller's job.
 */
export async function serializePolls(db: D1Database, viewerId: string | null, pollIds: string[]): Promise<Map<string, Poll>> {
  const out = new Map<string, Poll>();
  const ids = [...new Set(pollIds)];
  if (ids.length === 0) return out;
  const list = jsonList(ids);
  const [polls, options, votes] = await Promise.all([
    all<PollRow>(db, "SELECT * FROM polls WHERE id IN (SELECT value FROM json_each(?1))", list),
    all<PollOptionRow>(db, "SELECT * FROM poll_options WHERE poll_id IN (SELECT value FROM json_each(?1)) ORDER BY poll_id, idx", list),
    viewerId === null
      ? Promise.resolve([] as Array<{ poll_id: string; option_idx: number }>)
      : all<{ poll_id: string; option_idx: number }>(
          db,
          "SELECT poll_id, option_idx FROM poll_votes WHERE user_id = ?1 AND poll_id IN (SELECT value FROM json_each(?2)) ORDER BY poll_id, option_idx",
          viewerId,
          list,
        ),
  ]);
  const t = now();
  for (const p of polls) {
    out.set(p.id, {
      id: p.id,
      context_type: p.context_type,
      context_id: p.context_id,
      creator_id: p.creator_id,
      question: p.question,
      multi: p.multi === 1,
      options: [],
      voter_count: p.voter_count,
      closes_at: isoOrNull(p.closes_at),
      closed: pollClosed(p, t),
      my_votes: [],
    });
  }
  for (const o of options) out.get(o.poll_id)?.options.push({ idx: o.idx, text: o.text, vote_count: o.vote_count });
  for (const v of votes) out.get(v.poll_id)?.my_votes.push(v.option_idx);
  return out;
}

function parseMeta<T>(meta: string | null): T | null {
  if (meta === null) return null;
  try {
    const v: unknown = JSON.parse(meta);
    return v !== null && typeof v === "object" ? (v as T) : null;
  } catch {
    return null;
  }
}

/**
 * messages rows -> Message (api.md 2), in row order. Batch-loads (one query each, json_each): authors, reactions
 * (per emoji, ordered by first reaction time then the first row's rowid; `me` for the viewer), pins, polls, explicit
 * mentions (is_here = 0, insertion order), thread participants (roots with reply_count >= 1; most recent first, <= 5).
 * roll / system come from `meta`. A deleted row is a tombstone: body "", deleted true, no poll/roll/system.
 * A kind "system" row always has thread null and reactions [], even over reply / reaction rows written before v0.1.1.
 * With viewerId === null (event payloads) reactions[].me is false and poll.my_votes is [].
 * A row whose author no longer exists is dropped.
 */
export async function serializeMessages(db: D1Database, viewerId: string | null, rows: MessageRow[]): Promise<Message[]> {
  if (rows.length === 0) return [];
  const ids = jsonList(rows.map((r) => r.id));
  const roots = rows.filter((r) => r.kind !== "system" && r.thread_root_id === null && r.reply_count > 0).map((r) => r.id);
  const pollIds = rows.flatMap((r) => (r.poll_id !== null && r.deleted_at === null ? [r.poll_id] : []));

  const [users, reactionRows, pinRows, mentionRows, participantRows, polls] = await Promise.all([
    loadUserSummaries(
      db,
      rows.map((r) => r.author_id),
    ),
    all<{ message_id: string; emoji: string; n: number; me: number | null }>(
      db,
      `SELECT message_id, emoji, COUNT(*) AS n, MAX(user_id = ?2) AS me, MIN(created_at) AS first_at, MIN(rowid) AS first_rowid
         FROM message_reactions
        WHERE message_id IN (SELECT value FROM json_each(?1))
        GROUP BY message_id, emoji
        ORDER BY message_id, first_at, first_rowid`,
      ids,
      viewerId,
    ),
    all<{ message_id: string }>(db, "SELECT message_id FROM message_pins WHERE message_id IN (SELECT value FROM json_each(?1))", ids),
    all<{ message_id: string; user_id: string; handle: string }>(
      db,
      `SELECT mm.message_id, mm.user_id, u.handle
         FROM message_mentions mm JOIN users u ON u.id = mm.user_id
        WHERE mm.message_id IN (SELECT value FROM json_each(?1)) AND mm.is_here = 0
        ORDER BY mm.message_id, mm.rowid`,
      ids,
    ),
    roots.length === 0
      ? Promise.resolve([] as Array<{ root_id: string; user_id: string }>)
      : all<{ root_id: string; user_id: string }>(
          db,
          `SELECT root_id, user_id FROM (
             SELECT root_id, user_id,
                    ROW_NUMBER() OVER (PARTITION BY root_id ORDER BY last_reply_at DESC, user_id) AS rn
               FROM thread_participants
              WHERE root_id IN (SELECT value FROM json_each(?1)))
            WHERE rn <= ${THREAD_PARTICIPANTS_MAX}
            ORDER BY root_id, rn`,
          jsonList(roots),
        ),
    serializePolls(db, viewerId, pollIds),
  ]);

  const reactions = new Map<string, ReactionSummary[]>();
  for (const r of reactionRows) {
    const list = reactions.get(r.message_id) ?? [];
    list.push({ emoji: r.emoji, count: r.n, me: r.me === 1 });
    reactions.set(r.message_id, list);
  }
  const pinned = new Set(pinRows.map((p) => p.message_id));
  const mentions = new Map<string, MentionRef[]>();
  for (const m of mentionRows) {
    const list = mentions.get(m.message_id) ?? [];
    list.push({ user_id: m.user_id, handle: m.handle });
    mentions.set(m.message_id, list);
  }
  const participants = new Map<string, string[]>();
  for (const p of participantRows) {
    const list = participants.get(p.root_id) ?? [];
    list.push(p.user_id);
    participants.set(p.root_id, list);
  }

  return rows.flatMap((r): Message[] => {
    const author = users.get(r.author_id);
    if (!author) return [];
    const deleted = r.deleted_at !== null;
    // kind "system" is never a thread root and takes no reactions (api.md 2, v0.1.1 D7): rows v0.1.0 accepted stay hidden.
    const system = r.kind === "system";
    return [
      {
        id: r.id,
        channel_id: r.channel_id,
        author,
        kind: r.card_json !== null && r.card_json !== undefined ? "card" : r.kind,
        body: deleted ? "" : r.body,
        mentions: deleted ? [] : (mentions.get(r.id) ?? []),
        mention_here: !deleted && r.mention_here === 1,
        thread_root_id: r.thread_root_id,
        thread:
          !system && r.thread_root_id === null && r.reply_count > 0
            ? { reply_count: r.reply_count, last_reply_at: isoOrNull(r.last_reply_at), participant_ids: participants.get(r.id) ?? [] }
            : null,
        reactions: deleted || system ? [] : (reactions.get(r.id) ?? []),
        pinned: !deleted && pinned.has(r.id),
        poll: deleted || r.poll_id === null ? null : (polls.get(r.poll_id) ?? null),
        roll: !deleted && r.kind === "roll" ? parseMeta<RollResult>(r.meta) : null,
        system: !deleted && r.kind === "system" ? parseMeta<SystemEvent>(r.meta) : null,
        card: !deleted && r.card_json !== null && r.card_json !== undefined ? parseMeta<Card>(r.card_json) : null,
        client_nonce: r.client_nonce,
        created_at: iso(r.created_at),
        edited_at: isoOrNull(r.edited_at),
        deleted,
      },
    ];
  });
}

// ---- spaces (plan 01 Task 21) ----

function toSpace(row: SpaceRow, myRole: SpaceRole): Space {
  return {
    id: row.id,
    name: row.name,
    description: row.description,
    icon_emoji: row.icon_emoji,
    member_count: row.member_count,
    owner_id: row.owner_id,
    default_channel_id: row.default_channel_id ?? "",
    created_at: iso(row.created_at),
    my_role: myRole,
  };
}

/**
 * spaces rows -> Space (api.md 2), in row order; the viewer's roles are batch-loaded in one query. With
 * viewerId === null (event payloads) my_role is the events.md 4.4 default "member"; so is a viewer who is not a member.
 */
export async function serializeSpaces(db: D1Database, viewerId: string | null, rows: SpaceRow[]): Promise<Space[]> {
  if (rows.length === 0) return [];
  const roles =
    viewerId === null
      ? new Map<string, SpaceRole>()
      : new Map(
          (
            await all<{ space_id: string; role: SpaceRole }>(
              db,
              "SELECT space_id, role FROM space_members WHERE user_id = ?1 AND space_id IN (SELECT value FROM json_each(?2))",
              viewerId,
              jsonList(rows.map((r) => r.id)),
            )
          ).map((r) => [r.space_id, r.role]),
        );
  return rows.map((r) => toSpace(r, roles.get(r.id) ?? "member"));
}

export async function serializeSpace(db: D1Database, viewerId: string | null, row: SpaceRow): Promise<Space> {
  const [space] = await serializeSpaces(db, viewerId, [row]);
  return space!;
}

/** Text channels of `spaceId` that `viewerId` can see (public, or private with a channel_members row), by position. */
export async function serializeSpaceChannels(db: D1Database, viewerId: string, spaceId: string): Promise<Channel[]> {
  const rows = await all<ChannelRow>(
    db,
    `SELECT c.* FROM channels c
      WHERE c.space_id = ?1 AND c.kind = 'text' AND c.deleted_at IS NULL
        AND (c.is_private = 0 OR EXISTS (SELECT 1 FROM channel_members cm WHERE cm.channel_id = c.id AND cm.user_id = ?2))
      ORDER BY c.position, c.created_at, c.id`,
    spaceId,
    viewerId,
  );
  return serializeChannels(db, viewerId, rows);
}

/** Space + the text channels visible to the viewer, ordered by position (api.md SpaceDetail). Membership is the caller's job. */
export async function serializeSpaceDetail(db: D1Database, viewerId: string, row: SpaceRow): Promise<SpaceDetail> {
  const [space, channels] = await Promise.all([serializeSpace(db, viewerId, row), serializeSpaceChannels(db, viewerId, row.id)]);
  return { ...space, channels };
}

/** space_members rows -> SpaceMember (api.md 2), in row order; users batch-loaded (one query). Gone users are dropped. */
export async function serializeSpaceMembers(db: D1Database, rows: SpaceMemberRow[]): Promise<SpaceMember[]> {
  const users = await loadUserSummaries(
    db,
    rows.map((r) => r.user_id),
  );
  return rows.flatMap((r) => {
    const user = users.get(r.user_id);
    return user ? [{ user, role: r.role, joined_at: iso(r.joined_at) }] : [];
  });
}

export interface SpaceBan {
  user: UserSummary;
  banned_by: UserSummary;
  reason: string;
  created_at: string;
}

/** space_bans rows -> api.md 5.7 GET /spaces/{id}/bans items, in row order; both users batch-loaded (one query). */
export async function serializeSpaceBans(db: D1Database, rows: SpaceBanRow[]): Promise<SpaceBan[]> {
  const users = await loadUserSummaries(
    db,
    rows.flatMap((r) => [r.user_id, r.banned_by]),
  );
  return rows.flatMap((r) => {
    const user = users.get(r.user_id);
    const by = users.get(r.banned_by);
    return user && by ? [{ user, banned_by: by, reason: r.reason, created_at: iso(r.created_at) }] : [];
  });
}

// ---- invites (plan 01 Task 22 additions) ----

function toSpacePreview(row: SpaceRow): SpacePreview {
  return { id: row.id, name: row.name, description: row.description, icon_emoji: row.icon_emoji, member_count: row.member_count };
}

/** Live (not deleted) spaces by id, one json_each query. */
async function loadSpacePreviews(db: D1Database, ids: readonly string[]): Promise<Map<string, SpacePreview>> {
  if (ids.length === 0) return new Map();
  const rows = await all<SpaceRow>(
    db,
    "SELECT * FROM spaces WHERE id IN (SELECT value FROM json_each(?1)) AND deleted_at IS NULL",
    jsonList([...new Set(ids)]),
  );
  return new Map(rows.map((r) => [r.id, toSpacePreview(r)]));
}

/** space_invites rows -> Invite (api.md 2), in row order; spaces and creators batch-loaded (two queries). A row whose
 *  space is deleted or whose creator is gone is dropped. */
export async function serializeInvites(db: D1Database, rows: SpaceInviteRow[]): Promise<Invite[]> {
  const [spaces, users] = await Promise.all([
    loadSpacePreviews(
      db,
      rows.map((r) => r.space_id),
    ),
    loadUserSummaries(
      db,
      rows.map((r) => r.created_by),
    ),
  ]);
  return rows.flatMap((r) => {
    const space = spaces.get(r.space_id);
    const by = users.get(r.created_by);
    if (!space || !by) return [];
    return [
      {
        code: r.code,
        space,
        created_by: by,
        max_uses: r.max_uses,
        use_count: r.use_count,
        expires_at: isoOrNull(r.expires_at),
        revoked: r.revoked_at !== null,
        created_at: iso(r.created_at),
      },
    ];
  });
}

/** What `viewerId` sees for an invite code (api.md InvitePreview). Validity and the space being live are the caller's job. */
export async function serializeInvitePreview(db: D1Database, viewerId: string, row: SpaceInviteRow): Promise<InvitePreview> {
  const [spaces, users, member] = await Promise.all([
    loadSpacePreviews(db, [row.space_id]),
    loadUserSummaries(db, [row.created_by]),
    all<{ x: number }>(db, "SELECT 1 AS x FROM space_members WHERE space_id = ?1 AND user_id = ?2", row.space_id, viewerId),
  ]);
  const space = spaces.get(row.space_id);
  const by = users.get(row.created_by);
  if (!space || !by) throw err.notFound("invite");
  return { code: row.code, space, created_by: by, expires_at: isoOrNull(row.expires_at), already_member: member.length > 0 };
}

/** space_direct_invites rows -> DirectInvite (api.md 2), in row order; spaces and both users batch-loaded (two
 *  queries). A row whose space is deleted or whose inviter/invitee is gone is dropped. */
export async function serializeDirectInvites(db: D1Database, rows: SpaceDirectInviteRow[]): Promise<DirectInvite[]> {
  const [spaces, users] = await Promise.all([
    loadSpacePreviews(
      db,
      rows.map((r) => r.space_id),
    ),
    loadUserSummaries(
      db,
      rows.flatMap((r) => [r.inviter_id, r.invitee_id]),
    ),
  ]);
  return rows.flatMap((r) => {
    const space = spaces.get(r.space_id);
    const inviter = users.get(r.inviter_id);
    const invitee = users.get(r.invitee_id);
    if (!space || !inviter || !invitee) return [];
    return [{ id: r.id, space, inviter, invitee, status: r.status, created_at: iso(r.created_at) }];
  });
}

// ---- media and posts (plan 01 Task 29; plan 02 pins these rules with its own tests) ----

/** uploads row -> Media (api.md 2). Pure; never exposes r2_key / owner_id / attached_to. */
export function serializeMedia(row: UploadRow): Media {
  return {
    id: row.id,
    url: `/v1/media/${row.id}`,
    content_type: row.content_type,
    size: row.size,
    width: row.width,
    height: row.height,
  };
}

/** uploads row -> Upload (api.md 2, server-architecture.md 4.9): Media + purpose, status, created_at. */
export function serializeUpload(row: UploadRow): Upload {
  return { ...serializeMedia(row), purpose: row.purpose, status: row.status, created_at: iso(row.created_at) };
}

/** Post.author of a placeholder row (Post.author is non-null): the real author is never exposed. */
const HIDDEN_AUTHOR: UserSummary = {
  id: "00000000000000000000000000",
  handle: "",
  display_name: "",
  avatar_url: null,
  status: null,
  is_private: false,
  suspended: false,
};

function pushTo<K, V>(map: Map<K, V[]>, key: K, value: V): void {
  const list = map.get(key);
  if (list) list.push(value);
  else map.set(key, [value]);
}

/**
 * posts rows -> Post (api.md 2), same length and order as `rows`. Every related table is read once per call with
 * json_each (no N+1), in three rounds: (1) quoted rows (one level only), (2) reply parents of rows + quoted rows,
 * media (removed uploads dropped, position order), polls, hashtags and mentions (insertion order), viewer likes/reposts,
 * (3) user summaries (authors + parent authors) and the viewer's blocks/active follows towards quoted/parent authors.
 *
 * - A row with deleted_at set is a placeholder: HIDDEN_AUTHOR, empty content, zero like/repost/quote counts, viewer
 *   flags false; only id, reply_to_id, root_id, reply_count, created_at survive (also used by callers that mark
 *   "not visible" rows by copying them with deleted_at set).
 * - reply_to_author: the parent's author only when the viewer may see the parent (api.md 3.4 — parent not deleted,
 *   author not suspended, no block either way, author public / the viewer / actively followed). viewerId null (event
 *   payloads, one object for every recipient): the parent's author is named only when not suspended and either public
 *   or listed in `opts.privateOk` (a private author the publish VisibilitySpec already restricts recipients to). The
 *   publisher must then add every named parent author (eventParentAuthorIds) to VisibilitySpec.authors so recipients
 *   blocked either way with them are filtered out (events.md 4.8: an event object matches that recipient's REST view).
 * - quote: missing or deleted original -> {available:false, reason:"deleted"}; not visible by the same rule ->
 *   "not_visible"; otherwise the original serialized by these rules with quote = null.
 * - viewerId null -> viewer {liked:false, reposted:false} and poll.my_votes [] (events.md 4.4).
 */
export async function serializePosts(
  db: D1Database,
  viewerId: string | null,
  rows: PostRow[],
  opts: { privateOk?: readonly string[] } = {},
): Promise<Post[]> {
  if (rows.length === 0) return [];
  const privateOk = new Set(opts.privateOk ?? []);
  const live = rows.filter((r) => r.deleted_at === null);
  const quoteIds = [...new Set(live.flatMap((r) => (r.quote_of_id !== null ? [r.quote_of_id] : [])))];

  // (1) quoted originals, one level.
  const quotedRows =
    quoteIds.length === 0 ? [] : await all<PostRow>(db, "SELECT * FROM posts WHERE id IN (SELECT value FROM json_each(?1))", jsonList(quoteIds));
  const quoted = new Map(quotedRows.map((q) => [q.id, q]));

  // Rows whose content is serialized: live input rows + live quoted originals.
  const content = new Map<string, PostRow>();
  for (const r of [...live, ...quotedRows]) if (r.deleted_at === null) content.set(r.id, r);
  const contentRows = [...content.values()];
  const contentIds = jsonList(contentRows.map((r) => r.id));
  const parentIds = [...new Set(contentRows.flatMap((r) => (r.reply_to_id !== null ? [r.reply_to_id] : [])))];
  const pollIds = contentRows.flatMap((r) => (r.poll_id !== null ? [r.poll_id] : []));

  // (2) parents and content-related rows.
  const [parentRows, mediaRows, hashtagRows, mentionRows, likeRows, repostRows, polls] = await Promise.all([
    parentIds.length === 0
      ? Promise.resolve([] as Array<{ id: string; author_id: string; deleted_at: number | null }>)
      : all<{ id: string; author_id: string; deleted_at: number | null }>(
          db,
          "SELECT id, author_id, deleted_at FROM posts WHERE id IN (SELECT value FROM json_each(?1))",
          jsonList(parentIds),
        ),
    all<UploadRow & { post_id: string }>(
      db,
      `SELECT pm.post_id, u.*
         FROM post_media pm JOIN uploads u ON u.id = pm.upload_id
        WHERE pm.post_id IN (SELECT value FROM json_each(?1)) AND u.status <> 'removed'
        ORDER BY pm.post_id, pm.position`,
      contentIds,
    ),
    all<{ post_id: string; tag: string }>(
      db,
      "SELECT post_id, tag FROM post_hashtags WHERE post_id IN (SELECT value FROM json_each(?1)) ORDER BY rowid",
      contentIds,
    ),
    all<{ post_id: string; user_id: string; handle: string }>(
      db,
      `SELECT pm.post_id, u.id AS user_id, u.handle
         FROM post_mentions pm JOIN users u ON u.id = pm.user_id
        WHERE pm.post_id IN (SELECT value FROM json_each(?1))
        ORDER BY pm.rowid`,
      contentIds,
    ),
    viewerId === null
      ? Promise.resolve([] as Array<{ post_id: string }>)
      : all<{ post_id: string }>(db, "SELECT post_id FROM post_likes WHERE user_id = ?1 AND post_id IN (SELECT value FROM json_each(?2))", viewerId, contentIds),
    viewerId === null
      ? Promise.resolve([] as Array<{ post_id: string }>)
      : all<{ post_id: string }>(db, "SELECT post_id FROM reposts WHERE user_id = ?1 AND post_id IN (SELECT value FROM json_each(?2))", viewerId, contentIds),
    serializePolls(db, viewerId, pollIds),
  ]);
  const parents = new Map(parentRows.map((p) => [p.id, p]));

  // (3) summaries + the viewer's relation to authors whose visibility we judge here (quoted / parent authors).
  const judged = [...new Set([...quotedRows.map((q) => q.author_id), ...parentRows.map((p) => p.author_id)])].filter((id) => id !== viewerId);
  const [users, blockRows, followRows] = await Promise.all([
    loadUserSummaries(db, [...contentRows.map((r) => r.author_id), ...parentRows.map((p) => p.author_id), ...quotedRows.map((q) => q.author_id)]),
    viewerId === null || judged.length === 0
      ? Promise.resolve([] as Array<{ other: string }>)
      : all<{ other: string }>(
          db,
          `SELECT blocked_id AS other FROM blocks WHERE blocker_id = ?1 AND blocked_id IN (SELECT value FROM json_each(?2))
           UNION
           SELECT blocker_id AS other FROM blocks WHERE blocked_id = ?1 AND blocker_id IN (SELECT value FROM json_each(?2))`,
          viewerId,
          jsonList(judged),
        ),
    viewerId === null || judged.length === 0
      ? Promise.resolve([] as Array<{ followee_id: string }>)
      : all<{ followee_id: string }>(
          db,
          "SELECT followee_id FROM follows WHERE follower_id = ?1 AND status = 'active' AND followee_id IN (SELECT value FROM json_each(?2))",
          viewerId,
          jsonList(judged),
        ),
  ]);
  const blocked = new Set(blockRows.map((b) => b.other));
  const following = new Set(followRows.map((f) => f.followee_id));

  /** api.md 3.4 rules 2–4 for a live post by `authorId` (rule 1 is checked by the caller). */
  const canSee = (authorId: string): boolean => {
    const author = users.get(authorId);
    if (!author || author.suspended) return false;
    if (viewerId === null || authorId === viewerId) return true;
    if (blocked.has(authorId)) return false;
    return !author.is_private || following.has(authorId);
  };
  /** reply_to_author rule: canSee, and for event payloads (no viewer) only public or publisher-cleared private authors. */
  const canName = (authorId: string): boolean => {
    if (!canSee(authorId)) return false;
    if (viewerId !== null) return true;
    return !users.get(authorId)!.is_private || privateOk.has(authorId);
  };

  const media = new Map<string, Media[]>();
  for (const m of mediaRows) pushTo(media, m.post_id, serializeMedia(m));
  const hashtags = new Map<string, string[]>();
  for (const h of hashtagRows) pushTo(hashtags, h.post_id, h.tag);
  const mentions = new Map<string, MentionRef[]>();
  for (const m of mentionRows) pushTo(mentions, m.post_id, { user_id: m.user_id, handle: m.handle });
  const liked = new Set(likeRows.map((l) => l.post_id));
  const reposted = new Set(repostRows.map((r) => r.post_id));

  const toPost = (r: PostRow, withQuote: boolean): Post => {
    if (r.deleted_at !== null) {
      return {
        id: r.id,
        author: HIDDEN_AUTHOR,
        body: "",
        media: [],
        poll: null,
        reply_to_id: r.reply_to_id,
        root_id: r.root_id,
        reply_to_author: null,
        quote: null,
        hashtags: [],
        mentions: [],
        like_count: 0,
        repost_count: 0,
        reply_count: r.reply_count,
        quote_count: 0,
        viewer: { liked: false, reposted: false },
        created_at: iso(r.created_at),
        deleted: true,
      };
    }
    const parent = r.reply_to_id !== null ? parents.get(r.reply_to_id) : undefined;
    const replyToAuthor = parent && parent.deleted_at === null && canName(parent.author_id) ? (users.get(parent.author_id) ?? null) : null;
    let quote: Post["quote"] = null;
    if (withQuote && r.quote_of_id !== null) {
      const q = quoted.get(r.quote_of_id);
      if (!q || q.deleted_at !== null) quote = { available: false, id: r.quote_of_id, reason: "deleted" };
      else if (!canSee(q.author_id)) quote = { available: false, id: r.quote_of_id, reason: "not_visible" };
      else quote = { available: true, post: toPost(q, false) };
    }
    return {
      id: r.id,
      author: users.get(r.author_id) ?? HIDDEN_AUTHOR,
      body: r.body,
      media: media.get(r.id) ?? [],
      poll: r.poll_id === null ? null : (polls.get(r.poll_id) ?? null),
      reply_to_id: r.reply_to_id,
      root_id: r.root_id,
      reply_to_author: replyToAuthor,
      quote,
      hashtags: hashtags.get(r.id) ?? [],
      mentions: mentions.get(r.id) ?? [],
      like_count: r.like_count,
      repost_count: r.repost_count,
      reply_count: r.reply_count,
      quote_count: r.quote_count,
      viewer: { liked: liked.has(r.id), reposted: reposted.has(r.id) },
      created_at: iso(r.created_at),
      deleted: false,
    };
  };
  return rows.map((r) => toPost(r, true));
}

/**
 * Ids of the reply-parent authors an event Post (serializePosts with viewerId null) names: its own reply_to_author and
 * its quoted post's. Publishers add them to VisibilitySpec.authors (see serializePosts).
 */
export function eventParentAuthorIds(post: Post): string[] {
  const ids: string[] = [];
  if (post.reply_to_author !== null) ids.push(post.reply_to_author.id);
  if (post.quote?.available === true && post.quote.post.reply_to_author !== null) ids.push(post.quote.post.reply_to_author.id);
  return ids;
}

/**
 * Timeline rows (UNION of posts and reposts) -> TimelineItem[] (contract 4.9), input order kept. One posts query for
 * the distinct post ids, one serializePosts call (a post shown in several items is serialized once) and one
 * loadUserSummaries call for the reposters. Items whose post row is gone are dropped. Visibility was already applied
 * by the caller's SQL; this function does not filter.
 */
export async function serializeTimelineItems(db: D1Database, viewerId: string, rows: TimelineRow[]): Promise<TimelineItem[]> {
  if (rows.length === 0) return [];
  const postIds = [...new Set(rows.map((r) => r.post_id))];
  const postRows = await all<PostRow>(db, "SELECT * FROM posts WHERE id IN (SELECT value FROM json_each(?1))", jsonList(postIds));
  const reposterIds = rows.flatMap((r) => (r.type === "repost" && r.reposter_id !== null ? [r.reposter_id] : []));
  const [posts, reposters] = await Promise.all([serializePosts(db, viewerId, postRows), loadUserSummaries(db, reposterIds)]);
  const byId = new Map(posts.map((p) => [p.id, p]));
  return rows.flatMap((r): TimelineItem[] => {
    const post = byId.get(r.post_id);
    if (post === undefined) return [];
    const reposted_by = r.type === "repost" && r.reposter_id !== null ? (reposters.get(r.reposter_id) ?? null) : null;
    if (r.type === "repost" && reposted_by === null) return [];
    return [{ id: r.item_id, type: r.type, post, reposted_by, at: iso(r.at) }];
  });
}

/**
 * stories rows -> Story (contract 4.9), input order kept; rows whose author is gone are dropped. One query per related
 * table whatever the row count: author summaries, media (removed uploads -> media null), the viewer's story_views on
 * other people's stories (seen) and view counts of the viewer's own stories. The author's own story is always seen and
 * only the author gets a numeric view_count. Visibility (expiry, deletion, blocks, private, suspension) is the caller's
 * SQL; this function does not filter.
 */
export async function serializeStories(db: D1Database, viewerId: string, rows: StoryRow[]): Promise<Story[]> {
  if (rows.length === 0) return [];
  const mediaIds = rows.flatMap((r) => (r.media_upload_id !== null ? [r.media_upload_id] : []));
  const ownIds = rows.filter((r) => r.author_id === viewerId).map((r) => r.id);
  const otherIds = rows.filter((r) => r.author_id !== viewerId).map((r) => r.id);
  const [authors, media, seenRows, countRows] = await Promise.all([
    loadUserSummaries(
      db,
      rows.map((r) => r.author_id),
    ),
    mediaIds.length === 0
      ? Promise.resolve([] as UploadRow[])
      : all<UploadRow>(
          db,
          "SELECT * FROM uploads WHERE id IN (SELECT value FROM json_each(?1)) AND status <> 'removed'",
          jsonList([...new Set(mediaIds)]),
        ),
    otherIds.length === 0
      ? Promise.resolve([] as Array<{ story_id: string }>)
      : all<{ story_id: string }>(
          db,
          "SELECT story_id FROM story_views WHERE viewer_id = ?1 AND story_id IN (SELECT value FROM json_each(?2))",
          viewerId,
          jsonList(otherIds),
        ),
    ownIds.length === 0
      ? Promise.resolve([] as Array<{ story_id: string; n: number }>)
      : all<{ story_id: string; n: number }>(
          db,
          "SELECT story_id, COUNT(*) AS n FROM story_views WHERE story_id IN (SELECT value FROM json_each(?1)) GROUP BY story_id",
          jsonList(ownIds),
        ),
  ]);
  const mediaById = new Map(media.map((u) => [u.id, serializeMedia(u)]));
  const seen = new Set(seenRows.map((v) => v.story_id));
  const counts = new Map(countRows.map((v) => [v.story_id, v.n]));
  return rows.flatMap((r): Story[] => {
    const author = authors.get(r.author_id);
    if (author === undefined) return [];
    const own = r.author_id === viewerId;
    return [
      {
        id: r.id,
        author,
        body: r.body,
        media: r.media_upload_id !== null ? (mediaById.get(r.media_upload_id) ?? null) : null,
        bg: r.bg,
        created_at: iso(r.created_at),
        expires_at: iso(r.expires_at),
        seen: own || seen.has(r.id),
        view_count: own ? (counts.get(r.id) ?? 0) : null,
      },
    ];
  });
}

/**
 * GuestbookEntry list (api.md 4.9 / 5.20) in input order. Author summaries in one query (authors of entries the viewer
 * cannot read are left out). A secret entry viewed by anyone but the owner or its author is `hidden`: author null and
 * body "" (is_secret stays true so the client can show a placeholder); a deleted entry has body "". Rows whose author
 * no longer exists are dropped. List filtering (deleted, suspended authors, blocks) is the caller's SQL.
 */
export async function serializeGuestbook(db: D1Database, viewerId: string, rows: GuestbookRow[]): Promise<GuestbookEntry[]> {
  if (rows.length === 0) return [];
  const isHidden = (r: GuestbookRow): boolean => r.is_secret === 1 && viewerId !== r.owner_id && viewerId !== r.author_id;
  const authors = await loadUserSummaries(
    db,
    rows.filter((r) => !isHidden(r)).map((r) => r.author_id),
  );
  return rows.flatMap((r): GuestbookEntry[] => {
    const hidden = isHidden(r);
    const author = hidden ? null : authors.get(r.author_id);
    if (author === undefined) return [];
    const deleted = r.deleted_at !== null;
    return [
      {
        id: r.id,
        owner_id: r.owner_id,
        author,
        body: hidden || deleted ? "" : r.body,
        is_secret: r.is_secret === 1,
        hidden,
        created_at: iso(r.created_at),
        deleted,
      },
    ];
  });
}

/**
 * AdminAction list (api.md 4.9 / 5.22, plan 02 Task 15 addition) in input order. Admin summaries in one query; a row
 * whose admin no longer exists is dropped.
 */
export async function serializeAdminActions(db: D1Database, rows: AdminActionRow[]): Promise<AdminAction[]> {
  if (rows.length === 0) return [];
  const admins = await loadUserSummaries(
    db,
    rows.map((r) => r.admin_id),
    { reveal: true }, // operator view
  );
  return rows.flatMap((r): AdminAction[] => {
    const admin = admins.get(r.admin_id);
    if (admin === undefined) return [];
    return [
      {
        id: r.id,
        admin,
        action: r.action,
        target_type: r.target_type,
        target_id: r.target_id,
        report_id: r.report_id,
        reason: r.reason,
        created_at: iso(r.created_at),
      },
    ];
  });
}

/** Report target kind -> SQL telling whether the target still exists (?1 = json id list, ?2 = now). */
const REPORT_TARGET_EXISTS_SQL: Record<ReportRow["target_type"], string> = {
  user: "SELECT id FROM users WHERE id IN (SELECT value FROM json_each(?1))",
  message: "SELECT id FROM messages WHERE id IN (SELECT value FROM json_each(?1)) AND deleted_at IS NULL",
  post: "SELECT id FROM posts WHERE id IN (SELECT value FROM json_each(?1)) AND deleted_at IS NULL",
  // the cleanup cron hard-deletes expired stories, so a missing row is "gone" as well
  story: "SELECT id FROM stories WHERE id IN (SELECT value FROM json_each(?1)) AND deleted_at IS NULL AND expires_at > ?2",
  guestbook_entry: "SELECT id FROM guestbook_entries WHERE id IN (SELECT value FROM json_each(?1)) AND deleted_at IS NULL",
  space: "SELECT id FROM spaces WHERE id IN (SELECT value FROM json_each(?1)) AND deleted_at IS NULL",
};

/**
 * Report list (api.md 4.9 / 5.22) in input order. One query for every person involved (reporter, target user,
 * resolver), one existence query per target kind present (at most 6, sent as one batch) and one open-report count
 * query grouped by (target_type, target_id). A row whose reporter no longer exists is dropped.
 */
export async function serializeReports(db: D1Database, rows: ReportRow[]): Promise<Report[]> {
  if (rows.length === 0) return [];
  const t = now();
  const personIds = rows.flatMap((r) => [r.reporter_id, r.target_user_id, r.resolved_by].filter((id): id is string => id !== null));
  const idsByType = new Map<ReportRow["target_type"], Set<string>>();
  for (const r of rows) {
    const set = idsByType.get(r.target_type) ?? new Set<string>();
    set.add(r.target_id);
    idsByType.set(r.target_type, set);
  }
  const types = [...idsByType.keys()];
  const [people, existsResults, counts] = await Promise.all([
    loadUserSummaries(db, personIds, { reveal: true }), // operator view: a reported, suspended account stays recognisable
    batch(
      db,
      types.map((type) => {
        const ids = jsonList([...idsByType.get(type)!]);
        // only the story query has ?2: extra bindings are an error
        return type === "story" ? stmt(db, REPORT_TARGET_EXISTS_SQL[type], ids, t) : stmt(db, REPORT_TARGET_EXISTS_SQL[type], ids);
      }),
    ),
    all<{ target_type: string; target_id: string; n: number }>(
      db,
      `SELECT target_type, target_id, COUNT(*) AS n FROM reports
        WHERE status = 'open' AND target_id IN (SELECT value FROM json_each(?1))
        GROUP BY target_type, target_id`,
      jsonList([...new Set(rows.map((r) => r.target_id))]),
    ),
  ]);
  const key = (type: string, id: string) => `${type}:${id}`;
  const existing = new Set<string>();
  types.forEach((type, i) => {
    for (const row of (existsResults[i]?.results ?? []) as Array<{ id: string }>) existing.add(key(type, row.id));
  });
  const openCounts = new Map(counts.map((c) => [key(c.target_type, c.target_id), c.n]));

  return rows.flatMap((r): Report[] => {
    const reporter = people.get(r.reporter_id);
    if (reporter === undefined) return [];
    let snapshot: Record<string, unknown> = {};
    try {
      const parsed: unknown = JSON.parse(r.snapshot);
      if (typeof parsed === "object" && parsed !== null && !Array.isArray(parsed)) snapshot = parsed as Record<string, unknown>;
    } catch {
      // a malformed snapshot reads as empty rather than failing the whole list
    }
    return [
      {
        id: r.id,
        reporter,
        target_type: r.target_type,
        target_id: r.target_id,
        target_user: r.target_user_id === null ? null : (people.get(r.target_user_id) ?? null),
        reason: r.reason,
        note: r.note,
        snapshot,
        target_exists: existing.has(key(r.target_type, r.target_id)),
        open_reports_on_target: openCounts.get(key(r.target_type, r.target_id)) ?? 0,
        status: r.status,
        resolution: r.resolution,
        resolution_note: r.resolution_note,
        resolved_by: r.resolved_by === null ? null : (people.get(r.resolved_by) ?? null),
        resolved_at: isoOrNull(r.resolved_at),
        created_at: iso(r.created_at),
      },
    ];
  });
}

/**
 * AdminUserView list (api.md 4.9 / 5.22, plan 02 Task 16 addition) in input order. `open_report_count` = open reports
 * whose target_user_id is the user, read for all rows in one query.
 */
export async function serializeAdminUsers(db: D1Database, rows: UserRow[]): Promise<AdminUserView[]> {
  if (rows.length === 0) return [];
  const counts = await all<{ target_user_id: string; n: number }>(
    db,
    `SELECT target_user_id, COUNT(*) AS n FROM reports
      WHERE status = 'open' AND target_user_id IN (SELECT value FROM json_each(?1))
      GROUP BY target_user_id`,
    jsonList([...new Set(rows.map((r) => r.id))]),
  );
  const byUser = new Map(counts.map((c) => [c.target_user_id, c.n]));
  const t = now();
  return rows.map((r) => ({
    ...toUserSummary(r, t, { reveal: true }), // operator view
    role: r.role,
    created_at: iso(r.created_at),
    suspended_at: isoOrNull(r.suspended_at),
    suspended_until: isoOrNull(r.suspended_until),
    suspend_reason: r.suspend_reason,
    open_report_count: byUser.get(r.id) ?? 0,
  }));
}
