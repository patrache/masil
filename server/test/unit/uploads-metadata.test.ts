// Pending item 12 (server side): WebP/GIF uploads carrying EXIF/XMP/ICC metadata are refused, malformed containers too.
import { describe, expect, test } from "vitest";
import { imageMetadataCheck } from "../../src/lib/uploads";

const enc = (s: string) => [...new TextEncoder().encode(s)];
const le32 = (n: number) => [n & 0xff, (n >> 8) & 0xff, (n >> 16) & 0xff, (n >>> 24) & 0xff];
const chunk = (fourcc: string, data: number[]) => [...enc(fourcc), ...le32(data.length), ...data, ...(data.length % 2 ? [0] : [])];
const riff = (...chunks: number[][]) => {
  const body = [...enc("WEBP"), ...chunks.flat()];
  return new Uint8Array([...enc("RIFF"), ...le32(body.length), ...body]);
};
const VP8X = chunk("VP8X", [0, 0, 0, 0, 9, 0, 0, 9, 0, 0]);
const VP8L = chunk("VP8L", [0x2f, 0, 0, 0, 0]);
const VP8 = chunk("VP8 ", [1, 2, 3]);

describe("webp", () => {
  test("plain VP8 / VP8L / VP8X+ALPH+ANIM+ANMF -> ok", () => {
    expect(imageMetadataCheck(riff(VP8), "webp")).toBe("ok");
    expect(imageMetadataCheck(riff(VP8L), "webp")).toBe("ok");
    const frame = [0, 0, 0, 0, 0, 0, 8, 0, 0, 8, 0, 0, 0x64, 0, 0, 0, ...VP8L];
    expect(imageMetadataCheck(riff(VP8X, chunk("ANIM", [0, 0, 0, 0, 0, 0]), chunk("ANMF", frame)), "webp")).toBe("ok");
  });
  for (const id of ["EXIF", "XMP ", "ICCP"]) {
    test(`${id} chunk -> metadata`, () => {
      expect(imageMetadataCheck(riff(VP8X, VP8L, chunk(id, [1, 2, 3])), "webp")).toBe("metadata");
    });
  }
  test("chunk size past the end / RIFF size lie / trailing junk -> malformed", () => {
    const bad = riff(VP8L);
    bad[16] = 0xff; // VP8L size
    expect(imageMetadataCheck(bad, "webp")).toBe("malformed");
    const lie = riff(VP8L);
    lie[4] = 0xff; lie[5] = 0xff;
    expect(imageMetadataCheck(lie, "webp")).toBe("malformed");
    expect(imageMetadataCheck(new Uint8Array([...riff(VP8L), 1, 2, 3]), "webp")).toBe("malformed");
    expect(imageMetadataCheck(riff(), "webp")).toBe("malformed");
  });
  test("unknown chunk ids are refused (cannot vouch for them)", () => {
    expect(imageMetadataCheck(riff(VP8L, chunk("ABCD", [1])), "webp")).toBe("metadata");
  });

  // An ANMF payload is a 16-byte frame header followed by its own chunks; metadata hidden there leaks the same way.
  const frameHead = [0, 0, 0, 0, 0, 0, 8, 0, 0, 8, 0, 0, 0x64, 0, 0, 0];
  const anim = chunk("ANIM", [0, 0, 0, 0, 0, 0]);
  const animated = (...frames: number[][]) => riff(VP8X, anim, ...frames);
  test("ANMF frames holding ALPH+VP8 or VP8L -> ok", () => {
    expect(imageMetadataCheck(animated(chunk("ANMF", [...frameHead, ...VP8L])), "webp")).toBe("ok");
    expect(imageMetadataCheck(animated(chunk("ANMF", [...frameHead, ...chunk("ALPH", [0, 1]), ...VP8])), "webp")).toBe("ok");
  });
  for (const id of ["EXIF", "XMP ", "ICCP", "ABCD"]) {
    test(`${id} inside an ANMF frame -> metadata`, () => {
      const leak = chunk(id, enc("Exif GPS 37.5665 iPhone"));
      expect(imageMetadataCheck(animated(chunk("ANMF", [...frameHead, ...VP8L, ...leak])), "webp")).toBe("metadata");
      expect(imageMetadataCheck(animated(chunk("ANMF", [...frameHead, ...leak, ...VP8L])), "webp")).toBe("metadata");
    });
  }
  test("ANMF shorter than its frame header, or whose sub-chunks do not add up -> malformed", () => {
    expect(imageMetadataCheck(animated(chunk("ANMF", [1, 2, 3, 4])), "webp")).toBe("malformed");
    expect(imageMetadataCheck(animated(chunk("ANMF", frameHead)), "webp")).toBe("malformed"); // no frame data
    const overrun = [...frameHead, ...VP8L];
    overrun[16 + 4] = 0x40; // the VP8L sub-chunk claims more than the frame holds
    expect(imageMetadataCheck(animated(chunk("ANMF", overrun)), "webp")).toBe("malformed");
    expect(imageMetadataCheck(animated(chunk("ANMF", [...frameHead, ...VP8L, 7])), "webp")).toBe("malformed"); // stray byte
  });
});

