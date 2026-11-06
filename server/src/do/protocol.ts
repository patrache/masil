// Realtime wire protocol (events.md 1.2, 2, 3, 5, 7): close codes, limits, client frame schema,
// and the server event types a notify (daemon) connection may receive.
import * as z from "zod";
import { zId } from "../lib/validate";
import type { ServerEventType } from "../types/events";

/** events.md 1.2 */
export const CloseCode = {
  normal: 1000,
  goingAway: 1001,
  tooBig: 1009,
  internal: 1011,
  protocol: 4000,
  unauthorized: 4001,
  heartbeat: 4002,
  suspended: 4003,
  rateLimited: 4008,
  replaced: 4009,
  upgradeRequired: 4010,
} as const;

/** events.md 7: the only server -> client types a notify connection gets (live and on replay). `pong` is the auto-response. */
export const NOTIFY_ALLOWED_TYPES: ReadonlySet<ServerEventType> = new Set<ServerEventType>([
  "hello",
  "ready",
  "reply",
  "session.revoked",
  "alert",
  "notification.created",
  "notifications.read",
  "focus.started",
  "focus.ended",
  "device.tui_state",
]);

/** events.md 3.4 */
export const HEARTBEAT = { intervalMs: 25_000, timeoutMs: 120_000 } as const;

/** events.md 2 / 3.2 `hello.limits` / 5 */
export const LIMITS = { maxFrameBytes: 8192, watchTopicsMax: 50, typingMinIntervalMs: 3000 } as const;

const ref = z.string().min(1).max(32).optional();

const ResumeData = z.union([
  z.object({ epoch: z.string().nullable(), last_seq: z.int().min(0).nullable() }),
  z.object({ use_device_ack: z.literal(true) }),
]);

/** `post:<ULID>`; the 50-topic cap is checked by the handler (reply{limit_exceeded}), not here. */
const Topic = z.string().regex(/^post:[0-9A-Z]{26}$/);

/** events.md 5 (`ping` never reaches the schema — it is the fixed auto-response request string). */
export const ClientFrameSchema = z.discriminatedUnion("type", [
  z.object({ type: z.literal("resume"), ref, data: ResumeData }),
  z.object({ type: z.literal("ack"), ref, data: z.object({ seq: z.int().min(0) }) }),
  z.object({ type: z.literal("typing"), ref, data: z.object({ channel_id: zId, thread_root_id: zId.nullable() }) }),
  z.object({ type: z.literal("idle"), ref, data: z.object({ idle: z.boolean() }) }),
  z.object({ type: z.literal("watch"), ref, data: z.object({ topics: z.array(Topic) }) }),
]);

export type ClientFrame = z.infer<typeof ClientFrameSchema>;
