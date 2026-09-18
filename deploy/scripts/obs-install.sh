#!/usr/bin/env bash
# Install kube-prometheus-stack and provision the three dashboards.
# Idempotent: safe to re-run against a cluster that already has it.
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"

helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update prometheus-community
helm upgrade --install kps prometheus-community/kube-prometheus-stack \
  -n monitoring --create-namespace \
  -f "$HERE/../monitoring/values-kps.yaml" --wait --timeout 15m

for d in gateway-red saga-health queue-outbox; do
  kubectl -n monitoring create configmap "tb-dash-$d" \
    --from-file="$d.json=$HERE/../monitoring/dashboards/$d.json" \
    --dry-run=client -o yaml \
  | kubectl label --local -f - grafana_dashboard=1 -o yaml \
  | kubectl apply -f -
done