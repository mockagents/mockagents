# Demo app image: the Google ADK customer-support agent.
#
# ADK is a pure-Python framework (no CLI subprocess, unlike the Claude demo), so
# this is a plain Python image.
FROM python:3.12-slim

WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY app/ ./app/

ENV PYTHONUNBUFFERED=1
# Server ROOT (no /v1). The native Gemini path uses it directly; the LiteLLM
# bridge appends /v1 itself.
ENV MOCKAGENTS_BASE_URL=http://mockagents:8080
ENV MOCKAGENTS_API_KEY=mock-key

# Default to the native-Gemini triage demo; override to run the others, e.g.
#   docker compose run --rm demo python -m app.litellm_demo
CMD ["python", "-m", "app.triage_demo"]
