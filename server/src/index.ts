import { createApp } from "./app";
import { handleScheduled } from "./cron";
import type { Env } from "./env";

export { UserHub } from "./do/user-hub";
export { TopicHub } from "./do/topic-hub";

const app = createApp();

export default {
  fetch: app.fetch,
  scheduled: handleScheduled,
} satisfies ExportedHandler<Env>;
