// P3-owned test helpers for the `arch` document type (P3 Task 21, src/lib/collab/arch.ts).
import { afterAll, beforeAll } from "vitest";
import { DOC_TYPES } from "../src/lib/collab-doctypes";

/**
 * The P0 CollabHub tests exercise sequencing, limits and sockets with generic objects ("n1", field "cpu", 16 KB
 * strings) that the `arch` field rules refuse. Calling this at the top of such a file turns the arch validateOp off
 * for that file only (restored after it); the arch rules have their own tests (unit/arch-doctype.test.ts,
 * realtime/collab-arch.test.ts). Kept out of the shared helpers.ts so P1·P2·P4's appends to it do not meet it.
 */
export function archRulesOff(): void {
  const real = DOC_TYPES.arch.validateOp;
  beforeAll(() => {
    DOC_TYPES.arch.validateOp = () => null;
  });
  afterAll(() => {
    DOC_TYPES.arch.validateOp = real;
  });
}
