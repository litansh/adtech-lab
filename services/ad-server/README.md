# ad-server

The decisioning core: *which of my eligible demand sources serves this slot?*

Not an exchange. No auction, no bidding vocabulary — see the terminology table
in `docs/architecture-proposal.md`. Bids arrive in Phase 3 with independent buyers.

## The decision, in order

```
1. eligibility     can this line item serve this opportunity at all?
                   status, flight dates, targeting, a creative that fits, budget
2. priority        guaranteed demand outranks non-guaranteed, regardless of price
3. economic value  expected_ecpm, normalised across CPM / CPC / CPA
```

Price is the **last** consideration, not the first. A $12 guaranteed line item
beats a $15 remnant because the Publisher contracted to deliver it. GAM's
dynamic allocation is the industry example of the same rule.

## Endpoints

| Route | Purpose |
|---|---|
| `POST /ad/request` | Decision. Returns a creative + signed tracking token, or `no_ad`. Always returns `request_id` |
| `GET /event/impression?token=` | Counts an impression. Rejects anything unsigned or expired |
| `GET /event/click?token=` | Counts a click, then redirects to the destination **from the creative record** |
| `POST /collect` | Batched product analytics from the Publisher |

## Two kinds of spend

`serving_budget_state` controls delivery and may be approximate — bounded
over-delivery is normal and contracted-for in real ad servers. `billing_ledger`
is derived from the event stream, is idempotent via `event_id`, and is the only
record that means money. Do not merge them.

You can watch this happen: exhaust a $5.00 daily budget at a $4.00 CPM and the
counter lands at `$5.004` — slightly over, because eligibility is checked before
the impression is charged.

## Run locally

```bash
# ad server, Lab mode (Decision Trace is returned)
ADLAB_ENV=lab ADLAB_TOKEN_KEY=dev-key ADLAB_DEFAULT_COUNTRY=IL go run .

# one origin for Publisher + ad server, as CloudFront will do in AWS
python3 ../../tools/dev/devproxy.py     # http://127.0.0.1:8139
```

| Env var | Meaning |
|---|---|
| `ADLAB_ENV=lab` | Return the Decision Trace. **Never set in production** |
| `ADLAB_TOKEN_KEY` | HMAC key for tracking tokens |
| `ADLAB_CONTROL_PLANE` | Path to the control-plane JSON |
| `ADLAB_DEFAULT_COUNTRY` | Geo fallback when there is no CloudFront header |

## Deliberately simplified

Single static token key, no rotation. Control plane from a JSON file rather than
DynamoDB. In-memory budget counters. No pacing, no frequency capping, no
viewability. Each of those arrives with the phase that needs it.
