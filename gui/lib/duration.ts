// Duration formatting for values that arrive from Go.
//
// Deliberately in its own module rather than in lib/api.ts: api.ts imports
// next/headers to read the auth cookie, which makes it server-only. A client
// component that imports a runtime value from it drags next/headers into the
// browser bundle and the build fails. Types are fine (they are erased);
// functions are not.

/** Convert a Go duration to milliseconds.
 *
 * Go marshals time.Duration as an integer count of NANOSECONDS. Reading the
 * number as milliseconds under-reports by a factor of a million, which is the
 * kind of mistake that makes a mock look instant. */
export function nsToMs(ns: number): number {
  return ns / 1_000_000;
}

/** Format a Go duration for humans.
 *
 * Three honesty rules, each from something a mock server actually produces:
 *
 *  - A value that is not a sane duration is "unknown", never rendered as 0.
 *  - Below a microsecond is "<1µs", not "0µs". The engine really does report
 *    latency: 0 for an in-memory call — Go's clock cannot resolve it — and
 *    "0µs" would assert a measurement of no elapsed time. "<1µs" says what is
 *    actually known: it was faster than the timer can see.
 *  - Sub-millisecond runs stay in microseconds rather than collapsing to
 *    "0ms", which reads as "not measured". */
export function formatDuration(ns: number): string {
  if (!Number.isFinite(ns) || ns < 0) return "unknown";
  if (ns < 1000) return "<1µs";
  const ms = nsToMs(ns);
  if (ms < 1) return `${Math.round(ns / 1000)}µs`;
  if (ms < 1000) return `${ms.toFixed(ms < 10 ? 1 : 0)}ms`;
  return `${(ms / 1000).toFixed(2)}s`;
}
