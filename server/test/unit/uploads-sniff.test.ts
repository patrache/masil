import { expect, test } from "vitest";
import { r2Key, sniffImage } from "../../src/lib/uploads";
import { PNG_1X1 } from "../helpers";

test("png magic + IHDR size", () => {
  expect(sniffImage(PNG_1X1)).toEqual({ contentType: "image/png", ext: "png", width: 1, height: 1 });
});
test("jpeg SOF0 size", () => {
  const jpg = new Uint8Array([0xff,0xd8,0xff,0xe0,0,16,0x4a,0x46,0x49,0x46,0,1,1,0,0,1,0,1,0,0, 0xff,0xc0,0,17,8,0,20,0,30,3,1,0x22,0,2,0x11,1,3,0x11,1]);
  expect(sniffImage(jpg)).toMatchObject({ contentType: "image/jpeg", ext: "jpg", width: 30, height: 20 });
});
test("gif89a and webp VP8X sizes", () => {
  const gif = new Uint8Array([...new TextEncoder().encode("GIF89a"), 3,0, 2,0, 0,0,0]);
  expect(sniffImage(gif)).toMatchObject({ contentType: "image/gif", width: 3, height: 2 });
  const webp = new Uint8Array([...new TextEncoder().encode("RIFF"),0,0,0,0,...new TextEncoder().encode("WEBPVP8X"),10,0,0,0,0,0,0,0, 99,0,0, 49,0,0]);
  expect(sniffImage(webp)).toMatchObject({ contentType: "image/webp", width: 100, height: 50 });
});
test("unknown bytes / truncated header", () => {
  expect(sniffImage(new TextEncoder().encode("hello"))).toBeNull();
  expect(sniffImage(PNG_1X1.slice(0, 12))).toMatchObject({ contentType: "image/png", width: null, height: null });
});
test("r2Key layout", () => { expect(r2Key("story", "U1", "X1", "webp")).toBe("stories/U1/X1.webp"); });
