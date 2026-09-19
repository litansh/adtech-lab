# "More data makes the models more precise"

The most common justification for keeping everything forever, and the one least
often tested. It is **true in four specific cases and false in three**, and which
case you are in is an empirical question with a cheap experiment attached.

This document is about how to find out, not about which answer is right.

---

## Where the claim is genuinely true

**1. Rare events.** Modelling conversions at 0.1% means the binding constraint is
the count of *positive* examples, not rows. Ten million impressions with ten
thousand conversions is a ten-thousand-row problem wearing a ten-million-row
costume. Doubling volume genuinely doubles the signal.

**2. High-cardinality tails.** Publisher × placement × geo × device × hour is
millions of cells, and each needs its own observations. The head is learnable
from a sample; the tail is only learnable from volume. Most of the inventory
lives in the tail.

**3. Per-entity models.** A model per buyer, per placement or per publisher needs
data *per entity*. Aggregate volume is irrelevant if it is concentrated in
entities you are not modelling.

**4. Re-processing optionality.** This is the strongest argument and it is not
about precision at all: **you cannot retroactively compute a feature from data
you did not keep.** Aggregates are lossy and irreversible. Keeping raw events
preserves the ability to build features you have not thought of yet.

Note that (4) argues for keeping *raw* data, not for keeping it *forever*, and
those are different policies.

## Where the claim is false

**1. Learning curves saturate, and nobody checks where.**

Model performance against training size is roughly logarithmic. Going from 10M
to 100M rows might buy 1–2% AUC. The question is never *"does more data help?"*
— it almost always helps a little. The question is:

> **Does the next 10× of data improve the model enough to pay for storing and
> scanning it?**

That is a measurable trade and it is almost never measured.

**2. Non-stationarity means data has a half-life — and old data can be actively harmful.**

This is the strongest counter-argument, and it is specific to domains like ours.

AdTech distributions shift constantly: campaigns start and stop, budgets move,
seasonality, competitors change bidding behaviour, a privacy regulation lands, a
DSP changes its parser. **Data from eighteen months ago may describe a market
that no longer exists.** Training on it does not merely add noise — it adds
*confident, systematic* error, because the model learns relationships that were
true and are not any more.

In a non-stationary domain, more history is not more signal. It is more of a
different signal.

**3. The bottleneck is usually features, not rows.**

If the predictive information is not in the columns you have, adding rows of the
same columns does not help. Teams reach for volume because it is easy to obtain
and feature work is not.

## The experiment that settles it

Two learning curves. Cheap, one-off, and decisive.

### Curve A — how much does volume help?

Train on 1%, 5%, 10%, 25%, 50%, 100% of available data. Plot the metric you
actually optimise.

**Read the elbow.** Wherever the curve flattens is the point past which volume
is waste for *this* model. If 25% of the data reaches 99% of the performance,
the remaining 75% is being stored, scanned and paid for to buy 1%.

### Curve B — does recency beat volume? *(the important one)*

Hold **sample size constant** and vary only the window:

| Training set | Rows | What it tests |
|---|---|---|
| most recent 90 days | N | recency |
| random sample across 2 years | N | volume-with-history |
| most recent 30 days | N | extreme recency |

**If the recent-90-day model beats the 2-year sample at equal size, old data is
actively harmful** — and long retention is costing money twice: once in storage
and scan, once in accuracy.

This is the single most valuable experiment in this document, and it takes a day.
It is rarely run because it can only produce answers somebody will find
inconvenient.

### What the result implies

| Outcome | Retention policy it argues for |
|---|---|
| Curve A flattens early, B favours recency | short raw retention, aggressive expiry |
| Curve A still climbing, B neutral | keep raw longer; volume is genuinely working |
| B strongly favours recency | **shorten retention and expect accuracy to improve** |
| A flat but re-processing matters | keep raw *cheaply* (cold storage), not query-optimised |

## Separate the four reasons for keeping data

They get conflated, and they imply completely different policies:

| Reason | What it justifies | Typical honest horizon |
|---|---|---|
| **Model training** | whatever the learning curve shows | often 90–180 days |
| **Re-processing optionality** | raw events, cheap storage | 1–2 years, cold |
| **Dispute, audit, contractual** | immutable records, not features | per contract, often 12–24 months |
| **"We might need it"** | nothing | — |

The third is a genuinely good reason that has nothing to do with models. A
reporting discrepancy argued six months later needs the *records*, not a
training set — and it needs them queryable-if-necessary rather than
query-optimised.

**The fourth is the one to hunt.** It is usually the largest by volume and it is
invisible, because nobody is asked to justify it.

## The organisational reason data accumulates

Worth naming, because it explains more than any technical argument:

**Deleting data is risky and visible. Keeping it is safe and invisible.** If you
delete something and it turns out to be needed, that is your fault and everyone
knows. If you keep everything, the cost is spread across an infrastructure line
item nobody owns.

Nobody is rewarded for deleting a pipeline. So the default is accumulation, and
"the models need it" becomes the post-hoc justification for a decision the
incentives already made.

**The diagnostic question:** *when was the last time anyone deleted a dataset,
and who decided?* If the answer is "never" or "nobody", the retention policy is
not a policy.

## The compliance angle, which is not optional

Under GDPR, data minimisation and storage limitation require a **documented
purpose** and retention proportionate to it. **"Models might be more precise" is
not a purpose** — it is a hypothesis, and an untested one.

That converts an unmeasured retention policy from a cost problem into a legal
exposure. Which also means the learning-curve experiment above has a second
value: it is the evidence that a retention period is *proportionate*, which is
exactly what a regulator asks for.

## For this project

We keep raw events in S3 as gzipped NDJSON, partitioned hourly, and query them
with Athena.

**Correction.** The first version of this section said no expiry was configured.
That was wrong — writing this document prompted a check, and the check found
something worse than a missing policy: a policy that **contradicted our stated
privacy commitment.**

`privacy-baseline.md` commits to *"raw event data retained 90 days, then
aggregated and the raw records deleted."* The lifecycle rule transitioned to
Glacier IR at 90 days and expired at **365** — so raw records outlived the
promise by four times. A privacy commitment the infrastructure does not honour
is worse than no commitment, because it is relied upon.

Now expiring at 90 days, matching what we said.

The Glacier IR transition was removed rather than rescheduled, and the reason
generalises. **Cold storage classes bill a minimum object size** — 128KB for
Glacier IR — against Standard's actual size:

```
standard   = $0.023 * size_kb
glacier_ir = $0.004 * max(size_kb, 128)
break-even = 0.512 / 0.023 = 22.3 KB
```

Below ~22KB, Glacier IR costs **more** than Standard, before the per-object
transition fee. Our objects are one Lambda invocation's buffer of gzipped
NDJSON, far under that. The tier that exists to save money was costing several
times what it saved.

> **Small objects are the case where cold storage loses.** Storage-class
> optimisation is an object-size question before it is an access-frequency
> question, and almost every "move old data to Glacier" recommendation omits it.

Our own version of the four reasons:

Our own version of the four reasons:

| Reason | Applies to us? |
|---|---|
| Model training | not yet — no models |
| Re-processing | **yes** — the schema is still changing |
| Dispute/audit | **yes in principle** — no real advertiser yet |
| "Might need it" | honestly, this is most of it today |
