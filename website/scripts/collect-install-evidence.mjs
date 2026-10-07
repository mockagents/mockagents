import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import {
  existsSync,
  lstatSync,
  mkdirSync,
  readFileSync,
  realpathSync,
  writeFileSync,
} from 'node:fs';
import { dirname, isAbsolute, relative, resolve, sep } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const repositoryRoot = resolve(
  dirname(fileURLToPath(import.meta.url)),
  '../..',
);
const revisionPattern = /^[a-f0-9]{40}$/;
const digestPattern = /^[a-f0-9]{64}$/;
const semverPattern =
  /^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$/;
const idPattern = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;
const platformOs = new Map([
  ['linux', 'linux'],
  ['Linux', 'linux'],
  ['macos', 'macos'],
  ['macOS', 'macos'],
  ['windows', 'windows'],
  ['Windows', 'windows'],
]);
const platformArch = new Map([
  ['amd64', 'amd64'],
  ['x64', 'amd64'],
  ['X64', 'amd64'],
  ['arm64', 'arm64'],
  ['ARM64', 'arm64'],
]);

function sha256(value) {
  return createHash('sha256').update(value).digest('hex');
}

function canonicalize(value) {
  if (Array.isArray(value)) return value.map(canonicalize);
  if (value && typeof value === 'object') {
    return Object.fromEntries(
      Object.keys(value)
        .sort()
        .map((key) => [key, canonicalize(value[key])]),
    );
  }
  return value;
}

function canonicalJson(value) {
  return JSON.stringify(canonicalize(value));
}

function exactKeys(value, fields, context) {
  assert(
    value && typeof value === 'object' && !Array.isArray(value),
    `${context} must be an object`,
  );
  assert.deepEqual(
    Object.keys(value).sort(),
    [...fields].sort(),
    `${context} has unexpected or missing fields`,
  );
}

function inside(root, path) {
  const rel = relative(root, path);
  return (
    rel === '' ||
    (!rel.startsWith(`..${sep}`) && rel !== '..' && !isAbsolute(rel))
  );
}

function safeRepoPath(path, context) {
  assert(
    typeof path === 'string' &&
      path.length > 0 &&
      !path.includes('\\') &&
      !path.split('/').some((part) => ['', '.', '..'].includes(part)),
    `${context} is not a safe repository path`,
  );
  return path;
}

function readRegularFile(root, path, context) {
  safeRepoPath(path, context);
  const absolute = resolve(root, path);
  assert(inside(root, absolute), `${context} escapes the repository`);
  assert(existsSync(absolute), `${context} is missing: ${path}`);
  assert(lstatSync(absolute).isFile(), `${context} is not a regular file`);
  assert.equal(
    realpathSync.native(absolute),
    absolute,
    `${context} resolves through an unexpected link`,
  );
  return { absolute, bytes: readFileSync(absolute) };
}

function parseTimestamp(value, context) {
  assert.equal(typeof value, 'string', `${context} must be a string`);
  const parsed = Date.parse(value);
  assert(
    Number.isFinite(parsed) && new Date(parsed).toISOString() === value,
    `${context} must be a canonical ISO 8601 UTC timestamp`,
  );
  return value;
}

function readIdFile(root, path, context) {
  const { bytes } = readRegularFile(root, path, context);
  const ids = bytes
    .toString('utf8')
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter((line) => line && !line.startsWith('#'));
  assert(ids.length > 0, `${context} contains no IDs`);
  assert.equal(
    new Set(ids).size,
    ids.length,
    `${context} contains duplicate IDs`,
  );
  for (const id of ids)
    assert(idPattern.test(id), `${context} has invalid ID ${id}`);
  return ids;
}

function parsePlatform(bytes, context) {
  const platform = JSON.parse(bytes.toString('utf8'));
  exactKeys(platform, ['os', 'arch', 'checkedAt'], context);
  const os = platformOs.get(platform.os);
  const arch = platformArch.get(platform.arch);
  assert(os, `${context} has unsupported OS ${platform.os}`);
  assert(arch, `${context} has unsupported architecture ${platform.arch}`);
  return {
    os,
    arch,
    checkedAt: parseTimestamp(platform.checkedAt, `${context} checkedAt`),
  };
}

