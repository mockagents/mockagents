'use strict';
const test = require('node:test');
const assert = require('node:assert');
const fs = require('fs');
const os = require('os');
const path = require('path');
const bin = require('../lib/binary');

function withPlatform(platform, arch, fn) {
  const op = process.platform;
  const oa = process.arch;
  Object.defineProperty(process, 'platform', { value: platform, configurable: true });
  Object.defineProperty(process, 'arch', { value: arch, configurable: true });
  try {
    fn();
  } finally {
    Object.defineProperty(process, 'platform', { value: op, configurable: true });
    Object.defineProperty(process, 'arch', { value: oa, configurable: true });
  }
}

test('assetOsArch maps platform/arch to goreleaser tokens', () => {
  withPlatform('linux', 'x64', () => assert.deepStrictEqual(bin.assetOsArch(), { os: 'linux', arch: 'amd64' }));
  withPlatform('darwin', 'arm64', () => assert.deepStrictEqual(bin.assetOsArch(), { os: 'darwin', arch: 'arm64' }));
  withPlatform('win32', 'x64', () => assert.deepStrictEqual(bin.assetOsArch(), { os: 'windows', arch: 'amd64' }));
  withPlatform('sunos', 'x64', () => assert.throws(() => bin.assetOsArch(), /unsupported OS/));
});

test('binaryName is platform-specific', () => {
  withPlatform('win32', 'x64', () => assert.strictEqual(bin.binaryName(), 'mockagents.exe'));
  withPlatform('linux', 'x64', () => assert.strictEqual(bin.binaryName(), 'mockagents'));
});

test('findBinary honors MOCKAGENTS_BINARY but rejects a directory', () => {
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'ma-npx-'));
  const f = path.join(tmp, 'fake-binary'); // not named "mockagents" (avoids AV heuristics)
  fs.writeFileSync(f, 'x');
  const orig = process.env.MOCKAGENTS_BINARY;
  try {
    process.env.MOCKAGENTS_BINARY = f;
    assert.strictEqual(bin.findBinary(), f);
    process.env.MOCKAGENTS_BINARY = tmp; // a directory must NOT be accepted
    assert.notStrictEqual(bin.findBinary(), tmp);
  } finally {
    if (orig === undefined) delete process.env.MOCKAGENTS_BINARY;
    else process.env.MOCKAGENTS_BINARY = orig;
    fs.rmSync(tmp, { recursive: true, force: true });
  }
});
