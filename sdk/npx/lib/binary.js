'use strict';
// Resolve — or download — the MockAgents Go binary for the npx launcher.
// Shares the Python SDK's release-asset naming and FAIL-CLOSED sha256 check.
// NOTE: binary lookup is intentionally narrow ($MOCKAGENTS_BINARY + cache only,
// NOT PATH). When run via `npx mockagents`, this package's own bin shim is on
// PATH, so resolving via PATH could re-exec the launcher (a fork bomb). Set
// MOCKAGENTS_BINARY to reuse an already-installed binary instead of downloading.

const fs = require('fs');
const os = require('os');
const path = require('path');
const https = require('https');
const crypto = require('crypto');
const { execFileSync } = require('child_process');

const REPO = 'mockagents/mockagents';

function assetOsArch() {
  const osMap = { linux: 'linux', darwin: 'darwin', win32: 'windows' };
  const o = osMap[process.platform];
  if (!o) throw new Error(`unsupported OS for auto-download: ${process.platform}`);
  const archMap = { x64: 'amd64', arm64: 'arm64' };
  const a = archMap[process.arch];
  if (!a) throw new Error(`unsupported architecture for auto-download: ${process.arch}`);
  return { os: o, arch: a };
}

function binaryName() {
  return process.platform === 'win32' ? 'mockagents.exe' : 'mockagents';
}

function cacheDir() {
  if (process.platform === 'win32') {
    const base = process.env.LOCALAPPDATA || path.join(os.homedir(), 'AppData', 'Local');
    return path.join(base, 'mockagents', 'bin');
  }
  if (process.platform === 'darwin') {
    return path.join(os.homedir(), 'Library', 'Caches', 'mockagents', 'bin');
  }
  const base = process.env.XDG_CACHE_HOME || path.join(os.homedir(), '.cache');
  return path.join(base, 'mockagents', 'bin');
}

function isFile(p) {
  try {
    return fs.statSync(p).isFile();
  } catch {
    return false;
  }
}

function findBinary() {
  const explicit = process.env.MOCKAGENTS_BINARY;
  if (explicit && isFile(explicit)) return explicit; // reject a directory etc.
  const cached = path.join(cacheDir(), binaryName());
  if (isFile(cached)) return cached;
  return null;
}

// get follows redirects (GitHub release URLs 302 to a signed object store) and
// bounds each request with a timeout so a stalled connection can't hang npx.
function get(url, redirects = 0) {
  return new Promise((resolve, reject) => {
    if (redirects > 5) return reject(new Error('too many redirects'));
    const req = https.get(url, { headers: { 'User-Agent': 'mockagents-npx' }, timeout: 60000 }, (res) => {
      if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
        res.resume();
        resolve(get(res.headers.location, redirects + 1));
        return;
      }
      if (res.statusCode !== 200) {
        res.resume();
        reject(Object.assign(new Error(`HTTP ${res.statusCode}`), { statusCode: res.statusCode }));
        return;
      }
      const chunks = [];
      res.on('data', (c) => chunks.push(c));
      res.on('end', () => resolve(Buffer.concat(chunks)));
    });
    req.on('timeout', () => req.destroy(new Error('download timed out after 60s')));
    req.on('error', reject);
  });
}

const INSTALL_HINT =
  'Install another way:\n' +
  '  brew install mockagents/tap/mockagents\n' +
  '  docker run -p 8080:8080 mockagents/mockagents\n' +
  '  go install github.com/mockagents/mockagents/cmd/mockagents@latest\n' +
  'Or point at an existing binary: export MOCKAGENTS_BINARY=/path/to/mockagents';

async function download(version) {
  const { os: o, arch: a } = assetOsArch();
  const ver = version.replace(/^v/, '');
  const ext = o === 'windows' ? 'zip' : 'tar.gz';
  const asset = `mockagents_${ver}_${o}_${a}.${ext}`;
  const base = `https://github.com/${REPO}/releases/download/v${ver}`;

  let data;
  try {
    data = await get(`${base}/${asset}`);
  } catch (e) {
    if (e.statusCode === 404) {
      throw new Error(`no release asset for ${asset} (v${ver} may not be published yet).\n${INSTALL_HINT}`);
    }
    throw e;
  }

  // FAIL-CLOSED checksum: the binary is executed, so an unobtainable or
  // mismatched checksum must abort the install.
  let checksums;
  try {
    checksums = (await get(`${base}/checksums.txt`)).toString('utf8');
  } catch (e) {
    throw new Error(`could not fetch checksums.txt to verify ${asset}: ${e.message}. Refusing to install unverified.`);
  }
  let want = null;
  for (const line of checksums.split('\n')) {
    const parts = line.trim().split(/\s+/);
    if (parts.length >= 2 && parts[parts.length - 1].replace(/^\*/, '') === asset) {
      want = parts[0];
      break;
    }
  }
  if (!want) throw new Error(`no checksum for ${asset} in checksums.txt; refusing to install unverified.`);
  const got = crypto.createHash('sha256').update(data).digest('hex');
  if (got !== want) {
    throw new Error(`checksum mismatch for ${asset}: expected ${want}, got ${got}. Refusing to install.`);
  }

  const dir = cacheDir();
  fs.mkdirSync(dir, { recursive: true });
  const archivePath = path.join(dir, asset);
  fs.writeFileSync(archivePath, data);

  // Extract just the binary. bsdtar (Windows 10+/macOS) handles .zip via -xf;
  // .tar.gz is handled by tar everywhere via -xzf.
  const bin = binaryName();
  const args = ext === 'zip' ? ['-xf', archivePath, '-C', dir, bin] : ['-xzf', archivePath, '-C', dir, bin];
  try {
    execFileSync('tar', args, { stdio: 'inherit' });
  } catch (e) {
    try { fs.unlinkSync(archivePath); } catch (_) { /* best effort */ }
    if (e && e.code === 'ENOENT') {
      throw new Error(`extraction needs 'tar' on PATH, which was not found.\n${INSTALL_HINT}`);
    }
    throw new Error(`failed to extract ${asset}: ${e.message}`);
  }
  fs.unlinkSync(archivePath);

  const out = path.join(dir, bin);
  if (process.platform !== 'win32') fs.chmodSync(out, 0o755);
  return out;
}

async function ensureBinary(version) {
  const found = findBinary();
  if (found) return found;
  return download(version);
}

module.exports = { ensureBinary, findBinary, download, assetOsArch, cacheDir, binaryName, INSTALL_HINT };
