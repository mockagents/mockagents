# Demo app image: the Claude Agent SDK customer-support agent.
#
# Unlike the OpenAI demo, the Claude Agent SDK drives the `claude` CLI as a
# subprocess, so the image needs BOTH Node (for the CLI) and Python (for the SDK).
FROM node:20-bookworm-slim

# Python + the Claude Code CLI.
RUN apt-get update \
    && apt-get install -y --no-install-recommends python3 python3-venv ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && npm install -g @anthropic-ai/claude-code

# Python deps in a venv (Debian's Python is externally-managed).
ENV VIRTUAL_ENV=/opt/venv
RUN python3 -m venv "$VIRTUAL_ENV"
ENV PATH="$VIRTUAL_ENV/bin:$PATH"
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# The `claude` CLI refuses bypassPermissions when running as root, so run as a
# non-root user with a writable HOME.
RUN useradd --create-home --uid 10001 demo
WORKDIR /app
COPY app/ ./app/
RUN chown -R demo:demo /app
USER demo
ENV HOME=/home/demo

ENV PYTHONUNBUFFERED=1
# docker-compose points this at the mockagents service ROOT (no /v1 — the CLI
# and the Anthropic SDK append the path themselves).
ENV MOCKAGENTS_BASE_URL=http://mockagents:8080
ENV MOCKAGENTS_API_KEY=mock-key

# Default to the headline triage demo; override to run the others, e.g.
#   docker compose run --rm demo python -m app.deterministic_smoke
CMD ["python", "-m", "app.triage_demo"]
