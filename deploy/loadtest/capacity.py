"""What bound purchase throughput in one capacity run (decision 0029).

Reads the files deploy/scripts/capacity-sweep.sh saved for a run and prints two
lines: what the run served with the signals that name its limit, then the
busiest pods. How to read the verdict: docs/plans/2026-10-09-capacity-on-a-bigger-box.md.
  python3 deploy/loadtest/capacity.py RUN_DIR
"""
import json
import os
import re
import sys
from datetime import datetime

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import saga_latency  # noqa: E402

CPU_BOUND = 0.85      # share of the node's vCPUs busy
EVENT_LOOP = 0.85     # cores one Node.js pod spends; its JavaScript runs on one thread
GP3_IOPS = 3000       # the box's root volume, gp3 at its baseline
DISK_BOUND = 0.8      # share of GP3_IOPS
WAIT_SHARE = 0.5      # share of active Postgres sessions in one wait class
TEMPORAL_WAIT = 0.5   # seconds a saga's first workflow task waits, p50
ERROR_SHARE = 0.01    # unexpected failures per attempted checkout
THROTTLED = 0.05      # k6's throttled CFS periods; past this k6 measured itself
NODE_JS = ("app-gateway-", "user-service-", "event-service-", "payment-service-",
           "payment-webhook-", "outbox-relay-")


def vector(path, label):
    """Returns {label value: sample} from a saved Prometheus query response."""
    with open(path) as f:
        return {r["metric"].get(label, ""): float(r["value"][1]) for r in json.load(f)["data"]["result"]}


def deltas(before, after):
    """Counter growth for each key in both; a key in one only started or ended mid-run."""
    return {k: after[k] - before[k] for k in after.keys() & before.keys()}


def pg_summary(lines, t0, t1):
    """Returns peak connections, max_connections, each wait class's share of active sessions, and the database most waited on for locks, in [t0, t1).

    Lines: S|epoch|connections|max_connections and W|epoch|class|event|database|count.
    An LWLock on the WAL counts as IO: either way the commit waits on the disk.
    """
    peak, limit, by_class, lock_dbs = 0, 0, {}, {}
    for line in lines:
        f = line.strip().split("|")
        if len(f) < 4 or not f[1].isdigit() or not t0 <= int(f[1]) < t1:
            continue
        if f[0] == "S":
            peak, limit = max(peak, int(f[2])), int(f[3])
        elif f[0] == "W" and len(f) == 6:
            kind = "IO" if f[2] == "LWLock" and f[3].startswith("WAL") else f[2]
            by_class[kind] = by_class.get(kind, 0) + int(f[5])
            if kind == "Lock":
                lock_dbs[f[4]] = lock_dbs.get(f[4], 0) + int(f[5])
    total = sum(by_class.values())
    shares = {k: v / total for k, v in by_class.items()} if total else {}
    return peak, limit, shares, max(lock_dbs, key=lock_dbs.get) if lock_dbs else "-"


def verdict(s):
    """Every limit the signals show, in the order the plan reads them; [] when none binds."""
    fired = []
    pod, cores = s["busiest_node_js"]
    if cores >= EVENT_LOOP:
        fired.append(f"event loop: {pod}")
    if s["busy_cores"] >= CPU_BOUND * s["vcpu"]:
        fired.append("cpu")
    if s["iops"] >= DISK_BOUND * GP3_IOPS or s["pg_shares"].get("IO", 0) >= WAIT_SHARE:
        fired.append("disk")
    if s["pg_shares"].get("Lock", 0) >= WAIT_SHARE:
        fired.append(f"locks: {s['lock_db']}")
    if s["first_task_p50"] >= TEMPORAL_WAIT:
        fired.append("temporal dispatch")
    if s["unexpected_share"] > ERROR_SHARE:
        fired.append("errors")
    return fired


def void(meta, throttled, periods):
    """Why the run measured something other than the system; [] when it did not."""
    reasons = []
    if meta["restarts_after"] > meta["restarts_before"]:
        reasons.append(f"{meta['restarts_after'] - meta['restarts_before']} container restarts")
    if periods == 0:
        reasons.append("k6 throttling unread")
    elif throttled / periods > THROTTLED:
        reasons.append(f"k6 throttled {100 * throttled / periods:.0f}%")
    return reasons


