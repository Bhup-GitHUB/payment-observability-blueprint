# payment-observability-blueprint

A generalized educational prototype showing how distributed tracing, metrics, and structured logging work across a multi-service payment flow.

> This repository is a generalized educational prototype. It does not represent Razorpay's exact architecture or implementation.

---

## The problem

Once a payment system splits into more than two services, answering "why did that payment fail?" stops being a one-log-file problem. A request moves across HTTP boundaries, spawns async work over a message queue, and hits a database — each step in its own process with its own logs. Without a shared request identity threaded through all of them, debugging becomes educated guessing.

This prototype makes the observability infrastructure concrete. Every service emits traces, metrics, and structured logs. A single payment request produces one root trace that spans six services. The bank simulator can be made deliberately slow, and the distributed trace will show exactly which span ate the latency.

---

## Architecture

```
Merchant Client
  └── API Gateway           :8080  entry point, trace root, web UI
        └── Payment Service  :8081  orchestrator
              ├── Risk Service       :8082  fraud evaluation
              ├── Payment Router     :8083  bank selection + forwarding
              │     └── Bank Simulator :8084  auth simulation
              ├── Ledger Service     :8085  PostgreSQL write
              └── NATS publish ──────────── Notification Worker  async consumer
```

**Observability stack**

```
All services ──OTLP gRPC──► OTel Collector
                               ├── traces  ──► Tempo
                               └── metrics ──► Prometheus (port 8889)

Docker logs ──► Promtail ──► Loki
Prometheus  ──► Grafana (dashboards + alerts)
            ──► Alertmanager
```

---

## Services

**API Gateway** receives and validates payment requests. It assigns a request ID, starts the root span, forwards the request to the payment service, and attaches the trace ID to the response so the client can look it up in Tempo. It also serves the web demo UI.

**Payment Service** is the orchestrator. It generates the payment ID, calls risk evaluation, calls the payment router on approval, writes to the ledger on bank success, and publishes an event to NATS. It coordinates but contains no fraud logic, bank logic, or storage logic itself.

**Risk Service** makes a fraud decision based on the `scenario` field. The `fraud` scenario returns rejected; everything else is approved. This is intentionally deterministic so demos are reproducible.

**Payment Router** selects a bank and forwards the authorization request. In this prototype there is only one bank (bank-simulator), but the routing layer is isolated so additional banks can be added without touching the orchestrator.

**Bank Simulator** handles three scenarios: `success` (small random delay, succeeds), `slow` (2–4s random delay, still succeeds — making the bank span the dominant latency contributor in Tempo), and `failure` (immediate decline, code 05).

**Ledger Service** writes completed payments to PostgreSQL using `ON CONFLICT (payment_id) DO NOTHING`, making it idempotent. It runs database migrations at startup. The readiness probe checks the database connection.

**Notification Worker** is a NATS subscriber. It extracts the W3C trace context from the `PaymentEvent.TraceCarrier` field before starting its span, so the notification span appears as a child of the original payment trace even though it runs asynchronously.

---

## How trace context crosses boundaries

**HTTP**: `otelhttp.NewTransport` on HTTP clients and `otelhttp.NewHandler` on servers automatically inject and extract W3C `traceparent` headers. No manual header handling required.

**NATS**: NATS core pub/sub has no native header propagation. `PaymentEvent.TraceCarrier` is a `map[string]string` that carries the W3C headers. The `internal/natsprop` package provides `Inject(ctx)` to serialize the active span into a map and `Extract(ctx, map)` to deserialize it on the consumer side. The notification worker calls `Extract` before starting its child span — if it started the span first, the parent relationship would be lost.

---

## Metrics, traces, and logs — different questions

**Traces** answer: what happened during this specific request? Which service was slow? Where did it fail? You navigate to Tempo with a trace ID and see the full call graph.

**Metrics** answer: how is the system behaving right now across all requests? What is the P95 payment latency? What fraction of risk checks are rejections? Metrics aggregate across all requests and are queryable in Prometheus and Grafana.

**Logs** answer: what was the application doing at a specific moment? They complement traces for debugging because they can carry arbitrary structured context that is too expensive to put in every span attribute.

---

## Why the OTel Collector matters

Services do not push metrics directly to Prometheus or traces directly to Tempo. All telemetry goes to the OTel Collector via OTLP gRPC. The collector decouples services from backend choices (swapping Tempo for Jaeger means updating collector config, not rebuilding services), provides batching and memory limiting, and exposes a single Prometheus scrape endpoint on port 8889 that aggregates metrics from all services.

---

## Why Prometheus labels must stay bounded

A Prometheus label that contains a payment ID, trace ID, or merchant ID creates a new time series per unique value. At modest scale this exhausts Prometheus memory and makes queries unusably slow. All labels in this codebase use bounded values: `status`, `decision`, `recorded`. Never IDs.

