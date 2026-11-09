// docs/contract/arch.md 2 / collab.md 6: the `arch` doc type's validator (P3 Task 21). Keys, field names and ranges
// mirror internal/archdoc (arch.md 2.1, 1.4); every refusal is {code: invalid | limit_exceeded, detail: arch_*}.
import { describe, it, expect } from "vitest";
import { DOC_TYPES, type DocStateView, type Json, type Op } from "../../src/lib/collab-doctypes";

const arch = DOC_TYPES.arch;

type Obj = { type: string; fields: Record<string, Json>; deleted: boolean };

/** A DocStateView over plain objects (key -> object). */
function view(objs: Record<string, Partial<Obj> & { type: string }> = {}): DocStateView {
  const m = new Map<string, Obj>();
  for (const [k, o] of Object.entries(objs)) m.set(k, { fields: {}, deleted: false, ...o });
  return {
    get: (id) => m.get(id),
    count: (type) => [...m.values()].filter((o) => o.type === type && !o.deleted).length,
    bytes: () => 0,
  };
}

/** n live objects of `type` keyed prefix:x<i>. */
function many(prefix: string, type: string, n: number): DocStateView {
  const o: Record<string, { type: string }> = {};
  for (let i = 0; i < n; i++) o[`${prefix}:x${i}`] = { type };
  return view(o);
}

const ok = (op: Op, st = view()) => expect(arch.validateOp(op, st)).toBeNull();
const refused = (op: Op, detail: string, st = view(), code = "invalid") => expect(arch.validateOp(op, st)).toEqual({ code, detail });

const apiNode = { kind: "api", label: "api", pos: [16, 2], parent: "", instances: 2, cpu_cores: 2, "disk.type": "ssd", "params.lb_algo": "" };

describe("arch validateOp: objects and fields", () => {
  it("valid put / set / del / undel pass", () => {
    ok({ op: "put", id: "n:n_01JAABCDEFGHJKMNPQRSTVWXYZ", type: "node", fields: apiNode });
    ok({ op: "put", id: "meta", type: "meta", fields: { title: "결제 시스템 v3", seed: 42, duration_s: 300 } });
    ok({ op: "put", id: "b:az-1", type: "box", fields: { kind: "az", label: "AZ a", region_code: "", rect: [0, 0, 32, 18], parent: "" } });
    ok({ op: "put", id: "e:e1", type: "edge", fields: { from: "a", to: "b", protocol: "http2", mbps: 0, latency_micros: -1, read_permille: 1000, write_permille: 1000 } });
    ok({ op: "put", id: "f:f1", type: "fault", fields: { at_s: 30, kind: "az_off", target: "az-1", count: 0, add_ms: 0, dur_s: 0 } });
    const st = view({ "n:api": { type: "node" } });
    ok({ op: "set", id: "n:api", field: "instances", value: 4 }, st);
    ok({ op: "set", id: "n:api", field: "traffic.rps", value: 100 }, st);
    ok({ op: "set", id: "n:api", field: "autoscale.enabled", value: true }, st);
    ok({ op: "set", id: "n:api", field: "label", value: "\u{1f9d1}\u200d\u{1f4bb} 서버" }, st); // a ZWJ emoji sequence is one grapheme
    ok({ op: "set", id: "n:api", field: "traffic.rps", value: null }, st); // null clears a field
    ok({ op: "del", id: "n:api" }, st);
    ok({ op: "undel", id: "n:api" }, view({ "n:api": { type: "node", deleted: true } }));
  });

  it("unknown object or field → arch_bad_object / arch_bad_field", () => {
    refused({ op: "put", id: "n1", type: "node", fields: {} }, "arch_bad_object");
    refused({ op: "put", id: "x:n1", type: "node", fields: {} }, "arch_bad_object");
    refused({ op: "put", id: "n:" + "a".repeat(63), type: "node", fields: {} }, "arch_bad_object");
    refused({ op: "put", id: "n:a.b", type: "node", fields: {} }, "arch_bad_object");
    refused({ op: "put", id: "n:api", type: "box", fields: {} }, "arch_bad_object"); // type is not the key's
    refused({ op: "put", id: "meta", type: "node", fields: {} }, "arch_bad_object");
    refused({ op: "set", id: "q:1", field: "label", value: "x" }, "arch_bad_object");
    refused({ op: "del", id: "zzz" }, "arch_bad_object");
    refused({ op: "put", id: "n:api", type: "node", fields: { ...apiNode, cpu: 8 } }, "arch_bad_field");
    refused({ op: "set", id: "e:e1", field: "label", value: "x" }, "arch_bad_field");
    refused({ op: "set", id: "b:b1", field: "instances", value: 1 }, "arch_bad_field");
    refused({ op: "set", id: "meta", field: "nodes", value: [] }, "arch_bad_field");
  });

  it("returns {code, detail}: ranges mirror archdoc", () => {
    const st = view({ "n:api": { type: "node" }, "b:r": { type: "box" }, meta: { type: "meta" } });
    const bad = (field: string, value: Json, id = "n:api") => refused({ op: "set", id, field, value }, "arch_bad_value", st);
    bad("instances", 0);
    bad("instances", 1001);
    bad("instances", 2.5);
    bad("instances", "2");
    bad("cpu_cores", 449);
    bad("label", "가".repeat(41));
    bad("label", "a\u202eb");
    bad("label", "a\nb");
    bad("label", "a\u0085b");
    bad("label", "\ud800");
    bad("kind", "mainframe");
    bad("disk.type", "nvme");
    bad("pos", [1]);
    bad("pos", [0, 100_001]);
    bad("provider", "x".repeat(65));
    bad("autoscale.enabled", 1);
    bad("traffic.curve", "square");
    bad("rect", [0, 0, 0, 10], "b:r");
    bad("kind", "subnet", "b:r");
    bad("title", "x".repeat(81), "meta");
    bad("duration_s", 59, "meta");
    bad("seed", -1, "meta");
    ok({ op: "set", id: "n:api", field: "label", value: "가".repeat(40) }, st);
    ok({ op: "set", id: "meta", field: "title", value: "x".repeat(80) }, st);
    ok({ op: "set", id: "n:api", field: "custom_hourly_micro_usd", value: 1_000_000_000_000 }, st);
    // the code is always within P0's closed set
    for (const r of [arch.validateOp({ op: "set", id: "n:api", field: "instances", value: 0 }, st), arch.validateOp({ op: "put", id: "n1", type: "node", fields: {} }, st)]) {
      expect(["invalid", "limit_exceeded"]).toContain(r!.code);
    }
  });
});

