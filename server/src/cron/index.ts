// Cron entry (server-architecture.md 5, data-model.md 6): dispatch on controller.cron.
// Errors propagate so a failed run shows up in the Workers dashboard.
import type { Env } from "../env";
import { runCleanup } from "./cleanup";
import { recomputeTrends } from "./trends";

export async function handleScheduled(controller: ScheduledController, env: Env, _ctx: ExecutionContext): Promise<void> {
  switch (controller.cron) {
    case "*/5 * * * *":
      await recomputeTrends(env);
      return;
    case "17 * * * *":
      await runCleanup(env);
      return;
    default:
      console.warn("unknown cron", controller.cron);
  }
}
