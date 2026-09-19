# Observability — what to look at, and what it means

Two different problems, two different tools. Getting this split right is the
lesson; the tooling is downstream of it.

| | Examples | Cardinality | Tool |
|---|---|---|---|
| **Operational** | latency, errors, throttles, concurrency, request rate | bounded | CloudWatch |
| **Business** | fill rate, eCPM, RPM, session RPM, spend | **unbounded** | Athena over the event log |

Business metrics are the trap. `line_item × creative × placement × country ×
game × device` is a combinatorial explosion, and a time-series database charges
per series. **Real ad platforms do not put fill rate in Prometheus.** "Revenue
by line item by country last Tuesday" is a `GROUP BY`, not a gauge. See
[ADR 0002](adr/0002-observability-without-a-metrics-stack.md).

---

## 1. The CloudWatch dashboard

**Where:** CloudWatch → Dashboards → `adtech-lab` (us-east-1). Free — AWS gives 3.

### Widget: Requests by route *(the Cost Fuse's own signal)*

Four lines, one per route: `POST /ad/request`, `GET /event/impression`,
`GET /event/click`, `POST /collect`. Two horizontal annotations mark the warn
and trip thresholds.

**Why it is drawn this way:** these are the *exact* metrics the Cost Fuse alarms
on. Plotting them with the thresholds annotated is how you judge whether my
threshold arithmetic survives contact with real traffic, rather than trusting it.

**What to read from it:**

| You see | It means |
|---|---|
| `/collect` far above `/ad/request` | Normal. Product events outnumber ad requests ~9:1 — this is *why* the fuse is route-aware |
| `/event/impression` ≈ 60% of `/ad/request` | Healthy fill. That ratio *is* the render rate |
| `/event/impression` well below `/ad/request` | Ads decided but not rendering. Investigate the client, not the server |
| `/event/impression` **above** `/ad/request` | Something is wrong. Impressions cannot exceed decisions |
| Any line approaching its annotation | The fuse is about to matter |

### Widget: Ad server latency (p50 / p95 / p99)

**Why it matters here specifically:** in AdTech, latency is *eligibility*, not
comfort. Past a buyer's `tmax` you do not lose the auction — you were never in
it. Phase 1 has no external buyer, so this is the baseline Phase 4 will be held
to when one arrives with a real deadline.

| You see | It means |
|---|---|
| p50 ~5–10ms | Warm container, control plane cached in memory. Expected |
| p99 ~150ms with low traffic | Cold starts. Expected at this volume, and it *would* miss a 100ms `tmax` |
| p50 climbing | Something entered the hot path — usually an uncached lookup |
| p99 ≫ p50 persistently | Cold starts, or a slow dependency on a fraction of requests |

### Widget: Errors and throttles

Lambda `Errors`, Lambda `Throttles`, API Gateway `4xx` and `5xx`.

| You see | It means |
|---|---|
| `Throttles` > 0 | Either genuine load, **or the Cost Fuse has tripped** — check `adlabctl fuse status` before assuming load |
| `4xx` climbing | Usually forged or expired tracking tokens — i.e. the integrity checks doing their job |
| `5xx` > 0 | A real bug. There is no benign cause |

### Widget: Concurrency (account limit 10)

Max concurrent executions against the account ceiling.

**This account cannot reserve concurrency** — the Free plan caps the account at
10 and requires 10 unreserved. That limit is therefore a hard throughput cap,
and also the reason cost-fuse cannot reserve capacity for itself. If this pins
at 10, a flood could in principle starve the function whose job is to stop it.

### Widget: Cost drivers

DynamoDB `ConsumedWriteCapacityUnits` and Lambda `Invocations` — the two lines
that actually scale the bill. Reads are absent by design: the control plane is
cached in memory, so the hot path issues ~zero reads.

---

## 2. Athena — the business questions

