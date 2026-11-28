#!/usr/bin/env bash
# Sourced. tcli CMD [args...] runs CMD in the temporal-cli pod, against the
# temporal Service, creating the pod on first use as pg.sh does pg-client.
#   tcli temporal workflow list -o jsonl
# Why: a CLI run inside the server's container counts against its memory limit (0044).
# The tag names the server's version: move it with values.yaml's temporal.image.
TCLI_NS=ticketbottle
TCLI_IMAGE=temporalio/admin-tools:1.27.2-tctl-1.18.2-cli-1.3.0

tcli_pod() {
  kubectl -n $TCLI_NS get pod temporal-cli >/dev/null 2>&1 && return
  kubectl -n $TCLI_NS apply -f - >/dev/null <<EOF
apiVersion: v1
kind: Pod
metadata: { name: temporal-cli, namespace: $TCLI_NS }
spec:
  containers:
    - name: cli
      image: $TCLI_IMAGE
      command: [sleep, infinity]
      # An overrun kills the CLI, not the server; pipefail makes it a failed run.
      resources: { requests: { memory: 64Mi }, limits: { memory: 1Gi } }
      env:
        - { name: TEMPORAL_ADDRESS, value: "temporal:7233" }
EOF
  kubectl -n $TCLI_NS wait --for=condition=Ready pod/temporal-cli --timeout=3m >/dev/null
}

tcli() {
  tcli_pod
  kubectl -n $TCLI_NS exec temporal-cli -- "$@"
}
