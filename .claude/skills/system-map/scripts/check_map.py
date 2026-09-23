#!/usr/bin/env python3
"""Checks the system map against the repository it maps.

Fails when the map cites a path that does not exist, or when a tracked document
is missing from the map. CI runs it; so can you:
    python3 .claude/skills/system-map/scripts/check_map.py
"""
import argparse
import os
import re
import subprocess
import sys

SKILL = os.path.join(".claude", "skills", "system-map")
CODE_SPAN = re.compile(r"`([^`\n]+)`")
LINK = re.compile(r"\]\(([^)\s]+)\)")
PLACEHOLDER = re.compile(r"[<>{}*\s]|NNNN|\.\.\.")
SUFFIX = re.compile(r"(#[^/]*|:\d+(?:[-,]\d+)*)$")
FILE_EXT = re.compile(r"\.(go|ts|js|mjs|py|sh|md|ya?ml|json|proto|prisma|sql|tf|tpl|xml)$")

# Read through an index or a skill, not one by one.
NOT_REQUIRED = [re.compile(p) for p in (
    r"(^|/)(node_modules|vendor)/",
    r"^docs/plans/",
    r"^docs/decisions/\d{4}-",
    r"^\.claude/hooks/",
    r"^\.claude/skills/[^/]+/references/",
)]


def git(root, *args):
    return subprocess.run(
        ["git", *args], cwd=root, capture_output=True, text=True, check=True
    ).stdout


def map_files(skill_dir):
    for dirpath, _, names in sorted(os.walk(skill_dir)):
        for name in sorted(names):
            if name.endswith(".md"):
                yield os.path.join(dirpath, name)


def scan(root, skill_dir):
    """Returns (repo-relative paths the map cites, problems with its citations)."""
    cited, found = set(), []
    top = set(os.listdir(root))
    for md in map_files(skill_dir):
        with open(md, encoding="utf-8") as fh:
            lines = fh.read().splitlines()
        for n, line in enumerate(lines, 1):
            where = f"{os.path.relpath(md, skill_dir)}:{n}"
            for target in LINK.findall(line):
                target = SUFFIX.sub("", target)
                if not target or "://" in target or target.startswith("mailto:"):
                    continue
                path = os.path.normpath(os.path.join(os.path.dirname(md), target))
                if os.path.exists(path):
                    cited.add(os.path.relpath(path, root))
                else:
                    found.append(f"{where} links to {target}, which does not exist")
            for span in CODE_SPAN.findall(line):
                token = span.strip()
                if PLACEHOLDER.search(token) or "://" in token:
                    continue
                token = SUFFIX.sub("", token)
                if "/" not in token:
                    if token.endswith(".md") and token in top:
                        cited.add(token)
                    continue
                if token.split("/")[0] in top:
                    if os.path.exists(os.path.join(root, token)):
                        cited.add(os.path.normpath(token))
                    else:
                        found.append(f"{where} cites {token}, which does not exist")
                elif os.path.exists(os.path.join(skill_dir, token)):
                    continue
                elif FILE_EXT.search(token):
                    found.append(f"{where} cites {token}, which is not a repo-relative path that exists")
    return cited, found


def required(root, skill_dir):
    own = os.path.relpath(skill_dir, root).replace(os.sep, "/") + "/"
    docs = [d for d in git(root, "ls-files", "-z", "--", "*.md").split("\0") if d]
    return [
        d for d in docs
        if not d.startswith(own) and not any(p.search(d) for p in NOT_REQUIRED)
    ]


def problems(root, skill_dir):
    cited, found = scan(root, skill_dir)
    for doc in required(root, skill_dir):
        if doc not in cited:
            found.append(f"{doc} is tracked but not in the map; add it to the guide that owns it")
    return found


def main(argv=None):
    parser = argparse.ArgumentParser(description="Check the system map against the repository.")
    parser.add_argument("--root", help="repository root (default: this checkout's toplevel)")
    args = parser.parse_args(argv)
    root = args.root or git(os.path.dirname(os.path.abspath(__file__)), "rev-parse", "--show-toplevel").strip()
    found = problems(root, os.path.join(root, SKILL))
    for problem in found:
        print(problem)
    if found:
        print(f"\nsystem map: {len(found)} problem(s). See {SKILL}/SKILL.md, 'Keeping the map true'.")
        return 1
    print("system map: every cited path exists and every tracked document is cited.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
