import { DurableObject } from "cloudflare:workers";

export class UserHub extends DurableObject<Cloudflare.Env> {}
