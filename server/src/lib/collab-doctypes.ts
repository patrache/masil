// The document type registry (docs/contract/collab.md 5): the only place the arch (P3) and apicol (P4) lines plug in.
// Each type lives in its own file under lib/collab/ and exports `docType: DocTypeSpec`; this module collects them.
// The CollabHub runs, per op frame and atomically (collab.md 3.3): shape checks -> validateOp per op -> limits.
import type { Json, Op } from "./collab-reducer";
import { docType as arch } from "./collab/arch";
import { docType as apicol } from "./collab/apicol";

export type { Json, Op };

export interface DocStateView {
  get(id: string): { type: string; fields: Record<string, Json>; deleted: boolean } | undefined;
  /** live (not deleted) objects of this type */
  count(type: string): number;
  /** UTF-8 bytes of the canonical snapshot's JSON */
  bytes(): number;
}

/** A doc type's refusal; per-type reasons go only in `detail` (^[a-z0-9_]{1,40}$, e.g. "arch_cycle"). */
export interface RejectReason {
  code: "invalid" | "too_large" | "limit_exceeded";
  detail?: string;
}

export type DocKind = "arch" | "apicol";

export interface DocTypeSpec {
  type: DocKind;
  maxDocBytes: number;
  /** object type name -> max live objects; types not listed have no count limit */
  maxObjects: Record<string, number>;
  /** one field value's JSON bytes; must stay below the 65,536-byte client frame so a single set fits */
  maxFieldBytes: number;
  validateOp(op: Op, state: DocStateView): RejectReason | null;
}

export const DOC_TYPES: Record<DocKind, DocTypeSpec> = { arch, apicol };

/** Server -> client frame cap (collab.md 4): the largest maxDocBytes + 65,536. */
export const SERVER_FRAME_MAX = Math.max(...Object.values(DOC_TYPES).map((t) => t.maxDocBytes)) + 65_536;

export function isDocKind(v: unknown): v is DocKind {
  return v === "arch" || v === "apicol";
}