function parseResultRows(bytes, context) {
  const rows = [];
  for (const [index, raw] of bytes.toString('utf8').split(/\r?\n/).entries()) {
    if (raw === '') continue;
    const fields = raw.split('|');
    assert.equal(
      fields.length,
      4,
      `${context}:${index + 1} must have four fields`,
    );
    const [id, status, observed, description] = fields;
    assert(idPattern.test(id), `${context}:${index + 1} has invalid ID`);
    assert(
      status === 'ok' || status === 'fail',
      `${context}:${index + 1} has invalid status`,
    );
    assert(observed !== '', `${context}:${index + 1} has no observed value`);
    assert(
      observed === 'none' || semverPattern.test(observed),
      `${context}:${index + 1} has invalid observed version`,
    );
    assert(
      description.trim() === description &&
        /^[\x20-\x7e]{1,200}$/.test(description),
      `${context}:${index + 1} has invalid description`,
    );
    rows.push({ id, status, observed, description });
  }
  assert(rows.length > 0, `${context} contains no result rows`);
  return rows;
}

function normalizeMetadata(metadata) {
  exactKeys(
    metadata,
    ['schemaVersion', 'target', 'collectedAt', 'snapshotPath', 'runs'],
    'Install collection metadata',
  );
  assert.equal(
    metadata.schemaVersion,
    1,
    'Unsupported install metadata version',
  );
  exactKeys(metadata.target, ['tag', 'version', 'revision'], 'Install target');
  assert(
    semverPattern.test(metadata.target.version),
    'Install target version must be exact SemVer',
  );
  assert.equal(
    metadata.target.tag,
    `v${metadata.target.version}`,
    'Install target tag must equal v plus the exact version',
  );
  assert(
    revisionPattern.test(metadata.target.revision),
    'Install target revision must be a full lowercase commit SHA',
  );
  parseTimestamp(metadata.collectedAt, 'Install collection time');
  safeRepoPath(metadata.snapshotPath, 'Install snapshot path');
  assert(
    metadata.snapshotPath.startsWith('website/manifests/install-evidence/') &&
      metadata.snapshotPath.endsWith('.json'),
    'Install snapshot must live under website/manifests/install-evidence',
  );
  assert(
    Array.isArray(metadata.runs) && metadata.runs.length > 0,
    'Install metadata requires at least one run',
  );
  for (const [index, run] of metadata.runs.entries()) {
    exactKeys(run, ['resultPath', 'platformPath'], `Install run ${index + 1}`);
    safeRepoPath(run.resultPath, `Install run ${index + 1} resultPath`);
    safeRepoPath(run.platformPath, `Install run ${index + 1} platformPath`);
  }
  return metadata;
}

