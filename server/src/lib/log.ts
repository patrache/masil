// Log shape (server-architecture.md 3.6): every console.* call is ONE structured object whose first key is a constant
// message, `console.error({ msg: "TopicHub deliver failed", topic, target, err: errFields(e) })`. Values never go into
// the message (no template literals or concatenation) and nothing follows the object: a request path, topic or error
// text spliced into a message string could forge or split log lines. The single-object form is the shape the Workers
// Logs docs show being indexed as filterable fields (developers.cloudflare.com/workers/observability/logs/workers-logs/,
// "log in JSON format"); a two-argument `console.error("msg", {...})` is not documented there.

const UNPRINTABLE = "[unprintable thrown value]";

/** String(v) that cannot throw (String() throws on Object.create(null), a throwing toString, a toPrimitive that
 *  returns an object, ...). */
function safeString(v: unknown): string {
  try {
    return typeof v === "string" ? v : String(v);
  } catch {
    return UNPRINTABLE;
  }
}

/** A thrown value as plain JSON fields: an Error's name/message/stack are own non-enumerable properties, so inside a
 *  fields object it would JSON-serialise as `{}`; they are copied out as strings.
 *  Total: it runs inside catch blocks that must never throw (blockConcurrencyWhile resets the object on a throw, a
 *  .catch handler's throw becomes an unhandled waitUntil rejection), so any access that throws is swallowed. */
export function errFields(e: unknown): { name?: string; message: string; stack?: string } {
  let isError: boolean;
  try {
    isError = e instanceof Error;
  } catch {
    return { message: UNPRINTABLE };
  }
  if (!isError) return { message: safeString(e) };
  const err = e as Error;
  const read = (k: "name" | "message" | "stack"): string | undefined => {
    try {
      const v: unknown = err[k];
      return v === undefined ? undefined : safeString(v);
    } catch {
      return UNPRINTABLE;
    }
  };
  const out: { name?: string; message: string; stack?: string } = { message: read("message") ?? "" };
  const name = read("name");
  const stack = read("stack");
  if (name !== undefined) out.name = name;
  if (stack !== undefined) out.stack = stack;
  return out;
}
