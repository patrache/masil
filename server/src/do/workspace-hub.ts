// WorkspaceHub (v0.1.3, server-architecture.md 3.1): relays P2 workspace-share ciphertext — the server never sees the
// content (docs/contract/workspace.md). P0 Task 26 only reserves the class, binding WORKSPACE_HUB and migration tag v2;
// P2 fills it in. Kept apart from CollabHub on purpose: a DO that stores plaintext and one that must not see it never
// share a class (v0.1.3 design C1.1).
import { DurableObject } from "cloudflare:workers";

export class WorkspaceHub extends DurableObject<Cloudflare.Env> {
  async fetch(_request: Request): Promise<Response> {
    return new Response("workspace shares are not implemented yet", { status: 501 });
  }
}
