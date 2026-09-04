// Accessibility assertion backed by axe-core (UX-08 / epic §10: WCAG 2.2 AA).
//
// axe-core is driven directly rather than through a wrapper matcher package so
// the rule set and its limits are explicit and reviewable here.
import axe, { type AxeResults, type ElementContext, type RunOptions } from "axe-core";

// jsdom has no layout engine and no CSSOM cascade, so these rules cannot
// produce a trustworthy verdict here. They are NOT waived — they are the
// Playwright smoke suite's job, where a real engine computes them.
const JSDOM_UNSUPPORTED = ["color-contrast", "target-size"] as const;

const DEFAULT_OPTIONS: RunOptions = {
  runOnly: {
    type: "tag",
    // WCAG 2.2 AA and below, plus axe's best-practice rules for names/roles.
    values: ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"],
  },
  rules: Object.fromEntries(JSDOM_UNSUPPORTED.map((id) => [id, { enabled: false }])),
};

/** Run axe over a container and return its violations. */
export async function axeViolations(
  container: ElementContext,
  options: RunOptions = {},
): Promise<AxeResults["violations"]> {
  const results = await axe.run(container, { ...DEFAULT_OPTIONS, ...options });
  return results.violations;
}

function formatViolations(violations: AxeResults["violations"]): string {
  return violations
    .map((v) => {
      const targets = v.nodes.map((n) => `      at ${n.target.join(" ")}`).join("\n");
      return `  [${v.impact ?? "unknown"}] ${v.id}: ${v.help}\n${targets}`;
    })
    .join("\n");
}

/** Vitest matcher: asserts the given container has no axe violations.
 * Registered globally in test/setup.ts. */
export async function toHaveNoAxeViolations(received: ElementContext) {
  const violations = await axeViolations(received);
  return {
    pass: violations.length === 0,
    message: () =>
      violations.length === 0
        ? "expected accessibility violations, but found none"
        : `expected no accessibility violations, found ${violations.length}:\n${formatViolations(violations)}\n\n` +
          `(color-contrast and target-size are not evaluated under jsdom — see the Playwright smoke suite.)`,
  };
}
