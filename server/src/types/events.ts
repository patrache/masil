// events.md: server -> client event names (section 8, `pong` excluded — it is a fixed auto-response string)
// and the per-type `data` shapes (section 4 tables, 3.2 hello, 3.3 ready, 2 reply).
import type {
  Alert,
  Channel,
  ChannelMyState,
  DirectInvite,
  Doc,
  DocComment,
  Focus,
  FocusSummary,
  Friend,
  FriendRequest,
  ID,
  Message,
  Notification,
  Poll,
  Post,
  PresenceState,
  PresenceStatus,
  Settings,
  Space,
  SpaceDetail,
  SpaceMember,
  ThreadSummary,
  TimelineItem,
  Timestamp,
  UnreadState,
  UserSummary,
} from "./api";

export type ServerEventType =
  | "hello"
  | "ready"
  | "reply"
  | "session.revoked"
  | "message.created"
  | "message.updated"
  | "message.deleted"
  | "thread.updated"
  | "reaction.added"
  | "reaction.removed"
  | "message.pinned"
  | "message.unpinned"
  | "poll.updated"
  | "typing"
  | "read_state.updated"
  | "channel.created"
  | "channel.updated"
  | "channel.deleted"
  | "channel.member_added"
  | "channel.member_removed"
  | "channel.prefs_updated"
  | "space.joined"
  | "space.updated"
  | "space.deleted"
  | "space.removed"
  | "space.member_joined"
  | "space.member_left"
  | "space.member_updated"
  | "space.invite_received"
  | "space.invite_removed"
  | "friend.request_received"
  | "friend.request_accepted"
  | "friend.request_removed"
  | "friend.added"
  | "friend.removed"
  | "block.updated"
  | "user.updated"
  | "settings.updated"
  | "presence.snapshot"
  | "presence.updated"
  | "presence.self"
  | "focus.started"
  | "focus.ended"
  | "notification.created"
  | "notifications.read"
  | "alert"
  | "timeline.item_created"
  | "post.deleted"
  | "post.stats"
  | "post.reply_created"
  | "doc.created" // v0.1.3 events.md 4.10
  | "doc.updated"
  | "doc.comment_created"
  | "story.created"
  | "story.deleted"
  | "device.tui_state";

export type ConnectionMode = "full" | "notify";

/** events.md 3.2 */
export interface HelloData {
  connection_id: ID;
  session_id: ID;
  user_id: ID;
  mode: ConnectionMode;
  device_id: string;
  epoch: string;
  seq: number;
  oldest_seq: number;
  heartbeat_interval_ms: number;
  heartbeat_timeout_ms: number;
  local_tui_connected: boolean;
  server_time: Timestamp;
  limits: { max_frame_bytes: number; watch_topics_max: number; typing_min_interval_ms: number };
}

/** events.md 3.3 */
export interface ReadyData {
  resumed: boolean;
  resync_required: boolean;
  replayed: number;
  seq: number;
  epoch: string;
}

export type ReplyErrorCode =
  | "invalid_frame"
  | "unknown_type"
  | "not_found"
  | "not_allowed_in_mode"
  | "rate_limited"
  | "limit_exceeded";

/** events.md 2 */
export interface ReplyData {
  ref: string | null;
  ok: boolean;
  error: { code: ReplyErrorCode; message: string } | null;
}

export interface PresenceItem {
  user_id: ID;
  status: PresenceStatus;
  last_seen_at: Timestamp | null;
}

export interface ReactionEventData {
  channel_id: ID;
  message_id: ID;
  thread_root_id: ID | null;
  emoji: string;
  user_id: ID;
  count: number;
}

export type SpaceRemovalReason = "left" | "kicked" | "banned";

