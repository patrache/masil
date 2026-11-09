// docs/contract/collab.md 1, 3.3, 5, 8: the shared vectors (the Go client runs the same file) and the per-type limits.
import { describe, it, expect } from "vitest";
import { DocState, applyFrame, applyOps, parseOps, parseSnapshot, type Op } from "../../src/lib/collab-reducer";
import { DOC_TYPES } from "../../src/lib/collab-doctypes";

const raw = import.meta.glob("../../../docs/contract/collab-vectors.json", { query: "?raw", import: "default", eager: true }) as Record<string, string>;

interface Vector {
  name: string;
  start: unknown;
  batches: Array<{ seq?: number; ops: unknown; reject?: "invalid" }>;
  want: unknown;
}

const vectors = JSON.parse(Object.values(raw)[0]!) as Vector[];

/** Key-sorted JSON (collab.md 8: values compare by meaning, key order does not matter). */
function canon(v: unknown): string {
  if (Array.isArray(v)) return `[${v.map(canon).join(",")}]`;
  if (v !== null && typeof v === "object") {
    const o = v as Record<string, unknown>;
    return `{${Object.keys(o)
      .sort()
      .map((k) => `${JSON.stringify(k)}:${canon(o[k])}`)
      .join(",")}}`;
  }
  return JSON.stringify(v);
}

function stateOf(snapshot: unknown): DocState {
  const s = parseSnapshot(snapshot);
  if (s === null) throw new Error("bad start snapshot");
  return DocState.fromSnapshot(s);
}

describe("collab vectors (collab-vectors.json)", () => {
  it("has the contract's ten-plus vectors with unique names", () => {
    expect(vectors.length).toBeGreaterThanOrEqual(10);
    expect(new Set(vectors.map((v) => v.name)).size).toBe(vectors.length);
  });

  for (const v of vectors) {
    it(v.name, () => {
      const st = stateOf(v.start);
      for (const b of v.batches) {
        if (b.reject !== undefined) {
          expect(parseOps(b.ops), `${v.name}: batch must be refused`).toBeNull();
          continue;
        }
        const ops = parseOps(b.ops, true); // sequenced batches are what the server applies (reset allowed)
        expect(ops, `${v.name}: batch must parse`).not.toBeNull();
        applyOps(st, b.seq!, ops!);
      }
      expect(canon(JSON.parse(st.snapshotText()))).toBe(canon(v.want));
      // the canonical text itself: objects by id, deleted only when true
      expect(st.snapshotText()).toBe(st.snapshotText());
      expect(st.bytes()).toBe(new TextEncoder().encode(st.snapshotText()).byteLength);
    });
  }

  it("the vector file's __proto__ keys never reach a prototype", () => {
    const st = stateOf({ seq: 0, objects: [] });
    expect(parseOps([{ op: "put", id: "x", type: "node", fields: JSON.parse('{"__proto__": {"polluted": 1}}') }])).toBeNull();
    applyOps(st, 1, parseOps([{ op: "put", id: "x", type: "node", fields: { v: JSON.parse('{"__proto__": {"polluted": 1}}') } }])!);
    expect(({} as Record<string, unknown>).polluted).toBeUndefined();
    expect(st.get("x")!.fields.v).toEqual(JSON.parse('{"__proto__": {"polluted": 1}}'));
  });
});

describe("canonical snapshot", () => {
  it("sorts objects by id and fields by name, keeps deleted objects' fields", () => {
    const st = stateOf({ seq: 3, objects: [{ id: "b", type: "node", fields: { z: 1, a: 2 } }, { id: "a", type: "edge", fields: {}, deleted: true }] });
    expect(st.snapshotText()).toBe('{"seq":3,"objects":[{"id":"a","type":"edge","fields":{},"deleted":true},{"id":"b","type":"node","fields":{"a":2,"z":1}}]}');
  });

  it("bytes() tracks every mutation (UTF-8, cached per object)", () => {
    const st = stateOf({ seq: 0, objects: [] });
    const ops: Op[][] = [
      [{ op: "put", id: "n1", type: "node", fields: { label: "가나다" } }],
      [{ op: "set", id: "n1", field: "emoji", value: "🙂" }],
      [{ op: "del", id: "n1" }],
      [{ op: "undel", id: "n1" }],
      [{ op: "put", id: "n2", type: "node", fields: {} }],
    ];
    let seq = 0;
    for (const f of ops) {
      applyOps(st, ++seq, f);
      expect(st.bytes()).toBe(new TextEncoder().encode(st.snapshotText()).byteLength);
    }
  });
});

