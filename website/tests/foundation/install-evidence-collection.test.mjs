import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';
import { parse } from 'yaml';

import {
  buildInstallEvidenceCandidate,
  collectInstallEvidence,
} from '../../scripts/collect-install-evidence.mjs';

const required = [
  'go',
  'binary',
  'go-sdk',
  'docker-hub',
  'ghcr',
  'npx',
  'npm-sdk',
  'npm-vitest',
  'pipx',
  'pypi',
  'homebrew',
];
const pending = required.slice(3);
const revision = 'a'.repeat(40);
const repositoryRoot = resolve(
  dirname(fileURLToPath(import.meta.url)),
  '../../..',
);
const writeJson = (path, value) =>
  writeFileSync(path, `${JSON.stringify(value, null, 2)}\n`);

function fixture() {
  const root = mkdtempSync(resolve(tmpdir(), 'mockagents-installs-'));
  mkdirSync(resolve(root, '.github'), { recursive: true });
  mkdirSync(resolve(root, 'artifacts'), { recursive: true });
  mkdirSync(resolve(root, 'website'), { recursive: true });
  writeFileSync(
    resolve(root, '.github/install-paths-required.txt'),
    `${required.join('\n')}\n`,
  );
  writeFileSync(
    resolve(root, '.github/install-paths-pending.txt'),
    `${pending.join('\n')}\n`,
  );
  const linuxRows = required
    .filter((id) => id !== 'homebrew')
    .map((id) =>
      [
        id,
        pending.includes(id) ? 'fail' : 'ok',
        pending.includes(id) ? 'none' : '0.5.0',
        `${id} probe`,
      ].join('|'),
    );
  writeFileSync(
    resolve(root, 'artifacts/results-linux.txt'),
    `${linuxRows.join('\n')}\n`,
  );
  writeFileSync(
    resolve(root, 'artifacts/results-macos.txt'),
    'homebrew|fail|none|homebrew probe\n',
  );
  writeJson(resolve(root, 'artifacts/platform-linux.json'), {
    os: 'Linux',
    arch: 'X64',
    checkedAt: '2026-09-22T10:00:00.000Z',
  });
  writeJson(resolve(root, 'artifacts/platform-macos.json'), {
    os: 'macOS',
    arch: 'ARM64',
    checkedAt: '2026-09-22T10:01:00.000Z',
  });
  const metadata = {
    schemaVersion: 1,
    target: { tag: 'v0.5.0', version: '0.5.0', revision },
    collectedAt: '2026-09-22T10:02:00.000Z',
    snapshotPath:
      'website/manifests/install-evidence/candidates/0.5.0-123-1.json',
    runs: [
      {
        resultPath: 'artifacts/results-linux.txt',
        platformPath: 'artifacts/platform-linux.json',
      },
      {
        resultPath: 'artifacts/results-macos.txt',
        platformPath: 'artifacts/platform-macos.json',
      },
    ],
  };
  writeJson(resolve(root, 'website/metadata.json'), metadata);
  return { root, metadata };
}

test('collection produces exact source-bound per-platform records', () => {
  const { root, metadata } = fixture();
  const candidate = buildInstallEvidenceCandidate({ root, metadata });
  assert.equal(candidate.status, 'review-required');
  assert.equal(candidate.records.length, 11);
  assert.deepEqual(candidate.pendingAtCollection, pending);
  assert.equal(
    candidate.records.filter((item) => item.status === 'verified').length,
    3,
  );
  assert.equal(
    candidate.records.filter((item) => item.status === 'failed').length,
    8,
  );
  assert(candidate.records.every((item) => item.revision === revision));
  assert(candidate.records.every((item) => item.version === '0.5.0'));
  assert(candidate.records.every((item) => item.evidenceDigest.length === 64));
  assert(
    candidate.records.every((item) => item.source === metadata.snapshotPath),
  );
  const homebrew = candidate.records.find(
    (item) => item.channel === 'homebrew',
  );
  assert.equal(homebrew.os, 'macos');
  assert.equal(homebrew.arch, 'arm64');
  assert.equal(homebrew.checkedAt, '2026-09-22T10:01:00.000Z');
  assert.equal(homebrew.status, 'failed');
  const homebrewCheck = candidate.checks.find(
    (item) => item.channel === 'homebrew',
  );
  assert.equal(homebrewCheck.result.observed, 'none');
  assert.equal(homebrewCheck.result.status, 'fail');
  assert.equal(homebrewCheck.evidenceDigest, homebrew.evidenceDigest);
  assert.equal(candidate.declarationDigests.required.sha256.length, 64);
  assert.equal(candidate.declarationDigests.pending.sha256.length, 64);
});

