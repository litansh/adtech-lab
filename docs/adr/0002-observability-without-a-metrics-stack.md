# ADR 0002 — Observability without a metrics stack

**Status:** accepted · 2026-08-28

## Context

We want alerts, monitors and dashboards — including the option of Grafana with
Prometheus metrics — for the important parts, within the project budget.

The budget is the constraint that makes this interesting: the whole system runs
at **$0.51–2/month**. Any monitoring stack that costs more than the thing it
monitors has failed a basic sanity check.

## Two different problems

The decisive realisation is that "monitoring" here is really two problems, and
they want different tools:

| | Examples | Cardinality | Right tool |
|---|---|---|---|
| **Operational** | latency p50/p95/p99, errors, throttles, concurrency, request rate | Low, bounded | A metrics system |
| **Business** | fill rate, eCPM, RPM, session RPM, win rate, spend | **Unbounded** | A query layer over the event log |

Business metrics are the trap. `line_item × creative × placement × country ×
game × device` is a combinatorial explosion, and a time-series database charges
per series. **Real ad platforms do not put fill rate in Prometheus.** They put
it in a query layer over the event log, because that is the shape of the
question — "revenue by line item by country last Tuesday" is a GROUP BY, not a
gauge.

## Decision

**Operational → CloudWatch.** A dashboard of the golden signals plus the Cost
Fuse's own input metrics, so the fuse thresholds can be judged against reality.
CloudWatch gives 3 dashboards free. Alarms are already deployed.

**Business → the event stream → Athena.** The events already carry everything
needed. Phase 2 adds the queries and a small generated dashboard.

**Grafana → run it locally, for free.** If the goal is to learn Grafana, a
local container against CloudWatch and Athena data sources gives the entire
learning experience at **$0** AWS cost. Nothing about learning Grafana requires
paying AWS to host it.

## Alternatives, priced

| Option | Cost/month | vs our $0.51 baseline |
|---|---|---|
| CloudWatch dashboard (chosen) | **$0** (3 free) | — |
| Local Grafana in Docker (chosen for learning) | **$0** | — |
| Amazon Managed Grafana | **$9/editor** | ~18x the entire system |
| Amazon Managed Prometheus | ~$1–3 + a collector to run | plus operational burden |
| Self-hosted Grafana on t4g.small | ~$12 + EBS | always-on; breaks scale-to-zero, and we would patch it |

Rejected for now: all three paid options. Not because they are bad, but because
none buys anything at this scale that CloudWatch plus Athena does not, and the
cheapest of them triples the project's total cost.

## Why

The Architecture Honesty Rule. The concrete problem is "can I see latency,
errors and cost, and judge whether the fuse thresholds are sane?" CloudWatch
answers that today for $0. Prometheus answers a question we do not have yet:
high-cardinality operational metrics across many services.

## Cost impact

$0. Deliberately.

## When to reconsider

- Several services with operational metrics CloudWatch cannot express
- A genuine need for PromQL, recording rules, or long-retention operational data
- Real revenue making $9–12/month irrelevant — but note that even then, business
  metrics still belong in Athena, not in a metrics store