describe("arch validateOp: limits", () => {
  it("301st node put → arch_limit; 1001st edge → arch_limit; 101st box, 51st fault too", () => {
    ok({ op: "put", id: "n:new", type: "node", fields: {} }, many("n", "node", 299));
    refused({ op: "put", id: "n:new", type: "node", fields: {} }, "arch_limit", many("n", "node", 300), "limit_exceeded");
    refused({ op: "put", id: "e:new", type: "edge", fields: {} }, "arch_limit", many("e", "edge", 1000), "limit_exceeded");
    refused({ op: "put", id: "b:new", type: "box", fields: {} }, "arch_limit", many("b", "box", 100), "limit_exceeded");
    refused({ op: "put", id: "f:new", type: "fault", fields: {} }, "arch_limit", many("f", "fault", 50), "limit_exceeded");
    // a put on an existing node is not a new one; undel of a deleted one past the limit is
    ok({ op: "put", id: "n:x0", type: "node", fields: { label: "x" } }, many("n", "node", 300));
    const full = many("n", "node", 300);
    const st: DocStateView = { ...full, get: (id) => (id === "n:gone" ? { type: "node", fields: {}, deleted: true } : full.get(id)) };
    refused({ op: "undel", id: "n:gone" }, "arch_limit", st, "limit_exceeded");
  });

  it("the limits are the doc type's maxObjects too", () => {
    expect(arch.maxObjects).toEqual({ node: 300, edge: 1000, box: 100, fault: 50, meta: 1 });
    expect(arch).toMatchObject({ type: "arch", maxDocBytes: 1_048_576, maxFieldBytes: 16_384 });
  });
});

describe("arch validateOp: box nesting", () => {
  // r (region) > v (vpc) > a (az)
  const nested = () =>
    view({
      "b:r": { type: "box", fields: { kind: "region", parent: "" } },
      "b:v": { type: "box", fields: { kind: "vpc", parent: "r" } },
      "b:a": { type: "box", fields: { kind: "az", parent: "v" } },
    });

  it("parent cycle → arch_cycle", () => {
    refused({ op: "set", id: "b:r", field: "parent", value: "a" }, "arch_cycle", nested());
    refused({ op: "set", id: "b:r", field: "parent", value: "r" }, "arch_cycle", nested());
    refused({ op: "put", id: "b:r", type: "box", fields: { parent: "v" } }, "arch_cycle", nested());
  });

  it("4th nesting level → arch_depth; 3 levels and dangling parents pass", () => {
    refused({ op: "put", id: "b:x", type: "box", fields: { kind: "az", parent: "a" } }, "arch_depth", nested());
    refused({ op: "set", id: "b:x", field: "parent", value: "a" }, "arch_depth", view({ ...Object.fromEntries([["b:x", { type: "box" }]]), "b:r": { type: "box", fields: { parent: "" } }, "b:v": { type: "box", fields: { parent: "r" } }, "b:a": { type: "box", fields: { parent: "v" } } }));
    ok({ op: "put", id: "b:x", type: "box", fields: { kind: "az", parent: "v" } }, nested());
    ok({ op: "put", id: "b:x", type: "box", fields: { kind: "az", parent: "nowhere" } }, nested());
    ok({ op: "set", id: "n:api", field: "parent", value: "a" }, view({ "n:api": { type: "node" } })); // nodes may sit anywhere
  });

  it("a reset snapshot is checked as a whole", () => {
    const snap = (objects: Array<{ id: string; type: string; fields: Record<string, Json>; deleted?: boolean }>): Op => ({ op: "reset", snapshot: { seq: 0, objects } });
    ok(snap([{ id: "meta", type: "meta", fields: { title: "t" } }, { id: "n:a", type: "node", fields: apiNode }]));
    refused(snap([{ id: "n1", type: "node", fields: {} }]), "arch_bad_object");
    refused(snap([{ id: "n:a", type: "node", fields: { cpu: 1 } }]), "arch_bad_field");
    refused(snap([{ id: "n:a", type: "node", fields: { instances: 0 } }]), "arch_bad_value");
    refused(
      snap([
        { id: "b:p", type: "box", fields: { parent: "q" } },
        { id: "b:q", type: "box", fields: { parent: "p" } },
      ]),
      "arch_cycle",
    );
    const nodes = Array.from({ length: 301 }, (_, i) => ({ id: `n:x${i}`, type: "node", fields: {} }));
    refused(snap(nodes), "arch_limit", view(), "limit_exceeded");
    ok(snap(nodes.map((n, i) => (i === 0 ? { ...n, deleted: true } : n)))); // deleted objects do not count
  });
});
