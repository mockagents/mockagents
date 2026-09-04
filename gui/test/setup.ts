// Global test setup for the GUI component harness (UX-08).
import "@testing-library/jest-dom/vitest";

import { cleanup } from "@testing-library/react";
import { afterEach, expect } from "vitest";

import { toHaveNoAxeViolations } from "./a11y";

// Unmount between tests so a leaked tree can't satisfy a later query.
afterEach(() => {
  cleanup();
});

// WCAG 2.2 AA is an epic-level acceptance gate (§10), so the axe assertion is
// registered globally rather than imported ad hoc — a11y coverage should be
// cheap to add to any new test.
expect.extend({ toHaveNoAxeViolations });

declare module "vitest" {
  interface Assertion {
    toHaveNoAxeViolations(): Promise<void>;
  }
  interface AsymmetricMatchersContaining {
    toHaveNoAxeViolations(): Promise<void>;
  }
}
