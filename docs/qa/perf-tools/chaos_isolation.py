"""TC-PERF-08 — chaos isolation (MA-QA-PTP-001).

Run TWO processes so the load roles don't share a GIL:
    python chaos_isolation.py healthy   # 20 workers, 180s, perf-echo-model
    python chaos_isolation.py chaos     # 20 workers, 120s, perf-slow-model
Start `chaos` ~30s after `healthy`.

The chaos target must be LATENCY-ONLY (no rate cap): a rate-capped agent
turns the chaos role into a fast-429 flood, which measures load scaling, not
isolation (cycle-2 finding). Chaos nests under spec.behavior.chaos and needs
enabled: true — a mis-nested block validates clean but never fires, so
sanity-probe the target before trusting a pass.
"""
import http.client
import json
import sys
import threading
import time

HOST, PORT = "127.0.0.1", 8080
ROLE = sys.argv[1] if len(sys.argv) > 1 else "healthy"
MODEL = "perf-echo-model" if ROLE == "healthy" else "perf-slow-model"
DUR = 180 if ROLE == "healthy" else 120
CH_START, CH_END = 30, 150          # chaos window, wall-clock from healthy start
H = {"Content-Type": "application/json", "Authorization": "Bearer mock"}
payload = json.dumps({"model": MODEL,
                      "messages": [{"role": "user", "content": "hello"}]})

t0 = time.time()
stop = threading.Event()
lock = threading.Lock()
rows, errs = [], {}


def worker():
    c = http.client.HTTPConnection(HOST, PORT, timeout=60)
    local = []
    while not stop.is_set():
        ts = time.time() - t0
        s = time.perf_counter()
        try:
            c.request("POST", "/v1/chat/completions", body=payload, headers=H)
            r = c.getresponse()
            r.read()
            if r.status == 200:
                local.append((ts, (time.perf_counter() - s) * 1000))
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
            c = http.client.HTTPConnection(HOST, PORT, timeout=60)
    with lock:
        rows.extend(local)


ts_ = [threading.Thread(target=worker, daemon=True) for _ in range(20)]
for t in ts_:
    t.start()
time.sleep(DUR)
stop.set()
for t in ts_:
    t.join(timeout=70)


def stats(sub):
    if not sub:
        return "n=0"
    v = sorted(d for _, d in sub)
    n = len(v)
    p = lambda q: round(v[min(n - 1, int(n * q))], 2)
    return f"n={n} p50={p(.5)} p95={p(.95)} p99={p(.99)} max={p(1.0)}"


p95 = lambda v: (sorted(v)[min(len(v) - 1, int(len(v) * .95))] if v else 0)
if ROLE == "healthy":
    before = [d for t_, d in rows if t_ < CH_START - 1]
    over = [d for t_, d in rows if CH_START + 6 <= t_ <= CH_END - 6]
    after = [d for t_, d in rows if t_ > CH_END + 3]
    print("solo-before:", stats([(0, d) for d in before]))
    print("overlap:    ", stats([(0, d) for d in over]))
    print("solo-after: ", stats([(0, d) for d in after]))
    ratio = p95(over) / p95(before) if p95(before) else 0
    print(f"p95 ratio overlap/solo = {ratio:.3f} (pass band 0.75-1.25)")
    print("VERDICT:", "PASS" if 0.75 <= ratio <= 1.25 else "FAIL")
else:
    print("chaos-role:", stats(rows))
print("errors:", errs)
