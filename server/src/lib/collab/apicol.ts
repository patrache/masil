// Doc type `apicol` (docs/contract/collab.md 7): a shared API collection. The server checks the shape and the sizes only —
// it never looks for secrets in the content (design C3.12); the client's gate does that before an op leaves.
// maxFieldBytes 61,440 keeps one `set` inside a 65,536-byte client frame (collab.md 5).
import type { DocStateView, DocTypeSpec, Json, Op, RejectReason } from "../collab-doctypes";

/** JSON shape of a field value (`null` is always accepted: it is how a client clears a field). */
type FieldKind = "string" | "number" | "boolean" | "vars" | "auth" | "settings" | "extra" | "list" | "body_kind";

/** Object type -> its fields (collab.md 7.1; the client's `internal/apitool` shared.go writes the same table). */
export const APICOL_FIELDS: Record<string, Record<string, FieldKind>> = {
  col: { name: "string", vars: "vars" },
  folder: { name: "string", parent: "string", order: "number" },
  request: {
    name: "string",
    folder: "string",
    order: "number",
    method: "string",
    url: "string",
    body_kind: "body_kind",
    body: "string",
    content_type: "string",
    auth: "auth",
    settings: "settings",
  },
  row: { req: "string", list: "list", order: "number", key: "string", value: "string", off: "boolean", extra: "extra" },
  env: { name: "string", order: "number" },
  envvar: { env: "string", name: "string", secret: "boolean", order: "number", value: "string" },
};

/** Object id prefix -> type (`col` is the one id "col"). */
const PREFIXES: [string, string][] = [
  ["folder:", "folder"],
  ["req:", "request"],
  ["row:", "row"],
  ["envvar:", "envvar"],
  ["env:", "env"],
];

const MAX_REQUESTS = 1000;
const MAX_BODY_BYTES = 65_536;
const BODY_KINDS = new Set(["", "none", "json", "urlencoded", "multipart", "raw"]);
const LISTS = new Set(["query", "header", "form", "assert", "extract"]);
const AUTH_KEYS = new Set(["kind", "user", "pass", "token", "name", "value", "in"]);
const SETTINGS: Record<string, "number" | "boolean"> = { timeout_ms: "number", no_follow: "boolean", max_redirects: "number", insecure: "boolean" };
const EXTRA_KEYS = new Set(["file_name", "op", "path", "from"]);
const VAR_KEYS = new Set(["id", "name", "value", "secret"]);

const INVALID: RejectReason = { code: "invalid", detail: "invalid_op" };
const SECRET_VALUE: RejectReason = { code: "invalid", detail: "secret_value" };
const BODY_TOO_LARGE: RejectReason = { code: "too_large", detail: "body_too_large" };
const LIMIT_REQUESTS: RejectReason = { code: "limit_exceeded", detail: "limit_requests" };

const utf8 = new TextEncoder();

/** The type an object id must have, or null when the id names no apicol object. */
export function typeOfId(id: string): string | null {
  if (id === "col") return "col";
  for (const [p, t] of PREFIXES) if (id.startsWith(p) && id.length > p.length) return t;
  return null;
}

const isObj = (v: Json): v is { [k: string]: Json } => typeof v === "object" && v !== null && !Array.isArray(v);
const isNum = (v: Json): boolean => typeof v === "number" && Number.isFinite(v);

function stringsOnly(v: { [k: string]: Json }, keys: Set<string>): boolean {
  return Object.keys(v).every((k) => keys.has(k) && typeof v[k] === "string");
}

