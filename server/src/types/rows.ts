// D1 row types — one interface per table in data-model.md chapter 3 (`migrations/0001_init.sql`), columns 1:1.
// INTEGER → number, TEXT → string (CHECK ... IN enums as literal unions), 0/1 flags → 0 | 1,
// nullable → `| null`.

/** `users` */
export interface UserRow {
  id: string;
  handle: string;
  display_name: string;
  bio: string;
  avatar_upload_id: string | null;
  status_emoji: string | null;
  status_text: string | null;
  status_expires_at: number | null;
  password_hash: string;
  role: "user" | "admin";
  is_private: 0 | 1;
  suspended_at: number | null;
  suspended_until: number | null;
  suspend_reason: string | null;
  follower_count: number;
  following_count: number;
  post_count: number;
  created_at: number;
  updated_at: number;
}

/** `user_settings` */
export interface UserSettingsRow {
  user_id: string;
  dm_policy: "friends_and_spaces" | "friends_only";
  notify_dm: 0 | 1;
  notify_mention: 0 | 1;
  notify_invite: 0 | 1;
  notify_friend_request: 0 | 1;
  notify_thread_reply: 0 | 1;
  notify_social: 0 | 1;
  focus_default_min: number;
  updated_at: number;
}

/** `sessions` */
export interface SessionRow {
  id: string;
  user_id: string;
  token_hash: string;
  client: "tui" | "daemon" | "admin" | "other";
  device_name: string;
  ip: string | null;
  user_agent: string | null;
  created_at: number;
  last_used_at: number;
  expires_at: number;
  revoked_at: number | null;
  revoke_reason: "logout" | "revoked" | "password_changed" | "suspended" | null;
}

/** `friend_requests` */
export interface FriendRequestRow {
  id: string;
  from_user_id: string;
  to_user_id: string;
  status: "pending" | "accepted" | "declined" | "canceled";
  created_at: number;
  responded_at: number | null;
}

/** `friendships` */
export interface FriendshipRow {
  user_a: string;
  user_b: string;
  created_at: number;
}

/** `blocks` */
export interface BlockRow {
  blocker_id: string;
  blocked_id: string;
  created_at: number;
}

/** `follows` */
export interface FollowRow {
  follower_id: string;
  followee_id: string;
  status: "active" | "pending";
  created_at: number;
  accepted_at: number | null;
}

/** `spaces` */
export interface SpaceRow {
  id: string;
  name: string;
  description: string;
  icon_emoji: string | null;
  owner_id: string;
  default_channel_id: string | null;
  member_count: number;
  created_at: number;
  updated_at: number;
  deleted_at: number | null;
}

/** `space_members` */
export interface SpaceMemberRow {
  space_id: string;
  user_id: string;
  role: "owner" | "admin" | "member";
  joined_at: number;
}

/** `space_bans` */
export interface SpaceBanRow {
  space_id: string;
  user_id: string;
  banned_by: string;
  reason: string;
  created_at: number;
}

/** `space_invites` */
export interface SpaceInviteRow {
  id: string;
  space_id: string;
  code: string;
  created_by: string;
  max_uses: number | null;
  use_count: number;
  expires_at: number | null;
  revoked_at: number | null;
  created_at: number;
}

/** `space_direct_invites` */
export interface SpaceDirectInviteRow {
  id: string;
  space_id: string;
  inviter_id: string;
  invitee_id: string;
  status: "pending" | "accepted" | "declined" | "canceled";
  created_at: number;
  responded_at: number | null;
}

/** `channels` */
export interface ChannelRow {
  id: string;
  kind: "text" | "dm" | "group_dm";
  space_id: string | null;
  name: string | null;
  topic: string;
  is_private: 0 | 1;
  position: number;
  owner_id: string | null;
  created_by: string;
  dm_key: string | null;
  member_count: number;
  last_message_id: string | null;
  last_message_at: number | null;
  created_at: number;
  updated_at: number;
  deleted_at: number | null;
}

/** `channel_members` */
export interface ChannelMemberRow {
  channel_id: string;
  user_id: string;
  added_by: string | null;
  joined_at: number;
}

/** `channel_prefs` */
export interface ChannelPrefsRow {
  user_id: string;
  channel_id: string;
  notify: "all" | "mentions" | "none";
  muted: 0 | 1;
  updated_at: number;
}

/** `channel_reads` */
export interface ChannelReadRow {
  user_id: string;
  channel_id: string;
  last_read_message_id: string;
  updated_at: number;
}

/** `messages` */
export interface MessageRow {
  id: string;
  channel_id: string;
  author_id: string;
  kind: "text" | "me" | "roll" | "poll" | "system";
  body: string;
  meta: string | null;
  thread_root_id: string | null;
  reply_count: number;
  last_reply_at: number | null;
  mention_here: 0 | 1;
  poll_id: string | null;
  client_nonce: string | null;
  created_at: number;
  edited_at: number | null;
  deleted_at: number | null;
  deleted_by: string | null;
  card_json: string | null; // v0.1.3 0004_cards: JSON Card; set => served as kind "card" (data-model.md 3.12)
}

/** `thread_participants` */
export interface ThreadParticipantRow {
  root_id: string;
  user_id: string;
  last_reply_at: number;
}

