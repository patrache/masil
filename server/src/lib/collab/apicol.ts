// Doc type `apicol` (docs/contract/collab.md 7). P0 placeholder: the limits only — the P4 line owns this file and adds
// object types, field rules and reject details. maxFieldBytes 61,440 keeps one `set` inside a 65,536-byte frame.
import type { DocTypeSpec } from "../collab-doctypes";

export const docType: DocTypeSpec = {
  type: "apicol",
  maxDocBytes: 2_097_152,
  maxObjects: { request: 1000 },
  maxFieldBytes: 61_440,
  validateOp: () => null,
};
