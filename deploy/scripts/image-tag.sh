#!/usr/bin/env bash
# Print the one immutable sha- tag that ECR's moving tag $1 (a branch, e.g. dev) points at.
# Every target deploys a sha- tag: a moving tag leaves the pod spec unchanged, so helm
# rolls nothing. Images and chart must come from the same branch, or a chart change the
# old images cannot read deploys silently.
set -euo pipefail
REF=${1:?usage: image-tag.sh <ref>}
tags=$(aws ecr describe-images --region "${REGION:-us-east-1}" --repository-name ticketbottle/gateway \
         --image-ids imageTag="$REF" --query 'imageDetails[0].imageTags' --output text \
       | tr '\t' '\n' | grep '^sha-' || true)
if [ "$(echo "$tags" | grep -c .)" != 1 ]; then
  echo "REFUSING: :$REF must name exactly one sha- build, found: ${tags:-none}" >&2
  exit 1
fi
echo "$tags"
