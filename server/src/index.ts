import { createApp } from "./app";
import type { Env } from "./env";

export { UserHub } from "./do/user-hub";
export { TopicHub } from "./do/topic-hub";

const app = createApp();

// plan 02 adds `scheduled: handleScheduled` (server-architecture.md 5).
export default {
  fetch: app.fetch,
} satisfies ExportedHandler<Env>;