test('candidate identity and record order do not depend on run order', () => {
  const { root, metadata } = fixture();
  const first = buildInstallEvidenceCandidate({ root, metadata });
  const second = buildInstallEvidenceCandidate({
    root,
    metadata: { ...metadata, runs: [...metadata.runs].reverse() },
  });
  assert.equal(second.candidateId, first.candidateId);
  assert.deepEqual(second.records, first.records);
  assert.deepEqual(second.inputDigests, first.inputDigests);
});

test('missing, duplicate, and unexpected result IDs fail closed', () => {
  const missing = fixture();
  writeFileSync(
    resolve(missing.root, 'artifacts/results-macos.txt'),
    'unlisted|fail|none|unknown probe\n',
  );
  assert.throws(
    () => buildInstallEvidenceCandidate(missing),
    /Unexpected install result ID unlisted/,
  );

  const duplicate = fixture();
  writeFileSync(
    resolve(duplicate.root, 'artifacts/results-macos.txt'),
    'go|ok|0.5.0|duplicate probe\nhomebrew|fail|none|homebrew probe\n',
  );
  assert.throws(
    () => buildInstallEvidenceCandidate(duplicate),
    /Duplicate install result ID go/,
  );

  const absent = fixture();
  writeFileSync(resolve(absent.root, 'artifacts/results-macos.txt'), '\n');
  assert.throws(
    () => buildInstallEvidenceCandidate(absent),
    /contains no result rows/,
  );
});

test('target, timestamp, and platform metadata are strictly validated', () => {
  const invalidTag = fixture();
  invalidTag.metadata.target.tag = '0.5.0';
  assert.throws(
    () => buildInstallEvidenceCandidate(invalidTag),
    /tag must equal v plus/,
  );

  const invalidTime = fixture();
  const platformPath = resolve(
    invalidTime.root,
    'artifacts/platform-linux.json',
  );
  writeJson(platformPath, {
    os: 'Linux',
    arch: 'X64',
    checkedAt: 'yesterday',
  });
  assert.throws(
    () => buildInstallEvidenceCandidate(invalidTime),
    /canonical ISO 8601 UTC timestamp/,
  );

  const invalidPlatform = fixture();
  writeJson(resolve(invalidPlatform.root, 'artifacts/platform-macos.json'), {
    os: 'macOS',
    arch: 'mips64',
    checkedAt: '2026-09-22T10:01:00.000Z',
  });
  assert.throws(
    () => buildInstallEvidenceCandidate(invalidPlatform),
    /unsupported architecture/,
  );
});

test('collector writes once at the declared immutable snapshot path', () => {
  const { root, metadata } = fixture();
  const candidate = collectInstallEvidence({
    root,
    metadataPath: 'website/metadata.json',
    outputPath: metadata.snapshotPath,
  });
  const written = JSON.parse(
    readFileSync(resolve(root, metadata.snapshotPath)),
  );
  assert.equal(written.candidateId, candidate.candidateId);
  assert.throws(
    () =>
      collectInstallEvidence({
        root,
        metadataPath: 'website/metadata.json',
        outputPath: metadata.snapshotPath,
      }),
    /already exists/,
  );
  assert.throws(
    () =>
      collectInstallEvidence({
        root: fixture().root,
        metadataPath: 'website/metadata.json',
        outputPath: 'website/manifests/install-evidence/other.json',
      }),
    /must equal metadata snapshotPath/,
  );
});

test('install workflow captures platform metadata and uploads only a review candidate', () => {
  const source = readFileSync(
    resolve(repositoryRoot, '.github/workflows/install-paths.yml'),
    'utf8',
  );
  const workflow = parse(source);
  assert.equal(
    workflow.jobs.target.outputs.revision,
    '${{ steps.target.outputs.revision }}',
  );
  assert.match(
    workflow.jobs.target.steps[1].run,
    /revision=\$\(git rev-list -n 1 "\$tag"\)/,
  );
  const linuxUpload = workflow.jobs.linux.steps.find(
    (step) => step.with?.name === 'results-linux',
  );
  const macosUpload = workflow.jobs.macos.steps.find(
    (step) => step.with?.name === 'results-macos',
  );
  assert.match(linuxUpload.with.path, /platform-linux\.json/);
  assert.match(macosUpload.with.path, /platform-macos\.json/);

  const collection = workflow.jobs.report.steps.find(
    (step) => step.name === 'Build redacted website evidence review candidate',
  );
  assert.equal(collection.if, 'always()');
  assert.match(collection.run, /collect-install-evidence\.mjs/);
  assert.match(collection.run, /TARGET_REVISION/);
  const upload = workflow.jobs.report.steps.find((step) =>
    step.with?.name?.startsWith('install-evidence-review-'),
  );
  assert.equal(upload.if, 'always()');
  assert.equal(upload.with['if-no-files-found'], 'error');
  assert.equal(upload.with['retention-days'], 7);
  assert.doesNotMatch(source, /collect-install-evidence[\s\S]*installs\.json/);
});
