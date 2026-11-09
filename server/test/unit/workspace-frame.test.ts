// docs/contract/workspace.md 2-3: the frame kind table the WorkspaceHub routes by must be the contract table (Go
// internal/workspace/wire reads the same table in its own test), and the header parser rejects what section 2 says.
import { describe, it, expect } from "vitest";
import {
  HEADER_LEN,
  KIND_TABLE,
  LEVELS,
  MAX_FRAME,
  NO_PEER,
  buildFrame,
  kindOf,
  parseHeader,
  peerAllowed,
} from "../../src/do/workspace-frame";

const raw = import.meta.glob("../../../docs/contract/workspace.md", { query: "?raw", import: "default", eager: true }) as Record<string, string>;
const md = Object.values(raw)[0]!;

const LEVEL_OF: Record<string, number> = { "—": NO_PEER, None: 0, Viewer: 1, Prompter: 2, Editor: 3, Keyboard: 4 };

function contractRows() {
  const start = md.indexOf("\n## 3.");
  const end = md.indexOf("\n## ", start + 1);
  const rows: { value: number; name: string; cls: string; minLevel: number }[] = [];
  for (const line of md.slice(start, end).split("\n")) {
    if (!line.startsWith("| 0x")) continue;
    const cells = line.split("|").slice(1, -1).map((s) => s.trim());
    expect(cells, line).toHaveLength(4);
    const min = cells[3]!.startsWith("—") ? "—" : cells[3]!;
    expect(LEVEL_OF[min], line).toBeDefined();
    rows.push({
      value: parseInt(cells[0]!.slice(2), 16),
      name: cells[1]!.replace(/`/g, ""),
      cls: cells[2]!.split("/")[0]!.replace(/\(.*$/, "").trim(),
      minLevel: LEVEL_OF[min]!,
    });
  }
  return rows;
}

describe("workspace frame table", () => {
  it("KIND_TABLE matches docs/contract/workspace.md", () => {
    const rows = contractRows();
    expect(rows.length).toBeGreaterThan(30);
    expect(KIND_TABLE.map((k) => ({ value: k.value, name: k.name, cls: k.cls, minLevel: k.minLevel }))).toEqual(rows);
  });

  it("LEVELS are the contract's level names in rank order", () => {
    expect([...LEVELS]).toEqual(["", "viewer", "prompter", "editor", "keyboard"]);
  });

  it("peerAllowed follows MinLevel; owner-only and unknown kinds never", () => {
    expect(peerAllowed(kindOf("term_input"), 3)).toBe(false);
    expect(peerAllowed(kindOf("term_input"), 4)).toBe(true);
    expect(peerAllowed(kindOf("doc_open"), 1)).toBe(true);
    expect(peerAllowed(kindOf("hs_commit"), 0)).toBe(true);
    expect(peerAllowed(kindOf("doc_open"), 0)).toBe(false);
    expect(peerAllowed(kindOf("term_out"), 4)).toBe(false);
    expect(peerAllowed(kindOf("ctl_levels"), 4)).toBe(false);
    expect(peerAllowed(0x7f, 4)).toBe(false);
  });
});

describe("parseHeader", () => {
  const ab = (u: Uint8Array) => u.buffer.slice(u.byteOffset, u.byteOffset + u.byteLength) as ArrayBuffer;

  it("round-trips buildFrame", () => {
    const f = buildFrame(kindOf("doc_op"), 3, 0, new Uint8Array([1, 2, 3]), 2, 77n);
    expect(f.byteLength).toBe(HEADER_LEN + 3);
    expect(parseHeader(ab(f))).toEqual({ kind: kindOf("doc_op"), flags: 2, from: 3, to: 0, seq: 77n, len: 3 });
  });

  it("rejects short, wrong version, reserved bytes, wrong Len and frames over 256 KiB", () => {
    const ok = buildFrame(kindOf("term_out"), 0, 255, new Uint8Array(4));
    expect(parseHeader(ab(ok.slice(0, 19)))).toBeNull();
    const v2 = ok.slice();
    v2[0] = 2;
    expect(parseHeader(ab(v2))).toBeNull();
    const res = ok.slice();
    res[6] = 1;
    expect(parseHeader(ab(res))).toBeNull();
    expect(parseHeader(ab(ok.slice(0, ok.byteLength - 1)))).toBeNull();
    expect(parseHeader(ab(buildFrame(kindOf("term_out"), 0, 255, new Uint8Array(MAX_FRAME - HEADER_LEN))))).not.toBeNull();
    expect(parseHeader(ab(buildFrame(kindOf("term_out"), 0, 255, new Uint8Array(MAX_FRAME - HEADER_LEN + 1))))).toBeNull();
  });
});