describe("per-type limits (collab.md 3.3 step 3, 5)", () => {
  const nodes = (from: number, n: number): Op[] =>
    Array.from({ length: n }, (_, i) => ({ op: "put", id: `n${from + i}`, type: "node", fields: {} }) as Op);

  it("arch: the 301st live node -> limit_exceeded limit_node, nothing applied", () => {
    const st = stateOf({ seq: 0, objects: [] });
    expect(applyFrame(st, DOC_TYPES.arch, 1, nodes(0, 200)).ok).toBe(true);
    expect(applyFrame(st, DOC_TYPES.arch, 2, nodes(200, 100)).ok).toBe(true);
    expect(st.count("node")).toBe(300);
    const before = st.snapshotText();
    const r = applyFrame(st, DOC_TYPES.arch, 3, [{ op: "set", id: "n0", field: "x", value: 1 }, ...nodes(300, 1)]);
    expect(r).toEqual({ ok: false, reason: { code: "limit_exceeded", detail: "limit_node" } });
    expect(st.snapshotText()).toBe(before);
    expect(st.seq).toBe(2);
    // a deleted node does not count: delete one, then the 301st id fits
    expect(applyFrame(st, DOC_TYPES.arch, 3, [{ op: "del", id: "n0" }, ...nodes(300, 1)]).ok).toBe(true);
    expect(st.count("node")).toBe(300);
    // undel would make it 301 again
    expect(applyFrame(st, DOC_TYPES.arch, 4, [{ op: "undel", id: "n0" }])).toEqual({ ok: false, reason: { code: "limit_exceeded", detail: "limit_node" } });
  });

  /** A doc whose canonical snapshot is exactly `target` bytes: filler objects of one big string field each. */
  function sized(target: number, type: string, fieldMax: number): DocState {
    const st = stateOf({ seq: 0, objects: [] });
    let i = 0;
    for (;;) {
      const left = target - st.bytes();
      if (left <= 0) break;
      const id = `o${String(i).padStart(5, "0")}`;
      const sep = i === 0 ? 0 : 1;
      const empty = `{"id":"${id}","type":"${type}","fields":{"f":""}}`.length + sep;
      // a full object unless the rest fits in this one; never leave a remainder too small for one more object
      const len = left - empty <= fieldMax - 2 ? left - empty : Math.min(fieldMax - 2, left - empty - (empty + 2));
      if (len < 0) throw new Error("cannot hit the exact size");
      applyOps(st, 0, [{ op: "put", id, type, fields: { f: "x".repeat(len) } }]);
      i++;
    }
    expect(st.bytes()).toBe(target);
    return st;
  }

  it("arch: a document over 1,048,576 bytes -> too_large (exactly at the limit passes)", () => {
    const st = sized(1_048_576 - 9, "blob", 16_384);
    expect(applyFrame(st, DOC_TYPES.arch, 1, [{ op: "set", id: "o00000", field: "g", value: "xx" }]).ok).toBe(true); // +9 bytes: ,"g":"xx"
    expect(st.bytes()).toBe(1_048_576);
    const before = st.snapshotText();
    expect(applyFrame(st, DOC_TYPES.arch, 2, [{ op: "set", id: "o00000", field: "g", value: "x".repeat(3) }])).toEqual({ ok: false, reason: { code: "too_large" } });
    expect(st.bytes()).toBe(1_048_576);
    expect(st.snapshotText()).toBe(before);
  });

  it("apicol: up to 2,097,152 bytes passes, one more byte is too_large", () => {
    const st = sized(2_097_152 - 9, "request", 61_440);
    // the common limits alone: the filler ids / fields are not apicol objects (its validateOp is P4's, collab.md 7)
    const limits = { ...DOC_TYPES.apicol, validateOp: () => null };
    expect(applyFrame(st, limits, 1, [{ op: "set", id: "o00000", field: "g", value: "xx" }]).ok).toBe(true);
    expect(st.bytes()).toBe(2_097_152);
    expect(applyFrame(st, limits, 2, [{ op: "set", id: "o00000", field: "g", value: "xxx" }])).toEqual({ ok: false, reason: { code: "too_large" } });
  });

  it("a field over maxFieldBytes -> too_large; exactly at it passes", () => {
    const st = stateOf({ seq: 0, objects: [{ id: "n1", type: "node", fields: {} }] });
    expect(applyFrame(st, DOC_TYPES.arch, 1, [{ op: "set", id: "n1", field: "f", value: "x".repeat(16_382) }]).ok).toBe(true); // 16,384 with quotes
    expect(applyFrame(st, DOC_TYPES.arch, 2, [{ op: "set", id: "n1", field: "f", value: "x".repeat(16_383) }])).toEqual({ ok: false, reason: { code: "too_large" } });
    expect(st.get("n1")!.fields.f).toBe("x".repeat(16_382));
  });

  it("a refused frame with a reset in it restores the whole table", () => {
    const st = stateOf({ seq: 4, objects: [{ id: "a", type: "node", fields: { x: 1 } }] });
    const before = st.snapshotText();
    const snapshot = parseSnapshot({ seq: 0, objects: Array.from({ length: 301 }, (_, i) => ({ id: `n${i}`, type: "node", fields: {} })) })!;
    const r = applyFrame(st, DOC_TYPES.arch, 5, [{ op: "set", id: "a", field: "x", value: 2 }, { op: "reset", snapshot }]);
    expect(r).toEqual({ ok: false, reason: { code: "limit_exceeded", detail: "limit_node" } });
    expect(st.snapshotText()).toBe(before);
    expect(st.count("node")).toBe(1);
  });
});
