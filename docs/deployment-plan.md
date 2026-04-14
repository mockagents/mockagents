# MockAgents Deployment Plan

## 1. Document Info

| Field       | Value                                      |
|-------------|--------------------------------------------|
| Version     | 1.0                                        |
| Date        | 2026-04-07                                 |
| Status      | Draft                                      |
| Author      | MockAgents Team                            |
| Applies to  | MockAgents v0.1.0 (MVP)                    |
| Last Review | 2026-04-07                                 |

---

## 2. Deployment Overview

MockAgents is primarily a **local development tool**. Unlike traditional cloud services, "deployment" in this context means distributing the binary and packages to end users rather than standing up infrastructure. There is no centralized server to operate. Users run MockAgents on their own machines or in their own CI/CD pipelines.

### 2.1 Distribution Channels

| Channel         | Command / Action                          | Primary Audience            |
|-----------------|-------------------------------------------|-----------------------------|
| Binary download | `curl` + extract from GitHub Releases     | Go developers, CLI users    |
| pip install     | `pip install mockagents`                  | Python developers           |
| Docker          | `docker run mockagents/mockagents`        | CI/CD pipelines, teams      |

### 2.2 Deployment Targets

| Target               | Description                                                   |
|----------------------|---------------------------------------------------------------|
| Developer machines   | macOS, Linux, Windows workstations running MockAgents locally |
| CI/CD runners        | GitHub Actions, GitLab CI, Jenkins, Azure DevOps agents       |
| Docker containers    | Containerized mock server for team or CI use                  |

### 2.3 Key Principles

- **Zero infrastructure**: no cloud accounts, databases, or services to provision.
- **Single binary**: one statically linked executable per platform.
- **Offline-capable**: once installed, MockAgents requires no network access.
- **Deterministic**: agent definitions are version-controlled YAML; behavior is reproducible.

---

## 3. Deployment Artifacts

### 3.1 Go Binary

The primary artifact is a statically linked Go binary cross-compiled for five platform/architecture pairs.

| Property              | Value                                                              |
|-----------------------|--------------------------------------------------------------------|
| Build tool            | GoReleaser                                                         |
| Linking               | Static (`CGO_ENABLED=0`)                                           |
| Runtime dependencies  | None                                                               |
| Publish target        | GitHub Releases                                                    |
| Target size           | < 30 MB per binary                                                 |

**Supported platforms:**

| OS      | Architecture | Archive format                              |
|---------|-------------|----------------------------------------------|
| Linux   | amd64       | `mockagents_v0.1.0_linux_amd64.tar.gz`      |
| Linux   | arm64       | `mockagents_v0.1.0_linux_arm64.tar.gz`      |
| macOS   | amd64       | `mockagents_v0.1.0_darwin_amd64.tar.gz`     |
| macOS   | arm64       | `mockagents_v0.1.0_darwin_arm64.tar.gz`     |
| Windows | amd64       | `mockagents_v0.1.0_windows_amd64.zip`       |

Each release includes a `checksums.txt` file containing SHA-256 hashes for every archive.

**GoReleaser configuration (`.goreleaser.yaml`):**

```yaml
version: 2

project_name: mockagents

before:
  hooks:
    - go mod tidy

builds:
  - id: mockagents
    main: ./cmd/mockagents
    binary: mockagents
    env:
      - CGO_ENABLED=0
    goos:
      - linux
      - darwin
      - windows
    goarch:
      - amd64
      - arm64
    ignore:
      - goos: windows
        goarch: arm64
    ldflags:
      - -s -w
      - -X github.com/mockagents/mockagents/internal/version.Version={{.Version}}
      - -X github.com/mockagents/mockagents/internal/version.Commit={{.ShortCommit}}
      - -X github.com/mockagents/mockagents/internal/version.Date={{.Date}}

archives:
  - id: default
    format: tar.gz
    format_overrides:
      - goos: windows
        format: zip
    name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    files:
      - LICENSE
      - README.md

checksum:
  name_template: "checksums.txt"
  algorithm: sha256

release:
  github:
    owner: mockagents
    name: mockagents
  draft: false
  prerelease: auto
  name_template: "MockAgents {{ .Version }}"

changelog:
  sort: asc
  filters:
    exclude:
      - "^docs:"
      - "^test:"
      - "^ci:"
      - "^chore:"
```

### 3.2 Docker Image

A minimal Docker image for running MockAgents as a containerized mock server.

| Property           | Value                                                    |
|--------------------|----------------------------------------------------------|
| Build strategy     | Multi-stage (Go builder + Alpine runtime)                |
| Base image         | `alpine:3.19`                                            |
| Published to       | Docker Hub (`mockagents/mockagents`), GHCR (`ghcr.io/mockagents/mockagents`) |
| Exposed port       | 8080                                                     |
| Volume mount       | `/agents` (agent definition YAML files)                  |
| Target size        | < 50 MB                                                  |

**Tag strategy:**

| Tag             | Example              | Description                        |
|-----------------|----------------------|------------------------------------|
| Full version    | `v0.1.0`             | Immutable, points to exact release |
| Minor           | `v0.1`               | Moves with patch releases          |
| Major           | `v0`                 | Moves with minor releases          |
| `latest`        | `latest`             | Tracks most recent stable release  |
| Pre-release     | `v0.1.0-alpha`       | Pre-release builds                 |

