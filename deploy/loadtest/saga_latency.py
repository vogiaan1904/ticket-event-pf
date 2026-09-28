"""Where a checkout's time goes, from CreateOrder workflow histories.

Reads the dump saga-histories.sh writes; a k6 log from purchase.js adds the
client's view of each checkout, joined by order code.
  python3 deploy/loadtest/saga_latency.py HISTORIES [K6_LOG] [--burst-secs 2 | --burst-first 20]
"""
import argparse
import json
import re
from datetime import datetime

STEPS = ["ReserveInventory", "CreateOrder", "CreateOrderItems", "CreatePaymentIntent"]
CHECKOUT = re.compile(r'msg="CHECKOUT (\S+) ([\d.]+) (\d+)"')


def parse(workflow_id, events):
    """Returns one saga's timings, in seconds from its workflow's start."""
    t0 = datetime.fromisoformat(events[0]["eventTime"])

    def at(e):
        return (datetime.fromisoformat(e["eventTime"]) - t0).total_seconds()

    r = {"code": workflow_id.split(":", 1)[1], "start": t0, "total": at(events[-1]),
         "wft_queued": [], "queued": {}, "ran": {}, "retries": 0}
    scheduled, wft = {}, 0.0
    for e in events:
        kind = e["eventType"].removeprefix("EVENT_TYPE_")
        if kind == "WORKFLOW_TASK_SCHEDULED":
            wft = at(e)
        elif kind == "WORKFLOW_TASK_STARTED":
            r["wft_queued"].append(at(e) - wft)
        elif kind == "ACTIVITY_TASK_SCHEDULED":
            name = e["activityTaskScheduledEventAttributes"]["activityType"]["name"]
            scheduled[e["eventId"]] = [name, at(e)]
        elif kind == "ACTIVITY_TASK_STARTED":
            a = e["activityTaskStartedEventAttributes"]
            scheduled[a["scheduledEventId"]].append(at(e))
            r["retries"] += int(a.get("attempt", 1)) - 1
        elif kind == "ACTIVITY_TASK_COMPLETED":
            name, sched, start = scheduled[e["activityTaskCompletedEventAttributes"]["scheduledEventId"]]
            r["queued"][name], r["ran"][name] = start - sched, at(e) - start
    return r


def load_histories(path):
    runs, workflow_id = [], None
    with open(path) as f:
        for line in f:
            if line.startswith("=== "):
                workflow_id = line[4:].strip()
            elif line.strip():
                runs.append(parse(workflow_id, json.loads(line)["events"]))
    return runs


def load_client(path):
    """Returns (order code, seconds, HTTP status) per checkout; a rejected one has code '-'."""
    with open(path) as f:
        return [(code, float(ms) / 1000, int(status)) for code, ms, status in CHECKOUT.findall(f.read())]


def split_burst(runs, secs=3.0, first=None):
    """Splits sagas into the opening burst and the rest.

    The burst is the first `first` sagas to start, one per buyer, or else those
    started within secs of the first. Why 3s: two 1s admission ticks, up to 2.7s.
    """
    ordered = sorted(runs, key=lambda r: r["start"])
    if first is not None:
        return ordered[:first], ordered[first:]
    burst = [r for r in ordered if (r["start"] - ordered[0]["start"]).total_seconds() < secs]
    return burst, ordered[len(burst):]


def pct(xs, p):
    xs = sorted(xs)
    return xs[min(len(xs) - 1, round(p / 100 * (len(xs) - 1)))]


def row(name, xs):
    if xs:
        print(f"  {name:<32}" + "".join(f"{pct(xs, p):7.2f}" for p in (50, 90, 99)) + f"{max(xs):7.2f}")


def report(label, runs, client):
    if not runs:
        return
    seen = {c: s for c, s, status in client if status < 400}
    joined = [(seen[r["code"]], r) for r in runs if r["code"] in seen]
    print(f"\n{label}: n={len(runs)} retries={sum(r['retries'] for r in runs)}"
          f" over 2s: workflow {sum(r['total'] > 2 for r in runs)}"
          + (f", client {sum(s > 2 for s, _ in joined)} of {len(joined)}" if client else ""))
    print(f"  {'':<32}{'p50':>7}{'p90':>7}{'p99':>7}{'max':>7}")
    row("checkout, client", [s for s, _ in joined])
    row("outside the workflow", [s - r["total"] for s, r in joined])
    row("workflow", [r["total"] for r in runs])
    row("first workflow task queued", [r["wft_queued"][0] for r in runs if r["wft_queued"]])
    row("all workflow tasks queued", [sum(r["wft_queued"]) for r in runs])
    for step in STEPS:
        row(f"{step} queued", [r["queued"][step] for r in runs if step in r["queued"]])
        row(f"{step} ran", [r["ran"][step] for r in runs if step in r["ran"]])


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("histories")
    ap.add_argument("k6_log", nargs="?")
    ap.add_argument("--burst-secs", type=float, default=3.0,
                    help="sagas started this soon after the first are the opening burst")
    ap.add_argument("--burst-first", type=int,
                    help="the first N sagas to start are the opening burst; set N to the buyers")
    a = ap.parse_args()
    runs = load_histories(a.histories)
    client = load_client(a.k6_log) if a.k6_log else []
    burst, steady = split_burst(runs, a.burst_secs, a.burst_first)
    report("burst", burst, client)
    report("steady", steady, client)
    report("all", runs, client)
    rejected = [(s, status) for c, s, status in client if status >= 400]
    if rejected:
        print(f"\nrejected checkouts: {len(rejected)}, statuses {sorted({st for _, st in rejected})}")


if __name__ == "__main__":
    main()