/** A field value of the right JSON shape; secret col vars carry no value. Returns the refusal or null. */
function checkValue(kind: FieldKind, v: Json): RejectReason | null {
  if (v === null) return null;
  switch (kind) {
    case "string":
      return typeof v === "string" ? null : INVALID;
    case "number":
      return isNum(v) ? null : INVALID;
    case "boolean":
      return typeof v === "boolean" ? null : INVALID;
    case "body_kind":
      return typeof v === "string" && BODY_KINDS.has(v) ? null : INVALID;
    case "list":
      return typeof v === "string" && LISTS.has(v) ? null : INVALID;
    case "auth":
      return isObj(v) && stringsOnly(v, AUTH_KEYS) ? null : INVALID;
    case "extra":
      return isObj(v) && stringsOnly(v, EXTRA_KEYS) ? null : INVALID;
    case "settings":
      if (!isObj(v)) return INVALID;
      for (const [k, x] of Object.entries(v)) {
        const want = SETTINGS[k];
        if (want === undefined || (want === "number" ? !isNum(x) : typeof x !== "boolean")) return INVALID;
      }
      return null;
    case "vars": {
      if (!Array.isArray(v)) return INVALID;
      for (const x of v) {
        if (!isObj(x) || typeof x.name !== "string") return INVALID;
        for (const [k, y] of Object.entries(x)) {
          if (!VAR_KEYS.has(k)) return INVALID;
          if (k === "secret" ? typeof y !== "boolean" : y !== null && typeof y !== "string") return INVALID;
        }
        if (x.secret === true && typeof x.value === "string" && x.value !== "") return SECRET_VALUE;
      }
      return null;
    }
  }
}

/** Every field of a would-be object: known, of the right shape, the body within its size. */
function checkFields(type: string, fields: Record<string, Json>): RejectReason | null {
  const table = APICOL_FIELDS[type];
  if (table === undefined) return INVALID;
  for (const [k, v] of Object.entries(fields)) {
    const kind = table[k];
    if (kind === undefined) return INVALID;
    const r = checkValue(kind, v);
    if (r !== null) return r;
    if (type === "request" && k === "body" && typeof v === "string" && utf8.encode(v).length > MAX_BODY_BYTES) return BODY_TOO_LARGE;
  }
  return null;
}

/** An envvar that would hold `secret: true` and a non-empty value. */
function secretWithValue(fields: Record<string, Json>): boolean {
  return fields.secret === true && typeof fields.value === "string" && fields.value !== "";
}

function validateReset(objects: { id: string; type: string; fields: Record<string, Json>; deleted?: boolean }[]): RejectReason | null {
  let requests = 0;
  for (const o of objects) {
    if (typeOfId(o.id) !== o.type) return INVALID;
    const r = checkFields(o.type, o.fields);
    if (r !== null) return r;
    if (o.type === "envvar" && secretWithValue(o.fields)) return SECRET_VALUE;
    if (o.type === "request" && o.deleted !== true && ++requests > MAX_REQUESTS) return LIMIT_REQUESTS;
  }
  return null;
}

/** One op on the state so far (collab.md 7.2); the hub's common limits (field, document) come after. */
export function validateOp(op: Op, state: DocStateView): RejectReason | null {
  switch (op.op) {
    case "reset":
      return validateReset(op.snapshot.objects);
    case "put": {
      if (typeOfId(op.id) !== op.type) return INVALID;
      const r = checkFields(op.type, op.fields);
      if (r !== null) return r;
      const cur = state.get(op.id);
      if (cur !== undefined && cur.deleted) return null; // the hub ignores a put on a deleted object
      if (op.type === "envvar" && secretWithValue({ ...(cur?.fields ?? {}), ...op.fields })) return SECRET_VALUE;
      if (op.type === "request" && cur === undefined && state.count("request") >= MAX_REQUESTS) return LIMIT_REQUESTS;
      return null;
    }
    case "set": {
      const type = typeOfId(op.id);
      if (type === null) return INVALID;
      const r = checkFields(type, { [op.field]: op.value });
      if (r !== null) return r;
      const cur = state.get(op.id);
      if (type === "envvar" && cur !== undefined && !cur.deleted && secretWithValue({ ...cur.fields, [op.field]: op.value })) return SECRET_VALUE;
      return null;
    }
    case "del":
    case "undel": {
      const type = typeOfId(op.id);
      if (type === null) return INVALID;
      if (op.op === "undel" && type === "request") {
        const cur = state.get(op.id);
        if (cur !== undefined && cur.deleted && state.count("request") >= MAX_REQUESTS) return LIMIT_REQUESTS;
      }
      return null;
    }
  }
}

export const docType: DocTypeSpec = {
  type: "apicol",
  maxDocBytes: 2_097_152,
  maxObjects: { request: MAX_REQUESTS, col: 1 },
  maxFieldBytes: 61_440,
  validateOp,
};
