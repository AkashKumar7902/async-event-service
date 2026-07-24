# Quality and Efficiency Evidence

This document uses black-box HTTP verification, an external load test, complexity and resource-bound
analysis, and explicit failure-mode analysis. 

## Manual API verification

The following behaviours were exercised against the compiled service:

| Scenario | Expected result | Observed result |
|---|---|---|
| Submit a valid event | Event is admitted | `202 Accepted` |
| Read statistics after admission | Processed count becomes visible | `{"pageview":1}` |
| Submit invalid JSON | Request is rejected | `400 Bad Request` |
| Submit invalid fields | Structured validation failure | `400` with `validation_failed` |
| Submit a non-JSON content type | Media type is rejected | `415 Unsupported Media Type` |
| Use `GET /events` | Method is rejected | `405 Method Not Allowed` |
| Request an unknown route | Route is rejected | `404 Not Found` |
| Send `SIGTERM` after admission | Accepted work drains before exit | Successful drain logged |

These checks use only the public HTTP contract and process signals; they do not access internal
queues, maps, or methods.

## Reproducible black-box verification

The manual scenarios are automated by one script:

```bash
./scripts/verify.sh
```

The script:

1. Builds the production server.
2. Starts it on `127.0.0.1:18080` with two workers and a queue capacity of 64.
3. Exercises successful submission, eventual statistics, and representative error responses.
4. Sends `SIGTERM`.
5. Confirms that the processor reports a successful drain.
6. Exits nonzero if any assertion fails.

The observed run completed with every scenario passing. Set `VERIFY_PORT` if port `18080` is
unavailable.

## External HTTP load test

The real HTTP server was exercised with Autocannon:

```bash
npx --yes autocannon \
  --duration 5 \
  --connections 50 \
  --method POST \
  --headers 'Content-Type=application/json' \
  --body '{"user":"load@example.com","type":"pageview","timestamp":1710000000,"payload":{}}' \
  --renderStatusCodes \
  --no-progress \
  http://127.0.0.1:8080/events
```

Server configuration:

```text
WORKER_COUNT=4
QUEUE_CAPACITY=1024
```

Observed result:

| Measurement | Result |
|---|---:|
| Test duration | `5.01 s` |
| Concurrent connections | `50` |
| Average request rate | `118,560 requests/s` |
| Average latency | `0.03 ms` |
| 97.5th-percentile latency | `0 ms` |
| 99th-percentile latency | `1 ms` |
| Maximum observed latency | `6 ms` |
| Reported `202` responses | `592,829` |
| Non-`202` responses | `0` |
| Response data read | `80.6 MB` |

## Complexity and resource bounds

Let:

- `B` be the request-body size;
- `Q` be queue capacity;
- `W` be worker count;
- `L` be the event-type length; and
- `T` be the number of distinct event types.

| Operation | Time | Additional space |
|---|---:|---:|
| Decode and validate one request | `O(B)` | `O(B)` |
| Attempt queue admission | `O(1)` | `O(1)` |
| Increment one event-type count | Average `O(1)` | `O(1)` |
| Read queue depth | `O(1)` | `O(1)` |
| Produce a statistics snapshot | `O(T)` | `O(T)` |
| Drain `N` accepted unfinished events | `O(N)` | No second event copy |

Concrete bounds and design choices:

- A request body is limited to 1 MiB.
- User and event-type strings are limited to 256 and 128 bytes.
- The default queue holds at most 1,024 waiting work items.
- Exactly four workers run by default, so worker processing concurrency is fixed at four.
- A queued item retains only the event-type string, not the user, timestamp, or arbitrary payload.
- With default limits, queued raw event-type content is bounded by approximately
  `1,024 × 128 bytes = 128 KiB`, plus string headers, channel storage, and allocator overhead.
- Queue admission is nonblocking. Saturation returns `503` rather than accumulating blocked
  handlers behind the queue.
- `/stats` copies the map under a read lock and releases the lock before JSON/network output.

The queue and worker pool are bounded, but two dimensions remain outside those bounds:

- concurrent HTTP requests, constrained by HTTP timeouts and the body limit; and
- distinct event-type cardinality, because the in-memory statistics map uses `O(T)` memory.

