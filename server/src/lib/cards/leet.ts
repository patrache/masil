// leet.* card schemas — placeholder owned by P1 (LeetCode). Both types are server-only: created by the P1 endpoints,
// rejected from normal sends (v0.1.3 A10.3, C1.2). P1 replaces the data schemas.
import * as z from "zod";
import type { CardSpec, CardType } from "./index";

export const cards: Partial<Record<CardType, CardSpec>> = {
  "leet.daily": { serverOnly: true, versions: { 1: z.record(z.string(), z.unknown()) } },
  "leet.solve": { serverOnly: true, versions: { 1: z.record(z.string(), z.unknown()) } },
};
