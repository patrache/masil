// Log shape (server-architecture.md 3.6): every console.* call is a constant message plus at most one fields object,
// `console.error("TopicHub deliver failed", { topic, target, err: errFields(e) })`. Values never go into the message
// (no template literals, concatenation or extra positional args): a request path, topic or error text spliced there
// could forge or split log lines, and a `%` in the first argument is a console format directive. Workers Logs indexes
// the object's keys as fields (developers.cloudflare.com/workers/observability/logs/workers-logs/).

/** A thrown value as plain JSON fields: an Error's name/message/stack are own non-enumerable properties, so inside a
 *  fields object it would JSON-serialise as `{}`; they are copied out as strings. */
export function errFields(e: unknown): { name?: string; message: string; stack?: string } {
  if (e instanceof Error) return { name: e.name, message: e.message, stack: e.stack };
  return { message: String(e) };
}
