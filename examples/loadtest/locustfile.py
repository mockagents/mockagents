"""Locust load test against a MockAgents target (FB-05).

Load-test your LLM app's HTTP path for free: point Locust at the mock instead of
a real provider. The `load-target` agent streams with realistic latency
percentiles, so time-to-first-token / inter-token timing look production-like.

Run:
    mockagents start --agents-dir examples
    locust -f examples/loadtest/locustfile.py --host http://localhost:8080

Then open http://localhost:8089 and start a swarm, or headless:
    locust -f examples/loadtest/locustfile.py --host http://localhost:8080 \
           --headless -u 20 -r 5 -t 30s
"""
import time

from locust import HttpUser, task, between


class LLMAppUser(HttpUser):
    wait_time = between(0.5, 2.0)

    @task
    def chat_streaming(self):
        payload = {
            "model": "load-target-model",
            "stream": True,
            "messages": [{"role": "user", "content": "hello"}],
        }
        # stream=True so we can measure time-to-first-token: the mock applies the
        # configured TTFT before the first SSE frame, so the first line we read
        # arrives after ~TTFT. We report it as a separate "TTFT" metric in the
        # Locust stats, alongside the default full-stream response time.
        start = time.perf_counter()
        with self.client.post(
            "/v1/chat/completions",
            json=payload,
            headers={"Authorization": "Bearer mock"},
            stream=True,
            name="/v1/chat/completions (stream)",
            catch_response=True,
        ) as resp:
            if resp.status_code != 200:
                resp.failure(f"status {resp.status_code}")
                return
            first_byte_ms = None
            saw_done = False
            for line in resp.iter_lines():
                if line and first_byte_ms is None:
                    first_byte_ms = (time.perf_counter() - start) * 1000
                if line and b"[DONE]" in line:
                    saw_done = True
            if not saw_done:
                resp.failure("stream did not terminate with [DONE]")
                return
            resp.success()
            if first_byte_ms is not None:
                # Surface TTFT as its own entry in the Locust stats table.
                self.environment.events.request.fire(
                    request_type="TTFT",
                    name="/v1/chat/completions (first token)",
                    response_time=first_byte_ms,
                    response_length=0,
                    exception=None,
                    context={},
                )