**Dockerfile:**

```dockerfile
# =============================================================================
# Stage 1: Build the Go binary
# =============================================================================
FROM golang:1.22-alpine AS builder

# Install git for module downloads; ca-certificates for HTTPS
RUN apk add --no-cache git ca-certificates

WORKDIR /src

# Cache dependency downloads
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /bin/mockagents \
    ./cmd/mockagents

# =============================================================================
# Stage 2: Minimal runtime image
# =============================================================================
FROM alpine:3.19

# Add ca-certificates for any outbound HTTPS and a non-root user
RUN apk add --no-cache ca-certificates \
    && addgroup -S mockagents \
    && adduser -S mockagents -G mockagents

# Copy the binary from the builder
COPY --from=builder /bin/mockagents /usr/local/bin/mockagents

# Create the default agent definitions mount point
RUN mkdir -p /agents && chown mockagents:mockagents /agents
VOLUME ["/agents"]

# Create the data directory for SQLite
RUN mkdir -p /data && chown mockagents:mockagents /data

USER mockagents

EXPOSE 8080

# Health check: hit the health endpoint every 30 seconds
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget -qO- http://localhost:8080/api/v1/health || exit 1

ENTRYPOINT ["mockagents"]
CMD ["start", "--host", "0.0.0.0", "--port", "8080", "--dir", "/agents", "--db-path", "/data/db.sqlite"]
```

### 3.3 Python SDK (PyPI)

The Python SDK wraps the Go binary and provides a Pythonic interface to MockAgents.

| Property             | Value                                            |
|----------------------|--------------------------------------------------|
| Package name         | `mockagents`                                     |
| Build tool           | Poetry + setuptools                              |
| Published to         | PyPI (`https://pypi.org/project/mockagents/`)    |
| Python versions      | 3.10, 3.11, 3.12, 3.13                          |

**Distribution strategy:**

| Wheel type                      | Contents                                  | Use case                       |
|---------------------------------|-------------------------------------------|--------------------------------|
| Platform-specific wheel         | Python SDK + embedded Go binary           | Default install, zero friction |
| Pure Python wheel (fallback)    | Python SDK only                           | Unsupported platforms          |
| Source distribution (sdist)     | Full source                               | Build from source              |

Platform-specific wheels are produced for:

- `manylinux_2_17_x86_64`
- `manylinux_2_17_aarch64`
- `macosx_11_0_x86_64`
- `macosx_11_0_arm64`
- `win_amd64`

When the pure Python wheel is installed, the CLI falls back to looking for a system-installed `mockagents` binary on `$PATH`. If not found, the SDK raises a clear error message with installation instructions.

**pyproject.toml (excerpt):**

```toml
[project]
name = "mockagents"
version = "0.1.0"
description = "Python SDK for MockAgents - a mock agent platform for testing AI agent integrations"
readme = "README.md"
license = { text = "Apache-2.0" }
requires-python = ">=3.10"
authors = [
    { name = "MockAgents Team" },
]
classifiers = [
    "Development Status :: 3 - Alpha",
    "Intended Audience :: Developers",
    "License :: OSI Approved :: Apache Software License",
    "Programming Language :: Python :: 3.10",
    "Programming Language :: Python :: 3.11",
    "Programming Language :: Python :: 3.12",
    "Programming Language :: Python :: 3.13",
    "Topic :: Software Development :: Testing",
]

dependencies = [
    "pyyaml>=6.0",
    "httpx>=0.25.0",
    "pydantic>=2.0",
]

[project.scripts]
mockagents = "mockagents.cli:main"

[project.urls]
Homepage = "https://github.com/mockagents/mockagents"
Documentation = "https://mockagents.dev/docs"
Repository = "https://github.com/mockagents/mockagents"
Issues = "https://github.com/mockagents/mockagents/issues"
```

---

## 4. Environment Definitions

### 4.1 Development (Local)

The standard environment for individual developers building and testing agent integrations.

```
Developer Machine
+-----------------------------------------------+
|  Project Directory                             |
|  +-------------------------------------------+|
|  | agents/                                   ||
|  |   assistant.yaml                          ||
|  |   tool-caller.yaml                        ||
|  | .mockagents/                              ||
|  |   db.sqlite  (auto-created)               ||
|  +-------------------------------------------+|
|                                                |
|  $ mockagents start                            |
|  Listening on http://127.0.0.1:8080            |
+-----------------------------------------------+
```

**Installation methods (any one):**

```bash
# Option A: Go install
go install github.com/mockagents/mockagents/cmd/mockagents@latest

# Option B: Binary download
curl -fsSL https://github.com/mockagents/mockagents/releases/latest/download/install.sh | bash

# Option C: pip
pip install mockagents
```

**Characteristics:**
- Agent YAML files live in the project directory (typically `./agents/`).
- SQLite database stored at `.mockagents/db.sqlite` (auto-created on first run).
- Binds to `127.0.0.1` by default; no network exposure.
- No external dependencies or network access required after installation.
- Configuration via CLI flags or environment variables.

