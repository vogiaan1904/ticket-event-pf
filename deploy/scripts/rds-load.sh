#!/usr/bin/env bash
# What one EKS RDS instance did over a load run, from CloudWatch's 1-minute
# datapoints: CPU average and peak, peak connections. One line on stdout.
#   deploy/scripts/rds-load.sh shared 2026-11-23T06:43:00Z 2026-11-23T06:48:00Z
set -euo pipefail
NAME=${1:?usage: rds-load.sh shared|payment|inventory FROM TO  (RFC 3339, UTC)}
FROM=${2:?usage: rds-load.sh NAME FROM TO}
TO=${3:?usage: rds-load.sh NAME FROM TO}
ID=${EKS_CLUSTER:-ticketbottle-eks}-$NAME
REGION=${EKS_REGION:-us-east-1}

stat() {
  aws cloudwatch get-metric-statistics --region "$REGION" --namespace AWS/RDS \
    --metric-name "$1" --dimensions Name=DBInstanceIdentifier,Value="$ID" \
    --start-time "$FROM" --end-time "$TO" --period 60 --statistics Average Maximum \
    --output json
}

# A minute's datapoint lands a minute or two late: wait for the run's last one.
last=$(python3 -c 'import sys; from datetime import datetime, timedelta; t = datetime.fromisoformat(sys.argv[1].replace("Z", "+00:00")); print((t - timedelta(seconds=120)).isoformat())' "$TO")
for _ in $(seq 1 18); do
  stat CPUUtilization | python3 -c 'import sys, json; from datetime import datetime; d = json.load(sys.stdin)["Datapoints"]; sys.exit(0 if any(datetime.fromisoformat(p["Timestamp"]) >= datetime.fromisoformat(sys.argv[1]) for p in d) else 1)' "$last" && break
  sleep 10
done

python3 - "$NAME" "$(stat CPUUtilization)" "$(stat DatabaseConnections)" <<'EOF'
import json, sys
name, cpu, conns = sys.argv[1], json.loads(sys.argv[2])["Datapoints"], json.loads(sys.argv[3])["Datapoints"]
if not cpu:
    print(f"{name} no datapoints"); sys.exit()
avg = sum(p["Average"] for p in cpu) / len(cpu)
print(f"{name} cpu avg {avg:.0f}% max {max(p['Maximum'] for p in cpu):.0f}%,"
      f" conns max {max((p['Maximum'] for p in conns), default=float('nan')):.0f}")
EOF