---

## Prerequisites

- Docker with Compose v2
- Go 1.23+ (for running tests locally)
- `curl` and `jq` (for the traffic script)

---

## Quick start

```bash
git clone https://github.com/Bhup-GitHUB/payment-observability-blueprint
cd payment-observability-blueprint
make up
```

Wait about 30–60 seconds for all services to become healthy, then open `http://localhost:8080`.

---

## Service ports

| Service              | Port |
|----------------------|------|
| API Gateway / Web UI | 8080 |
| Payment Service      | 8081 |
| Risk Service         | 8082 |
| Payment Router       | 8083 |
| Bank Simulator       | 8084 |
| Ledger Service       | 8085 |
| Grafana              | 3000 |
| Prometheus           | 9090 |
| Alertmanager         | 9093 |
| Loki                 | 3100 |
| Tempo                | 3200 |
| NATS monitoring      | 8222 |

Grafana credentials: `admin / admin`

---

## Payment scenarios

Send a request from the web UI at `http://localhost:8080` or with curl:

```bash
curl -s -X POST http://localhost:8080/api/payments \
  -H "Content-Type: application/json" \
  -d '{"merchant_id":"merchant_demo","amount":1000,"currency":"INR","scenario":"success"}' | jq .
```

| Scenario  | What happens |
|-----------|-------------|
| `success` | Risk approves → bank succeeds → ledger write → NATS event |
| `slow`    | Risk approves → bank delays 2–4s then succeeds → ledger write |
| `failure` | Risk approves → bank declines → payment marked failed, no ledger write |
| `fraud`   | Risk rejects → stops here, no bank/ledger/notification calls |

---

## Generate traffic

```bash
make traffic
```

This sends one of each scenario per second. After a few minutes the Grafana dashboards populate with meaningful data.

---

## Finding a trace in Tempo

1. Make a payment — the response contains `"trace_id": "..."`.
2. Open Grafana → Explore → select the Tempo data source.
3. Paste the trace ID into the search field.
4. The trace shows all service spans with durations and attributes.

For the `slow` scenario, the `bank.authorize` span visibly dominates the timeline.

---

## Finding logs with a trace ID

1. Copy the `trace_id` from a payment response.
2. Open Grafana → Explore → select the Loki data source.
3. Query: `{service=~".+"} | json | trace_id = "your-trace-id"`

All log lines from all services that handled that payment appear together.

---

## Triggering alerts

Run `make traffic` for a few minutes. The `fraud` and `failure` scenarios push the failure rate above the alert threshold.

- `http://localhost:9090/alerts` — alert state in Prometheus
- `http://localhost:9093` — Alertmanager

---

## Running tests

```bash
go test ./...
```

Covers: risk evaluation logic, bank simulator scenario behavior, NATS trace propagation round-trip, and structured logging with span context injection.

---

## Makefile reference

| Command        | What it does                         |
|----------------|--------------------------------------|
| `make up`      | Build and start all containers       |
| `make down`    | Stop and remove containers + volumes |
| `make logs`    | Follow all container logs            |
| `make test`    | Run Go unit tests                    |
| `make traffic` | Continuous payment traffic generator |
| `make clean`   | Remove containers, volumes, images   |

---

## Engineering decisions

**Money as integers**: All amounts are `int64` in the smallest currency unit (paise for INR, cents for USD). No floating-point values anywhere in the payment path.

**Scenario field over test data**: Scenarios are driven by the `scenario` request field, not by special merchant IDs or card numbers. This keeps service logic readable and makes demos deterministic.

**Single go.mod**: All services share one module. This avoids dependency drift and means one `go mod tidy` keeps everything consistent.

**Embedded migrations**: The ledger service embeds its SQL migrations using `//go:embed` and runs them at startup via `golang-migrate`. The binary is self-contained — no separate migration step needed.

**NATS core, not JetStream**: This prototype uses core NATS pub/sub with no persistence. Events are lost if the notification worker is not running at publish time. For production, JetStream with durable consumers would be appropriate.

---

## Limitations

- No authentication or authorization on any endpoint
- Risk evaluation is deterministic by scenario, not a real ML model
- Bank latency is simulated with `time.Sleep`, not real network calls
- NATS events are lost if the notification worker is restarting when a payment completes
- Single PostgreSQL instance with no replication
- OTel Collector has no persistent queue

## What would need to change before production

- mTLS between services
- NATS JetStream with durable consumers and acknowledgment
- Read replicas and connection pooling for the ledger database
- Secret management instead of plain environment variables
- Horizontal scaling with load balancing
- Alertmanager routing to real on-call (PagerDuty, Slack)
- OTel Collector with persistent queue and retry exporters
- Rate limiting and circuit breakers on outbound bank calls
- Idempotency keys enforced at the API gateway level
