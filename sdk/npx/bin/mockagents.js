#!/usr/bin/env node
'use strict';
// `npx mockagents <args>` — resolve/download the Go binary and run it.

const { ensureBinary, INSTALL_HINT } = require('../lib/binary');
const { runBinary, mirrorExit } = require('../lib/run');
const pkg = require('../package.json');

(async () => {
  let binary;
  try {
    binary = await ensureBinary(pkg.version);
  } catch (e) {
    console.error(`mockagents: ${e.message}`);
    if (!String(e.message).includes('brew install')) console.error(INSTALL_HINT);
    process.exit(1);
  }
  // Async spawn with signal forwarding: a SIGTERM to this launcher must stop
  // the server too, not orphan it on its port.
  let result;
  try {
    result = await runBinary(binary, process.argv.slice(2));
  } catch (e) {
    console.error(`mockagents: failed to run ${binary}: ${e.message}`);
    process.exit(1);
  }
  mirrorExit(result);
})();
