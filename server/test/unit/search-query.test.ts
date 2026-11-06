import { it, expect } from "vitest";
import { buildMessageSearch, highlights } from "../../src/lib/search";

it("quotes terms and blocks FTS syntax injection", () => {
  expect(buildMessageSearch(' 배포는  "몇시"  ')).toEqual({ mode: "fts", match: '"배포는" AND """몇시"""', terms: ["배포는", '"몇시"'] });
  expect(buildMessageSearch("배포 OR*")).toEqual({ mode: "like", terms: ["배포", "OR*"] });
  expect(buildMessageSearch("배포".normalize("NFD") + "는")).toMatchObject({ mode: "fts", terms: ["배포는"] });
});
it("highlights in code points, case-insensitive", () => {
  expect(highlights("😀 Deploy 배포는 deploy", ["deploy", "배포"])).toEqual([{ start: 2, length: 6 }, { start: 9, length: 2 }, { start: 13, length: 6 }]);
});
it("highlights never overlap and come in start order", () => {
  expect(highlights("배포는배포", ["포는배", "배포"])).toEqual([{ start: 0, length: 2 }, { start: 3, length: 2 }]);
  expect(highlights("aaaa", ["aa"])).toEqual([{ start: 0, length: 2 }, { start: 2, length: 2 }]);
  expect(highlights("nothing here", ["xyz"])).toEqual([]);
});
