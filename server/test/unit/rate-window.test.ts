// inOneRateWindow (test/helpers.ts): the rate-limit tests' guard against miniflare's wall-clock-aligned fixed windows.
// A fake clock drives it here; the real-limiter behaviour (a boundary between hit N and N+1 resets the count) was
// observed against miniflare 5.20260815.0-alpha (pending item 8-a follow-up).
import { describe, it, expect } from "vitest";
import { inOneRateWindow } from "../helpers";

describe("inOneRateWindow", () => {
  const clock = (...ts: number[]) => {
    let i = 0;
    return () => ts[Math.min(i++, ts.length - 1)];
  };

  it("returns the first attempt's result without retrying", async () => {
    let calls = 0;
    expect(await inOneRateWindow(60, async () => ++calls, clock(1_000, 2_000))).toBe(1);
    expect(calls).toBe(1);
  });

  it("reruns an attempt that failed while the clock crossed a window boundary", async () => {
    let calls = 0;
    const now = clock(59_900, 60_100, 60_100, 60_200); // attempt 1 straddles 60s; attempt 2 stays in window 1
    const r = await inOneRateWindow(60, async () => {
      if (++calls === 1) throw new Error("got 201 where 429 was due");
      return "ok";
    }, now);
    expect([r, calls]).toEqual(["ok", 2]);
  });

  it("rethrows a failure within one window at once (a crossing can only reset counts, never hide a real failure)", async () => {
    let calls = 0;
    await expect(inOneRateWindow(60, async () => { calls++; throw new Error("real"); }, clock(1_000, 59_999))).rejects.toThrow("real");
    expect(calls).toBe(1);
  });

  it("uses the given period: 10s windows cross where 60s ones do not", async () => {
    let calls = 0;
    const attempt = async () => { if (++calls === 1) throw new Error("x"); return calls; };
    expect(await inOneRateWindow(10, attempt, clock(9_900, 10_100, 10_100, 10_200))).toBe(2);
    calls = 0;
    await expect(inOneRateWindow(60, attempt, clock(9_900, 10_100))).rejects.toThrow("x");
  });

  it("gives up after three attempts", async () => {
    let calls = 0;
    let t = 0;
    await expect(inOneRateWindow(10, async () => { calls++; throw new Error("x"); }, () => (t += 10_000))).rejects.toThrow("x");
    expect(calls).toBe(3);
  });
});
