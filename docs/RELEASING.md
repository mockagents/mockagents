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
- [ ] **Homebrew tap** (optional, macOS) — create the repo
      `github.com/mockagents/homebrew-tap`. The release publishes a Homebrew
      **cask** (the modern path for prebuilt binaries; macOS only — Linux users
      use the binary / `go install` / Docker / npx / pipx). Without the tap the
      Homebrew step auto-skips and the rest of the release still succeeds.

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

### 4. Channel setup — step by step

The `release-binaries` job (binaries + the GitHub Release + the Homebrew cask)
needs nothing but the automatic `GITHUB_TOKEN`. The other publish jobs each need
one prerequisite below; set up whichever channels you want — they are independent,
and a missing one only fails its own job.

#### Docker Hub → `release-docker`
The job pushes to **both** `mockagents/mockagents` (Docker Hub) and
`ghcr.io/mockagents/mockagents` (GHCR). It logs in to Docker Hub first, so a
missing Docker Hub credential blocks the GHCR push too.

1. Create the Docker Hub org **`mockagents`**, then a public repo **`mockagents`**.
2. Docker Hub → **Account Settings → Personal access tokens → Generate** (Read & Write). Copy it.
3. Set the secrets:
   ```bash
   gh secret set DOCKERHUB_USERNAME --repo mockagents/mockagents --body "<dockerhub-username>"
   gh secret set DOCKERHUB_TOKEN    --repo mockagents/mockagents   # paste at the prompt
   ```

> **GHCR-only fallback** (skip Docker Hub): in `release.yml`, remove the
> `mockagents/mockagents` line from the `docker/metadata-action` `images:` and
> delete the "Login to Docker Hub" step. GHCR needs no external account.

#### npm → `release-npm`
Publishes `mockagents` (the unscoped npx launcher), `@mockagents/sdk`, and
`@mockagents/vitest`.

1. On npmjs.com create the **`@mockagents`** org (Add Organization). Ensure the
   unscoped **`mockagents`** name is free (it is claimed on first publish).
2. npmjs.com → **Access Tokens → Generate New Token → Automation** (CI-safe; bypasses 2FA). Copy it.
3. ```bash
   gh secret set NPM_TOKEN --repo mockagents/mockagents   # paste the automation token
   ```

#### PyPI → `release-python` (Trusted Publishing — no token)
Uses OIDC, so there is no API token to store — register a **trusted publisher**:

1. pypi.org → account → **Publishing → Add a new pending publisher** (works before
   the project exists):
   - **PyPI Project Name:** `mockagents`
   - **Owner:** `mockagents` · **Repository:** `mockagents`
   - **Workflow name:** `release.yml`
   - **Environment name:** `pypi`  ← must match `release.yml`'s `environment: pypi`
2. Nothing else: the job already sets `permissions: id-token: write`, and the
   `pypi` GitHub environment auto-creates on first run (no protection rules needed).

#### Homebrew (optional) → the GoReleaser cask
Auto-skips when `HOMEBREW_TAP_TOKEN` is unset, so the release succeeds without it.

1. Create the public repo **`github.com/mockagents/homebrew-tap`** (empty).
2. Create a GitHub **PAT** (classic with `repo`, or fine-grained with Contents:write on `homebrew-tap`).
3. ```bash
   gh secret set HOMEBREW_TAP_TOKEN --repo mockagents/mockagents   # paste the PAT
   ```

---

## Recovering a partial release

If a tag was pushed before some channel's prerequisite was ready, the binaries +
GitHub Release still publish, while the unconfigured channels' jobs fail at their
login/auth step — **before** uploading anything. So once you add the missing
secret/namespace, **re-run only the failed jobs on the same run; no new tag is
needed:**

```bash
RUN=$(gh run list --repo mockagents/mockagents --workflow=release.yml --limit 1 --json databaseId -q '.[0].databaseId')
gh run rerun "$RUN" --failed --repo mockagents/mockagents
gh run watch "$RUN" --exit-status --repo mockagents/mockagents
```

`--failed` re-runs only the failed jobs (+ their dependents, e.g. `smoke-test`);
the already-succeeded `release-binaries` is left intact, so the live GitHub Release
is untouched.

> **The one case that needs a new version:** if a job *did* publish to npm or PyPI
> before failing (those versions are **immutable** and cannot be re-uploaded), bump
> to the next patch (`v0.4.1`) instead of re-running. Confirm what published with
> `npm view <pkg> version` and `https://pypi.org/pypi/mockagents/json` (404 = not
> published → safe to re-run).

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

Homebrew distribution uses GoReleaser's `homebrew_casks:` block (a cask, not the
deprecated `brews:` formula), so `goreleaser-action` floats to `latest`. The cask
is **macOS-only** — there is no Linux Homebrew path by design.
