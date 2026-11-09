// Chat card registry (v0.1.3 C1.2, api.md 2 "카드"). One message kind "card"; the type inside tells the tools apart.
// Each tool owns one file (leet.ts P1, arch.ts P3, api.ts P4) exporting `cards`; this file merges them (a type listed
// twice throws at load) and is the only place that validates a card. The server checks shape and size only.
import * as z from "zod";
import { err, type FieldError } from "../errors";
import { cards as apiCards } from "./api";
import { cards as archCards } from "./arch";
import { cards as leetCards } from "./leet";

export type CardType = "leet.daily" | "leet.solve" | "arch.design" | "arch.sim" | "api.result" | "api.run";
export const CARD_TYPE_NAMES: readonly CardType[] = ["leet.daily", "leet.solve", "arch.design", "arch.sim", "api.result", "api.run"];

export interface CardSpec {
  /** true: only server endpoints create it (normal sends -> fields.card.type "not_allowed") */
  serverOnly: boolean;
  /** schema version -> schema of `data` */
  versions: Record<number, z.ZodType>;
}

/** JSON.stringify(data) in UTF-8 bytes (api.md 2 Card). */
export const CARD_DATA_MAX = 16_384;

export interface CardInput {
  type: CardType;
  v: number;
  data: unknown;
}

/** What is stored in messages.card_json and returned as Message.card. */
export interface Card {
  type: CardType;
  v: number;
  data: Record<string, unknown>;
}

/** Merges the per-tool card files; a type in two files, or a type nobody defines, throws. */
export function mergeCards(parts: Array<Partial<Record<CardType, CardSpec>>>): Record<CardType, CardSpec> {
  const out: Partial<Record<CardType, CardSpec>> = {};
  for (const part of parts) {
    for (const [type, spec] of Object.entries(part) as Array<[CardType, CardSpec]>) {
      if (Object.prototype.hasOwnProperty.call(out, type)) throw new Error(`card type ${type} is defined in two cards/*.ts files`);
      out[type] = spec;
    }
  }
  return out as Record<CardType, CardSpec>;
}

export const CARD_TYPES: Record<CardType, CardSpec> = mergeCards([leetCards, archCards, apiCards]);
for (const t of CARD_TYPE_NAMES) if (!Object.prototype.hasOwnProperty.call(CARD_TYPES, t)) throw new Error(`card type ${t} has no cards/*.ts entry`);

const encoder = new TextEncoder();

/**
 * Validates a request's `card` (api.md 5.10 카드 오류): missing -> card "required"; not an object -> card "invalid";
 * unknown type -> card.type "invalid"; a server-only type when `serverPath` is false -> card.type "not_allowed"; a v
 * that type does not know -> card.v "invalid"; JSON.stringify(data) over 16,384 bytes -> card.data "too_long"; data
 * that fails the schema -> card.data "invalid". Returns the card to store; throws 400 invalid_request.
 */
export function validateCard(raw: unknown, serverPath: boolean): Card {
  const fail = (field: string, e: FieldError): never => {
    throw err.invalid({ [field]: e });
  };
  if (raw === undefined || raw === null) return fail("card", "required");
  if (typeof raw !== "object" || Array.isArray(raw)) return fail("card", "invalid");
  const c = raw as Record<string, unknown>;
  const type = c.type;
  if (typeof type !== "string" || !Object.prototype.hasOwnProperty.call(CARD_TYPES, type)) return fail("card.type", "invalid");
  const spec = CARD_TYPES[type as CardType];
  if (spec.serverOnly && !serverPath) return fail("card.type", "not_allowed");
  const v = c.v;
  if (typeof v !== "number" || !Number.isInteger(v) || v < 1 || !Object.prototype.hasOwnProperty.call(spec.versions, v)) return fail("card.v", "invalid");
  const data = c.data;
  if (data === null || typeof data !== "object" || Array.isArray(data)) return fail("card.data", "invalid");
  const json = JSON.stringify(data);
  if (encoder.encode(json).byteLength > CARD_DATA_MAX) return fail("card.data", "too_long");
  const parsed = spec.versions[v]!.safeParse(data);
  if (!parsed.success) return fail("card.data", "invalid");
  // store what was sent (re-parsed JSON: plain data only, no prototype tricks)
  return { type: type as CardType, v, data: JSON.parse(json) as Record<string, unknown> };
}
