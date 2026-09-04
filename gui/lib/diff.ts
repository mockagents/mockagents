// Line diff for the agent editor's change preview (UX-03).
//
// Written here rather than pulled from a package: the whole algorithm is ~40
// lines, and a diff shown before a destructive-ish action is something we want
// to be able to read and test directly.

export type DiffKind = "same" | "add" | "del";

export interface DiffLine {
  kind: DiffKind;
  text: string;
  /** 1-based line number in the original, for "same" and "del". */
  leftNo?: number;
  /** 1-based line number in the draft, for "same" and "add". */
  rightNo?: number;
}

/** Above this many lines the quadratic LCS table stops being reasonable. Agent
 * documents are far smaller; the cap exists so a pasted monster cannot hang the
 * browser. Past it the caller gets a truthful "too large to diff" rather than a
 * frozen tab. */
export const MAX_DIFF_LINES = 3000;

export interface DiffResult {
  lines: DiffLine[];
  added: number;
  removed: number;
  /** True when the inputs were too large and `lines` is empty. */
  tooLarge: boolean;
}

/** Diff two documents by line. */
export function diffLines(before: string, after: string): DiffResult {
  const a = before.split("\n");
  const b = after.split("\n");

  if (a.length > MAX_DIFF_LINES || b.length > MAX_DIFF_LINES) {
    return { lines: [], added: 0, removed: 0, tooLarge: true };
  }

  // Longest common subsequence table.
  const lcs: number[][] = Array.from({ length: a.length + 1 }, () =>
    new Array<number>(b.length + 1).fill(0),
  );
  for (let i = a.length - 1; i >= 0; i--) {
    for (let j = b.length - 1; j >= 0; j--) {
      lcs[i][j] = a[i] === b[j] ? lcs[i + 1][j + 1] + 1 : Math.max(lcs[i + 1][j], lcs[i][j + 1]);
    }
  }

  const lines: DiffLine[] = [];
  let added = 0;
  let removed = 0;
  let i = 0;
  let j = 0;
  while (i < a.length && j < b.length) {
    if (a[i] === b[j]) {
      lines.push({ kind: "same", text: a[i], leftNo: i + 1, rightNo: j + 1 });
      i++;
      j++;
    } else if (lcs[i + 1][j] >= lcs[i][j + 1]) {
      lines.push({ kind: "del", text: a[i], leftNo: i + 1 });
      removed++;
      i++;
    } else {
      lines.push({ kind: "add", text: b[j], rightNo: j + 1 });
      added++;
      j++;
    }
  }
  while (i < a.length) {
    lines.push({ kind: "del", text: a[i], leftNo: i + 1 });
    removed++;
    i++;
  }
  while (j < b.length) {
    lines.push({ kind: "add", text: b[j], rightNo: j + 1 });
    added++;
    j++;
  }

  return { lines, added, removed, tooLarge: false };
}

/** Collapse long runs of unchanged lines, keeping `context` around each change.
 * Returns the kept lines with `null` marking each elision. */
export function collapseUnchanged(
  lines: DiffLine[],
  context = 3,
): Array<DiffLine | null> {
  const keep = new Array<boolean>(lines.length).fill(false);
  lines.forEach((line, idx) => {
    if (line.kind === "same") return;
    for (let k = Math.max(0, idx - context); k <= Math.min(lines.length - 1, idx + context); k++) {
      keep[k] = true;
    }
  });

  const out: Array<DiffLine | null> = [];
  let eliding = false;
  lines.forEach((line, idx) => {
    if (keep[idx]) {
      out.push(line);
      eliding = false;
    } else if (!eliding) {
      out.push(null);
      eliding = true;
    }
  });
  return out;
}
