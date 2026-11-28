#!/usr/bin/env bash
# How many ConfirmOrder workflows started in [FROM, TO) ran at once (decision 0030).
# One partition never shows more than 1. Temporal keeps a history for 24h.
#   deploy/scripts/confirm-overlap.sh 2026-10-11T08:27:00Z 2026-10-11T08:27:30Z
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
FROM=${1:?usage: confirm-overlap.sh FROM TO  (RFC 3339, UTC)}
TO=${2:?usage: confirm-overlap.sh FROM TO}
LIST=$(mktemp)
trap 'rm -f "$LIST"' EXIT
. "$HERE/temporal-cli.sh"

tcli temporal workflow list \
  --query "WorkflowType=\"ConfirmOrder\" AND StartTime >= \"$FROM\" AND StartTime < \"$TO\"" \
  --limit 100000 -o jsonl > "$LIST"
python3 "$HERE/../loadtest/confirm_overlap.py" "$LIST"
