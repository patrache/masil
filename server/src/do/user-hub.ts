import { DurableObject } from "cloudflare:workers";
import type { ServerEventType } from "../types/events";

/** Per-socket state kept with `ws.serializeAttachment` (server-architecture.md 3.2; JSON, <= 16,384 bytes). */
export interface ConnAttachment {
  connId: string;
  sessionId: string;
  userId: string;
  mode: "full" | "notify";
  deviceId: string;
  connectedAt: number;
  ready: boolean; // resume handled
  idle: boolean;
  topics: string[]; // watched topics (<= 50)
  typingAt: Record<string, number>; // channelId(+":"+threadRootId) -> last forwarded ms (<= 20 keys)
  win: { start: number; count: number }; // frame rate window
}

/** One event handed to a UserHub (server-architecture.md 3.2). */
export interface OutboundEvent {
  type: ServerEventType; // events.md 8
  data: unknown;
  ts: number; // ms
  logged: boolean; // true => seq + log
  topic?: string; // set for topic events; delivered only to sockets watching it
  onlyDevice?: string; // device.tui_state
}

export interface DeliveryBatch {
  userId: string;
  events: OutboundEvent[];
}

export class UserHub extends DurableObject<Cloudflare.Env> {}
