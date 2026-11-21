#!/bin/sh
# Gives each topic at least its partition count, waiting for the broker first, and
# fails unless every topic has it. Never shrinks one: Kafka cannot remove a
# partition. Run by order-consumer's init container (decision 0030).
#   ensure-topics.sh BROKER TOPIC=PARTITIONS...
set -eu
broker=$1
shift
count() { rpk topic list -X brokers="$broker" | awk -v t="$1" '$1 == t { print $2 }'; }

until rpk cluster info -X brokers="$broker" >/dev/null 2>&1; do
  echo "waiting for $broker"
  sleep 2
done
for spec in "$@"; do
  topic=${spec%=*}
  want=${spec#*=}
  have=$(count "$topic")
  if [ -z "$have" ]; then
    rpk topic create "$topic" -p "$want" -X brokers="$broker"
  elif [ "$have" -lt "$want" ]; then
    rpk topic add-partitions "$topic" --num $((want - have)) -X brokers="$broker"
  fi
  got=$(count "$topic")
  echo "$topic: ${got:-0} partitions"
  [ "${got:-0}" -ge "$want" ] || { echo "$topic has ${got:-0} partitions, wants $want"; exit 1; }
done
