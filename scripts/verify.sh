#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
VERIFY_PORT="${VERIFY_PORT:-18080}"
ADDRESS="127.0.0.1:${VERIFY_PORT}"
BASE_URL="http://${ADDRESS}"
TEMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/async-event-verify.XXXXXX")"
SERVER_BINARY="$TEMP_DIR/event-service"
SERVER_LOG="$TEMP_DIR/server.log"
RESPONSE_BODY="$TEMP_DIR/response.json"
SERVER_PID=""

cleanup() {
	if [ -n "$SERVER_PID" ] && kill -0 "$SERVER_PID" 2>/dev/null; then
		kill -TERM "$SERVER_PID" 2>/dev/null || true
		wait "$SERVER_PID" 2>/dev/null || true
	fi
	rm -rf "$TEMP_DIR"
}
trap cleanup EXIT INT TERM

fail() {
	echo "FAIL: $1" >&2
	if [ -s "$SERVER_LOG" ]; then
		echo "Server log:" >&2
		cat "$SERVER_LOG" >&2
	fi
	exit 1
}

assert_status() {
	expected="$1"
	actual="$2"
	scenario="$3"

	if [ "$actual" != "$expected" ]; then
		fail "$scenario returned HTTP $actual; expected $expected"
	fi
	printf 'PASS: %-34s HTTP %s\n' "$scenario" "$actual"
}

echo "Building service..."
(
	cd "$PROJECT_ROOT"
	go build -o "$SERVER_BINARY" ./cmd/server
)

echo "Starting service on $ADDRESS..."
HTTP_ADDR="$ADDRESS" \
	WORKER_COUNT=2 \
	QUEUE_CAPACITY=64 \
	"$SERVER_BINARY" >"$SERVER_LOG" 2>&1 &
SERVER_PID=$!

ready_status=""
attempt=0
while [ "$attempt" -lt 50 ]; do
	ready_status="$(curl -sS -o /dev/null -w '%{http_code}' "$BASE_URL/stats" 2>/dev/null || true)"
	if [ "$ready_status" = "200" ]; then
		break
	fi
	sleep 0.1
	attempt=$((attempt + 1))
done

if [ "$ready_status" != "200" ]; then
	fail "service did not become ready"
fi

status="$(curl -sS -o "$RESPONSE_BODY" -w '%{http_code}' \
	-X POST "$BASE_URL/events" \
	-H 'Content-Type: application/json' \
	--data '{"user":"verify@example.com","type":"pageview","timestamp":1710000000,"payload":{"page":"/pricing"}}')"
assert_status "202" "$status" "valid event"

processed="false"
attempt=0
while [ "$attempt" -lt 50 ]; do
	curl -sS "$BASE_URL/stats" >"$RESPONSE_BODY"
	if grep -Eq '"pageview"[[:space:]]*:[[:space:]]*1' "$RESPONSE_BODY"; then
		processed="true"
		break
	fi
	sleep 0.1
	attempt=$((attempt + 1))
done

if [ "$processed" != "true" ]; then
	fail "accepted event did not appear in /stats"
fi
echo "PASS: accepted event appeared in /stats"

status="$(curl -sS -o "$RESPONSE_BODY" -w '%{http_code}' \
	-X POST "$BASE_URL/events" \
	-H 'Content-Type: application/json' \
	--data '{"user":')"
assert_status "400" "$status" "invalid JSON"

status="$(curl -sS -o "$RESPONSE_BODY" -w '%{http_code}' \
	-X POST "$BASE_URL/events" \
	-H 'Content-Type: application/json' \
	--data '{"user":"","type":"login","timestamp":0,"payload":{}}')"
assert_status "400" "$status" "invalid fields"
grep -q '"validation_failed"' "$RESPONSE_BODY" ||
	fail "invalid fields response did not contain validation_failed"

status="$(curl -sS -o "$RESPONSE_BODY" -w '%{http_code}' \
	-X POST "$BASE_URL/events" \
	-H 'Content-Type: text/plain' \
	--data '{}')"
assert_status "415" "$status" "unsupported content type"

status="$(curl -sS -o "$RESPONSE_BODY" -w '%{http_code}' \
	-X GET "$BASE_URL/events")"
assert_status "405" "$status" "unsupported method"

status="$(curl -sS -o "$RESPONSE_BODY" -w '%{http_code}' "$BASE_URL/unknown")"
assert_status "404" "$status" "unknown route"

echo "Stopping service with SIGTERM..."
kill -TERM "$SERVER_PID"
if ! wait "$SERVER_PID"; then
	fail "service returned an error during graceful shutdown"
fi
SERVER_PID=""

grep -q '"msg":"event processor drained successfully"' "$SERVER_LOG" ||
	fail "service did not report a successful processor drain"
echo "PASS: graceful shutdown drained accepted work"

echo "Black-box verification passed."