### 4.2 CI/CD (GitHub Actions)

MockAgents in CI validates that your application correctly integrates with agent APIs without calling real services.

**Example workflow (`.github/workflows/test-with-mockagents.yml`):**

```yaml
name: Test with MockAgents

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  test:
    runs-on: ubuntu-latest

    steps:
      - name: Checkout code
        uses: actions/checkout@v4

      - name: Install MockAgents
        run: |
          MOCKAGENTS_VERSION="v0.1.0"
          curl -fsSL "https://github.com/mockagents/mockagents/releases/download/${MOCKAGENTS_VERSION}/mockagents_${MOCKAGENTS_VERSION}_linux_amd64.tar.gz" \
            | tar xz -C /usr/local/bin/

      - name: Cache MockAgents binary
        uses: actions/cache@v4
        with:
          path: /usr/local/bin/mockagents
          key: mockagents-${{ runner.os }}-v0.1.0

      - name: Start MockAgents in background
        run: |
          mockagents start --dir ./agents --log-format json &
          # Wait for server to be ready
          for i in $(seq 1 30); do
            curl -sf http://localhost:8080/api/v1/health && break
            sleep 1
          done

      - name: Run integration tests
        run: |
          # Point your tests at the mock server
          export AGENT_API_URL="http://localhost:8080"
          go test ./... -tags=integration

      - name: Stop MockAgents
        if: always()
        run: pkill mockagents || true
```

**Using Docker service container:**

```yaml
name: Test with MockAgents (Docker)

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest

    services:
      mockagents:
        image: mockagents/mockagents:v0.1.0
        ports:
          - 8080:8080
        volumes:
          - ${{ github.workspace }}/agents:/agents
        options: >-
          --health-cmd "wget -qO- http://localhost:8080/api/v1/health"
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5

    steps:
      - uses: actions/checkout@v4

      - name: Run tests
        run: |
          export AGENT_API_URL="http://localhost:8080"
          npm test
```

### 4.3 CI/CD (Other Platforms)

#### GitLab CI (`.gitlab-ci.yml`)

```yaml
stages:
  - test

test-with-mockagents:
  stage: test
  image: golang:1.22
  services:
    - name: mockagents/mockagents:v0.1.0
      alias: mockagents
  variables:
    AGENT_API_URL: "http://mockagents:8080"
  before_script:
    - |
      for i in $(seq 1 30); do
        curl -sf http://mockagents:8080/api/v1/health && break
        sleep 1
      done
  script:
    - go test ./... -tags=integration
```

#### Jenkins (`Jenkinsfile`)

```groovy
pipeline {
    agent any

    stages {
        stage('Setup MockAgents') {
            steps {
                sh '''
                    MOCKAGENTS_VERSION="v0.1.0"
                    curl -fsSL "https://github.com/mockagents/mockagents/releases/download/${MOCKAGENTS_VERSION}/mockagents_${MOCKAGENTS_VERSION}_linux_amd64.tar.gz" \
                        | tar xz -C /usr/local/bin/
                    mockagents start --dir ./agents --log-format json &
                    sleep 5
                '''
            }
        }

        stage('Integration Tests') {
            environment {
                AGENT_API_URL = 'http://localhost:8080'
            }
            steps {
                sh 'go test ./... -tags=integration'
            }
        }
    }

    post {
        always {
            sh 'pkill mockagents || true'
        }
    }
}
```

#### Azure DevOps (`azure-pipelines.yml`)

```yaml
trigger:
  - main

pool:
  vmImage: 'ubuntu-latest'

steps:
  - script: |
      MOCKAGENTS_VERSION="v0.1.0"
      curl -fsSL "https://github.com/mockagents/mockagents/releases/download/${MOCKAGENTS_VERSION}/mockagents_${MOCKAGENTS_VERSION}_linux_amd64.tar.gz" \
        | tar xz -C /usr/local/bin/
    displayName: 'Install MockAgents'

  - script: |
      mockagents start --dir ./agents --log-format json &
      for i in $(seq 1 30); do
        curl -sf http://localhost:8080/api/v1/health && break
        sleep 1
      done
    displayName: 'Start MockAgents'

  - script: |
      export AGENT_API_URL="http://localhost:8080"
      go test ./... -tags=integration
    displayName: 'Run Integration Tests'

  - script: pkill mockagents || true
    condition: always()
    displayName: 'Cleanup'
```

### 4.4 Docker Compose (Team Development)

For teams that want a shared mock server running alongside other services.

**`docker-compose.yml`:**

```yaml
version: "3.9"

services:
  mockagents:
    image: mockagents/mockagents:v0.1.0
    container_name: mockagents
    ports:
      - "8080:8080"
    volumes:
      - ./agents:/agents:ro
      - mockagents-data:/data
    environment:
      MOCKAGENTS_LOG_LEVEL: debug
      MOCKAGENTS_LOG_FORMAT: json
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost:8080/api/v1/health"]
      interval: 10s
      timeout: 5s
      retries: 3
      start_period: 5s
    restart: unless-stopped

  # Example: your application under test
  app:
    build: .
    depends_on:
      mockagents:
        condition: service_healthy
    environment:
      AGENT_API_URL: http://mockagents:8080

volumes:
  mockagents-data:
```

