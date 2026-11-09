// Doc type `arch` (docs/contract/collab.md 6). P0 placeholder: the limits only — the P3 line owns this file and adds
// object types, field rules and reject details.
import type { DocTypeSpec } from "../collab-doctypes";

export const docType: DocTypeSpec = {
  type: "arch",
  maxDocBytes: 1_048_576,
  maxObjects: { node: 300, edge: 1000 },
  maxFieldBytes: 16_384,
  validateOp: () => null,
};
