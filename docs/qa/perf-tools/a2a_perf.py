"""TC-PERF-13 — A2A throughput (MA-QA-PTP-001 v1.3).

Prereq:  mockagents a2a --agents-dir agents --server weather-a2a   (port 8083)
Usage:   python a2a_perf.py [port]

Phase A: agent-card probe.
Phase B: message/send, 50 workers x 60s — RPS, latency, id-echo check.
Phase C: message/stream, 20 workers x 60s — full-stream time, frame count,
         and that every stream terminates with a final:true status-update.
"""
import http.client
import itertools
import json
import sys
import threading
import time

PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 8083
HOST = "127.0.0.1"
_id = itertools.count(1)
H = {"Content-Type": "application/json"}


def send_rpc(conn, method, want_stream=False):
    rid = next(_id)
    body = json.dumps({
        "jsonrpc": "2.0", "id": rid, "method": method,
        "params": {"message": {
            "role": "user",
            "kind": "message",
            "messageId": f"m{rid}",
            "parts": [{"kind": "text", "text": "hello"}],
        }},
    })
    h = dict(H, Accept="text/event-stream") if want_stream else H
    conn.request("POST", "/", body=body, headers=h)
    r = conn.getresponse()
    data = r.read()
    return rid, r, data


def run(method, workers, dur, want_stream):
    durs, errs, meta = [], {}, {"id_mismatch": 0, "no_final": 0, "frames": 0}
    lock = threading.Lock()
    stop = threading.Event()

    def worker():
        c = http.client.HTTPConnection(HOST, PORT, timeout=60)
        local = []
        while not stop.is_set():
            s = time.perf_counter()
            try:
                rid, r, data = send_rpc(c, method, want_stream)
                el = (time.perf_counter() - s) * 1000
                if r.status != 200:
                    with lock:
                        errs[f"s{r.status}"] = errs.get(f"s{r.status}", 0) + 1
                    continue
                if want_stream:
                    frames = data.count(b"data:")
                    final = b'"final":true' in data.replace(b" ", b"")
                    with lock:
                        meta["frames"] += frames
                        if not final:
                            meta["no_final"] += 1
                    local.append(el)
                else:
                    obj = json.loads(data)
                    if obj.get("id") != rid:
                        with lock:
                            meta["id_mismatch"] += 1
                    if obj.get("error"):
                        with lock:
                            k = str(obj["error"])[:40]
                            errs[k] = errs.get(k, 0) + 1
                        continue
                    local.append(el)
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
    print(f"{method}: ok={n} rps={n/el:.0f} p50={p(.5)} p95={p(.95)} "
          f"p99={p(.99)} errs={errs} meta={meta}")
    return n / el


# Phase A — agent card
c = http.client.HTTPConnection(HOST, PORT, timeout=15)
c.request("GET", "/.well-known/agent-card.json")
r = c.getresponse()
card = r.read()
print(f"agent-card: {r.status} bytes={len(card)} "
      f"name={json.loads(card).get('name') if r.status == 200 else 'n/a'}")
c.close()

run("message/send", 50, 60, False)
time.sleep(2)
run("message/stream", 20, 60, True)
print("DONE")