// GIF: header + logical screen descriptor (no GCT), then blocks, then trailer 0x3b.
const gifHead = [...enc("GIF89a"), 1, 0, 1, 0, 0, 0, 0];
const image = [0x2c, 0, 0, 0, 0, 1, 0, 1, 0, 0, 2, 2, 0x4c, 0x01, 0];
const gce = [0x21, 0xf9, 4, 0, 0, 0, 0, 0];
const netscape = [0x21, 0xff, 11, ...enc("NETSCAPE2.0"), 3, 1, 0, 0, 0];
const gif = (...blocks: number[][]) => new Uint8Array([...gifHead, ...blocks.flat(), 0x3b]);

describe("gif", () => {
  test("image + GCE + NETSCAPE2.0 loop -> ok", () => {
    expect(imageMetadataCheck(gif(netscape, gce, image, gce, image), "gif")).toBe("ok");
  });
  test("global colour table is skipped correctly", () => {
    const head = [...enc("GIF89a"), 1, 0, 1, 0, 0x80, 0, 0, 0, 0, 0, 0xff, 0xff, 0xff];
    expect(imageMetadataCheck(new Uint8Array([...head, ...image, 0x3b]), "gif")).toBe("ok");
  });
  test("comment extension -> metadata", () => {
    expect(imageMetadataCheck(gif([0x21, 0xfe, 3, ...enc("GPS"), 0], image), "gif")).toBe("metadata");
  });
  test("XMP application extension -> metadata", () => {
    expect(imageMetadataCheck(gif([0x21, 0xff, 11, ...enc("XMP DataXMP"), 2, 1, 2, 0], image), "gif")).toBe("metadata");
  });
  // The client writes a GCE as one 4-byte sub-block and a loop extension as one 3-byte loop sub-block, and drops plain
  // text (0x01); anything more is data a client never sends and the server cannot vouch for (api.md 5.14).
  test("GCE with extra or odd-sized sub-blocks -> metadata", () => {
    expect(imageMetadataCheck(gif([0x21, 0xf9, 4, 0, 0, 0, 0, 6, ...enc("SECRET"), 0], image), "gif")).toBe("metadata");
    expect(imageMetadataCheck(gif([0x21, 0xf9, 5, 0, 0, 0, 0, 0, 0], image), "gif")).toBe("metadata");
    expect(imageMetadataCheck(gif([0x21, 0xf9, 0], image), "gif")).toBe("metadata");
  });
  test("loop extension with anything but one loop sub-block -> metadata", () => {
    const loop = (...sub: number[]) => [0x21, 0xff, 11, ...enc("NETSCAPE2.0"), ...sub];
    expect(imageMetadataCheck(gif(loop(3, 1, 0, 0, 6, ...enc("SECRET"), 0), image), "gif")).toBe("metadata");
    expect(imageMetadataCheck(gif(loop(5, 2, 0, 0, 1, 0, 0), image), "gif")).toBe("metadata"); // buffering sub-block
    expect(imageMetadataCheck(gif(loop(0), image), "gif")).toBe("metadata");
    expect(imageMetadataCheck(gif([0x21, 0xff, 11, ...enc("ANIMEXTS1.0"), 3, 1, 0, 0, 0], image), "gif")).toBe("ok");
  });
  test("plain text extension (0x01) -> metadata", () => {
    const text = [0x21, 0x01, 12, 0, 0, 0, 0, 4, 0, 3, 0, 1, 1, 1, 0, 18, ...enc("GPS 37.5665 iPhone"), 0];
    expect(imageMetadataCheck(gif(text, image), "gif")).toBe("metadata");
  });
  test("truncated / no trailer / bad block -> malformed", () => {
    expect(imageMetadataCheck(new Uint8Array([...gifHead, ...image]), "gif")).toBe("malformed");
    expect(imageMetadataCheck(new Uint8Array([...gifHead, 0x99, 0x3b]), "gif")).toBe("malformed");
    expect(imageMetadataCheck(new Uint8Array([...gifHead, 0x21, 0xfe, 9, 1]), "gif")).toBe("malformed");
  });
});

test("png / jpeg are not inspected here", () => {
  expect(imageMetadataCheck(new Uint8Array([1]), "png")).toBe("ok");
  expect(imageMetadataCheck(new Uint8Array([1]), "jpg")).toBe("ok");
});

// The Go client's PrepareUpload output for metadata-laden WebP/GIF inputs, pinned by
// internal/imgrender/server_fixture_test.go: what the client sends must always pass this check.
const clientFixture = import.meta.glob("./client-stripped.json", { query: "?raw", import: "default", eager: true }) as Record<
  string,
  string
>;
const b64 = (s: string) => Uint8Array.from(atob(s), (c) => c.charCodeAt(0));

describe("client-stripped uploads (internal/imgrender)", () => {
  const raw = clientFixture["./client-stripped.json"];
  test("fixture is present", () => expect(raw).toBeTypeOf("string"));
  const pairs = Object.entries(JSON.parse(raw ?? "{}") as Record<string, { in: string; out: string }>);
  test("covers stills, animations and GIFs", () => {
    expect(pairs.map(([n]) => n).sort()).toEqual(["gif-animated.gif", "webp-animated.webp", "webp-simple.webp", "webp-still-alpha.webp"]);
  });
  for (const [name, p] of pairs) {
    const ext = name.endsWith(".gif") ? "gif" : "webp";
    test(`${name}: the original is refused, the client's stripped bytes pass`, () => {
      expect(imageMetadataCheck(b64(p.in), ext)).toBe("metadata");
      expect(imageMetadataCheck(b64(p.out), ext)).toBe("ok");
    });
  }
});
