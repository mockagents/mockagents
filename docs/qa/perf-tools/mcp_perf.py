"""TC-PERF-12 — MCP tools/call throughput (MA-QA-PTP-001 v1.2).

Prereq: mockagents mcp --transport http --port 8081 --agents-dir agents
        (serving the weather-mcp example: tool get_forecast{city})
Usage:  python mcp_perf.py [port]

Run A: 50 workers sharing ONE session.
Run B: one session per worker.
A ratio >2x between them would indicate a session-lock bottleneck.
"""
import http.client
import itertools
import json
import sys
import threading
import time

HOST = "127.0.0.1"
PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 8081
_id = itertools.count(1)


def rpc(conn, method, params, session=None, proto=None):
    body = json.dumps({"jsonrpc": "2.0", "id": next(_id),
                       "method": method, "params": params})
    h = {"Content-Type": "application/json", "Accept": "application/json"}
    if session:
        h["Mcp-Session-Id"] = session
    if proto:
        h["MCP-Protocol-Version"] = proto
    conn.request("POST", "/mcp", body=body, headers=h)
    r = conn.getresponse()
    data = r.read()
    return r, (json.loads(data) if data else {})


def new_session():
    c = http.client.HTTPConnection(HOST, PORT, timeout=30)
    r, resp = rpc(c, "initialize", {
        "protocolVersion": "2025-06-18", "capabilities": {},
        "clientInfo": {"name": "qa-perf", "version": "1"}})
    sid = r.getheader("Mcp-Session-Id")
    proto = resp.get("result", {}).get("protocolVersion", "2025-06-18")
    c.close()
    return sid, proto


def run(mode, dur=60, workers=50):
    shared = new_session() if mode == "single" else None
    durs, errs = [], {}
    lock = threading.Lock()
    stop = threading.Event()

    def worker():
        sid, proto = shared if shared else new_session()
        c = http.client.HTTPConnection(HOST, PORT, timeout=60)
        local = []
        while not stop.is_set():
            s = time.perf_counter()
            try:
                r, resp = rpc(c, "tools/call",
                              {"name": "get_forecast",
                               "arguments": {"city": "tokyo"}}, sid, proto)
                if r.status == 200 and "result" in resp and not resp.get("error"):
                    local.append((time.perf_counter() - s) * 1000)
                else:
                    with lock:
                        k = f"s{r.status}:{str(resp.get('error'))[:30]}"
                        errs[k] = errs.get(k, 0) + 1
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
            durs.extend(local)

    ts = [threading.Thread(target=worker, daemon=True) for _ in range(workers)]
    t0 = time.time()
    for t in ts:
        t.start()
    time.sleep(dur)
    stop.set()
    for t in ts:
        t.join(timeout=70)
    el = time.time() - t0
    durs.sort()
    n = len(durs)
    p = lambda q: round(durs[min(n - 1, int(n * q))], 2) if n else 0
    print(f"{mode}-session: ok={n} errs={errs} rps={n/el:.0f} "
          f"p50={p(.5)} p95={p(.95)} p99={p(.99)}")
    return n / el


a = run("single")
time.sleep(3)
b = run("per-worker")
ratio = max(a, b) / max(min(a, b), 1)
print(f"single vs per-worker ratio: {ratio:.2f} "
      f"({'OK <2x' if ratio < 2 else 'SESSION BOTTLENECK >2x'})")
print("DONE")
