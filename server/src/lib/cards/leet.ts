// leet.* card schemas (P1 — v0.1.3 A10.3, C1.2). Both types are server-only: created by the P1 endpoints
// (routes/leet.ts), rejected from normal sends. body = the problem link only (no summary, no spoiler).
import { LeetDailyCardData, LeetSolveCardData } from "../leet";
import type { CardSpec, CardType } from "./index";

export const cards: Partial<Record<CardType, CardSpec>> = {
  "leet.daily": { serverOnly: true, versions: { 1: LeetDailyCardData } },
  "leet.solve": { serverOnly: true, versions: { 1: LeetSolveCardData } },
};
