import fs from "node:fs";
import path from "node:path";
import { defineConfig } from "vitest/config";
import { cloudflareTest, readD1Migrations } from "@cloudflare/vitest-pool-workers";
// The workerd that @cloudflare/vitest-pool-workers 0.23.0 runs (via its pinned miniflare 5.20260815) refuses
// compatibility dates newer than its own, so tests run at min(wrangler.jsonc date, runtime date).
import { compatibilityDate as runtimeCompatibilityDate } from "workerd";

const wranglerCompatibilityDate = /"compatibility_date"\s*:\s*"(\d{4}-\d{2}-\d{2})"/.exec(
  fs.readFileSync(path.join(__dirname, "wrangler.jsonc"), "utf8"),
)![1];
const testCompatibilityDate =
  runtimeCompatibilityDate < wranglerCompatibilityDate ? runtimeCompatibilityDate : wranglerCompatibilityDate;

export default defineConfig({
  plugins: [
    cloudflareTest(async () => {
      const migrations = await readD1Migrations(path.join(__dirname, "migrations"));
      return {
        wrangler: { configPath: "./wrangler.jsonc" },
        miniflare: {
          compatibilityDate: testCompatibilityDate,
          bindings: { TEST_MIGRATIONS: migrations, APP_ENV: "test" },
        },
      };
    }),
  ],
  test: {
    setupFiles: ["./test/apply-migrations.ts"],
  },
});
