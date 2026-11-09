// docs/contract/collab.md 3.3 / 5: the doc type registry — the only place P3 (arch) and P4 (apicol) plug in.
import { describe, it, expect } from "vitest";
import { DOC_TYPES, SERVER_FRAME_MAX, type DocTypeSpec } from "../../src/lib/collab-doctypes";
import { DocState, parseOps, parseSnapshot } from "../../src/lib/collab-reducer";
import { COLLAB_LIMITS, decideOp } from "../../src/do/collab-protocol";

const docSources = import.meta.glob("../../src/lib/collab/*.ts", { query: "?raw", import: "default", eager: true }) as Record<string, string>;

describe("DOC_TYPES", () => {
  it("has exactly arch and apicol, each from its own lib/collab file", () => {
    expect(Object.keys(DOC_TYPES).sort()).toEqual(["apicol", "arch"]);
    expect(DOC_TYPES.arch.type).toBe("arch");
    expect(DOC_TYPES.apicol.type).toBe("apicol");
    expect(Object.keys(docSources).map((p) => p.split("/").pop()).sort()).toEqual(["apicol.ts", "arch.ts"]);
    for (const src of Object.values(docSources)) expect(src).toContain("export const docType: DocTypeSpec");
  });

  it("limits match collab.md 5; one field fits a client frame; the server frame cap is maxDocBytes + 65,536", () => {
    expect(DOC_TYPES.arch).toMatchObject({ maxDocBytes: 1_048_576, maxObjects: { node: 300, edge: 1000 }, maxFieldBytes: 16_384 });
    expect(DOC_TYPES.apicol).toMatchObject({ maxDocBytes: 2_097_152, maxObjects: { request: 1000 }, maxFieldBytes: 61_440 });
    for (const t of Object.values(DOC_TYPES)) expect(t.maxFieldBytes).toBeLessThan(COLLAB_LIMITS.clientFrameBytes);
    expect(SERVER_FRAME_MAX).toBe(2_162_688);
  });
});

describe("validateOp refusals (collab.md 3.3 step 2)", () => {
  const cycle: DocTypeSpec = {
    ...DOC_TYPES.arch,
    validateOp: (op) => (op.op === "put" && op.type === "edge" && op.fields.from === op.fields.to ? { code: "invalid", detail: "arch_cycle" } : null),
  };

  it("the reject frame carries the type's detail and no op of the frame is applied", () => {
    const st = DocState.fromSnapshot(parseSnapshot({ seq: 7, objects: [{ id: "a", type: "node", fields: { x: 1 } }] })!);
    const before = st.snapshotText();
    const ops = parseOps([
      { op: "set", id: "a", field: "x", value: 2 },
      { op: "put", id: "b", type: "node", fields: {} },
      { op: "put", id: "e1", type: "edge", fields: { from: "a", to: "a" } },
      { op: "del", id: "a" },
    ])!;
    const r = decideOp(st, cycle, 8, "c1", ops);
    expect(r.ok).toBe(false);
    if (r.ok) return;
    expect(JSON.parse(r.frame)).toEqual({ t: "reject", client_op_id: "c1", code: "invalid", detail: "arch_cycle" });
    expect(st.snapshotText()).toBe(before);
    expect(st.seq).toBe(7);
  });

  it("validateOp sees the state as the earlier ops of the same frame left it", () => {
    const seen: number[] = [];
    const spec: DocTypeSpec = {
      ...DOC_TYPES.arch,
      validateOp: (_op, state) => {
        seen.push(state.count("node"));
        return null;
      },
    };
    const st = DocState.fromSnapshot({ seq: 0, objects: [] });
    const r = decideOp(st, spec, 1, "c1", parseOps([{ op: "put", id: "a", type: "node", fields: {} }, { op: "put", id: "b", type: "node", fields: {} }])!);
    expect(r.ok).toBe(true);
    expect(seen).toEqual([0, 1]);
  });

  it("a detail outside ^[a-z0-9_]{1,40}$ is dropped, the code stays", () => {
    const spec: DocTypeSpec = { ...DOC_TYPES.arch, validateOp: () => ({ code: "invalid", detail: "Bad Detail!" }) };
    const st = DocState.fromSnapshot({ seq: 0, objects: [] });
    const r = decideOp(st, spec, 1, "c1", parseOps([{ op: "put", id: "a", type: "node", fields: {} }])!);
    expect(r.ok ? null : JSON.parse(r.frame)).toEqual({ t: "reject", client_op_id: "c1", code: "invalid" });
  });
});
