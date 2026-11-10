// Test fixture for the share engine (not real code).
import { DurableObject } from "cloudflare:workers";

export class Hub extends DurableObject {
  private seats = new Map<number, WebSocket>();

  async fetch(req: Request): Promise<Response> {
    const url = new URL(req.url);
    if (url.pathname === "/connect") {
      return this.connect(req);
    }
    return new Response("not found", { status: 404 });
  }

  private connect(req: Request): Response {
    const pair = new WebSocketPair();
    this.ctx.acceptWebSocket(pair[1]);
    return new Response(null, { status: 101, webSocket: pair[0] });
  }
}