def text(path):
    with open(path) as f:
        return f.read()


def epoch(ts):
    return int(datetime.fromisoformat(ts.replace("Z", "+00:00")).timestamp())


def counter(log, name):
    m = re.search(rf'"{name}":\s*\{{[^}}]*"count":\s*(\d+)', log)
    return int(m.group(1)) if m else 0


def summarize(d):
    meta = json.loads(text(f"{d}/meta.json"))
    t0, t1 = epoch(meta["from"]), epoch(meta["to"])
    secs = t1 - t0
    log = text(f"{d}/k6.log")
    completed, unexpected = counter(log, "tb_orders_completed"), counter(log, "tb_unexpected_errors")
    client = saga_latency.load_client(f"{d}/k6.log")
    ok = [s for _, s, status in client if status < 400]

    pods = {p: c / secs for p, c in deltas(vector(f"{d}/pods-from.json", "pod"), vector(f"{d}/pods-to.json", "pod")).items()}
    modes = deltas(vector(f"{d}/node-from.json", "mode"), vector(f"{d}/node-to.json", "mode"))
    disk = deltas(vector(f"{d}/disk-from.json", ""), vector(f"{d}/disk-to.json", ""))
    node_js = [(p, c) for p, c in pods.items() if p.startswith(NODE_JS)]
    peak, limit, shares, lock_db = pg_summary(text(f"{d}/pg.txt").splitlines(), t0, t1)
    waits = [r["wft_queued"][0] for r in saga_latency.load_histories(f"{d}/histories.txt") if r["wft_queued"]]
    temporal = sum(c for p, c in pods.items() if p.startswith("temporal-") and not p.startswith("temporal-ui-"))
    postgres = pods.get("postgres-0", 0)

    s = {
        "vcpu": meta["vcpu"],
        "busy_cores": sum(v for m, v in modes.items() if m not in ("idle", "iowait", "steal")) / secs,
        "iowait_cores": modes.get("iowait", 0) / secs,
        "iops": sum(disk.values()) / secs,
        "busiest_node_js": max(node_js, key=lambda pc: pc[1]) if node_js else ("-", 0),
        "pg_shares": shares,
        "lock_db": lock_db,
        "first_task_p50": saga_latency.pct(waits, 50) if waits else 0,
        "unexpected_share": unexpected / max(1, len(client) + unexpected),
    }
    throttled = sum(vector(f"{d}/k6-throttled.json", "pod").values())
    periods = sum(vector(f"{d}/k6-periods.json", "pod").values())
    bad = void(meta, throttled, periods)
    per = lambda cores: cores * secs / max(1, completed)
    share = lambda k: f"{100 * shares.get(k, 0):.0f}%"
    first = f"{saga_latency.pct(waits, 50):.2f}/{max(waits):.2f}s" if waits else "n/a"
    line = (f"{meta['label']} {meta['vcpu']} vCPU {meta['vus']} buyers | {completed / secs:.2f} purchases/s,"
            + (f" checkout p50 {saga_latency.pct(ok, 50):.2f}s p99 {saga_latency.pct(ok, 99):.2f}s" if ok else " no checkout")
            + f" | busy {s['busy_cores']:.1f}/{meta['vcpu']} iowait {s['iowait_cores']:.1f} | {s['iops']:.0f} IOPS"
            + f" | pg {peak}/{limit} conns, CPU {share('CPU')} IO {share('IO')} Lock {share('Lock')}"
            + f" | first task p50/max {first} | core-s/purchase temporal {per(temporal):.3f} postgres {per(postgres):.3f}"
            + f" | {unexpected} unexpected -> {', '.join(verdict(s)) or 'none'}")
    top = sorted(pods.items(), key=lambda pc: -pc[1])[:6]
    return [("VOID (" + "; ".join(bad) + ") " if bad else "") + line,
            "  busiest: " + ", ".join(f"{p} {c:.2f}" for p, c in top)]


if __name__ == "__main__":
    print("\n".join(summarize(sys.argv[1])))
