// Path parameters that name a row (pending item 5, defence in depth): a value not in the form the server issues is
// 404 before it reaches a query, so a client bug or a URL normaliser cannot make one stand for something else.
// Read after requireAuth (route handlers), so an unauthenticated request still gets 401 first.
import type { AppContext } from "../env";
import { err } from "./errors";
import { isId, isInviteCode } from "./ids";

/** c.req.param(name) when it is a ULID, else 404 not_found for `what`. */
export function idParam(c: AppContext, name: string, what: string): string {
  const v = c.req.param(name) ?? "";
  if (!isId(v)) throw err.notFound(what);
  return v;
}

/** The :code invite link parameter when it has the invite code form, else 404 not_found (invite). */
export function inviteCodeParam(c: AppContext): string {
  const v = c.req.param("code") ?? "";
  if (!isInviteCode(v)) throw err.notFound("invite");
  return v;
}
