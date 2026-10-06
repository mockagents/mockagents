'use strict';
// Launcher process handling (review K-26): async spawn, signal forwarding and
// exit mirroring.
const test = require('node:test');
const assert = require('node:assert');
const { EventEmitter } = require('events');
const { spawn } = require('child_process');
const os = require('os');
const path = require('path');
const { runBinary, mirrorExit, FORWARDED_SIGNALS } = require('../lib/run');

class FakeChild extends EventEmitter {
  constructor() {
    super();
    this.killed = [];
  }
  kill(sig) {
    this.killed.push(sig);
    return true;
  }
}

class FakeProc extends EventEmitter {
  constructor() {
    super();
    this.pid = 4242;
    this.calls = [];
  }
  kill(pid, sig) {
    this.calls.push(['kill', pid, sig]);
  }
  exit(code) {
    this.calls.push(['exit', code]);
  }
}

test('runBinary spawns asynchronously with inherited stdio', async () => {
  const child = new FakeChild();
  const seen = [];
  const proc = new FakeProc();
  const done = runBinary('/bin/mockagents', ['start', '--port', '1'], {
    proc,
    spawn: (bin, args, opts) => {
      seen.push([bin, args, opts.stdio]);
      return child;
    },
  });
  child.emit('exit', 0, null);
  assert.deepStrictEqual(await done, { code: 0, signal: null });
  assert.deepStrictEqual(seen, [['/bin/mockagents', ['start', '--port', '1'], 'inherit']]);
});

test('runBinary forwards SIGINT, SIGTERM and SIGHUP to the child, then detaches', async () => {
  const child = new FakeChild();
  const proc = new FakeProc();
  const done = runBinary('bin', [], { proc, spawn: () => child });
  for (const sig of FORWARDED_SIGNALS) {
    assert.strictEqual(proc.listenerCount(sig), 1, `${sig} handler installed`);
    proc.emit(sig);
  }
  assert.deepStrictEqual(child.killed, ['SIGINT', 'SIGTERM', 'SIGHUP']);
  child.emit('exit', null, 'SIGTERM');
  assert.deepStrictEqual(await done, { code: null, signal: 'SIGTERM' });
  for (const sig of FORWARDED_SIGNALS) assert.strictEqual(proc.listenerCount(sig), 0);
});

test('runBinary rejects on a spawn error and removes its handlers', async () => {
  const child = new FakeChild();
  const proc = new FakeProc();
  const done = runBinary('missing', [], { proc, spawn: () => child });
  child.emit('error', Object.assign(new Error('spawn missing ENOENT'), { code: 'ENOENT' }));
  await assert.rejects(done, /ENOENT/);
  for (const sig of FORWARDED_SIGNALS) assert.strictEqual(proc.listenerCount(sig), 0);
});

test('mirrorExit propagates the exit code', () => {
  const proc = new FakeProc();
  mirrorExit({ code: 3, signal: null }, proc);
  assert.deepStrictEqual(proc.calls, [['exit', 3]]);
  const proc2 = new FakeProc();
  mirrorExit({ code: null, signal: null }, proc2);
  assert.deepStrictEqual(proc2.calls, [['exit', 1]]);
});

test('mirrorExit re-raises the child signal, falling back to 128+n', () => {
  const proc = new FakeProc();
  mirrorExit({ code: null, signal: 'SIGTERM' }, proc);
  assert.deepStrictEqual(proc.calls, [
    ['kill', 4242, 'SIGTERM'],
    ['exit', 128 + os.constants.signals.SIGTERM],
  ]);
});

// End to end on a real process tree: SIGTERM to the launcher must take the
// child down with it instead of orphaning it. POSIX signals only.
test('SIGTERM to the launcher stops the child', { skip: process.platform === 'win32' }, async () => {
  const runPath = path.join(__dirname, '..', 'lib', 'run.js');
  const childScript = 'console.log(process.pid); setInterval(() => {}, 1000);';
  const launcherScript =
    `const { runBinary, mirrorExit } = require(${JSON.stringify(runPath)});` +
    `runBinary(process.execPath, ['-e', ${JSON.stringify(childScript)}]).then(mirrorExit);`;
  const launcher = spawn(process.execPath, ['-e', launcherScript], { stdio: ['ignore', 'pipe', 'inherit'] });
  const childPid = await new Promise((resolve, reject) => {
    launcher.stdout.once('data', (buf) => resolve(Number(String(buf).trim())));
    launcher.once('error', reject);
  });
  const exited = new Promise((resolve) => launcher.once('exit', (code, signal) => resolve({ code, signal })));
  launcher.kill('SIGTERM');
  const result = await exited;
  assert.ok(result.signal === 'SIGTERM' || result.code === 128 + os.constants.signals.SIGTERM, JSON.stringify(result));
  // The child must be gone too.
  let alive = true;
  for (let i = 0; i < 50 && alive; i++) {
    try {
      process.kill(childPid, 0);
      await new Promise((r) => setTimeout(r, 20));
    } catch {
      alive = false;
    }
  }
  assert.strictEqual(alive, false, `child ${childPid} survived the launcher`);
});
