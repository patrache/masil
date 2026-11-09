// Text channel creation (api.md 5.9), shared by POST /v1/spaces/{space_id}/channels and the #daily switch
// (PUT /v1/spaces/{space_id}/leet, P1 Task 2). P5 Task 20 passes its default channel name through here.
import type { Runtime } from "../env";
import { batch, first, jsonList, stmt } from "../lib/db";
import { err } from "../lib/errors";
import { newId } from "../lib/ids";
import { publishEvent } from "../lib/realtime";
import { serializeChannels } from "../lib/serialize";
import { now } from "../lib/time";
import type { Channel } from "../types/api";
import type { ChannelRow } from "../types/rows";
import { postSystemMessage } from "./messages";

/** api.md 3.5: text channels per space (the default channel included). */
export const CHANNELS_PER_SPACE_MAX = 100;

export interface CreateTextChannelInput {
  /** already a valid channel name (zChannelName: NFC + trim + CHANNEL_NAME_RE) */
  name: string;
  topic?: string;
  isPrivate?: boolean;
  /** private only: resolved user IDs to add (the actor is always added); each must be a current space member */
  memberIds?: string[];
}

function isUniqueViolation(e: unknown): boolean {
  return e instanceof Error && /UNIQUE constraint failed/i.test(e.message);
}

/**
 * Creates a text channel in `spaceId` as `actorId` (the caller checked the actor is owner/admin). Pre-checks:
 * (private) the members are current space members (404 member), the name is free among the space's live text
 * channels (409 already_exists), fewer than 100 live text channels (422 channels_per_space). One batch: channels
 * INSERT (position = last + 1, gated on the channel cap so concurrent creates cannot pass 100; uq_channels_space_name
 * catches a concurrent same-name create) -> (private) channel_members for the actor + members -> member_count. After
 * commit: channel.created (public: the space; private: the channel members), then the system message
 * channel_created{name} in the new channel. Returns the channel as the actor sees it (with that system message).
 */
export async function createTextChannel(rt: Runtime, spaceId: string, actorId: string, input: CreateTextChannelInput): Promise<Channel> {
  const db = rt.env.DB;
  const isPrivate = input.isPrivate === true;
  let memberIds: string[] = [];
  if (isPrivate) {
    memberIds = [...new Set([actorId, ...(input.memberIds ?? [])])];
    const inSpace = await first<{ n: number }>(
      db,
      "SELECT COUNT(*) AS n FROM space_members WHERE space_id = ?1 AND user_id IN (SELECT value FROM json_each(?2))",
      spaceId,
      jsonList(memberIds),
    );
    if ((inSpace?.n ?? 0) !== memberIds.length) throw err.notFound("member");
  }
  const facts = await first<{ n: number; taken: number }>(
    db,
    `SELECT COUNT(*) AS n, COALESCE(SUM(name = ?2), 0) AS taken
       FROM channels WHERE space_id = ?1 AND kind = 'text' AND deleted_at IS NULL`,
    spaceId,
    input.name,
  );
  if ((facts?.taken ?? 0) > 0) throw err.conflict("already_exists");
  if ((facts?.n ?? 0) >= CHANNELS_PER_SPACE_MAX) throw err.limit("channels_per_space", CHANNELS_PER_SPACE_MAX);

  const t = now();
  const channelId = newId(t);
  const live = "FROM channels WHERE space_id = ?2 AND kind = 'text' AND deleted_at IS NULL";
  const stmts: D1PreparedStatement[] = [
    stmt(
      db,
      `INSERT INTO channels (id, kind, space_id, name, topic, is_private, position, created_by, created_at, updated_at)
       SELECT ?1, 'text', ?2, ?3, ?4, ?5, COALESCE((SELECT MAX(position) ${live}), -1) + 1, ?6, ?7, ?7
        WHERE (SELECT COUNT(*) ${live}) < ?8`,
      channelId,
      spaceId,
      input.name,
      input.topic ?? "",
      isPrivate ? 1 : 0,
      actorId,
      t,
      CHANNELS_PER_SPACE_MAX,
    ),
  ];
  if (isPrivate) {
    stmts.push(
      stmt(
        db,
        `INSERT INTO channel_members (channel_id, user_id, added_by, joined_at)
         SELECT ?1, j.value, ?2, ?3 FROM json_each(?4) j WHERE EXISTS (SELECT 1 FROM channels WHERE id = ?1)`,
        channelId,
        actorId,
        t,
        jsonList(memberIds),
      ),
      stmt(db, "UPDATE channels SET member_count = (SELECT COUNT(*) FROM channel_members WHERE channel_id = ?1) WHERE id = ?1", channelId),
    );
  }
  let results: D1Result[];
  try {
    results = await batch(db, stmts);
  } catch (e) {
    if (isUniqueViolation(e)) throw err.conflict("already_exists");
    throw e;
  }
  if ((results[0]!.meta.changes ?? 0) === 0) throw err.limit("channels_per_space", CHANNELS_PER_SPACE_MAX);

  const loadRow = async () => {
    const row = await first<ChannelRow>(db, "SELECT * FROM channels WHERE id = ?1", channelId);
    if (row === null) throw err.notFound("channel");
    return row;
  };
  const [forEvent] = await serializeChannels(db, null, [await loadRow()]);
  await publishEvent(rt, isPrivate ? { channel: channelId } : { space: spaceId }, "channel.created", { channel: forEvent! });
  await postSystemMessage(rt, channelId, actorId, { event: "channel_created", actor_id: actorId, target_user_id: null, data: { name: input.name } });
  // re-read so the response carries the system message as last_message_*
  const [forActor] = await serializeChannels(db, actorId, [await loadRow()]);
  return forActor!;
}
