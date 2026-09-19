# Cold-path reports

One file per day, written by `.github/workflows/nightly-report.yml`.

Each contains the yield analysis (per-arm revenue with bootstrap confidence
intervals, guardrail breaches) and the traffic integrity review (impressions
that should not have been billed, and why).

**Both tools report only.** Nothing in here has been applied — no floor was
moved and no billing record was revoked. Applying is a separate, deliberate
step, which is the point: see [ADR 0006](../adr/0006-every-money-number-is-a-variant.md)
and [traffic-filtering.md](../traffic-filtering.md).

Expect these to be empty or absent until the site has real traffic.
