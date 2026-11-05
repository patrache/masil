import { DurableObject } from "cloudflare:workers";

export class TopicHub extends DurableObject<Cloudflare.Env> {}
