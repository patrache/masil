// arch.* card schemas — placeholder owned by P3 (architecture design). User-sent. P3 replaces the data schemas.
import * as z from "zod";
import type { CardSpec, CardType } from "./index";

export const cards: Partial<Record<CardType, CardSpec>> = {
  "arch.design": { serverOnly: false, versions: { 1: z.record(z.string(), z.unknown()) } },
  "arch.sim": { serverOnly: false, versions: { 1: z.record(z.string(), z.unknown()) } },
};
