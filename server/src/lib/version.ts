// Client version gate (server-architecture.md 4.13, api.md 0.5 upgrade_required).
// Only masil clients are gated; any other User-Agent (curl, browsers) passes.
import type { MiddlewareHandler } from "hono";
import type { AppEnv } from "../env";
import { ApiError } from "./errors";

const MASIL_UA_RE = /^masil\/(\d+)\.(\d+)\.(\d+)/;

export type Semver = [number, number, number];

export function parseSemver(s: string): Semver | null {
  const m = /^(\d+)\.(\d+)\.(\d+)/.exec(s.trim());
  return m ? [Number(m[1]), Number(m[2]), Number(m[3])] : null;
}

/** Version of a "masil/x.y.z (...)" User-Agent, or null for anything else. */
export function parseClientVersion(userAgent: string | undefined): Semver | null {
  if (!userAgent) return null;
  const m = MASIL_UA_RE.exec(userAgent);
  return m ? [Number(m[1]), Number(m[2]), Number(m[3])] : null;
}

export function compareSemver(a: Semver, b: Semver): number {
  for (let i = 0; i < 3; i++) if (a[i] !== b[i]) return a[i] < b[i] ? -1 : 1;
  return 0;
}

export const clientVersionGate: MiddlewareHandler<AppEnv> = async (c, next) => {
  const client = parseClientVersion(c.req.header("User-Agent"));
  const minRaw = c.env.MIN_CLIENT_VERSION;
  const min = minRaw ? parseSemver(minRaw) : null;
  if (client && min && compareSemver(client, min) < 0) {
    throw new ApiError(426, "upgrade_required", `client ${client.join(".")} is older than ${minRaw}`, {
      min_client_version: minRaw,
    });
  }
  await next();
};
