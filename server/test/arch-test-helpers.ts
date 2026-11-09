// P3-owned test helpers for the `arch` document type (P3 Task 21, src/lib/collab/arch.ts) and the arch.* cards
// (P3 Task 26, src/lib/cards/arch.ts).
import { afterAll, beforeAll } from "vitest";
import * as z from "zod";
import { CARD_TYPES, type CardSpec, type CardType } from "../src/lib/cards/index";
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

/**
 * The P0 card tests (api/cards.test.ts) send arch.design / arch.sim as generic user cards ({ n: 14 }, {}, a 16 KB
 * filler) to test kind "card" itself. Calling this at the top of such a file swaps the arch card schemas for "any
 * object" in that file only (restored after it); the real schemas have their own test (api/arch-cards.test.ts).
 */
export function archCardsLoose(): void {
  const saved = new Map<CardType, CardSpec["versions"]>();
  beforeAll(() => {
    for (const t of ["arch.design", "arch.sim"] as const) {
      saved.set(t, CARD_TYPES[t].versions);
      CARD_TYPES[t].versions = { 1: z.record(z.string(), z.unknown()) };
    }
  });
  afterAll(() => {
    for (const [t, v] of saved) CARD_TYPES[t].versions = v;
  });
}
