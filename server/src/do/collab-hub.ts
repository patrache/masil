// CollabHub (v0.1.3, server-architecture.md 3.1, docs/contract/collab.md): one Durable Object per room document,
// name "doc:<doc_id>". P0 Task 26 reserves the class, binding COLLAB_HUB and the RPC surface; Task 27 fills it in.
import { DurableObject } from "cloudflare:workers";

export type DocKickReason = "left" | "kicked" | "banned" | "space_deleted" | "doc_deleted";
export type DocConnRole = "creator" | "admin" | "member";

export class CollabHub extends DurableObject<Cloudflare.Env> {
  async fetch(_request: Request): Promise<Response> {
    return new Response("document sockets are not implemented yet", { status: 501 });
  }

  /** First state of a new document; returns the head seq. */
  async init(_docType: "arch" | "apicol", _snapshot: unknown | null): Promise<number> {
    return 0;
  }

  async kick(_userId: string, _reason: DocKickReason): Promise<void> {}

  async closeAll(_reason: DocKickReason): Promise<void> {}

  async setLocked(_locked: boolean): Promise<void> {}

  async head(): Promise<number> {
    return 0;
  }

  async revokeSession(_userId: string, _sessionIds: string[] | "all", _code: 4001 | 4003): Promise<void> {}

  async setRole(_userId: string, _role: "admin" | "member"): Promise<void> {}
}
