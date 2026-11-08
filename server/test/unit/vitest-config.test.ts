// v0.1.1 S4: Vite's native config loader (configLoader: 'native', planned default) has no CommonJS `__dirname` /
// `__filename`; vitest.config.ts uses `import.meta.dirname` / `import.meta.filename`, which both loaders provide.
import { it, expect } from "vitest";

const configs = import.meta.glob("../../vitest.config.ts", { query: "?raw", import: "default", eager: true }) as Record<string, string>;

it("vitest.config.ts uses import.meta.dirname, not __dirname / __filename", () => {
  const src = configs["../../vitest.config.ts"];
  expect(src).toBeDefined();
  const code = src!.replace(/\/\/.*$/gm, ""); // comments may name the old globals
  expect(code).not.toMatch(/\b__dirname\b|\b__filename\b/);
  expect(code).toMatch(/import\.meta\.dirname/);
});
