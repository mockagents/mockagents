// Surgical scalar edits to a YAML document, for the guided form (UX-03).
//
// # Why this exists
//
// The obvious way to build a form over YAML is to parse it into an object,
// bind inputs to fields, and serialize the object back. That is also how a
// form silently destroys data: anything the form does not model — spec.chaos,
// spec.tools, streaming config, a field added in a newer server — is absent
// from the object, so it is absent from what gets written. The user sees a
// tidy form, presses Apply, and loses configuration they never looked at.
//
// So the form never serializes the document. The YAML text stays the single
// source of truth, and each control performs a targeted edit of exactly the
// line it owns. Every other byte — including comments, ordering, and fields
// this build has never heard of — is carried through untouched.
//
// # Why line-based rather than a YAML AST
//
// A full AST library would be more general, but this only needs to read and
// replace scalar leaves at known paths. The rule that keeps it honest is that
// it REFUSES anything it cannot do safely (block scalars, flow mappings,
// ambiguous duplicate keys) instead of guessing. The form then disables that
// control with an explanation rather than risking a bad edit.

export type PathOk<T> = { ok: true; value: T };
export type PathErr = { ok: false; reason: string };
export type PathResult<T> = PathOk<T> | PathErr;

const DASH = "- ";

interface Region {
  /** Inclusive first line index of the region being searched. */
  start: number;
  /** Exclusive last line index. */
  end: number;
  /** Indentation of keys directly inside this region. -1 = derive from the
   * first non-blank line. */
  indent: number;
  /** When the region is a list item, the index of its `- ` line, whose text
   * after the dash holds the item's first key. */
  dashLine?: number;
}

function indentOf(line: string): number {
  return line.length - line.trimStart().length;
}

function isBlank(line: string): boolean {
  const t = line.trim();
  return t === "" || t.startsWith("#");
}

/** True when a line's value is a block scalar introducer (`key: |`, `key: >`). */
function isBlockScalar(valuePart: string): boolean {
  return /^[|>][+-]?\d*\s*(#.*)?$/.test(valuePart.trim());
}

/** True when a line's value opens a flow collection (`key: {a: 1}` / `[1,2]`). */
function isFlow(valuePart: string): boolean {
  const v = valuePart.trim();
  return v.startsWith("{") || v.startsWith("[");
}

/** Split `key: value` on a line, honouring quotes so a colon inside a quoted
 * string is not mistaken for the separator. Returns null when the line is not
 * a mapping entry. */
function splitEntry(text: string): { key: string; value: string } | null {
  let quote: string | null = null;
  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    if (quote) {
      if (c === quote && text[i - 1] !== "\\") quote = null;
      continue;
    }
    if (c === '"' || c === "'") {
      quote = c;
      continue;
    }
    if (c === "#") return null; // a comment before any colon
    if (c === ":" && (i + 1 >= text.length || /\s/.test(text[i + 1]))) {
      return { key: text.slice(0, i).trim(), value: text.slice(i + 1) };
    }
  }
  return null;
}

/** Resolve the indentation of the keys directly inside a region. */
function resolveIndent(lines: string[], region: Region): number {
  if (region.indent >= 0) return region.indent;
  for (let i = region.start; i < region.end; i++) {
    if (!isBlank(lines[i])) return indentOf(lines[i]);
  }
  return -1;
}

interface KeyHit {
  line: number;
  /** Column where the key starts (after `- ` for a first-in-item key). */
  keyStart: number;
  value: string;
}

/** Find a mapping key directly inside a region. Duplicates are an error rather
 * than a silent first-match, because editing the wrong one loses data. */
function findKey(lines: string[], region: Region, key: string): PathResult<KeyHit | null> {
  const indent = resolveIndent(lines, region);
  if (indent < 0) return { ok: true, value: null };

  const hits: KeyHit[] = [];

  // A list item's first key shares the `- ` line: `- name: default`.
  if (region.dashLine !== undefined) {
    const line = lines[region.dashLine];
    const dashAt = line.indexOf(DASH);
    if (dashAt >= 0) {
      const after = line.slice(dashAt + DASH.length);
      const entry = splitEntry(after);
      if (entry && entry.key === key) {
        hits.push({ line: region.dashLine, keyStart: dashAt + DASH.length, value: entry.value });
      }
    }
  }

  for (let i = region.start; i < region.end; i++) {
    const line = lines[i];
    if (isBlank(line)) continue;
    const ind = indentOf(line);
    if (ind < indent) break; // dedented out of the region
    if (ind !== indent) continue; // nested deeper: not a direct child
    if (line.trimStart().startsWith(DASH.trim())) continue; // a list item, not a key
    const entry = splitEntry(line.trimStart());
    if (entry && entry.key === key) hits.push({ line: i, keyStart: ind, value: entry.value });
  }

  if (hits.length > 1) {
    return {
      ok: false,
      reason: `the key "${key}" appears more than once at the same level, so an edit would be ambiguous`,
    };
  }
  return { ok: true, value: hits[0] ?? null };
}

