"""How many ConfirmOrder workflows ran at once (decision 0030).

Reads `temporal workflow list -o jsonl` output and prints one line: how many closed,
over how long, the most open at one instant, their mean duration and rate. One
partition never shows more than 1.
  python3 deploy/loadtest/confirm_overlap.py LIST.jsonl
"""
import json
import sys
from datetime import datetime, timezone


def seconds(ts):
    """Epoch seconds from an RFC 3339 UTC time with any number of fractional digits."""
    base, _, frac = ts.rstrip("Z").partition(".")
    whole = datetime.fromisoformat(base).replace(tzinfo=timezone.utc).timestamp()
    return whole + (float("0." + frac) if frac else 0.0)


def intervals(lines):
    """(start, close) of each closed workflow, in start order."""
    rows = (json.loads(l) for l in lines if l.strip())
    return sorted((seconds(r["startTime"]), seconds(r["closeTime"])) for r in rows if r.get("closeTime"))


def peak(spans):
    """The most spans open at one instant; a close sorts before a start at the same time."""
    open_now, most = 0, 0
    for _, step in sorted([(s, 1) for s, _ in spans] + [(c, -1) for _, c in spans]):
        open_now += step
        most = max(most, open_now)
    return most


def summary(spans):
    if not spans:
        return "0 confirms"
    span = max(c for _, c in spans) - spans[0][0]
    mean = sum(c - s for s, c in spans) / len(spans)
    return (f"{len(spans)} confirms over {span:.1f}s, at most {peak(spans)} at once, "
            f"mean {1000 * mean:.0f}ms, {len(spans) / span:.2f}/s")


if __name__ == "__main__":
    with open(sys.argv[1]) as f:
        print(summary(intervals(f)))
