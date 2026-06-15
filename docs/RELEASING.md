# Releasing MockAgents

This is the operator runbook for cutting a public release. The release is fully
automated by [`.github/workflows/release.yml`](../.github/workflows/release.yml),
which fires on any pushed `v*` tag and:

- builds cross-platform binaries + checksums and attaches them to a GitHub
  Release (GoReleaser, `.goreleaser.yml`);
- builds and pushes the container image to Docker Hub (`mockagents/mockagents`)
  and GHCR (`ghcr.io/mockagents/mockagents`);
- publishes the Python SDK to PyPI (`mockagents`, via Trusted Publishing);
- publishes the npm packages: `mockagents` (the `npx` launcher),
  `@mockagents/sdk`, and `@mockagents/vitest`;
- optionally publishes the Homebrew formula to `mockagents/homebrew-tap`.

The whole thing is driven by **one tag push** — but several **one-time account
setups** must be done first, or individual publish jobs will fail.

> **Version coupling — read this first.** The `npx mockagents` launcher and the
> Python/npm wrappers download the GitHub release binary whose version equals
> their own `package.json` / `pyproject.toml` version. So **the tag must equal
> the package versions.** They are all currently **`0.4.0`**. The release
> workflow enforces this (`Verify package versions match the tag`) and fails
> fast on a mismatch. To release a different version, bump all of these together
> first: `sdk/npx/package.json`, `sdk/typescript/package.json`,
> `sdk/vitest/package.json`, `sdk/python/pyproject.toml`, and
> `sdk/python/mockagents/__init__.py` (`__version__`).

---

## One-time setup (account actions — only a human can do these)

### 1. Namespaces (claim as `mockagents` to keep the brand consistent)

- [ ] **GitHub org `mockagents`** — already done (the repo lives at
      `github.com/mockagents/mockagents`).
- [ ] **Docker Hub org `mockagents`** — create the org and a `mockagents`
      repository. If the org name is taken, fall back to GHCR only and drop the
      `mockagents/mockagents` image line from `release.yml` (GHCR needs no
      external account).
- [ ] **PyPI project `mockagents`** — register the project and configure
      [Trusted Publishing](https://docs.pypi.org/trusted-publishers/) for this
      repo's `release-python` job (environment `pypi`, workflow
      `release.yml`). No API token needed with Trusted Publishing.
- [ ] **npm** — the unscoped package `mockagents` and the `@mockagents` org/scope
      (for `@mockagents/sdk` and `@mockagents/vitest`). Create the `@mockagents`
      org on npmjs.com.
- [ ] **Homebrew tap** (optional) — create the repo
      `github.com/mockagents/homebrew-tap`. Without it, the Homebrew step
      auto-skips and the rest of the release still succeeds.

### 2. Repository secrets (`Settings → Secrets and variables → Actions`)

| Secret | Used by | Needed for |
|---|---|---|
| `DOCKERHUB_USERNAME` | `release-docker` | Docker Hub push |
| `DOCKERHUB_TOKEN` | `release-docker` | Docker Hub push (access token, not password) |
| `NPM_TOKEN` | `release-npm` | npm publish (Automation token with publish rights) |
| `HOMEBREW_TAP_TOKEN` | `release-binaries` (GoReleaser) | push the formula to the tap (a PAT with `repo` write on `homebrew-tap`). Omit to skip Homebrew. |

`GITHUB_TOKEN` (GHCR push, GitHub Release) and PyPI Trusted Publishing need no
manually-created secret.

### 3. Repository settings

- [ ] **Flip the repo to Public** when ready:
      `gh repo edit mockagents/mockagents --visibility public --accept-visibility-change-consequences`.
      (Before this, decide whether the internal `autobuild/state` branch should
      stay private — see the project notes.)
- [ ] **Add discovery topics** (D-07):
      ```bash
      gh repo edit mockagents/mockagents \
        --add-topic mock-server --add-topic api-mocking --add-topic testing \
        --add-topic openai-api --add-topic anthropic --add-topic gemini \
        --add-topic mcp --add-topic sse --add-topic llm-testing \
        --add-topic chaos-engineering
      ```

---

## Cutting a release

Once the one-time setup is in place:

1. **Land everything** you want in the release on `main` (merge open PRs first) —
   do this **before** the next step so the changelog captures exactly what ships.
2. **Finalize the changelog.** Promote the accumulated `## [Unreleased]` section
   to the release version (renames it to `## [0.4.0] - <today>` and opens a fresh
   empty `## [Unreleased]`), then commit:
   ```bash
   make changelog-finalize VERSION=0.4.0     # or: sh scripts/finalize-changelog.sh 0.4.0
   git add CHANGELOG.md && git commit -m "docs(changelog): release 0.4.0"
   ```
3. **Confirm versions are aligned** to the tag you're about to push (see the
   version-coupling note above) — all should read `0.4.0`.
4. **Tag and push:**
   ```bash
   git checkout main && git pull
   git tag -a v0.4.0 -m "MockAgents v0.4.0"
   git push origin v0.4.0
   ```
5. **Watch the release workflow:** `gh run watch` (or the Actions tab). Jobs:
   `test` → `release-binaries` / `release-docker` / `release-python` /
   `release-npm` → `smoke-test`.

### Verify

```bash
gh release view v0.4.0                                   # binaries + checksums attached
docker run --rm -p 8080:8080 mockagents/mockagents &     # image runs
pipx run mockagents==0.4.0 --version                     # PyPI wheel bootstraps the binary
npx mockagents@0.4.0 --version                           # npx launcher downloads the binary
npm view @mockagents/sdk version                         # SDK published
brew install mockagents/tap/mockagents                   # (if the tap is set up)
```

The README install lines should all resolve once the matching namespace setup
above is done.

---

## Dry runs (no publish)

- **GoReleaser** (binaries/archives/checksums into `./dist`, nothing pushed):
  ```bash
  make release            # goreleaser release --snapshot --clean
  goreleaser check        # validate .goreleaser.yml
  ```
- **Docker image** locally: `make docker` then
  `docker run --rm -p 8080:8080 mockagents:latest`.
- **Python wheel**: `cd sdk/python && python -m build` → inspect `dist/`.
- **npm packs** (see exactly what would publish, nothing sent):
  `cd sdk/npx && npm pack --dry-run` (and likewise in `sdk/typescript`,
  `sdk/vitest` after `npm run build`).

## Maintenance note

The Homebrew formula uses GoReleaser's `brews:` block, which is deprecated in
favor of `homebrew_casks` and will be removed in GoReleaser v3. The
`goreleaser-action` is therefore pinned to `~> v2` in `release.yml`. Migrate the
`brews:` block to `homebrew_casks:` before moving to v3.
