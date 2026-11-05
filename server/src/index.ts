export { UserHub } from "./do/user-hub";
export { TopicHub } from "./do/topic-hub";

export default {
  fetch: () => new Response("not found", { status: 404 }),
} satisfies ExportedHandler<Cloudflare.Env>;
