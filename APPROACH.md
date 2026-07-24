# Approach

## Design

```text
POST /events
     |
decode + validate
     |
bounded channel ---> fixed worker pool ---> synchronized statistics map
                                               ^
                                               |
                                         GET /stats
```

After validating the complete event, the API queues only its type because the aggregator does not
need the user, timestamp, or payload. This reduces retained memory and avoids unnecessarily keeping
user data.

The buffered channel has fixed capacity, and the worker count is configurable. Admission uses a
nonblocking send: success returns `202 Accepted`; saturation returns `503 Service Unavailable` with
`Retry-After`. This provides explicit backpressure instead of accumulating blocked handlers or
unbounded work.

Workers are long-lived goroutines created at startup. Each increments a `map[string]uint64`
protected by `sync.RWMutex`. `GET /stats` copies the map under a read lock, releases the lock, and
then serializes the copy, producing a safe point-in-time snapshot without holding a lock during
network I/O.

## Correctness and lifecycle

The processor owns the queue. Submission holds an admission read lock through the nonblocking send;
shutdown takes the write lock before closing the channel. This prevents a sender from racing with
channel closure.

On `SIGINT` or `SIGTERM`, the service stops admission, shuts down HTTP handling, closes the queue,
and lets workers drain buffered events within a 15-second deadline.

Requests are body- and field-size-limited, top-level JSON is strict, HTTP timeouts are configured,
and logs never include the user or payload.

## Semantics and trade-offs

`202` means admitted to volatile process memory, not processed or persisted. Statistics are
eventually consistent, identical submissions are counted separately, and a crash can lose queued
events and reset all counts. Distinct event-type cardinality is assumed to remain bounded.

For durable or multi-instance processing, the next step is a shared durable inbox and statistics
store. Kafka becomes appropriate when replay, independent consumer groups, or sustained streaming
throughput justify it.
