# Demo app image: the OpenAI Agents SDK customer-support agent.
FROM python:3.12-slim

WORKDIR /app

# Install deps first for layer caching.
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# App code.
COPY app/ ./app/

ENV PYTHONUNBUFFERED=1
# Default target; docker-compose points this at the mockagents service.
ENV MOCKAGENTS_BASE_URL=http://mockagents:8080/v1
ENV MOCKAGENTS_API_KEY=mock-key

# Default to the headline triage demo; override the command to run others, e.g.
#   docker compose run --rm demo python -m app.streaming_demo
CMD ["python", "-m", "app.triage_demo"]