/** The region nested under a key line. */
function childRegion(lines: string[], keyLine: number, keyIndent: number): Region {
  let end = lines.length;
  for (let i = keyLine + 1; i < lines.length; i++) {
    if (isBlank(lines[i])) continue;
    if (indentOf(lines[i]) <= keyIndent) {
      end = i;
      break;
    }
  }
  return { start: keyLine + 1, end, indent: -1 };
}

/** The region of the nth list item inside a region. */
function itemRegion(lines: string[], region: Region, index: number): PathResult<Region | null> {
  const indent = resolveIndent(lines, region);
  if (indent < 0) return { ok: true, value: null };

  const starts: number[] = [];
  for (let i = region.start; i < region.end; i++) {
    const line = lines[i];
    if (isBlank(line)) continue;
    const ind = indentOf(line);
    if (ind < indent) break;
    if (ind === indent && line.trimStart().startsWith(DASH.trim())) starts.push(i);
  }
  if (index >= starts.length) return { ok: true, value: null };

  const start = starts[index];
  const end = index + 1 < starts.length ? starts[index + 1] : region.end;
  // Keys inside the item align after the dash.
  return { ok: true, value: { start: start + 1, end, indent: indent + DASH.length, dashLine: start } };
}

/** Walk a dotted path to its final key, returning the containing region and the
 * key hit (null when the key is absent). Numeric segments index a list. */
function locate(
  lines: string[],
  path: string,
): PathResult<{ region: Region; leaf: string; hit: KeyHit | null }> {
  const segments = path.split(".");
  let region: Region = { start: 0, end: lines.length, indent: -1 };

  for (let s = 0; s < segments.length - 1; s++) {
    const seg = segments[s];

    if (/^\d+$/.test(seg)) {
      const item = itemRegion(lines, region, Number(seg));
      if (!item.ok) return item;
      if (!item.value) return { ok: false, reason: `no list item at index ${seg} in "${path}"` };
      region = item.value;
      continue;
    }

    const hit = findKey(lines, region, seg);
    if (!hit.ok) return hit;
    if (!hit.value) return { ok: false, reason: `"${seg}" is not present, so "${path}" cannot be edited here` };
    if (isFlow(hit.value.value)) {
      return { ok: false, reason: `"${seg}" uses inline flow syntax, which this form does not edit` };
    }
    if (hit.value.value.trim() !== "" && !isBlockScalar(hit.value.value)) {
      return { ok: false, reason: `"${seg}" holds a scalar, so "${path}" does not exist` };
    }
    region = childRegion(lines, hit.value.line, indentOf(lines[hit.value.line]));
  }

  const leaf = segments[segments.length - 1];
  const hit = findKey(lines, region, leaf);
  if (!hit.ok) return hit;
  return { ok: true, value: { region, leaf, hit: hit.value } };
}

