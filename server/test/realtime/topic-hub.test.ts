import { env, runInDurableObject, runDurableObjectAlarm } from "cloudflare:test";
import { describe, it, expect, vi } from "vitest";
import type { OutboundEvent } from "../../src/do/user-hub";
import { newId } from "../../src/lib/ids";
import { connectWs, signup } from "../helpers";

// ---- local fixtures (this file only) ----

type U = { user: { id: string } };

/** Short wait so anything still in flight would have arrived. */
const settle = (ms = 150) => new Promise<void>((r) => setTimeout(r, ms));

async function blockSql(blocker: U, blocked: U): Promise<void> {
  await env.DB.prepare("INSERT INTO blocks (blocker_id, blocked_id, created_at) VALUES (?1, ?2, ?3)").bind(blocker.user.id, blocked.user.id, Date.now()).run();
}

/** Shape only matters to the client; the hubs frame whatever data they get. */
function replyEvent(): OutboundEvent {
  const parentId = newId();
  return { type: "post.reply_created", data: { parent_id: parentId, post: { id: newId(), body: "hi" } }, ts: Date.now(), logged: false };
}

describe("TopicHub", () => {
  it("watch -> publish reaches only watching sockets", async () => {
    const u = await signup(); const c1 = await connectWs(u.token); const c2 = await connectWs(u.token); const topic = `post:${newId()}`;
    c1.send({ type: "watch", ref: "w", data: { topics: [topic] } }); await c1.next("reply", (d) => d.ref === "w");
    const n = await env.TOPIC_HUB.getByName(topic).publish(topic, [{ type: "post.stats", data: { post_id: topic.slice(5), like_count: 1, repost_count: 0, reply_count: 0, quote_count: 0 }, ts: Date.now(), logged: false }]);
    expect(n).toBe(1); expect((await c1.next("post.stats")).seq).toBeNull();
    await settle(); expect(c2.frames.some((f) => f.type === "post.stats")).toBe(false);
  });

  it("visibleTo filters blocked watcher", async () => {
    const [author, w] = await Promise.all([signup(), signup()]); await blockSql(author, w); const topic = `post:${newId()}`;
    await env.TOPIC_HUB.getByName(topic).join(w.user.id, 300_000);
    expect(await env.TOPIC_HUB.getByName(topic).publish(topic, [replyEvent()], { visibleTo: { authors: [author.user.id], privateAuthors: [] } })).toBe(0);
  });

  it(">50 topics -> limit_exceeded; empty list leaves; lease expiry purges", async () => {
    const u = await signup(); const c = await connectWs(u.token);
    c.send({ type: "watch", ref: "big", data: { topics: Array.from({ length: 51 }, () => `post:${newId()}`) } });
    expect((await c.next("reply", (d) => d.ref === "big")).data.error.code).toBe("limit_exceeded");
    const topic = `post:${newId()}`; c.send({ type: "watch", ref: "a", data: { topics: [topic] } }); await c.next("reply", (d) => d.ref === "a");
    c.send({ type: "watch", ref: "b", data: { topics: [] } }); await c.next("reply", (d) => d.ref === "b");
    expect(await env.TOPIC_HUB.getByName(topic).inspect()).toEqual({ watchers: 0 });
    const t2 = `post:${newId()}`; const stub = env.TOPIC_HUB.getByName(t2); await stub.join(u.user.id, 1000);
    await runInDurableObject(stub, (inst) => { (inst as any).now = () => Date.now() + 2000; }); await runDurableObjectAlarm(stub);
    expect(await stub.inspect()).toEqual({ watchers: 0 });
  });

  it("a failed deliver logs one object: constant msg plus topic/target/err as fields (pending item 7)", async () => {
    const topic = `post:${newId()}`; const target = "u_x\nFAKE LOG LINE %s"; const stub = env.TOPIC_HUB.getByName(topic);
    await stub.join(target, 300_000);
    const spy = vi.spyOn(console, "error").mockImplementation(() => {});
    try {
      const n = await runInDurableObject(stub, (inst) => {
        const boom = new Error("deliver boom");
        (inst as any).env = { ...(inst as any).env, USER_HUB: { getByName: () => ({ deliver: () => Promise.reject(boom) }) } };
        return inst.publish(topic, [replyEvent()]);
      });
      expect(n).toBe(0);
      expect(spy).toHaveBeenCalledTimes(1);
      expect(spy.mock.calls[0]).toHaveLength(1);
      const [fields] = spy.mock.calls[0];
      expect(fields).toMatchObject({ msg: "TopicHub deliver failed", topic, target, err: { name: "Error", message: "deliver boom" } });
      expect(Object.keys(fields)[0]).toBe("msg");
    } finally {
      spy.mockRestore();
    }
  });
});
