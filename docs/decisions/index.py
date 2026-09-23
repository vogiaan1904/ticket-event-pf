#!/usr/bin/env python3
"""Builds the review index in README.md from the decision records.

Each record's review row is the one home of its summary; the index is derived.
  python3 docs/decisions/index.py            rewrite the block
  python3 docs/decisions/index.py --check    fail if the block is stale
"""
import os
import re
import sys
from dataclasses import dataclass

START = "<!-- decisions:index:start -->"
END = "<!-- decisions:index:end -->"
HEADER = ["Question", "Chose", "Instead of", "Cost"]
STATUSES = ("proposed", "accepted", "superseded")
FILENAME = re.compile(r"^(\d{4})-[a-z0-9-]+\.md$")


class RecordError(Exception):
    pass


@dataclass
class Record:
    number: str
    filename: str
    arc: str
    status: str
    row: list


def _cells(line):
    return [c.strip() for c in re.split(r"(?<!\\)\|", line.strip())[1:-1]]


def parse_record(path):
    name = os.path.basename(path)
    number = FILENAME.match(name).group(1)
    with open(path) as f:
        lines = f.read().splitlines()

    problems = []
    title = re.match(r"^# (\d{4}) — \S", lines[0] if lines else "")
    if not title or title.group(1) != number:
        problems.append(f"title must read '# {number} — <decision>'")

    fields = {}
    for line in lines:
        m = re.match(r"^\*\*(Date|Status|Arc):\*\*\s*(.+)$", line)
        if m:
            fields.setdefault(m.group(1), m.group(2).strip())

    if not re.match(r"^\d{4}-\d{2}-\d{2}$", fields.get("Date", "")):
        problems.append("missing **Date:** YYYY-MM-DD")

    raw = fields.get("Status", "")
    kind = re.match(r"^[a-z]+", raw)
    status = ""
    if not kind or kind.group(0) not in STATUSES:
        problems.append(f"**Status:** must start with one of {', '.join(STATUSES)}")
    elif kind.group(0) == "superseded":
        successor = re.search(r"\[(\d{4})\]", raw)
        status = f"superseded by {successor.group(1)}" if successor else "superseded"
    else:
        status = kind.group(0)

    arc = re.match(r"^([a-z0-9-]+)", fields.get("Arc", ""))
    if not arc:
        problems.append("missing **Arc:** <arc-slug>")

    row = None
    for i, line in enumerate(lines):
        if line.startswith("|") and _cells(line) == HEADER and i + 2 < len(lines):
            row = _cells(lines[i + 2])
            break
    if not row or len(row) != 4 or not all(row):
        problems.append("missing the review row: | Question | Chose | Instead of | Cost |")

    if problems:
        raise RecordError(f"{name}: " + "; ".join(problems))
    return Record(number, name, arc.group(1), status, row)


def load_records(directory):
    records, errors = [], []
    for name in sorted(os.listdir(directory)):
        if not FILENAME.match(name):
            continue
        try:
            records.append(parse_record(os.path.join(directory, name)))
        except RecordError as e:
            errors.append(str(e))
    if errors:
        raise RecordError("\n".join(errors))
    return records


def render(records):
    arcs = {}
    for r in records:
        arcs.setdefault(r.arc, []).append(r)
    out = []
    for arc, rows in arcs.items():
        out += [f"### {arc}", "", "| # | " + " | ".join(HEADER) + " | Status |", "|---|---|---|---|---|---|"]
        out += [f"| [{r.number}]({r.filename}) | " + " | ".join(r.row) + f" | {r.status} |" for r in rows]
        out.append("")
    return "\n".join(out)


def main(argv):
    check = "--check" in argv
    args = [a for a in argv if a != "--check"]
    directory = args[0] if args else os.path.dirname(os.path.abspath(__file__))
    readme = os.path.join(directory, "README.md")

    try:
        records = load_records(directory)
    except RecordError as e:
        print(f"invalid decision records:\n{e}", file=sys.stderr)
        return 1

    with open(readme) as f:
        text = f.read()
    if START not in text or END not in text:
        print(f"{readme}: missing the {START} / {END} markers", file=sys.stderr)
        return 1

    head, rest = text.split(START, 1)
    tail = rest.split(END, 1)[1]
    fresh = f"{head}{START}\n\n{render(records)}{END}{tail}"

    if check:
        if fresh != text:
            print("the decision index is stale: run python3 docs/decisions/index.py", file=sys.stderr)
            return 1
        return 0
    with open(readme, "w") as f:
        f.write(fresh)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
