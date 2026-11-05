// api.md section 2 object schemas, field-for-field. These are the return types of lib/serialize.ts.
// Response fields always exist (null when absent, [] for empty lists).

export type ID = string; // ULID
export type Timestamp = string; // RFC 3339 UTC, ms precision

export type PresenceStatus = "online" | "away" | "dnd" | "offline";
export type SpaceRole = "owner" | "admin" | "member";

export interface UserStatus {
  emoji: string | null;
  text: string;
  expires_at: Timestamp | null;
}

export interface UserSummary {
  id: ID;
  handle: string;
  display_name: string;
  avatar_url: string | null; // "/v1/media/<upload_id>"
  status: UserStatus | null; // null if unset or expired
  is_private: boolean;
  suspended: boolean;
}

export interface Relationship {
  is_friend: boolean;
  friend_request: { id: ID; direction: "incoming" | "outgoing" } | null; // pending only
  following: "none" | "pending" | "active"; // me -> them
  followed_by: "none" | "pending" | "active"; // them -> me
  blocked: boolean; // I blocked them (never reveals whether they blocked me)
  can_dm: boolean;
  can_view_posts: boolean;
  shared_space_ids: ID[]; // up to 10
}

export interface Profile extends UserSummary {
  bio: string;
  created_at: Timestamp;
  follower_count: number;
  following_count: number;
  post_count: number;
  presence: PresenceStatus | null; // null if not visible to viewer
  last_seen_at: Timestamp | null;
  relationship: Relationship | null; // null when viewing self
}

export interface Settings {
  dm_policy: "friends_and_spaces" | "friends_only";
  is_private: boolean;
  notify: {
    dm: boolean;
    mention: boolean;
    invite: boolean;
    friend_request: boolean;
    thread_reply: boolean;
    social: boolean;
  };
  focus_default_min: number; // 1..720, default 25
}

export interface Focus {
  started_at: Timestamp;
  ends_at: Timestamp;
}

export interface PresenceState {
  status: PresenceStatus; // effective
  mode: "auto" | "away" | "dnd"; // user's manual choice
  focus: Focus | null;
}

export interface Me extends UserSummary {
  bio: string;
  created_at: Timestamp;
  role: "user" | "admin";
  follower_count: number;
  following_count: number;
  post_count: number;
  settings: Settings;
  presence: PresenceState;
}

export interface Session {
  id: ID;
  client: "tui" | "daemon" | "admin" | "other";
  device_name: string;
  created_at: Timestamp;
  last_used_at: Timestamp;
  expires_at: Timestamp;
  current: boolean;
}

export interface Media {
  id: ID; // upload id
  url: string; // "/v1/media/<id>"
  content_type: "image/png" | "image/jpeg" | "image/gif" | "image/webp";
  size: number;
  width: number | null;
  height: number | null;
}

export interface Upload extends Media {
  purpose: "avatar" | "post" | "story";
  status: "pending" | "attached" | "removed";
  created_at: Timestamp;
}

export interface FriendRequest {
  id: ID;
  from: UserSummary;
  to: UserSummary;
  status: "pending" | "accepted" | "declined" | "canceled";
  created_at: Timestamp;
}

export interface Friend {
  user: UserSummary;
  since: Timestamp;
  presence: PresenceStatus;
}

export interface SpacePreview {
  id: ID;
  name: string;
  description: string;
  icon_emoji: string | null;
  member_count: number;
}

export interface Space extends SpacePreview {
  owner_id: ID;
  default_channel_id: ID;
  created_at: Timestamp;
  my_role: SpaceRole;
}

export interface SpaceDetail extends Space {
  channels: Channel[]; // visible to me, ordered by position
}

export interface SpaceMember {
  user: UserSummary;
  role: SpaceRole;
  joined_at: Timestamp;
}

export interface Invite {
  code: string; // 8 chars
  space: SpacePreview;
  created_by: UserSummary;
  max_uses: number | null;
  use_count: number;
  expires_at: Timestamp | null;
  revoked: boolean;
  created_at: Timestamp;
}

