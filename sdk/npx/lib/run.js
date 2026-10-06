'use strict';
// Run the resolved Go binary as a child of the npx launcher.
//
// `spawn` (not `spawnSync`) keeps Node's event loop alive, so a SIGINT /
// SIGTERM / SIGHUP sent to the launcher (a process supervisor, a cancelled CI
// step) is forwarded to the server instead of killing Node and orphaning the
// server on its port. The launcher then exits the way the child did: the same
// exit code, or the same signal.

const { spawn } = require('child_process');
const os = require('os');

const FORWARDED_SIGNALS = ['SIGINT', 'SIGTERM', 'SIGHUP'];

/**
 * Spawn `binary` with `args`, forward FORWARDED_SIGNALS to it, and resolve to
 * `{ code, signal }` once it exits. `deps` exists for tests: `spawn` and `proc`
 * (the current process) can be substituted.
 */
function runBinary(binary, args, deps = {}) {
  const spawnImpl = deps.spawn || spawn;
  const proc = deps.proc || process;
  return new Promise((resolve, reject) => {
    let child;
    try {
      child = spawnImpl(binary, args, { stdio: 'inherit' });
    } catch (err) {
      reject(err);
      return;
    }
    const handlers = new Map();
    const detach = () => {
      for (const [sig, fn] of handlers) proc.removeListener(sig, fn);
      handlers.clear();
    };
    for (const sig of FORWARDED_SIGNALS) {
      const fn = () => {
        try {
          child.kill(sig);
        } catch {
          /* child already gone */
        }
      };
      handlers.set(sig, fn);
      proc.on(sig, fn);
    }
    child.once('error', (err) => {
      detach();
      reject(err);
    });
    child.once('exit', (code, signal) => {
      detach();
      resolve({ code, signal });
    });
  });
}

/**
 * Make the launcher terminate the way the child did. A signal is re-raised on
 * the launcher itself (with its forwarding handler already removed, so the
 * default action applies); if that does not end the process (a signal Windows
 * cannot deliver), fall back to the conventional 128 + signal-number status.
 */
function mirrorExit(result, proc = process) {
  if (result.signal) {
    try {
      proc.kill(proc.pid, result.signal);
    } catch {
      /* fall through to an exit status */
    }
    const num = os.constants.signals[result.signal];
    proc.exit(typeof num === 'number' ? 128 + num : 1);
    return;
  }
  proc.exit(result.code == null ? 1 : result.code);
}

module.exports = { runBinary, mirrorExit, FORWARDED_SIGNALS };
