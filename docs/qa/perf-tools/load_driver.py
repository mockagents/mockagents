"""Generic throughput driver (MA-QA-PTP-001) — stdlib only, no k6 needed.

Usage:
    python load_driver.py <port> <seconds> [workers] [model]

Reports RPS and client-side p50/p95/p99/max. Cross-check any surprising
`max` against the server's own numbers before filing:
    SELECT MAX(latency_ms) FROM interaction_logs;
(see TROUBLESHOOTING.md §5 — big client max with a millisecond server max is
a loopback artifact, not a server stall).
"""
import http.client
import json
import sys
import threading
import time

PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 8080
DUR = int(sys.argv[2]) if len(sys.argv) > 2 else 60
WORKERS = int(sys.argv[3]) if len(sys.argv) > 3 else 50
MODEL = sys.argv[4] if len(sys.argv) > 4 else "perf-echo-model"

payload = json.dumps({"model": MODEL,
                      "messages": [{"role": "user", "content": "hello"}]})
H = {"Content-Type": "application/json", "Authorization": "Bearer mock"}
durs, errs = [], {}
lock = threading.Lock()
stop = threading.Event()


def worker():
    c = http.client.HTTPConnection("127.0.0.1", PORT, timeout=60)
    local = []
    while not stop.is_set():
        s = time.perf_counter()
        try:
            c.request("POST", "/v1/chat/completions", body=payload, headers=H)
            r = c.getresponse()
            r.read()
            if r.status == 200:
                local.append((time.perf_counter() - s) * 1000)
            else:
                with lock:
                    errs[r.status] = errs.get(r.status, 0) + 1
        except Exception as e:
            with lock:
                k = repr(e)[:40]
                errs[k] = errs.get(k, 0) + 1
            try:
                c.close()
            except Exception:
                pass
            c = http.client.HTTPConnection("127.0.0.1", PORT, timeout=60)
    with lock:
        durs.extend(local)


ts = [threading.Thread(target=worker, daemon=True) for _ in range(WORKERS)]
t0 = time.time()
for t in ts:
    t.start()
time.sleep(DUR)
stop.set()
for t in ts:
    t.join(timeout=70)
el = time.time() - t0
durs.sort()
n = len(durs)
p = lambda q: round(durs[min(n - 1, int(n * q))], 2) if n else 0
print(f"port={PORT} model={MODEL} ok={n} errs={errs} rps={n/el:.0f} "
      f"p50={p(.5)} p95={p(.95)} p99={p(.99)} max={p(1.0)}")