export interface ServerEventDataMap {
  // 4.1 connection control
  hello: HelloData;
  ready: ReadyData;
  reply: ReplyData;
  "session.revoked": { session_id: ID; reason: "logout" | "revoked" | "password_changed" | "suspended" };
  // 4.2 messages
  "message.created": { message: Message };
  "message.updated": { message: Message };
  "message.deleted": { channel_id: ID; message_id: ID; thread_root_id: ID | null; tombstone: boolean };
  "thread.updated": { channel_id: ID; root_id: ID; thread: ThreadSummary };
  "reaction.added": ReactionEventData;
  "reaction.removed": ReactionEventData;
  "message.pinned": { channel_id: ID; message_id: ID; pinned_by: ID };
  "message.unpinned": { channel_id: ID; message_id: ID; unpinned_by: ID };
  "poll.updated": { poll: Poll }; // message polls (4.2) and post polls (4.8); my_votes always []
  // 4.3 typing / read state
  typing: { channel_id: ID; thread_root_id: ID | null; user_id: ID; expires_in_ms: number };
  "read_state.updated": { state: UnreadState };
  // 4.4 channels / DMs / spaces
  "channel.created": { channel: Channel };
  "channel.updated": { channel: Channel };
  "channel.deleted": { channel_id: ID; space_id: ID | null; reason: "deleted" | "removed" };
  "channel.member_added": { channel_id: ID; user: UserSummary };
  "channel.member_removed": { channel_id: ID; user_id: ID };
  "channel.prefs_updated": { channel_id: ID; my: ChannelMyState };
  "space.joined": { space: SpaceDetail };
  "space.updated": { space: Space };
  "space.deleted": { space_id: ID };
  "space.removed": { space_id: ID; reason: SpaceRemovalReason };
  "space.member_joined": { space_id: ID; member: SpaceMember };
  "space.member_left": { space_id: ID; user_id: ID; reason: SpaceRemovalReason };
  "space.member_updated": { space_id: ID; member: SpaceMember };
  "space.invite_received": { invite: DirectInvite };
  "space.invite_removed": { invite_id: ID; reason: "accepted" | "declined" | "canceled" };
  // 4.5 friends / blocks / users
  "friend.request_received": { request: FriendRequest };
  "friend.request_accepted": { request: FriendRequest };
  "friend.request_removed": { request_id: ID; reason: "declined" | "canceled" };
  "friend.added": { friend: Friend };
  "friend.removed": { user_id: ID };
  "block.updated": { user_id: ID; blocked: boolean };
  "user.updated": { user: UserSummary };
  "settings.updated": { settings: Settings };
  // 4.6 presence / focus
  "presence.snapshot": { items: PresenceItem[] };
  "presence.updated": PresenceItem;
  "presence.self": { presence: PresenceState };
  "focus.started": { focus: Focus };
  "focus.ended": { summary: FocusSummary };
  // 4.7 inbox / desktop alerts
  "notification.created": { notification: Notification; unread_count: number };
  "notifications.read": { ids: ID[] | null; all: boolean; before: ID | null; unread_count: number };
  alert: { alert: Alert };
  // 4.8 SNS
  "timeline.item_created": { item: TimelineItem };
  "post.deleted": { post_id: ID };
  "post.stats": { post_id: ID; like_count: number; repost_count: number; reply_count: number; quote_count: number };
  "post.reply_created": { parent_id: ID; post: Post };
  "doc.created": { doc: Doc };
  "doc.updated": { doc: Doc; deleted: boolean };
  "doc.comment_created": { doc_id: ID; comment: DocComment };
  "story.created": { user_id: ID; story_id: ID; expires_at: Timestamp };
  "story.deleted": { user_id: ID; story_id: ID };
  // 4.9 daemon coordination
  "device.tui_state": { device_id: string; tui_connected: boolean };
}

export type ServerEventData<T extends ServerEventType> = ServerEventDataMap[T];

export type ClientFrameType = "ping" | "resume" | "ack" | "typing" | "idle" | "watch";

/** Compile-time check: the union and the data map cover exactly the same names. */
type _Assert<T extends true> = T;
type _Exact<A, B> = [A] extends [B] ? ([B] extends [A] ? true : false) : false;
export type _EventMapCoversUnion = _Assert<_Exact<ServerEventType, keyof ServerEventDataMap>>;
