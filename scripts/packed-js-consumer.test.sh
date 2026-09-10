#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d "$root/.tmp-packed-consumer.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

pack() { (cd "$tmp" && npm pack --silent "$root/$1"); }
sdk=$(pack sdk/typescript | tail -1)
helper=$(pack sdk/vitest | tail -1)
cd "$tmp"
printf '%s\n' '{"name":"mockagents-packed-consumer","private":true,"type":"module"}' > package.json
npm install --strict-peer-deps --no-audit --no-fund "./$sdk" "./$helper" vitest@2 >/dev/null
node --input-type=module -e "await import('@mockagents/sdk'); await import('@mockagents/vitest/jest')"
cat > packed.test.mjs <<'EOF'
import { expect, test } from "vitest";
import * as helper from "@mockagents/vitest";
test("packed Vitest entry imports", () => expect(helper.setupMockAgents).toBeTypeOf("function"));
EOF
npx vitest run packed.test.mjs >/dev/null
