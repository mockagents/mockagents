#!/usr/bin/env node
'use strict';
// `npx mockagents <args>` — resolve/download the Go binary and run it.

const { spawnSync } = require('child_process');
const { ensureBinary, INSTALL_HINT } = require('../lib/binary');
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
  const res = spawnSync(binary, process.argv.slice(2), { stdio: 'inherit' });
  if (res.error) {
    console.error(`mockagents: failed to run ${binary}: ${res.error.message}`);
    process.exit(1);
  }
  process.exit(res.status == null ? 1 : res.status);
})();
