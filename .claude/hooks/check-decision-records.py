#!/usr/bin/env python3
"""Flags a plan whose chosen decisions lack records, or whose records stay proposed.

Reads the PostToolUse hook payload on stdin, reports on stdout as additionalContext.
Advisory only: it never blocks a write. The rules live in docs/decisions/README.md.
"""
import glob
import json
import os
import re
import sys

RECORD_LINK = re.compile(r"decisions/(\d{4})-[a-z0-9-]+\.md")


def main():
    try:
        payload = json.load(sys.stdin)
    except Exception:
        return
    path = payload.get("tool_input", {}).get("file_path") or payload.get(
        "tool_response", {}
    ).get("filePath")
    if not path or not path.endswith(".md") or not os.path.isfile(path):
        return
    path = os.path.abspath(path)
    if os.path.basename(os.path.dirname(path)) != "plans":
        return

    with open(path, encoding="utf-8", errors="replace") as fh:
        text = fh.read()

    found = []
    chosen = text.count("(Chosen)")
    linked = sorted(set(RECORD_LINK.findall(text)))
    if chosen > len(linked):
        found.append(
            f"{chosen} option(s) marked (Chosen) but {len(linked)} decision record(s) linked — "
            "draft each as `proposed` and link it from its plan item"
        )

    head = "\n".join(text.splitlines()[:5])
    if re.search(r"\*\*Status: COMPLETE", head):
        decisions = os.path.join(os.path.dirname(os.path.dirname(path)), "decisions")
        for num in linked:
            for record in glob.glob(os.path.join(decisions, f"{num}-*.md")):
                with open(record, encoding="utf-8", errors="replace") as fh:
                    if re.search(r"^\*\*Status:\*\*\s*proposed", fh.read(), re.M):
                        found.append(
                            f"{os.path.basename(record)} is still proposed but the plan is COMPLETE — "
                            "accept it and add its Outcome"
                        )

    if found:
        print(json.dumps({
            "hookSpecificOutput": {
                "hookEventName": "PostToolUse",
                "additionalContext": (
                    f"Decision records (docs/decisions/README.md) — {path}:\n  " + "\n  ".join(found)
                ),
            }
        }))


main()
