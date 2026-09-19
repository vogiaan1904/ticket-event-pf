#!/usr/bin/env bash
# Pins kubectl and helm to the EKS cluster, independent of the caller's environment.
# Invariant: no EKS script acts on whatever cluster the shell happens to point at.
unset KUBECONFIG
EKS_CLUSTER=${EKS_CLUSTER:-ticketbottle-eks}
EKS_REGION=${EKS_REGION:-us-east-1}
KCTX="arn:aws:eks:${EKS_REGION}:$(aws sts get-caller-identity --query Account --output text):cluster/${EKS_CLUSTER}"

command kubectl config get-contexts -o name | grep -qx "$KCTX" ||
  { echo "no kubeconfig entry for $EKS_CLUSTER — run 'make -C deploy eks-kubeconfig'"; exit 1; }

kubectl() { command kubectl --context "$KCTX" "$@"; }
helm() { command helm --kube-context "$KCTX" "$@"; }
