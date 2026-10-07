// ESLint flat config for the console (review K-21).
//
// `next lint` was removed in Next 16, so `npm run lint` runs ESLint directly
// with eslint-config-next's core-web-vitals preset, which includes the
// react-hooks rules (rules-of-hooks, exhaustive-deps) the editors rely on.
//
// TypeScript 7 shim: this project compiles with TypeScript 7, which no longer
// ships the JavaScript compiler API. typescript-eslint (pulled in by
// eslint-config-next) needs that API and refuses to load against TS 7. The
// TypeScript team's guidance is to keep the TS 6 API available as
// @typescript/typescript6
// (https://devblogs.microsoft.com/typescript/announcing-typescript-7-0/#running-side-by-side-with-typescript-6.0).
// Rather than re-alias the `typescript` dependency for the whole project —
// which would change what `next build` type-checks with — the redirect below
// applies only inside the ESLint process. Remove it once typescript-eslint
// supports TS 7 (https://github.com/typescript-eslint/typescript-eslint/issues/10940).
import { registerHooks } from "node:module";

if (typeof registerHooks !== "function") {
  throw new Error("eslint.config.mjs needs Node.js >= 22.15 (module.registerHooks).");
}
registerHooks({
  resolve(specifier, context, nextResolve) {
    if (specifier === "typescript") return nextResolve("@typescript/typescript6", context);
    return nextResolve(specifier, context);
  },
});

// Imported after the hook is registered, so typescript-eslint sees TS 6.
const { default: nextCoreWebVitals } = await import("eslint-config-next/core-web-vitals");
const { default: nextTypeScript } = await import("eslint-config-next/typescript");

const config = [
  ...nextCoreWebVitals,
  ...nextTypeScript,
  {
    rules: {
      // Hooks rules are errors, not warnings: a violated rule of hooks is a
      // runtime bug, and a stale dependency array is how an editor loses state.
      "react-hooks/rules-of-hooks": "error",
      "react-hooks/exhaustive-deps": "error",
      "@typescript-eslint/no-unused-vars": [
        "error",
        { argsIgnorePattern: "^_", varsIgnorePattern: "^_", caughtErrors: "none" },
      ],
    },
  },
  {
    ignores: [
      ".next/**",
      "node_modules/**",
      "playwright-report/**",
      "test-results/**",
      "coverage/**",
      "next-env.d.ts",
    ],
  },
];

export default config;
