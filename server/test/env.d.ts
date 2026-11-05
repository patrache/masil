import type { D1Migration } from "cloudflare:test";
declare global {
  namespace Cloudflare {
    interface Env { TEST_MIGRATIONS: D1Migration[] }
  }
  /** Vite's import.meta.glob (vite/types/importGlob.d.ts), narrowed to the eager `?raw` form the tests use. */
  interface ImportMeta {
    glob(pattern: string | string[], options: { query: "?raw"; import: "default"; eager: true }): Record<string, string>;
  }
}
export {};
