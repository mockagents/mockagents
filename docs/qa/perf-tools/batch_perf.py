"""TC-PERF-14 — Batch API fan-out throughput (MA-QA-PTP-001 v1.3).

Usage: python batch_perf.py [port]

A batch dispatches N engine calls from ONE request, so the metric that matters
is per-request fan-out cost as N grows — a rising curve means super-linear
behavior. Default batch delay is 0 (instant completion), so no delay header is
sent; the X-Mockagents-Batch-Delay-Ms header exists only for lifecycle tests.

Part A: Anthropic inline batches at N = 100 / 1,000 / 5,000 (x3 each).
Part B: OpenAI file-based lifecycle at N = 1,000 (upload -> create -> poll ->
        fetch output), timing the phases separately.
"""
import http.client
import json
import sys
import time
import uuid

PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 8080
HOST = "127.0.0.1"
ANT_MODEL = "perf-echo-ant-model"
OAI_MODEL = "perf-echo-model"


def conn():
    return http.client.HTTPConnection(HOST, PORT, timeout=300)


def req(c, method, path, body=None, headers=None, raw=False):
    h = headers or {"Content-Type": "application/json"}
    c.request(method, path, body=body, headers=h)
    r = c.getresponse()
    data = r.read()
    if raw:
        return r, data
    return r, (json.loads(data) if data else {})


# ---------- Part A: Anthropic inline ----------
def anthropic_batch(n):
    c = conn()
    reqs = [{"custom_id": f"c{i}",
             "params": {"model": ANT_MODEL, "max_tokens": 64,
                        "messages": [{"role": "user", "content": "hello"}]}}
            for i in range(n)]
    body = json.dumps({"requests": reqs})
    h = {"Content-Type": "application/json", "anthropic-version": "2023-06-01",
         "x-api-key": "mock"}
    t0 = time.perf_counter()
    r, obj = req(c, "POST", "/v1/messages/batches", body, h)
    create_ms = (time.perf_counter() - t0) * 1000
    if r.status not in (200, 201):
        print(f"  N={n} CREATE FAILED status={r.status} {str(obj)[:120]}")
        c.close()
        return None
    bid = obj["id"]
    # poll to terminal
    polls = 0
    while True:
        r, st = req(c, "GET", f"/v1/messages/batches/{bid}", None, h)
        polls += 1
        if st.get("processing_status") == "ended":
            break
        if polls > 600:
            print(f"  N={n} POLL TIMEOUT")
            c.close()
            return None
        time.sleep(0.01)
    total_ms = (time.perf_counter() - t0) * 1000
    counts = st.get("request_counts", {})
    c.close()
    return {"n": n, "create_ms": round(create_ms, 1),
            "total_ms": round(total_ms, 1),
            "per_req_ms": round(total_ms / n, 4),
            "succeeded": counts.get("succeeded"), "errored": counts.get("errored"),
            "polls": polls}


print("=== Part A: Anthropic inline fan-out ===")
for n in (100, 1000, 5000):
    runs = [anthropic_batch(n) for _ in range(3)]
    runs = [x for x in runs if x]
    if not runs:
        continue
    best = min(r["per_req_ms"] for r in runs)
    tot = [r["total_ms"] for r in runs]
    print(f"N={n:>5}  total_ms={tot}  per_req_ms(best)={best}  "
          f"succeeded={runs[0]['succeeded']} errored={runs[0]['errored']}")

# ---------- Part B: OpenAI file-based lifecycle ----------
print("=== Part B: OpenAI file-based lifecycle (N=1000) ===")
N = 1000
lines = "\n".join(json.dumps({
    "custom_id": f"c{i}", "method": "POST", "url": "/v1/chat/completions",
    "body": {"model": OAI_MODEL,
             "messages": [{"role": "user", "content": "hello"}]},
}) for i in range(N)).encode()

boundary = uuid.uuid4().hex
parts = (
    f"--{boundary}\r\nContent-Disposition: form-data; name=\"purpose\"\r\n\r\nbatch\r\n"
    f"--{boundary}\r\nContent-Disposition: form-data; name=\"file\"; filename=\"in.jsonl\"\r\n"
    f"Content-Type: application/jsonl\r\n\r\n"
).encode() + lines + f"\r\n--{boundary}--\r\n".encode()

c = conn()
t0 = time.perf_counter()
r, fobj = req(c, "POST", "/v1/files", parts,
              {"Content-Type": f"multipart/form-data; boundary={boundary}"})
upload_ms = (time.perf_counter() - t0) * 1000
if r.status not in (200, 201):
    print(f"  UPLOAD FAILED status={r.status} {str(fobj)[:160]}")
else:
    fid = fobj["id"]
    t1 = time.perf_counter()
    r, bobj = req(c, "POST", "/v1/batches", json.dumps({
        "input_file_id": fid, "endpoint": "/v1/chat/completions",
        "completion_window": "24h"}))
    create_ms = (time.perf_counter() - t1) * 1000
    bid = bobj["id"]
    t2 = time.perf_counter()
    polls = 0
    while True:
        r, st = req(c, "GET", f"/v1/batches/{bid}")
        polls += 1
        if st.get("status") in ("completed", "failed", "cancelled"):
            break
        if polls > 600:
            print("  POLL TIMEOUT")
            break
        time.sleep(0.01)
    poll_ms = (time.perf_counter() - t2) * 1000
    out_id = st.get("output_file_id")
    fetch_ms, out_lines = 0, 0
    if out_id:
        t3 = time.perf_counter()
        r, data = req(c, "GET", f"/v1/files/{out_id}/content", None, None, raw=True)
        fetch_ms = (time.perf_counter() - t3) * 1000
        out_lines = len([l for l in data.split(b"\n") if l.strip()])
    print(f"  upload={upload_ms:.1f}ms create={create_ms:.1f}ms "
          f"poll={poll_ms:.1f}ms(x{polls}) fetch={fetch_ms:.1f}ms")
    print(f"  status={st.get('status')} counts={st.get('request_counts')} "
          f"output_lines={out_lines} (expected {N})")
c.close()
print("DONE")
