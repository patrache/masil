import { describe, it, expect } from "vitest";
import { ClientFrameSchema, CloseCode, NOTIFY_ALLOWED_TYPES } from "../../src/do/protocol";
import { newId } from "../../src/lib/ids";

const ok = (f: unknown) => ClientFrameSchema.safeParse(f).success;

describe("protocol", () => {
  it("accepts every client frame shape (events.md 5)", () => {
    expect(ok({ type: "resume", data: { epoch: null, last_seq: null } })).toBe(true);
    expect(ok({ type: "resume", data: { epoch: newId(), last_seq: 12 } })).toBe(true);
    expect(ok({ type: "resume", data: { use_device_ack: true } })).toBe(true);
    expect(ok({ type: "ack", data: { seq: 0 } })).toBe(true);
    expect(ok({ type: "typing", ref: "c-17", data: { channel_id: newId(), thread_root_id: null } })).toBe(true);
    expect(ok({ type: "typing", data: { channel_id: newId(), thread_root_id: newId() } })).toBe(true);
    expect(ok({ type: "idle", data: { idle: true } })).toBe(true);
    expect(ok({ type: "watch", ref: "w-3", data: { topics: [`post:${newId()}`] } })).toBe(true);
    expect(ok({ type: "watch", data: { topics: [] } })).toBe(true);
  });

  it("rejects malformed frames", () => {
    expect(ok({ type: "dance", data: {} })).toBe(false);
    expect(ok({ type: "resume", data: { use_device_ack: false } })).toBe(false);
    expect(ok({ type: "resume", data: { epoch: null } })).toBe(false);
    expect(ok({ type: "ack", data: { seq: -1 } })).toBe(false);
    expect(ok({ type: "ack", data: { seq: 1.5 } })).toBe(false);
    expect(ok({ type: "typing", data: { channel_id: "nope", thread_root_id: null } })).toBe(false);
    expect(ok({ type: "typing", data: { channel_id: newId() } })).toBe(false);
    expect(ok({ type: "idle", data: { idle: "yes" } })).toBe(false);
    expect(ok({ type: "watch", data: { topics: ["post:abc"] } })).toBe(false);
    expect(ok({ type: "watch", data: { topics: [`user:${newId()}`] } })).toBe(false);
    expect(ok({ type: "ack", ref: "", data: { seq: 1 } })).toBe(false);
    expect(ok({ type: "ack", ref: "x".repeat(33), data: { seq: 1 } })).toBe(false);
    expect(ok({ type: "ack" })).toBe(false);
  });

  it("constants match events.md 1.2 / 7", () => {
    expect(CloseCode).toMatchObject({ protocol: 4000, tooBig: 1009, replaced: 4009, upgradeRequired: 4010 });
    expect([...NOTIFY_ALLOWED_TYPES].sort()).toEqual(
      ["alert", "device.tui_state", "focus.ended", "focus.started", "hello", "notification.created", "notifications.read", "ready", "reply", "session.revoked"],
    );
  });
});
