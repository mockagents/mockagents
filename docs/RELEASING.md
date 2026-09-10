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
- publishes the Homebrew cask to `mockagents/homebrew-tap` for stable releases.

The whole thing is driven by **one tag push** — but several **one-time account
setups** must be done first, or individual publish jobs will fail.

All build, package, security, Helm, and install-channel checks run through the
reusable candidate verification workflow before a publisher can start.
`prepare-artifacts` then builds the binaries, wheel/source distribution, npm
tarballs and per-platform container images exactly once, records their SHA-256
digests and toolchain metadata, and uploads one candidate bundle keyed by the
commit SHA. Publisher jobs only verify and consume that bundle.
Account and registry checkboxes below are prerequisites that require live
verification; their presence here is not evidence that they are configured.

> **Version coupling — read this first.** Set `VERSION` to the candidate package
> version (without the `v` tag prefix). The `npx mockagents` launcher and the
> Python/npm wrappers download the GitHub release binary whose version equals
> their own `package.json` / `pyproject.toml` version. So **the tag must equal
> every package version.** The release
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
- [ ] **Homebrew tap** (required for stable releases, macOS) — create the repo
      `github.com/mockagents/homebrew-tap`. The release publishes a Homebrew
      **cask** (the modern path for prebuilt binaries; macOS only — Linux users
      use the binary / `go install` / Docker / npx / pipx). Prereleases do not
      update this stable channel.

### 2. Repository secrets (`Settings → Secrets and variables → Actions`)

| Secret | Used by | Needed for |
|---|---|---|
| `DOCKERHUB_USERNAME` | `release-docker` | Docker Hub push |
| `DOCKERHUB_TOKEN` | `release-docker` | Docker Hub push (access token, not password) |
| `NPM_TOKEN` | `release-npm` | npm publish (Automation token with publish rights) |
| `HOMEBREW_TAP_TOKEN` | `release-homebrew` | push the cask to the tap (a PAT with Contents write on `homebrew-tap`); required for stable tags. |

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

The `release-binaries` job needs only the automatic `GITHUB_TOKEN`. Registry
and stable Homebrew prerequisites are checked together before the first
publication mutation so a missing required channel cannot produce a partial
release by configuration alone.

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

#### Homebrew (stable releases) → cask from prepared archives

The cask references the same prepared macOS archives and checksums attached to
the GitHub Release. Prereleases leave the stable tap unchanged.

1. Create the public repo **`github.com/mockagents/homebrew-tap`** (empty).
2. Create a GitHub **PAT** (classic with `repo`, or fine-grained with Contents:write on `homebrew-tap`).
3. ```bash
   gh secret set HOMEBREW_TAP_TOKEN --repo mockagents/mockagents   # paste the PAT
   ```

---

## Recovering a partial release

Missing registry credentials and the stable Homebrew token fail the shared
preflight before any publication. A registry outage or permission change can
still interrupt publication after another channel succeeds. Once the external
problem is fixed, **re-run only the failed jobs on the same run; no new tag is
needed when that registry has not accepted its immutable version:**

```bash
RUN=$(gh run list --repo mockagents/mockagents --workflow=release.yml --limit 1 --json databaseId -q '.[0].databaseId')
gh run rerun "$RUN" --failed --repo mockagents/mockagents
gh run watch "$RUN" --exit-status --repo mockagents/mockagents
```

`--failed` re-runs only the failed jobs and their dependents. Every retry
downloads the original commit-keyed candidate bundle. Existing GitHub Release
assets are downloaded and byte-compared; a mismatch fails instead of replacing
the asset, while missing assets are uploaded.

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
2. **Set and verify the candidate version.** For example, `VERSION=0.5.0`.
   Promote the accumulated `## [Unreleased]` section to the release version and open a fresh
   empty `## [Unreleased]`), then commit:
   ```bash
   make changelog-finalize VERSION="$VERSION"
   git add CHANGELOG.md && git commit -m "docs(changelog): release $VERSION"
   ```
3. **Confirm versions are aligned** to the tag you're about to push (see the
   version-coupling note above), then run `scripts/release-preflight.sh "v$VERSION"`.
4. **Tag and push:**
   ```bash
   git checkout main && git pull
   git tag -a "v$VERSION" -m "MockAgents v$VERSION"
   git push origin "v$VERSION"
   ```
5. **Watch the release workflow:** `gh run watch` (or the Actions tab). The
   dependency chain is `verify` → `preflight` → `prepare-artifacts`, followed
   by `release-binaries` / `release-docker`; the Python and npm publishers also
   wait for `release-binaries`. Public-channel smoke verification runs through
   `install-paths.yml` after the release workflow completes and on its schedule.

### Verify

Verify immutable versions and digests for every enabled channel. A prerelease
must never update stable container/npm tags. If publication stops partway,
record each successful immutable artifact and resume only missing work; never
replace registry bytes under an existing version.

```bash
gh release view "v$VERSION"                             # binaries + checksums attached
docker run --rm -p 8080:8080 mockagents/mockagents &     # image runs
pipx run "mockagents==$VERSION" --version               # PyPI wheel bootstraps the binary
npx "mockagents@$VERSION" --version                     # npx launcher downloads the binary
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

Homebrew distribution is generated by `release-homebrew` from the checksums of
the already prepared GitHub archives. The cask is **macOS-only** — there is no
Linux Homebrew path by design. GoReleaser itself is pinned to the exact version
recorded in the candidate manifest.