/** `message_mentions` */
export interface MessageMentionRow {
  message_id: string;
  user_id: string;
  channel_id: string;
  is_here: 0 | 1;
  in_thread: 0 | 1;
}

/** `message_reactions` */
export interface MessageReactionRow {
  message_id: string;
  emoji: string;
  user_id: string;
  created_at: number;
}

/** `message_pins` */
export interface MessagePinRow {
  channel_id: string;
  message_id: string;
  pinned_by: string;
  pinned_at: number;
}

/** `uploads` */
export interface UploadRow {
  id: string;
  owner_id: string;
  purpose: "avatar" | "post" | "story";
  r2_key: string;
  content_type: "image/png" | "image/jpeg" | "image/gif" | "image/webp";
  size: number;
  width: number | null;
  height: number | null;
  status: "pending" | "attached" | "removed";
  attached_to: string | null;
  created_at: number;
  removed_at: number | null;
}

/** `posts` */
export interface PostRow {
  id: string;
  author_id: string;
  body: string;
  reply_to_id: string | null;
  root_id: string | null;
  quote_of_id: string | null;
  poll_id: string | null;
  like_count: number;
  repost_count: number;
  reply_count: number;
  quote_count: number;
  client_nonce: string | null;
  created_at: number;
  deleted_at: number | null;
  deleted_by: string | null;
}

/** `post_media` */
export interface PostMediaRow {
  post_id: string;
  position: number;
  upload_id: string;
}

/** `post_likes` */
export interface PostLikeRow {
  post_id: string;
  user_id: string;
  created_at: number;
}

/** `reposts` */
export interface RepostRow {
  id: string;
  post_id: string;
  user_id: string;
  created_at: number;
}

/** `post_mentions` */
export interface PostMentionRow {
  post_id: string;
  user_id: string;
}

/** `post_hashtags` */
export interface PostHashtagRow {
  tag: string;
  post_id: string;
  author_id: string;
  created_at: number;
}

/** `trends` */
export interface TrendRow {
  rank: number;
  tag: string;
  post_count: number;
  author_count: number;
  computed_at: number;
}

/** `stories` */
export interface StoryRow {
  id: string;
  author_id: string;
  body: string;
  media_upload_id: string | null;
  bg: "accent" | "accent2" | "accent3" | "accent4" | "plain";
  created_at: number;
  expires_at: number;
  deleted_at: number | null;
}

/** `story_views` */
export interface StoryViewRow {
  story_id: string;
  viewer_id: string;
  viewed_at: number;
}

/** `polls` */
export interface PollRow {
  id: string;
  creator_id: string;
  context_type: "message" | "post";
  context_id: string;
  question: string;
  multi: 0 | 1;
  voter_count: number;
  closes_at: number | null;
  closed_at: number | null;
  created_at: number;
}

/** `poll_options` */
export interface PollOptionRow {
  poll_id: string;
  idx: number;
  text: string;
  vote_count: number;
}

/** `poll_votes` */
export interface PollVoteRow {
  poll_id: string;
  user_id: string;
  option_idx: number;
  created_at: number;
}

/** `guestbook_entries` */
export interface GuestbookRow {
  id: string;
  owner_id: string;
  author_id: string;
  body: string;
  is_secret: 0 | 1;
  created_at: number;
  deleted_at: number | null;
  deleted_by: string | null;
}

/** `notifications` */
export interface NotificationRow {
  id: string;
  user_id: string;
  type:
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
  actor_id: string | null;
  subject_type: "user" | "post" | "message" | "space" | "friend_request" | "space_direct_invite" | "guestbook_entry";
  subject_id: string;
  extra: string | null;
  dedupe_key: string | null;
  created_at: number;
  read_at: number | null;
}

/** `user_presence` */
export interface UserPresenceRow {
  user_id: string;
  status: "online" | "away" | "dnd" | "offline";
  mode: "auto" | "away" | "dnd";
  focus_started_at: number | null; // 0003_focus_started_at.sql; set together with focus_ends_at
  focus_ends_at: number | null;
  online_until: number;
  notify_online_until: number;
  last_seen_at: number | null;
  updated_at: number;
}

/** `reports` */
export interface ReportRow {
  id: string;
  reporter_id: string;
  target_type: "user" | "message" | "post" | "story" | "guestbook_entry" | "space";
  target_id: string;
  target_user_id: string | null;
  reason: "spam" | "harassment" | "illegal" | "sexual" | "violence" | "other";
  note: string;
  snapshot: string;
  status: "open" | "resolved" | "dismissed";
  resolution: "content_removed" | "user_suspended" | "no_action" | null;
  resolution_note: string | null;
  resolved_by: string | null;
  resolved_at: number | null;
  created_at: number;
}

/** `admin_actions` */
export interface AdminActionRow {
  id: string;
  admin_id: string;
  action: "remove_content" | "suspend_user" | "unsuspend_user" | "resolve_report" | "dismiss_report";
  target_type: string;
  target_id: string;
  report_id: string | null;
  reason: string;
  created_at: number;
}

/** Timeline query result (UNION of posts and reposts), not a table. */
export interface TimelineRow {
  item_id: string;
  type: "post" | "repost";
  post_id: string;
  reposter_id: string | null;
  at: number;
}
