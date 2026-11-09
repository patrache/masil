// api.* card schemas — placeholder owned by P4 (API testing). User-sent. P4 replaces the data schemas.
import * as z from "zod";
import type { CardSpec, CardType } from "./index";

export const cards: Partial<Record<CardType, CardSpec>> = {
  "api.result": { serverOnly: false, versions: { 1: z.record(z.string(), z.unknown()) } },
  "api.run": { serverOnly: false, versions: { 1: z.record(z.string(), z.unknown()) } },
};