**Where:** Athena → workgroup `adtech-lab` → database `adtech_lab`, table `events`.
Queries live in [`analytics/queries/`](../analytics/queries/). Substitute
`{{FROM}}`/`{{TO}}`/`{{DATE}}`/`{{REQUEST_ID}}`.

> Always filter on `dt`. Partition projection means an unfiltered query scans
> every day ever recorded, and Athena bills by bytes scanned. The workgroup has
> a 1GB per-query cutoff as the backstop.

| Query | Question | What to look for |
|---|---|---|
| `01-trace-one-request` | *Why did this user see this ad?* | The whole life of one `request_id`: decision, impression, click |
| `02-fill-metrics` | The four fill metrics, kept distinct | The **gap** between `request_fill_rate` and `render_rate` — decided but never rendered |
| `03-why-we-did-not-fill` | The Decision Trace, aggregated | `geo_mismatch` = demand does not match audience. `budget_exhausted` = demand exists but is capped. `no_eligible_candidates` = nothing to sell |
| `04-follow-one-dollar` | Where the money went | `realised_ecpm` vs the configured rate. Perfect agreement is suspicious in a real system |
| `05-session-and-product` | Monetisation vs experience | `revenue_per_session`, not impressions. Raising ad frequency raises impressions and can *lower* this |
| `06-latency` | p50/p95/p99 over time | The baseline Phase 4 inherits |

### The one to internalise

`05-session-and-product` is the query the Publisher side exists for. **Raising
ad frequency raises `impressions_per_session` — but if it shortens sessions,
`revenue_per_session` falls while impressions rise.** Impressions are the
vanity number; revenue per session is the real one.

`returning_users` is deliberately absent. `session_id` dies with the tab and
there is no persistent identifier. Measuring it is a gated decision, not a
query — see [privacy-baseline.md](privacy-baseline.md).

---

## 3. Grafana — locally, for free

Managed Grafana is **$9/editor/month**, roughly 18× this system's entire cost.
Nothing about learning Grafana requires paying AWS to host it, so run it locally
against the same data.

```bash
docker run -d -p 3000:3000 --name grafana \
  -e "GF_INSTALL_PLUGINS=grafana-athena-datasource" \
  grafana/grafana-oss
```

Open <http://localhost:3000> — login `admin` / `admin`.

### Connect CloudWatch (operational)

Connections → Data sources → **CloudWatch**

```
Auth Provider    Access & secret key   (or Credentials file, profile: <aws-profile>)
Default Region   us-east-1
```

Credentials need `cloudwatch:GetMetricData`, `cloudwatch:ListMetrics`. **Use a
read-only IAM user or a short-lived session — never your admin keys, and never
commit them.**

Then rebuild the five widgets above as panels. Namespaces you need:
`AWS/ApiGateway` (`Count`, dimensioned by `ApiId`/`Stage`/`Route`),
`AWS/Lambda` (`Duration`, `Errors`, `Throttles`, `ConcurrentExecutions`),
`AWS/DynamoDB` (`ConsumedWriteCapacityUnits`).

### Connect Athena (business)

Connections → Data sources → **Amazon Athena**

```
Region       us-east-1
Catalog      AwsDataCatalog
Database     adtech_lab
Workgroup    adtech-lab
```

Paste any query from `analytics/queries/`, replacing the `{{...}}` placeholders
with Grafana variables (`$__timeFrom()`, or a `dt` template variable).

### What is worth building that CloudWatch cannot show

The point of Grafana here is **joining the two worlds on one screen** — which is
exactly what CloudWatch cannot do:

- **Fill rate against latency.** Does fill drop when the server slows down?
- **`revenue_per_session` against `session_duration`.** The monetisation/experience
  trade-off, on one axis pair. This is the panel worth building.
- **Rejection reasons over time.** Does `budget_exhausted` spike at a time of day?
- **Cost per thousand ad requests against realised eCPM.** Margin, plotted.

That last pair is the whole economic thesis of AdTech on one chart: marginal
cost in micro-dollars, marginal revenue in milli-dollars — and what happens to
the ratio when fill rate falls.
