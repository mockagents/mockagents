"""TC-PERF-07 / TC-PERF-15 — multi-tenant auth + quota under load.

Usage:
    python multitenant_perf.py <server-log-file> [port]

The bootstrap platform key is parsed from the server log and never printed.

Start the server FIRST, uncapped, capturing its output:

    # SQLite store (TC-PERF-07)
    MOCKAGENTS_MULTI_TENANT=1 MOCKAGENTS_LOG_BODIES=sanitized \\
      ./mockagents start --agents-dir agents --log-level warn > mt.log 2>&1 &

    # Postgres store (TC-PERF-15) — same command plus a DSN
    MOCKAGENTS_MULTI_TENANT=1 MOCKAGENTS_TENANCY_DSN='postgres://user:pw@host:5432/db' \\
      MOCKAGENTS_LOG_BODIES=sanitized \\
      ./mockagents start --agents-dir agents --log-level warn > mt.log 2>&1 &

Do NOT pipe the server through grep/head — buffering can swallow the
shown-once bootstrap-key banner this script needs.
Do NOT set MOCKAGENTS_DEFAULT_RATE_* at startup: phase B measures warm auth
overhead and a startup cap would limit the measurement itself. The cap is
applied at runtime in phase C.

Phases
  A  five sequential requests — request 1 pays bcrypt, 2-5 hit the auth cache
  B  50 workers x 60s uncapped — the warm authenticated throughput number
  C  apply rate cap via PUT /quota, then 200 workers x 30s — the burst must
     yield only 200s and 429s-with-Retry-After, never 5xx

For TC-PERF-15, run this against Postgres and compare with a SAME-DAY SQLite
run on the same machine. Expected if healthy: warm-path throughput within
+/-20% of SQLite (the auth cache should make the backend nearly irrelevant on
the hot path), unchanged cold bcrypt cost, identical burst behaviour. Note in
the results if the instance is remote — a network hop adds latency SQLite
does not have.
"""
import http.client
import json
import re
import sys
import threading
import time

LOG = sys.argv[1] if len(sys.argv) > 1 else "mt.log"
PORT = int(sys.argv[2]) if len(sys.argv) > 2 else 8080
HOST = "127.0.0.1"
MODEL = "perf-echo-model"

with open(LOG, encoding="utf-8", errors="replace") as f:
    m = re.search(r"shown once\):\s*(\S+)", f.read())
if not m:
    sys.exit(f"bootstrap key not found in {LOG} — was the server output piped "
             "through grep/head, or is it not in multi-tenant mode?")
PLATFORM_KEY = m.group(1)

payload = json.dumps({"model": MODEL,
                      "messages": [{"role": "user", "content": "hello"}]})


def api(method, path, body=None, key=PLATFORM_KEY):
    c = http.client.HTTPConnection(HOST, PORT, timeout=30)
    h = {"Authorization": f"Bearer {key}", "Content-Type": "application/json"}
    c.request(method, path, body=json.dumps(body) if body else None, headers=h)
    r = c.getresponse()
    data = r.read()
    c.close()
    return r.status, (json.loads(data) if data else {})


# --- setup: find the tenant, mint an editor key ---
_, tenants = api("GET", "/api/v1/tenants")
tenant = tenants["tenants"][0] if isinstance(tenants, dict) else tenants[0]
tid = tenant["id"]
st, keyresp = api("POST", f"/api/v1/tenants/{tid}/keys",
                  {"name": "perf-editor", "role": "editor"})
editor = next(v for k, v in keyresp.items()
              if isinstance(v, str) and ("plaintext" in k.lower() or len(v) > 30))
print(f"setup: tenant={tid} editor key minted (status {st})")

EH = {"Authorization": f"Bearer {editor}", "Content-Type": "application/json"}


def chat(conn):
    t0 = time.perf_counter()
    conn.request("POST", "/v1/chat/completions", body=payload, headers=EH)
    r = conn.getresponse()
    r.read()
    return r.status, (time.perf_counter() - t0) * 1000, r.getheader("Retry-After")


# --- Phase A: bcrypt cold vs warm ---
conn = http.client.HTTPConnection(HOST, PORT, timeout=60)
timings = [round(chat(conn)[1], 2) for _ in range(5)]
conn.close()
print(f"phaseA cold->warm ms: {timings}")

# --- Phase B: warm authenticated throughput, uncapped ---
durs, errs = [], [0]
lock = threading.Lock()
stopB = threading.Event()


def workerB():
    c = http.client.HTTPConnection(HOST, PORT, timeout=60)
    local = []
    while not stopB.is_set():
        try:
            s, ms, _ = chat(c)
            if s == 200:
                local.append(ms)
            else:
                with lock:
                    errs[0] += 1
        except Exception:
            with lock:
                errs[0] += 1
            try:
                c.close()
            except Exception:
                pass
            c = http.client.HTTPConnection(HOST, PORT, timeout=60)
    with lock:
        durs.extend(local)


tb = [threading.Thread(target=workerB, daemon=True) for _ in range(50)]
t0 = time.time()
for t in tb:
    t.start()
time.sleep(60)
stopB.set()
for t in tb:
    t.join(timeout=30)
el = time.time() - t0
durs.sort()
n = len(durs)
p = lambda q: round(durs[min(n - 1, int(n * q))], 2) if n else 0
print(f"phaseB warm: ok={n} errs={errs[0]} rps={n/el:.0f} "
      f"p50={p(.5)} p95={p(.95)} p99={p(.99)}")

# --- Phase C: apply the cap at runtime, then burst ---
st, _ = api("PUT", f"/api/v1/tenants/{tid}/quota",
            {"rate_per_sec": 100, "rate_burst": 200})
print(f"quota cap applied at runtime (status {st})")
time.sleep(1)

counts, retry_after = {}, [0, 0]
stopC = threading.Event()
lockC = threading.Lock()


def workerC():
    c = http.client.HTTPConnection(HOST, PORT, timeout=60)
    while not stopC.is_set():
        try:
            s, _, ra = chat(c)
            with lockC:
                counts[s] = counts.get(s, 0) + 1
                if s == 429:
                    retry_after[0 if ra else 1] += 1
        except Exception as e:
            with lockC:
                k = repr(e)[:40]
                counts[k] = counts.get(k, 0) + 1
            try:
                c.close()
            except Exception:
                pass
            c = http.client.HTTPConnection(HOST, PORT, timeout=60)


tc = [threading.Thread(target=workerC, daemon=True) for _ in range(200)]
for t in tc:
    t.start()
time.sleep(30)
stopC.set()
for t in tc:
    t.join(timeout=30)

print(f"phaseC burst: {dict(sorted(counts.items(), key=str))}")
print(f"  Retry-After present={retry_after[0]} missing={retry_after[1]}")
bad = [k for k in counts if isinstance(k, int) and k >= 500]
print("VERDICT:", "PASS — only 200/429, all 429s carry Retry-After"
      if not bad and retry_after[1] == 0
      else f"FAIL — unexpected statuses {bad} or 429s missing Retry-After")
print("DONE")
