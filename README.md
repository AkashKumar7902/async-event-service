# Asynchronous Event Service

A dependency-free Go service that accepts HTTP events, processes them asynchronously through a
bounded worker pool, and reports processed counts by event type.

The service uses only the Go standard library.

## Run locally

```bash
go run ./cmd/server
```

The default address is `:8080`.

Build a binary:

```bash
go build -o event-service ./cmd/server
./event-service
```

## API

### Submit an event

```bash
curl -i \
  -X POST http://localhost:8080/events \
  -H 'Content-Type: application/json' \
  -d '{
    "user": "xxxx@gmail.com",
    "type": "pageview",
    "timestamp": 1710000000,
    "payload": {
      "page": "/pricing",
      "device": "mobile"
    }
  }'
```

Successful admission:

```http
HTTP/1.1 202 Accepted
Content-Type: application/json

{"status":"accepted"}
```

`202` means the event entered this process's in-memory queue. It does not mean the event has already
been processed or persisted.

### Read statistics

```bash
curl -i http://localhost:8080/stats
```

Example:

```json
{
  "login": 3,
  "pageview": 12
}
```

Statistics contain processed events since the current process started. Because processing is
asynchronous, an event may not appear in an immediate `GET /stats`.

### Response statuses

| Condition | Status | Error code |
|---|---:|---|
| Event admitted to the queue | `202` | - |
| Invalid JSON | `400` | `invalid_json` |
| Field validation failure | `400` | `validation_failed` |
| Request body exceeds the configured limit | `413` | `request_too_large` |
| Content type is not JSON | `415` | `unsupported_media_type` |
| In-memory queue is full | `503` | `queue_full` |
| Service is shutting down | `503` | `shutting_down` |

Queue saturation returns `503` with `Retry-After: 1`.

## Validation

`POST /events` requires:

- exactly one JSON document;
- `Content-Type: application/json`;
- a nonblank `user` of at most 256 bytes;
- a nonblank, case-sensitive `type` of at most 128 bytes;
- a positive integer Unix `timestamp`; and
- a JSON object in `payload`.

Unknown top-level fields are rejected. Fields inside `payload` remain arbitrary.

## Configuration

settings that are useful to tune between deployments are:

| Environment variable | Default | Meaning |
|---|---:|---|
| `HTTP_ADDR` | `:8080` | HTTP listen address |
| `WORKER_COUNT` | `4` | Fixed worker goroutines |
| `QUEUE_CAPACITY` | `1024` | Maximum buffered work items |

Invalid or nonpositive worker and queue values fail startup.

Example:

```bash
WORKER_COUNT=8 QUEUE_CAPACITY=4096 go run ./cmd/server
```

Safety policies stay as named constants in `cmd/server/main.go`: a 1 MiB request limit, 256-byte
user limit, 128-byte type limit, conservative HTTP timeouts, and a 15-second shutdown budget.

## Verification

```bash
./scripts/verify.sh
```

The script builds the production server, exercises its public HTTP API, and verifies graceful shutdown.

It uses `127.0.0.1:18080` by default. Select another port when necessary:

```bash
VERIFY_PORT=18081 ./scripts/verify.sh
```

The external HTTP load-test command, observed results, complexity analysis, resource bounds are in [QUALITY.md](QUALITY.md).

## Graceful shutdown

On `SIGINT` or `SIGTERM`, the process:

1. stops accepting events;
2. shuts down HTTP handling;
3. closes the queue only after synchronizing with senders;
4. drains buffered work; and
5. exits when workers finish or the shutdown deadline expires.

## Delivery semantics and limitations

- The queue and statistics are process-local and volatile.
- An abrupt restart can lose admitted but unprocessed events.
- Counts reset after restart.
- Identical HTTP requests are counted independently; there is no deduplication.
- The implementation does not claim durable or exactly-once delivery.
- Multiple replicas would require a shared queue and shared statistics store for global results.

See [APPROACH.md](APPROACH.md) for the design rationale and [QUALITY.md](QUALITY.md) for measured
verification results.

## Project layout

```text
cmd/server/          process wiring and lifecycle
internal/api/        HTTP decoding, handlers, and responses
internal/config/     environment configuration
internal/model/      event model and validation
internal/processor/  bounded queue and worker lifecycle
internal/stats/      synchronized in-memory counters
scripts/             black-box service verification
```