export interface InvitePreview {
  code: string;
  space: SpacePreview;
  created_by: UserSummary;
  expires_at: Timestamp | null;
  already_member: boolean;
}

export interface DirectInvite {
  id: ID;
  space: SpacePreview;
  inviter: UserSummary;
  invitee: UserSummary;
  status: "pending" | "accepted" | "declined" | "canceled";
  created_at: Timestamp;
}

export interface ChannelMyState {
  notify: "all" | "mentions" | "none";
  muted: boolean;
  last_read_message_id: ID | null;
  unread_count: number; // capped at 999
  mention_count: number; // top-level only; capped at 999
}

export interface Channel {
  id: ID;
  kind: "text" | "dm" | "group_dm";
  space_id: ID | null;
  name: string | null;
  topic: string;
  is_private: boolean;
  position: number;
  owner_id: ID | null;
  member_count: number;
  members: UserSummary[] | null; // dm/group_dm: all members (<=10); text: null
  last_message_id: ID | null;
  last_message_at: Timestamp | null;
  created_at: Timestamp;
  read_only: boolean;
  read_only_reason: "not_eligible" | "blocked" | null;
  my: ChannelMyState;
}

export interface ChannelRef {
  id: ID;
  kind: "text" | "dm" | "group_dm";
  name: string | null;
  space_id: ID | null;
  space_name: string | null;
}

export interface UnreadState {
  channel_id: ID;
  unread_count: number;
  mention_count: number;
  last_message_id: ID | null;
  last_read_message_id: ID | null;
}

export interface MentionRef {
  user_id: ID;
  handle: string;
}

export interface ReactionSummary {
  emoji: string;
  count: number;
  me: boolean;
}

export interface ThreadSummary {
  reply_count: number;
  last_reply_at: Timestamp | null;
  participant_ids: ID[]; // most recent first, <= 5
}

export interface RollResult {
  expr: string;
  rolls: number[];
  modifier: number;
  total: number;
}

export interface SystemEvent {
  event:
    | "member_joined"
    | "member_left"
    | "member_kicked"
    | "channel_created"
    | "channel_renamed"
    | "channel_topic_changed"
    | "message_pinned"
    | "group_renamed"
    | "group_member_added"
    | "group_member_removed";
  actor_id: ID;
  target_user_id: ID | null;
  data: { name?: string; topic?: string; message_id?: ID };
}

export interface PollOption {
  idx: number;
  text: string;
  vote_count: number;
}

export interface Poll {
  id: ID;
  context_type: "message" | "post";
  context_id: ID;
  creator_id: ID;
  question: string;
  multi: boolean;
  options: PollOption[];
  voter_count: number;
  closes_at: Timestamp | null;
  closed: boolean;
  my_votes: number[];
}

export interface PollInput {
  question: string;
  options: string[]; // 2..10
  multi?: boolean; // default false
  duration_sec?: number | null; // 60..604800; null/omitted = until closed manually
}

export interface Message {
  id: ID;
  channel_id: ID;
  author: UserSummary;
  kind: "text" | "me" | "roll" | "poll" | "system";
  body: string;
  mentions: MentionRef[];
  mention_here: boolean;
  thread_root_id: ID | null;
  thread: ThreadSummary | null;
  reactions: ReactionSummary[];
  pinned: boolean;
  poll: Poll | null;
  roll: RollResult | null;
  system: SystemEvent | null;
  client_nonce: string | null;
  created_at: Timestamp;
  edited_at: Timestamp | null;
  deleted: boolean;
}

export type QuotedPost =
  | { available: true; post: Post } // post.quote is always null here (1 level)
  | { available: false; id: ID; reason: "deleted" | "not_visible" };

export interface Post {
  id: ID;
  author: UserSummary;
  body: string;
  media: Media[];
  poll: Poll | null;
  reply_to_id: ID | null;
  root_id: ID | null;
  reply_to_author: UserSummary | null;
  quote: QuotedPost | null;
  hashtags: string[];
  mentions: MentionRef[];
  like_count: number;
  repost_count: number;
  reply_count: number;
  quote_count: number;
  viewer: { liked: boolean; reposted: boolean };
  created_at: Timestamp;
  deleted: boolean;
}

