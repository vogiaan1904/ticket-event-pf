#!/usr/bin/env bash
# The script order-consumer's init container runs (decision 0030), against a
# throwaway Redpanda of the chart's version. Needs Docker; CI does not run it.
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
IMAGE=$(awk '/^redpanda:/ { r = 1 } r && /image:/ { print $2; exit }' "$HERE/../values.yaml")
NAME=ensure-topics-test-$$
fail() { echo "FAIL: $1"; exit 1; }

docker run -d --rm --name "$NAME" "$IMAGE" redpanda start --mode dev-container --smp 1 >/dev/null
trap 'docker rm -f "$NAME" >/dev/null' EXIT
run() { docker exec -i "$NAME" sh -s -- localhost:9092 "$@" < "$HERE/../files/ensure-topics.sh" >/dev/null || fail "ensure-topics $* exited non-zero"; }
parts() { docker exec "$NAME" rpk topic list | awk -v t="$1" '$1 == t { print $2 }'; }

run payment.completed=8
[ "$(parts payment.completed)" = 8 ] || fail "a missing topic was not created with 8 partitions"
run payment.completed=8
[ "$(parts payment.completed)" = 8 ] || fail "a second run changed a topic that had its partitions"
docker exec "$NAME" rpk topic create older -p 1 >/dev/null
run older=4
[ "$(parts older)" = 4 ] || fail "a topic with 1 partition did not grow to 4"
run payment.completed=2
[ "$(parts payment.completed)" = 8 ] || fail "a topic shrank"
run a=2 b=3
{ [ "$(parts a)" = 2 ] && [ "$(parts b)" = 3 ]; } || fail "two topics in one run were not both created"
echo "ensure-topics: creates, grows, never shrinks, leaves a finished topic alone"
