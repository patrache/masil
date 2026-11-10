// v0.1.3 security (ad): field and settings names are looked up in plain object tables; a name that is an
// Object.prototype member ("constructor", "toString", "__proto__", "hasOwnProperty") must be refused, never taken for a
// known field (a function or object "kind" made checkValue fall through and accept or return undefined).
import { describe, it, expect } from "vitest";
import { DOC_TYPES, type DocStateView } from "../../src/lib/collab-doctypes";
import { parseOps, parseSnapshot } from "../../src/lib/collab-reducer";

// "__proto__" / "constructor" / "prototype" are already refused by the op parser (isName RESERVED); the rest of
// Object.prototype is the validators' job.
const PROTO_KEYS = ["toString", "hasOwnProperty", "valueOf", "isPrototypeOf", "toLocaleString", "propertyIsEnumerable"];
const INVALID = { code: "invalid", detail: "invalid_op" };

const empty: DocStateView = { get: () => undefined, count: () => 0, bytes: () => 0 };
const withReq: DocStateView = {
  get: (id) => (id === "req:r1" ? { type: "request", fields: { name: "a" }, deleted: false } : undefined),
  count: () => 1,
  bytes: () => 0,
};

function op(raw: unknown) {
  const ops = parseOps([raw]);
  expect(ops, JSON.stringify(raw)).not.toBeNull();
  return ops![0]!;
}

describe("apicol validator: prototype-chain names", () => {
  const v = DOC_TYPES.apicol.validateOp;
  for (const k of PROTO_KEYS) {
    for (const value of [null, "x", true, 1]) {
      it(`field ${k} = ${JSON.stringify(value)} -> invalid_op (put, set, reset)`, () => {
        expect(v(op(JSON.parse(`{"op":"put","id":"req:r2","type":"request","fields":{${JSON.stringify(k)}:${JSON.stringify(value)}}}`)), empty)).toEqual(INVALID);
        expect(v(op({ op: "set", id: "req:r1", field: k, value }), withReq)).toEqual(INVALID);
        const snap = parseSnapshot(JSON.parse(`{"seq":0,"objects":[{"id":"req:r2","type":"request","fields":{${JSON.stringify(k)}:${JSON.stringify(value)}}}]}`));
        expect(snap).not.toBeNull();
        expect(v({ op: "reset", snapshot: snap! }, empty)).toEqual(INVALID);
      });
    }
    it(`settings key ${k} -> invalid_op`, () => {
      for (const x of [true, 1]) {
        const raw = JSON.parse(`{"op":"set","id":"req:r1","field":"settings","value":{${JSON.stringify(k)}:${JSON.stringify(x)}}}`);
        expect(v(op(raw), withReq)).toEqual(INVALID);
      }
    });
  }
  it("known fields and settings still pass", () => {
    expect(v(op({ op: "set", id: "req:r1", field: "settings", value: { timeout_ms: 5000, insecure: false } }), withReq)).toBeNull();
    expect(v(op({ op: "set", id: "req:r1", field: "name", value: null }), withReq)).toBeNull();
  });
});

describe("op parser", () => {
  it("refuses the reserved names outright", () => {
    for (const k of ["__proto__", "constructor", "prototype"]) {
      expect(parseOps([JSON.parse(`{"op":"put","id":"req:r2","type":"request","fields":{${JSON.stringify(k)}:null}}`)])).toBeNull();
      expect(parseOps([{ op: "set", id: "req:r1", field: k, value: null }])).toBeNull();
    }
  });
});

describe("arch validator: prototype-chain names", () => {
  const v = DOC_TYPES.arch.validateOp;
  for (const k of PROTO_KEYS) {
    it(`field ${k} -> arch_bad_field`, () => {
      const r = v(op(JSON.parse(`{"op":"put","id":"n:a","type":"node","fields":{${JSON.stringify(k)}:null}}`)), empty);
      expect(r).toEqual({ code: "invalid", detail: "arch_bad_field" });
      expect(v(op({ op: "set", id: "n:a", field: k, value: 1 }), empty)).toEqual({ code: "invalid", detail: "arch_bad_field" });
    });
  }
});