export interface TimelineItem {
  id: ID;
  type: "post" | "repost";
  post: Post;
  reposted_by: UserSummary | null;
  at: Timestamp;
}

export interface Story {
  id: ID;
  author: UserSummary;
  body: string;
  media: Media | null;
  bg: "accent" | "accent2" | "accent3" | "accent4" | "plain";
  created_at: Timestamp;
  expires_at: Timestamp;
  seen: boolean;
  view_count: number | null; // author only, else null
}

export interface StoryGroup {
  user: UserSummary;
  stories: Story[];
  has_unseen: boolean;
  latest_at: Timestamp;
}

export type NotificationType =
  | "follow"
  | "follow_request"
  | "follow_accepted"
  | "friend_request"
  | "friend_accepted"
  | "space_invite"
  | "post_like"
  | "post_repost"
  | "post_reply"
  | "post_quote"
  | "post_mention"
  | "message_mention"
  | "thread_reply"
  | "guestbook_entry";

export interface Notification {
  id: ID;
  type: NotificationType;
  actor: UserSummary | null;
  subject: {
    type: "user" | "post" | "message" | "space" | "friend_request" | "space_direct_invite" | "guestbook_entry";
    id: ID;
  };
  preview: string; // <= 100 code points
  context: {
    post_id: ID | null;
    channel_id: ID | null;
    message_id: ID | null;
    thread_root_id: ID | null;
    space_id: ID | null;
    space_name: string | null;
  };
  created_at: Timestamp;
  read: boolean;
}

export type AlertKind =
  | "dm"
  | "mention"
  | "thread_reply"
  | "invite"
  | "friend_request"
  | "channel_message"
  | "social"
  | "focus_summary";

export interface Alert {
  id: ID;
  kind: AlertKind;
  title: string; // <= 80 code points
  body: string; // <= 200 code points, plain text
  actor: UserSummary | null;
  target: {
    type: "channel" | "message" | "post" | "space" | "user" | "notification";
    id: ID;
    channel_id: ID | null;
    message_id: ID | null;
    thread_root_id: ID | null;
    post_id: ID | null;
    space_id: ID | null;
  };
  created_at: Timestamp;
  suppressed: boolean;
  suppressed_reason: "focus" | "dnd" | null;
}

export interface FocusSummary {
  started_at: Timestamp;
  ended_at: Timestamp;
  counts: { dm: number; mention: number; thread_reply: number; invite: number; friend_request: number; other: number };
  items: Alert[]; // newest first, <= 20
}

export interface GuestbookEntry {
  id: ID;
  owner_id: ID;
  author: UserSummary | null; // null when hidden
  body: string; // "" when hidden or deleted
  is_secret: boolean;
  hidden: boolean;
  created_at: Timestamp;
  deleted: boolean;
}

export interface SearchHighlight {
  start: number; // in code points of body
  length: number;
}

export interface AdminUserView extends UserSummary {
  role: "user" | "admin";
  created_at: Timestamp;
  suspended_at: Timestamp | null;
  suspended_until: Timestamp | null;
  suspend_reason: string | null;
  open_report_count: number;
}

export interface Report {
  id: ID;
  reporter: UserSummary;
  target_type: "user" | "message" | "post" | "story" | "guestbook_entry" | "space";
  target_id: ID;
  target_user: UserSummary | null;
  reason: "spam" | "harassment" | "illegal" | "sexual" | "violence" | "other";
  note: string;
  snapshot: Record<string, unknown>;
  target_exists: boolean;
  open_reports_on_target: number;
  status: "open" | "resolved" | "dismissed";
  resolution: "content_removed" | "user_suspended" | "no_action" | null;
  resolution_note: string | null;
  resolved_by: UserSummary | null;
  resolved_at: Timestamp | null;
  created_at: Timestamp;
}

export interface AdminAction {
  id: ID;
  admin: UserSummary;
  action: "remove_content" | "suspend_user" | "unsuspend_user" | "resolve_report" | "dismiss_report";
  target_type: string;
  target_id: ID;
  report_id: ID | null;
  reason: string;
  created_at: Timestamp;
}