---

## 5. Release Process

### 5.1 Version Strategy

MockAgents follows [Semantic Versioning 2.0.0](https://semver.org/):

| Component | When to increment                                                  |
|-----------|--------------------------------------------------------------------|
| MAJOR     | Breaking changes to CLI interface, API, or agent definition format |
| MINOR     | New features, non-breaking additions to API or YAML schema         |
| PATCH     | Bug fixes, documentation updates, dependency bumps                 |

**Pre-release identifiers:**

| Tag               | Meaning                                             |
|--------------------|-----------------------------------------------------|
| `v0.1.0-alpha`    | Feature incomplete, API may change at any time       |
| `v0.1.0-beta`     | Feature complete, API may still change               |
| `v0.1.0-rc.1`     | Release candidate, API frozen, bug fixes only        |
| `v0.1.0`          | Stable release                                       |

The MVP release is **v0.1.0**. The `0.x` series signals that breaking changes may occur between minor versions. The commitment to full backward compatibility begins at `v1.0.0`.

### 5.2 Release Pipeline (GitHub Actions)

The release pipeline is triggered by pushing a version tag to the `main` branch.

**Pipeline stages:**

```
Tag push (v*)
    |
    v
[1. Build] --> [2. Test] --> [3. Publish] --> [4. Verify] --> [5. Announce]
    |               |             |                |               |
    |  Run tests    |  Smoke test |  GitHub Release|  Download     |  Update docs
    |  GoReleaser   |  each binary|  Docker push   |  pip install  |  Release notes
    |  Docker build |  Docker run |  PyPI publish  |  docker pull  |
    |  Python wheels|  pip install|                |               |
```

**Complete workflow (`.github/workflows/release.yml`):**

```yaml
name: Release

on:
  push:
    tags:
      - "v*"

permissions:
  contents: write
  packages: write
  id-token: write  # For PyPI trusted publishing

env:
  DOCKER_IMAGE: mockagents/mockagents
  GHCR_IMAGE: ghcr.io/mockagents/mockagents

jobs:
  # ===========================================================================
  # Stage 1: Build
  # ===========================================================================
  test:
    name: Run Test Suite
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"

      - name: Run unit tests
        run: go test ./... -race -coverprofile=coverage.out

      - name: Run integration tests
        run: go test ./... -tags=integration -race

      - name: Run E2E tests
        run: go test ./... -tags=e2e

  build-binaries:
    name: Build Go Binaries
    needs: test
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"

      - name: Run GoReleaser
        uses: goreleaser/goreleaser-action@v5
        with:
          version: latest
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}

      - name: Upload binary artifacts
        uses: actions/upload-artifact@v4
        with:
          name: binaries
          path: dist/mockagents_*
          retention-days: 5

  build-docker:
    name: Build Docker Image
    needs: test
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v3

      - name: Extract version from tag
        id: version
        run: |
          VERSION=${GITHUB_REF_NAME#v}
          MAJOR=$(echo $VERSION | cut -d. -f1)
          MINOR=$(echo $VERSION | cut -d. -f1-2)
          echo "version=$VERSION" >> $GITHUB_OUTPUT
          echo "major=v$MAJOR" >> $GITHUB_OUTPUT
          echo "minor=v$MINOR" >> $GITHUB_OUTPUT

      - name: Login to Docker Hub
        uses: docker/login-action@v3
        with:
          username: ${{ secrets.DOCKERHUB_USERNAME }}
          password: ${{ secrets.DOCKERHUB_TOKEN }}

      - name: Login to GHCR
        uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Build and push Docker image
        uses: docker/build-push-action@v5
        with:
          context: .
          push: true
          platforms: linux/amd64,linux/arm64
          tags: |
            ${{ env.DOCKER_IMAGE }}:${{ github.ref_name }}
            ${{ env.DOCKER_IMAGE }}:${{ steps.version.outputs.minor }}
            ${{ env.DOCKER_IMAGE }}:${{ steps.version.outputs.major }}
            ${{ env.DOCKER_IMAGE }}:latest
            ${{ env.GHCR_IMAGE }}:${{ github.ref_name }}
            ${{ env.GHCR_IMAGE }}:${{ steps.version.outputs.minor }}
            ${{ env.GHCR_IMAGE }}:${{ steps.version.outputs.major }}
            ${{ env.GHCR_IMAGE }}:latest
          cache-from: type=gha
          cache-to: type=gha,mode=max

  build-python:
    name: Build Python Wheels
    needs: test
    runs-on: ubuntu-latest
    strategy:
      matrix:
        include:
          - os: ubuntu-latest
            platform: manylinux_2_17_x86_64
          - os: ubuntu-latest
            platform: manylinux_2_17_aarch64
          - os: macos-latest
            platform: macosx_11_0_x86_64
          - os: macos-latest
            platform: macosx_11_0_arm64
          - os: windows-latest
            platform: win_amd64
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-python@v5
        with:
          python-version: "3.12"

      - name: Install build tools
        run: pip install build wheel

      - name: Build platform wheel
        working-directory: sdk/python
        run: python -m build --wheel

      - name: Build sdist
        if: matrix.platform == 'manylinux_2_17_x86_64'
        working-directory: sdk/python
        run: python -m build --sdist

      - name: Upload wheel artifacts
        uses: actions/upload-artifact@v4
        with:
          name: python-${{ matrix.platform }}
          path: sdk/python/dist/*
          retention-days: 5

  # ===========================================================================
  # Stage 2: Smoke Tests
  # ===========================================================================
  smoke-test-binary:
    name: Smoke Test Binary (${{ matrix.os }})
    needs: build-binaries
    runs-on: ${{ matrix.os }}
    strategy:
      matrix:
        os: [ubuntu-latest, macos-latest, windows-latest]
    steps:
      - name: Download binaries
        uses: actions/download-artifact@v4
        with:
          name: binaries

      - name: Extract and test (Linux/macOS)
        if: runner.os != 'Windows'
        run: |
          tar xzf mockagents_*_$(uname -s | tr '[:upper:]' '[:lower:]')_amd64.tar.gz
          chmod +x mockagents
          ./mockagents --version
          ./mockagents --help

      - name: Extract and test (Windows)
        if: runner.os == 'Windows'
        shell: pwsh
        run: |
          Expand-Archive -Path mockagents_*_windows_amd64.zip -DestinationPath .
          .\mockagents.exe --version
          .\mockagents.exe --help

  smoke-test-docker:
    name: Smoke Test Docker Image
    needs: build-docker
    runs-on: ubuntu-latest
    steps:
      - name: Pull and run image
        run: |
          docker pull ${{ env.DOCKER_IMAGE }}:${{ github.ref_name }}
          docker run -d --name mockagents-test \
            -p 8080:8080 \
            ${{ env.DOCKER_IMAGE }}:${{ github.ref_name }}
          sleep 5

      - name: Check health endpoint
        run: |
          HTTP_STATUS=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/api/v1/health)
          if [ "$HTTP_STATUS" != "200" ]; then
            echo "Health check failed with status: $HTTP_STATUS"
            docker logs mockagents-test
            exit 1
          fi
          echo "Health check passed"

      - name: Check version
        run: docker exec mockagents-test mockagents --version

      - name: Cleanup
        if: always()
        run: docker rm -f mockagents-test

  smoke-test-python:
    name: Smoke Test Python Package
    needs: build-python
    runs-on: ubuntu-latest
    strategy:
      matrix:
        python-version: ["3.10", "3.11", "3.12", "3.13"]
    steps:
      - uses: actions/setup-python@v5
        with:
          python-version: ${{ matrix.python-version }}

      - name: Download wheel
        uses: actions/download-artifact@v4
        with:
          name: python-manylinux_2_17_x86_64
          path: dist/

      - name: Install and test
        run: |
          pip install dist/*.whl
          python -c "import mockagents; print(mockagents.__version__)"
          mockagents --version

  # ===========================================================================
  # Stage 3: Publish
  # ===========================================================================
  publish-pypi:
    name: Publish to PyPI
    needs: [smoke-test-binary, smoke-test-docker, smoke-test-python]
    runs-on: ubuntu-latest
    environment: pypi
    steps:
      - name: Download all Python artifacts
        uses: actions/download-artifact@v4
        with:
          pattern: python-*
          path: dist/
          merge-multiple: true

      - name: Publish to PyPI
        uses: pypa/gh-action-pypi-publish@release/v1
        with:
          packages-dir: dist/

  # ===========================================================================
  # Stage 4: Verify
  # ===========================================================================
  verify:
    name: Post-Publish Verification
    needs: [publish-pypi]
    runs-on: ubuntu-latest
    steps:
      - name: Verify GitHub Release
        run: |
          VERSION=${{ github.ref_name }}
          # Check that release assets exist
          curl -fsSL "https://github.com/mockagents/mockagents/releases/download/${VERSION}/checksums.txt" > /dev/null
          echo "GitHub Release assets verified"

      - name: Verify PyPI package
        run: |
          sleep 30  # Allow PyPI index to update
          pip install mockagents==${VERSION#v}
          python -c "import mockagents; print(mockagents.__version__)"
          echo "PyPI package verified"

      - name: Verify Docker image
        run: |
          docker pull ${{ env.DOCKER_IMAGE }}:${{ github.ref_name }}
          docker run --rm ${{ env.DOCKER_IMAGE }}:${{ github.ref_name }} --version
          echo "Docker image verified"

  # ===========================================================================
  # Stage 5: Announce
  # ===========================================================================
  announce:
    name: Post-Release Announcements
    needs: verify
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Update documentation site
        run: |
          echo "Triggering docs rebuild for ${{ github.ref_name }}"
          # Trigger docs site deployment (e.g., Netlify build hook)
          # curl -X POST -d {} ${{ secrets.DOCS_BUILD_HOOK }}

      - name: Generate release summary
        run: |
          echo "## Release ${{ github.ref_name }}" >> $GITHUB_STEP_SUMMARY
          echo "" >> $GITHUB_STEP_SUMMARY
          echo "- Binary: https://github.com/mockagents/mockagents/releases/tag/${{ github.ref_name }}" >> $GITHUB_STEP_SUMMARY
          echo "- Docker: \`docker pull mockagents/mockagents:${{ github.ref_name }}\`" >> $GITHUB_STEP_SUMMARY
          echo "- PyPI: \`pip install mockagents==${VERSION#v}\`" >> $GITHUB_STEP_SUMMARY
```

### 5.3 Pre-Release Process

For alpha, beta, and release candidate builds:

1. **Tag with pre-release identifier:**
   ```bash
   git tag v0.1.0-alpha.1
   git push origin v0.1.0-alpha.1
   ```

2. **Automatic handling in pipeline:**
   - GoReleaser detects the pre-release suffix and marks the GitHub Release as a pre-release.
   - Docker tags include the full pre-release identifier (e.g., `v0.1.0-alpha.1`). The `latest` tag is NOT updated.
   - Python package is published to **TestPyPI** first for validation:
     ```bash
     pip install --index-url https://test.pypi.org/simple/ mockagents==0.1.0a1
     ```
   - After TestPyPI validation, the package is published to PyPI with the PEP 440 pre-release version (e.g., `0.1.0a1`, `0.1.0b1`, `0.1.0rc1`).

3. **Pre-release version mapping:**

   | Git tag           | PyPI version | Docker tag         | GitHub Release |
   |-------------------|--------------|--------------------|----------------|
   | `v0.1.0-alpha.1`  | `0.1.0a1`   | `v0.1.0-alpha.1`   | Pre-release    |
   | `v0.1.0-beta.1`   | `0.1.0b1`   | `v0.1.0-beta.1`    | Pre-release    |
   | `v0.1.0-rc.1`     | `0.1.0rc1`  | `v0.1.0-rc.1`      | Pre-release    |
   | `v0.1.0`          | `0.1.0`     | `v0.1.0`, `latest` | Stable         |

### 5.4 Hotfix Process

For emergency fixes to a released version:

```
main ─────●──────────────────────●─────────
          │ v0.1.0               │ (merge back)
          │                      │
          └──── hotfix/v0.1.1 ───┘
                  │           │
                  fix + test  tag v0.1.1
```

**Steps:**

1. **Create hotfix branch from the release tag:**
   ```bash
   git checkout -b hotfix/v0.1.1 v0.1.0
   ```

2. **Apply the fix:**
   - Commit the minimal fix.
   - Write a regression test.
   - Run the full test suite locally.

3. **Tag and release:**
   ```bash
   git tag v0.1.1
   git push origin v0.1.1
   ```
   This triggers the standard release pipeline.

4. **Merge back to main:**
   ```bash
   git checkout main
   git merge hotfix/v0.1.1
   git push origin main
   ```

5. **Update CHANGELOG.md** with the hotfix entry.

---

## 6. Deployment Configurations

### 6.1 CLI Configuration

All configuration can be set via CLI flags or environment variables. Environment variables take precedence over defaults; CLI flags take precedence over environment variables.

| Flag           | Environment Variable       | Default                | Description                          |
|----------------|----------------------------|------------------------|--------------------------------------|
| `--port`       | `MOCKAGENTS_PORT`          | `8080`                 | HTTP server port                     |
| `--host`       | `MOCKAGENTS_HOST`          | `127.0.0.1`            | Bind address                         |
| `--dir`        | `MOCKAGENTS_DIR`           | `.`                    | Agent definitions directory          |
| `--log-level`  | `MOCKAGENTS_LOG_LEVEL`     | `info`                 | Log level (debug/info/warn/error)    |
| `--log-format` | `MOCKAGENTS_LOG_FORMAT`    | `text`                 | Log format (text/json)               |
| `--db-path`    | `MOCKAGENTS_DB_PATH`       | `.mockagents/db.sqlite`| SQLite database path                 |

**Precedence order (highest to lowest):**

1. CLI flags
2. Environment variables
3. Configuration file (`.mockagents.yaml`, future)
4. Built-in defaults

### 6.2 Docker Configuration

When running in Docker, configuration is passed via environment variables and volume mounts.

**Environment variable mapping:**

```bash
docker run \
  -e MOCKAGENTS_PORT=8080 \
  -e MOCKAGENTS_HOST=0.0.0.0 \
  -e MOCKAGENTS_LOG_LEVEL=debug \
  -e MOCKAGENTS_LOG_FORMAT=json \
  mockagents/mockagents:v0.1.0
```

**Volume mounts:**

| Host Path          | Container Path | Purpose                         |
|--------------------|----------------|---------------------------------|
| `./agents`         | `/agents`      | Agent definition YAML files     |
| `./data` (optional)| `/data`        | Persistent SQLite database      |

**Port mapping:**

| Host Port | Container Port | Protocol |
|-----------|----------------|----------|
| 8080      | 8080           | TCP      |

**Complete Docker run example:**

```bash
docker run -d \
  --name mockagents \
  -p 8080:8080 \
  -v "$(pwd)/agents:/agents:ro" \
  -v mockagents-data:/data \
  -e MOCKAGENTS_LOG_LEVEL=info \
  -e MOCKAGENTS_LOG_FORMAT=json \
  --restart unless-stopped \
  mockagents/mockagents:v0.1.0
```

---

## 7. Installation Methods

### 7.1 Binary Download (curl)

**Linux / macOS:**

```bash
# Download the latest release for your platform
curl -fsSL "https://github.com/mockagents/mockagents/releases/latest/download/mockagents_$(uname -s | tr '[:upper:]' '[:lower:]')_$(uname -m).tar.gz" \
  | tar xz

# Move to a directory on your PATH
sudo mv mockagents /usr/local/bin/

# Verify installation
mockagents --version
```

**Windows (PowerShell):**

```powershell
# Download and extract
Invoke-WebRequest -Uri "https://github.com/mockagents/mockagents/releases/latest/download/mockagents_windows_amd64.zip" -OutFile mockagents.zip
Expand-Archive -Path mockagents.zip -DestinationPath .

# Move to a directory on your PATH
Move-Item mockagents.exe C:\usr\local\bin\

# Verify installation
mockagents --version
```

**Verifying checksums:**

```bash
# Download checksums
curl -fsSL "https://github.com/mockagents/mockagents/releases/latest/download/checksums.txt" -o checksums.txt

# Verify (Linux)
sha256sum --check --ignore-missing checksums.txt

# Verify (macOS)
shasum -a 256 --check --ignore-missing checksums.txt
```

### 7.2 Go Install

Requires Go 1.22 or later:

```bash
go install github.com/mockagents/mockagents/cmd/mockagents@latest

# Or install a specific version
go install github.com/mockagents/mockagents/cmd/mockagents@v0.1.0

# Verify installation
mockagents --version
```

The binary is placed in `$GOPATH/bin` (or `$HOME/go/bin` by default). Ensure this directory is on your `$PATH`.

### 7.3 pip install

Requires Python 3.10 or later:

```bash
# Install the latest stable release
pip install mockagents

# Install a specific version
pip install mockagents==0.1.0

# Install in a virtual environment (recommended)
python -m venv .venv
source .venv/bin/activate
pip install mockagents

# Verify installation
mockagents --version
python -c "import mockagents; print(mockagents.__version__)"
```

The pip install includes the Go binary embedded in the wheel for supported platforms. On unsupported platforms, install the Go binary separately and ensure it is on your `$PATH`.

### 7.4 Docker

```bash
# Run with agent definitions from the current directory
docker run -v "$(pwd)/agents:/agents" -p 8080:8080 mockagents/mockagents

# Run a specific version
docker run -v "$(pwd)/agents:/agents" -p 8080:8080 mockagents/mockagents:v0.1.0

# Run from GitHub Container Registry
docker run -v "$(pwd)/agents:/agents" -p 8080:8080 ghcr.io/mockagents/mockagents:v0.1.0

# Run in detached mode with a name
docker run -d --name mockagents \
  -v "$(pwd)/agents:/agents" \
  -p 8080:8080 \
  mockagents/mockagents:v0.1.0
```

### 7.5 Homebrew (Future)

Planned for post-MVP release. The Homebrew formula will be maintained in a dedicated tap:

```bash
# Planned (not yet available)
brew tap mockagents/tap
brew install mockagents
```

---

## 8. Rollback Plan

Since MockAgents is a local tool with no shared infrastructure, rollback is performed by each user independently.

### 8.1 Rollback by Distribution Channel

| Channel  | Rollback procedure                                                                           |
|----------|----------------------------------------------------------------------------------------------|
| Binary   | Re-download the previous version from GitHub Releases and replace the current binary.         |
| Docker   | Pin the image tag to the previous version: `docker pull mockagents/mockagents:v0.0.9`         |
| PyPI     | Install the previous version: `pip install mockagents==0.0.9`                                 |
| Go       | Install the previous version: `go install ...@v0.0.9`                                        |

### 8.2 Emergency Response

If a release contains a critical bug:

1. **Immediate (within 1 hour):**
   - Mark the GitHub Release as a pre-release to remove it from the "latest" endpoint.
   - Post an issue on GitHub labeled `bug` and `critical`.

2. **Short-term (within 24 hours):**
   - Publish a hotfix release (see Section 5.4).
   - Update Docker `latest` tag to point to the fixed version.

3. **PyPI considerations:**
   - PyPI does not support deleting versions. Publish a new patch version with the fix.
   - If the release is dangerous, use `pip install mockagents!=0.1.0` guidance in the GitHub issue.
   - In extreme cases, file a request with PyPI support to yank the release.

### 8.3 Database Compatibility

MockAgents uses SQLite for local state. If a release changes the database schema:

- The binary includes automatic migration on startup.
- Migrations are forward-only. Downgrading to a previous version may require deleting `.mockagents/db.sqlite` (session/request logs are ephemeral and safe to lose).
- A warning is printed if a database was created by a newer version.

---

## 9. Monitoring and Health Checks

### 9.1 Health Endpoint

MockAgents exposes a health endpoint for liveness and readiness probes:

```
GET /api/v1/health
```

**Response (200 OK):**

```json
{
  "status": "healthy",
  "version": "0.1.0",
  "uptime": "2h15m30s",
  "agents_loaded": 3
}
```

This endpoint is used by:
- Docker `HEALTHCHECK` directive (see Dockerfile in Section 3.2).
- Kubernetes liveness/readiness probes (future).
- CI/CD startup polling (see CI examples in Section 4).

### 9.2 Docker Health Check

The Dockerfile includes a built-in health check:

```dockerfile
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget -qO- http://localhost:8080/api/v1/health || exit 1
```

Check container health status:

```bash
docker inspect --format='{{.State.Health.Status}}' mockagents
```

### 9.3 Logging

MockAgents supports structured logging for integration with log aggregation tools.

**Text format (default, for local development):**

```
2026-04-07T10:15:30Z INF agent loaded name=assistant path=agents/assistant.yaml
2026-04-07T10:15:30Z INF server started host=127.0.0.1 port=8080
2026-04-07T10:15:35Z INF request received method=POST path=/api/v1/chat agent=assistant
```

**JSON format (for CI/CD and Docker):**

```json
{"time":"2026-04-07T10:15:30Z","level":"info","msg":"agent loaded","name":"assistant","path":"agents/assistant.yaml"}
{"time":"2026-04-07T10:15:30Z","level":"info","msg":"server started","host":"127.0.0.1","port":8080}
{"time":"2026-04-07T10:15:35Z","level":"info","msg":"request received","method":"POST","path":"/api/v1/chat","agent":"assistant"}
```

Enable JSON logging with `--log-format json` or `MOCKAGENTS_LOG_FORMAT=json`.

---

## 10. Security Considerations

### 10.1 Binary Integrity

- Every release includes a `checksums.txt` file with SHA-256 hashes for all archives.
- Users should verify checksums after downloading (see Section 7.1).
- Future: GPG signing of releases and Sigstore cosign signatures.

### 10.2 Docker Image Security

- **Minimal base image**: Alpine 3.19 minimizes the attack surface.
- **Non-root user**: The container runs as a dedicated `mockagents` user.
- **Trivy scanning**: Every Docker image is scanned for known vulnerabilities in CI before publishing.
- **No secrets**: The image contains only the binary and CA certificates. No secrets, tokens, or credentials are embedded.
- **Read-only agent mount**: Agent definitions are mounted as read-only (`:ro`) in the recommended Docker Compose configuration.

### 10.3 Dependency Auditing

| Tool            | Target            | Frequency          |
|-----------------|-------------------|--------------------|
| `govulncheck`   | Go dependencies   | Every CI run       |
| `pip-audit`     | Python SDK deps   | Every CI run       |
| Trivy           | Docker image      | Every image build  |
| Dependabot      | All dependencies  | Weekly             |

### 10.4 Network Security

- **Localhost by default**: MockAgents binds to `127.0.0.1`, not `0.0.0.0`. It is not accessible from other machines unless explicitly configured.
- **Docker override**: In Docker, the entrypoint binds to `0.0.0.0` to be reachable from the host. Users should control exposure via Docker port mapping.
- **No authentication (MVP)**: The MVP does not include authentication. MockAgents is a development tool and should not be exposed to untrusted networks. Authentication is planned for a future release.
- **No TLS (MVP)**: TLS termination should be handled by a reverse proxy if needed. Native TLS support is planned for a future release.

---

## 11. Post-Deployment Verification Checklist

Run through this checklist after every stable release:

### Binary Verification

- [ ] Download `mockagents_v{VERSION}_linux_amd64.tar.gz` from GitHub Releases
- [ ] Verify SHA-256 checksum matches `checksums.txt`
- [ ] Run `mockagents --version` and confirm it prints the correct version
- [ ] Run `mockagents start --dir examples/agents` and confirm the server starts
- [ ] Send a request to `http://localhost:8080/api/v1/health` and confirm 200 response

### Python Package Verification

- [ ] Run `pip install mockagents=={VERSION}` in a fresh virtual environment
- [ ] Run `python -c "import mockagents; print(mockagents.__version__)"` and confirm correct version
- [ ] Run `mockagents --version` from the CLI and confirm correct version
- [ ] Verify installation on Python 3.10, 3.11, 3.12, and 3.13

### Docker Image Verification

- [ ] Run `docker pull mockagents/mockagents:v{VERSION}` and confirm it succeeds
- [ ] Run `docker pull ghcr.io/mockagents/mockagents:v{VERSION}` and confirm it succeeds
- [ ] Start container and confirm health check passes within 30 seconds
- [ ] Verify `docker pull mockagents/mockagents:latest` resolves to the new version

### End-to-End Verification

- [ ] Follow the Quick Start guide from the documentation using each installation method
- [ ] Create a simple agent definition, start the server, and send a chat completion request
- [ ] Confirm structured JSON logs work with `--log-format json`
- [ ] Verify the documentation site reflects the new version

### Release Artifacts

- [ ] GitHub Release page shows correct version, changelog, and all binary assets
- [ ] GitHub Release is NOT marked as pre-release (for stable releases)
- [ ] CHANGELOG.md is updated in the `main` branch
- [ ] Documentation site version selector includes the new version
