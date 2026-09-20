#!/usr/bin/env python3
"""Flags comments that break the budget in CLAUDE.md -> Comment conventions.

Reads the PostToolUse hook payload on stdin, reports on stdout as additionalContext.
Advisory only: it never blocks a write.
"""
import json
import os
import re
import sys

EXTS = {".go", ".ts", ".tsx", ".js", ".mjs", ".tf", ".tfvars", ".yaml", ".yml", ".sh", ".py", ".sql"}
# The line budget is a code rule. Config files carry prose legitimately.
BUDGETED = {".go", ".ts", ".tsx", ".js", ".mjs", ".py"}

# Comments describing an incident, a past state, or a fix. The code says what it
# does now; these only date it.
NARRATIVE = [
    (r"\b(this|it|that|we|they) used to\b", "past state"),
    (r"\bpreviously\b", "past state"),
    (r"\boriginally\b", "past state"),
    (r"\bturns? out\b", "incident narration"),
    (r"\bwas (failing|broken|wrong)\b", "incident narration"),
    (r"\b(this|which) fixes\b", "incident narration"),
    (r"\bchanged? (it )?because\b", "incident narration"),
    (r"\bworkaround\b", "incident narration"),
    (r"\bfor now\b", "temporary marker"),
    (r"\btemporar(y|ily)\b", "temporary marker"),
    (r"\bhack\b", "temporary marker"),
]

COMMENT = re.compile(r"^(\s*)(//|#)\s?(.*)$")


def comment_blocks(lines):
    """Yields (start_line, indent, [text...]) for each run of comment-only lines."""
    block = None
    for n, raw in enumerate(lines, 1):
        m = COMMENT.match(raw)
        if m and not raw.lstrip().startswith("#!"):
            indent, text = m.group(1), m.group(3)
            if block is None:
                block = (n, indent, [])
            block[2].append(text)
        elif block:
            yield block
            block = None
    if block:
        yield block


def budget(start, indent):
    if start <= 3 and not indent:
        return 8, "file header"
    return (3, "inline") if indent else (5, "doc")


def main():
    try:
        payload = json.load(sys.stdin)
    except Exception:
        return
    path = payload.get("tool_input", {}).get("file_path") or payload.get(
        "tool_response", {}
    ).get("filePath")
    if not path or os.path.splitext(path)[1] not in EXTS or not os.path.isfile(path):
        return

    with open(path, encoding="utf-8", errors="replace") as fh:
        lines = fh.read().splitlines()

    found = []
    budgeted = os.path.splitext(path)[1] in BUDGETED
    for start, indent, texts in comment_blocks(lines):
        # CLAUDE.md exempts case tables and step lists from the line budget.
        prose = [
            t for t in texts
            if t.strip() and not re.match(r"^[-=*_\s]+$", t) and "->" not in t and "|" not in t
        ]
        limit, kind = budget(start, indent)
        if budgeted and len(prose) > limit:
            found.append(f"{path}:{start} {kind} comment is {len(prose)} lines, budget is {limit}")
        body = " ".join(texts).lower()
        for pattern, why in NARRATIVE:
            if re.search(pattern, body):
                found.append(f"{path}:{start} reads as {why}: \"{' '.join(texts)[:90]}\"")
                break

    if found:
        print(json.dumps({
            "hookSpecificOutput": {
                "hookEventName": "PostToolUse",
                "additionalContext": (
                    "Comment convention (CLAUDE.md) — rewrite these to state the invariant, "
                    "not the history, or delete them:\n  " + "\n  ".join(found)
                ),
            }
        }))


main()
