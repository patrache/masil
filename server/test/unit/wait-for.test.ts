// v0.1.1 S3: waitFor's default deadline must end before vitest's default testTimeout (5000 ms; vitest.config.ts
// sets none), so a condition that never holds returns the last value for the caller's assertion instead of the
// test being killed by the runner's timeout with no state in the message.
import { it, expect } from "vitest";
import { waitFor } from "../helpers";

it("waitFor with default options returns the last probed value before the test timeout", async () => {
  let n = 0;
  const v = await waitFor(
    () => ++n,
    () => false,
  );
  expect(v).toBe(n);
  expect(n).toBeGreaterThan(1);
});