/** Strip surrounding quotes and a trailing comment from a scalar value. */
function decodeScalar(raw: string): string {
  const t = raw.trim();
  if (t.startsWith('"') && t.endsWith('"') && t.length >= 2) {
    return t.slice(1, -1).replace(/\\"/g, '"').replace(/\\\\/g, "\\");
  }
  if (t.startsWith("'") && t.endsWith("'") && t.length >= 2) {
    return t.slice(1, -1).replace(/''/g, "'");
  }
  return t;
}

/** Quote a value when YAML would otherwise misread it. Conservative: anything
 * that is not plainly safe gets double-quoted. */
export function encodeScalar(value: string): string {
  if (value === "") return '""';
  const needsQuote =
    /^[\s]|[\s]$/.test(value) ||
    /[:#{}[\],&*!|>'"%@`]/.test(value) ||
    /^[-?]/.test(value) ||
    /^(true|false|null|yes|no|on|off|~)$/i.test(value) ||
    /^[0-9.+-]+$/.test(value) ||
    value.includes("\n");
  if (!needsQuote) return value;
  return `"${value.replace(/\\/g, "\\\\").replace(/"/g, '\\"')}"`;
}

/** Read the scalar at a dotted path. `value: null` means the key is absent. */
export function readScalar(yaml: string, path: string): PathResult<string | null> {
  const lines = yaml.split("\n");
  const found = locate(lines, path);
  if (!found.ok) return found;
  const hit = found.value.hit;
  if (!hit) return { ok: true, value: null };
  if (isBlockScalar(hit.value)) {
    return { ok: false, reason: `"${path}" is a multi-line block, which this form does not edit` };
  }
  if (isFlow(hit.value)) {
    return { ok: false, reason: `"${path}" uses inline flow syntax, which this form does not edit` };
  }
  return { ok: true, value: decodeScalar(hit.value) };
}

/** Replace (or insert) the scalar at a dotted path, leaving every other byte of
 * the document untouched. */
export function writeScalar(yaml: string, path: string, value: string): PathResult<string> {
  const lines = yaml.split("\n");
  const found = locate(lines, path);
  if (!found.ok) return found;

  const { region, leaf, hit } = found.value;

  if (hit) {
    if (isBlockScalar(hit.value)) {
      return { ok: false, reason: `"${path}" is a multi-line block, which this form does not edit` };
    }
    if (isFlow(hit.value)) {
      return { ok: false, reason: `"${path}" uses inline flow syntax, which this form does not edit` };
    }
    // Preserve any trailing comment on the line.
    const comment = hit.value.match(/\s+(#.*)$/);
    const line = lines[hit.line];
    const head = line.slice(0, hit.keyStart) + leaf + ": ";
    lines[hit.line] = head + encodeScalar(value) + (comment ? " " + comment[1] : "");
    return { ok: true, value: lines.join("\n") };
  }

  // Absent key: insert it as a new child of an existing parent. We never invent
  // intermediate structure — if the parent is missing, locate() already failed.
  const indent = resolveIndent(lines, region);
  if (indent < 0) {
    return { ok: false, reason: `"${path}" has no sibling to align with, so it cannot be inserted safely` };
  }
  let insertAt = region.start;
  for (let i = region.start; i < region.end; i++) {
    if (!isBlank(lines[i]) && indentOf(lines[i]) >= indent) insertAt = i + 1;
  }
  lines.splice(insertAt, 0, " ".repeat(indent) + leaf + ": " + encodeScalar(value));
  return { ok: true, value: lines.join("\n") };
}

/** The direct child keys of a mapping at a path, in document order.
 *
 * The guided form uses this to tell the user what it is NOT showing them. A
 * form that silently covers three of an agent's nine settings invites the
 * belief that the other six do not exist. */
export function listKeys(yaml: string, path: string): string[] {
  const lines = yaml.split("\n");
  let region: Region = { start: 0, end: lines.length, indent: -1 };
  if (path !== "") {
    for (const seg of path.split(".")) {
      const hit = findKey(lines, region, seg);
      if (!hit.ok || !hit.value) return [];
      region = childRegion(lines, hit.value.line, indentOf(lines[hit.value.line]));
    }
  }
  const indent = resolveIndent(lines, region);
  if (indent < 0) return [];

  const keys: string[] = [];
  for (let i = region.start; i < region.end; i++) {
    const line = lines[i];
    if (isBlank(line)) continue;
    const ind = indentOf(line);
    if (ind < indent) break;
    if (ind !== indent) continue;
    if (line.trimStart().startsWith(DASH.trim())) continue;
    const entry = splitEntry(line.trimStart());
    if (entry) keys.push(entry.key);
  }
  return keys;
}

/** How many items a list at a path has. Used to enumerate scenarios. */
export function countItems(yaml: string, path: string): number {
  const lines = yaml.split("\n");
  const segments = path.split(".");
  let region: Region = { start: 0, end: lines.length, indent: -1 };
  for (const seg of segments) {
    const hit = findKey(lines, region, seg);
    if (!hit.ok || !hit.value) return 0;
    region = childRegion(lines, hit.value.line, indentOf(lines[hit.value.line]));
  }
  const indent = resolveIndent(lines, region);
  if (indent < 0) return 0;
  let n = 0;
  for (let i = region.start; i < region.end; i++) {
    const line = lines[i];
    if (isBlank(line)) continue;
    const ind = indentOf(line);
    if (ind < indent) break;
    if (ind === indent && line.trimStart().startsWith(DASH.trim())) n++;
  }
  return n;
}
