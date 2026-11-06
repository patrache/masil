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
    expect(imageMetadataCheck(riff(VP8X, chunk("ANIM", [0, 0, 0, 0, 0, 0]), chunk("ANMF", [1, 2, 3, 4])), "webp")).toBe("ok");
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