export function buildInstallEvidenceCandidate({
  root = repositoryRoot,
  metadata,
  requiredPath = '.github/install-paths-required.txt',
  pendingPath = '.github/install-paths-pending.txt',
} = {}) {
  normalizeMetadata(metadata);
  const requiredIds = readIdFile(root, requiredPath, 'Required install paths');
  const pendingIds = readIdFile(root, pendingPath, 'Pending install paths');
  const requiredDeclaration = readRegularFile(
    root,
    requiredPath,
    'Required install paths',
  );
  const pendingDeclaration = readRegularFile(
    root,
    pendingPath,
    'Pending install paths',
  );
  const declarationDigests = {
    required: {
      path: requiredPath,
      sha256: sha256(requiredDeclaration.bytes),
    },
    pending: {
      path: pendingPath,
      sha256: sha256(pendingDeclaration.bytes),
    },
  };
  const required = new Set(requiredIds);
  for (const id of pendingIds) {
    assert(required.has(id), `Pending install path is not required: ${id}`);
  }

  const inputDigests = [];
  const collectedRows = new Map();
  for (const [index, run] of metadata.runs.entries()) {
    const result = readRegularFile(
      root,
      run.resultPath,
      `Install run ${index + 1} results`,
    );
    const platformFile = readRegularFile(
      root,
      run.platformPath,
      `Install run ${index + 1} platform`,
    );
    const platform = parsePlatform(
      platformFile.bytes,
      `Install run ${index + 1} platform`,
    );
    const resultSha256 = sha256(result.bytes);
    const platformSha256 = sha256(platformFile.bytes);
    inputDigests.push({
      resultPath: run.resultPath,
      resultSha256,
      platformPath: run.platformPath,
      platformSha256,
    });
    for (const row of parseResultRows(
      result.bytes,
      `Install run ${index + 1} results`,
    )) {
      assert(required.has(row.id), `Unexpected install result ID ${row.id}`);
      assert(
        !collectedRows.has(row.id),
        `Duplicate install result ID ${row.id}`,
      );
      collectedRows.set(row.id, {
        row,
        platform,
        resultSha256,
        platformSha256,
      });
    }
  }

  for (const id of requiredIds) {
    assert(collectedRows.has(id), `Missing install result ID ${id}`);
  }

  inputDigests.sort((left, right) =>
    left.resultPath.localeCompare(right.resultPath),
  );
  const checks = [];
  const records = requiredIds.map((id) => {
    const { row, platform, resultSha256, platformSha256 } =
      collectedRows.get(id);
    const exact =
      row.status === 'ok' && row.observed === metadata.target.version;
    const evidenceDigest = sha256(
      canonicalJson({
        schemaVersion: 1,
        target: metadata.target,
        platform,
        row,
        resultSha256,
        platformSha256,
      }),
    );
    assert(digestPattern.test(evidenceDigest));
    checks.push({
      channel: id,
      result: row,
      platform,
      resultSha256,
      platformSha256,
      evidenceDigest,
    });
    const safeVersion = metadata.target.version.replace(/[^0-9A-Za-z-]/g, '-');
    const reason = exact
      ? `Exact-version ${row.description} probe reported ${row.observed} on ${platform.os}/${platform.arch}.`
      : `The ${row.description} probe did not verify ${metadata.target.version} on ${platform.os}/${platform.arch}; observed ${row.observed}.`;
    return {
      id: `${id}-${safeVersion}-${platform.os}-${platform.arch}-${evidenceDigest.slice(0, 12)}`,
      channel: id,
      version: metadata.target.version,
      revision: metadata.target.revision,
      os: platform.os,
      arch: platform.arch,
      status: exact ? 'verified' : 'failed',
      checkedAt: platform.checkedAt,
      evidenceDigest,
      source: metadata.snapshotPath,
      reason,
    };
  });

  const identity = {
    target: metadata.target,
    collectedAt: metadata.collectedAt,
    snapshotPath: metadata.snapshotPath,
    declarationDigests,
    inputDigests,
    checks,
    records,
  };
  return {
    schemaVersion: 1,
    status: 'review-required',
    candidateId: `install-${sha256(canonicalJson(identity)).slice(0, 20)}`,
    target: metadata.target,
    collectedAt: metadata.collectedAt,
    snapshotPath: metadata.snapshotPath,
    pendingAtCollection: pendingIds,
    declarationDigests,
    inputDigests,
    checks,
    records,
    reviewNotice:
      'This read-only collection candidate is not website proof until its source files, workflow run, exact revision, result semantics, and redaction are reviewed and the accepted snapshot is committed at snapshotPath.',
  };
}

export function collectInstallEvidence({
  root = repositoryRoot,
  metadataPath,
  outputPath,
} = {}) {
  assert(metadataPath, 'Missing --metadata path');
  assert(outputPath, 'Missing --output path');
  const metadataFile = readRegularFile(root, metadataPath, 'Install metadata');
  const metadata = JSON.parse(metadataFile.bytes.toString('utf8'));
  const candidate = buildInstallEvidenceCandidate({ root, metadata });
  const normalizedOutput = safeRepoPath(outputPath, 'Install output path');
  assert.equal(
    normalizedOutput,
    metadata.snapshotPath,
    'Install output path must equal metadata snapshotPath',
  );
  const absolute = resolve(root, normalizedOutput);
  assert(inside(root, absolute), 'Install output escapes the repository');
  assert(
    !existsSync(absolute),
    `Install output already exists: ${normalizedOutput}`,
  );
  mkdirSync(dirname(absolute), { recursive: true });
  writeFileSync(absolute, `${JSON.stringify(candidate, null, 2)}\n`, {
    flag: 'wx',
  });
  return candidate;
}

function argument(name) {
  const index = process.argv.indexOf(name);
  return index === -1 ? null : process.argv[index + 1];
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(resolve(process.argv[1])).href
) {
  const candidate = collectInstallEvidence({
    metadataPath: argument('--metadata'),
    outputPath: argument('--output'),
  });
  console.log(
    JSON.stringify({
      candidateId: candidate.candidateId,
      records: candidate.records.length,
      snapshotPath: candidate.snapshotPath,
      status: candidate.status,
    }),
  );
}
