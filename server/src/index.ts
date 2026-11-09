import { createApp } from "./app";
import { handleScheduled } from "./cron";
import type { Env } from "./env";

export { UserHub } from "./do/user-hub";
export { TopicHub } from "./do/topic-hub";
export { CollabHub } from "./do/collab-hub"; // v0.1.3 room documents
export { WorkspaceHub } from "./do/workspace-hub"; // v0.1.3 slot, filled by P2

const app = createApp();

export default {
  fetch: app.fetch,
  scheduled: handleScheduled,
} satisfies ExportedHandler<Env>;
