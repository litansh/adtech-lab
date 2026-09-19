# If the fleet became a product

> "Agents should replace the whole SDLC lifecycle — and that's a huge product,
> if it can be plug and play. Along with optimisations, learning the demand and
> publishers, which fields to pass from the ORTB and in what versions, how each
> demand and publisher works, with generic agentic rules and per publisher /
> demand. And it should also understand where the product should be in the
> AdTech ecosystem — closer to the demand or publishers, with an SDK or without?
> Using its own agentic skills, and if it's too risky use a human — but if it
> has a rollback, and there's a good SRE agent and production severity rules, it
> shouldn't be too risky."

There is a real product in here. It is **not** the SDLC part, and this document
argues that the weakest idea in the message is the one stated first, while the
strongest is stated fourth almost in passing.

---

## 1. Where I disagree: "replace the SDLC" is the wrong wedge

Two problems with it.

**It is horizontal, and horizontal means competing with everyone.** "Agents that
write, test and ship code" is the most crowded category in software. Winning
there requires beating general-purpose coding agents at their own game, with a
fraction of the resources, and none of the advantage is AdTech-specific.

**And the SDLC is not the bottleneck.** In AdTech the expensive part has never
been writing the integration — it is *knowing what the integration should do*.
Which fields this DSP actually reads. Why bid rate dropped 4% after a release
three weeks ago. Whether that publisher's traffic is worth its infrastructure.
Shipping code faster does not touch any of that.

There is a hint of this in our own work already: **most of the machinery in
`portable-agents.md` exists to make agents adoptable, not to make them better.**
Adoption is a legitimate goal, but it is not a moat.

## 2. Where the real product is: ORTB field and version learning

This is the strongest idea in the message and it deserves to be first.

**The problem.** Every SSP maintains a per-DSP configuration: which OpenRTB
fields to send, which extensions, which version, which combinations break
things. It is accumulated tribal knowledge — encoded in configs, half-remembered
by two people, never revalidated after the DSP changes its parser. Nobody knows
which of those settings still earn their place, because nobody has ever tested
one in isolation.

**The insight: this is an experiment, not a configuration.**

> Every field we could send to a buyer is a **variant**.
> "Send `device.ua`" versus "do not" is an A/B test with a revenue outcome.

Which means it needs no new machinery here. It is
[ADR 0006](adr/0006-every-money-number-is-a-variant.md) applied to the *bid
request* rather than to the floor:

| Experiment | Arms | Measured on |
|---|---|---|
| `field.device_ua` | send / omit | bid rate, CPM, win rate for that buyer |
| `field.user_eids` | send / omit | bid rate, CPM |
| `ortb.version` | 2.5 / 2.6 | bid rate, error rate |
| `field.imp_video_placement` | 2.5 semantics / 2.6 semantics | bid rate |

Assignment unit is **placement**, not session — a DSP learns a supply path's
shape over days, so splitting per session teaches it an average that exists
nowhere. That is exactly the case `UnitPlacement` was written for.

**Why this is defensible and the SDLC idea is not:** the output is proprietary
knowledge derived from traffic nobody else has. "For this DSP, omitting field X
costs 12% of bid rate" is not in any spec, cannot be looked up, and is worth
money. It compounds. And it is boring enough that no general-purpose coding
agent will ever wander into it.

### Built, and the first finding

Implemented in `services/ad-server/bidrequest.go` and seeded in the shipped
control plane. A buyer in the Lab quietly depends on `user.eids` for 55% of its
bid rate — the kind of dependency a real DSP never tells you about — and the
experiment recovers it.

But the *first* run recovered almost nothing, and the reason is the most
important lesson the exercise produced:

```
arm send: 53.2% bid rate
arm omit: 50.2% bid rate
aggregate effect: 5.7%          <- looks like noise
```

Measured per buyer, on exactly the same traffic:

```
buyer-premium     32.8% -> 15.3%   53.4% drop   <- the real effect
buyer-midmarket   65.7% -> 66.8%   ~0
buyer-mobile      77.3% -> 78.4%   ~0
buyer-longtail    90.2% -> 90.4%   ~0
```

**One buyer of five depends on the field; the other four dilute the aggregate by
a factor of nine.** A product that reported the aggregate would conclude
`user.eids` does not matter and drop it — losing half the bid rate of its best
buyer while showing a 5.7% metric movement that looks like variance.

> **Field experiments must be measured per buyer. The aggregate is not a weaker
> version of the answer, it is the wrong answer.**

This generalises beyond fields: any experiment whose effect is concentrated in a
minority of a population will be diluted into invisibility by a population-level
metric. It is the same shape as the *paid fill vs total fill* finding from the
floor sweep, where the house fallback kept total fill at 100% while paid fill
collapsed.

### The two constraints that make it honest

**Combinatorial explosion.** With *n* fields there are 2ⁿ combinations, and at
realistic traffic you can resolve maybe a handful of arms at a time. So: one
field at a time, prioritised by suspected impact, and an explicit statement that
interactions between fields are **not** being measured. Pretending otherwise
manufactures winners.

**Protected fields must never be experimented on.** Some fields are not
optimisation surface:

- consent signals (TCF, GPP) — legal
- `source.schain` — supply chain transparency; removing it to see whether bid
  rate improves is fraud-adjacent, whatever the result
- `regs` / privacy flags — legal
- anything whose absence misrepresents the inventory

An agent that can experiment on schain is an agent that will eventually discover
that lying pays. **The protected set is a hard-coded refusal, not a
configuration**, for the same reason age assurance sits outside the agent layer
in [identity.md](identity.md).

## 3. Where I disagree again: "plug and play" fights the value

The value comes from learning *your* partners' behaviour from *your* traffic.
That needs data, and data needs time. A product that works on day one has no
data and therefore no learning — it is a config UI.

**The cold start is real.** But it also points at the actual moat: *learning
transfers across customers*. "DSP X's bid rate collapses without `device.ua`"
is likely true at every SSP. A product with fifty customers learns in a week
what one customer learns in a year.

That is a genuine network effect, and it comes with a genuine problem: it is
**one customer's traffic informing a competitor's configuration**. That must be
handled explicitly — aggregate, anonymised, opt-in, never per-customer figures —
or the first serious customer's security review ends the conversation. Worth
designing before it is sold, not after.

**Honest framing:** not plug-and-play. *Plug in, observe for two weeks, then
recommend* — which is precisely Stage 0 → Stage 1 of the ladder that already
exists. The ladder is the onboarding path.

## 4. Where I agree, with a sharpening: the risk model

The claim — *rollback plus a good SRE agent plus severity rules makes autonomy
acceptable* — is right in shape and has one hole worth closing.

**Rollback only helps for failures you detect.** A large class of AdTech failures
is slow and silent:

- a field change that costs 3% of bid rate looks like normal variance for days
- a floor change that suppresses a single buyer shows up in nobody's dashboard
- a traffic-quality change that over-blocks reads as "a quiet week"

You cannot roll back what you have not noticed. So the gate is not *"is there a
rollback?"* — it is:

> **Is the failure detectable within the rollback window, by something that is
> already watching?**

That reframes the severity rules from blast radius to **detectability**:

| Class | Detection | Autonomy |
|---|---|---|
| **Fast + loud** — errors, latency, spend spikes | seconds, alarms exist | full autonomy, auto-rollback |
| **Fast + quiet** — bid rate, fill, win rate | minutes, needs a monitor built *first* | autonomy only once that monitor exists |
| **Slow + quiet** — CPM drift, buyer relationship, retention | days to weeks, statistical | **never autonomous** — proposal only, human review |
| **Not measurable** — reputation, contracts, legal | not detectable at all | never automated at any stage |

The third row is where most AdTech value *and* most AdTech damage live, and it
is the row an enthusiastic autonomy story quietly omits.

**Practical consequence:** the SRE agent's first job is not responding to
incidents. It is **certifying which changes are in which row** — because that
certification is what determines whether any other agent may act at all. An
agent asking for Stage 3 must name the monitor that would catch it being wrong,
and how fast.

That is also a much better answer to "is this too risky for an agent?" than
seniority or intuition, and it is auditable.

## 5. The positioning question — SDK or not

**Corrected 2026-08-29.** The first version of this section concluded "an
adaptive product should not be SDK-first". That answered the wrong question, and
the corrected version is more useful.

There are two questions hiding in "should we be in an SDK?":

**A. Should our adaptive decision logic live in the SDK?**
No. An SDK update takes weeks to months to propagate, so anything decided inside
it changes at the publisher's release cadence — the slowest clock in the
industry. An agent that learns something on Tuesday cannot apply it until next
quarter.

**B. Should we be *present* in an SDK at all?**
**Yes, and it is one of the highest-value moves available.** This is a question
about supply access, not about velocity:

- **In-app is where the money is.** Rewarded video is $10–30 eCPM against $1–5
  for web display — a fact already recorded in [timeline.md](timeline.md) and
  not connected to this question in the first draft.
- **Fewer hops.** SDK presence is direct supply: shorter `schain`, fewer
  intermediaries taking a fee, a structurally better position than competing
  downstream.
- **Signal unavailable server-side.** Device context, viewability, session
  state.
- **Stickiness.** Removal requires a publisher release, which favours the
  incumbent.

The two answers do not conflict, and the resolution is the actual best practice:

> **Ship capability in the SDK. Ship policy on the server.**

The SDK is a transport, a renderer and a signal collector. Every *decision* —
floors, which partners to call, field selection, timeouts — resolves server-side
per request. Agent velocity is preserved *with* full SDK access.

**The failure mode is not having an SDK. It is an SDK that hardcodes
decisions**: a baked waterfall order, client-side floors, a compiled partner
list. That is what welds optimisation to the release cadence. The accurate rule
is therefore not "avoid SDKs" but:

> **Never ship a decision in an SDK. Only the ability to execute one.**

### Risks that survive, and their single shared mitigation

| Risk | Consequence |
|---|---|
| **Version fragmentation, permanently** | the long tail never updates; every server change must stay safe against a two-year-old client |
| **A bug ships for months** | no hotfix path without a publisher release |
| **Size and performance scrutiny** | heavy SDKs get rejected; ANRs get you removed |

All three are mitigated by the same discipline — capability in the client,
policy on the server, and a **server-controlled kill switch per feature**.

### Mediation adapter versus in-app bidding

"Being in an SDK" is two very different positions:

- **Mediation adapter (waterfall):** ranked against others on *historical*
  eCPM. Structurally weak — you are guessing, and being guessed about.
- **In-app bidding (unified auction):** you bid in real time on the actual
  impression.

If the strategy is "join an SDK", being in the **bidding** path rather than the
waterfall is most of the value, and it is worth treating as the actual objective
rather than a detail of the integration.

### Where the agents sit, given all of the above

- **Server-side**, always. That is where every parameter changes in seconds.
- **Closer to the publisher than to demand**, because that is where the
  unmodelled information is. Demand behaviour can be *learned by observing
  responses* — that is section 2. Publisher context cannot be inferred from
  outside at all.
- **SDK presence is an asset to the agents, not a constraint on them** — as
  long as the SDK carries no policy.

The general rule survives the correction: **put the agent where the feedback
loop is fastest, and keep everything slow-to-change out of its path.** An SDK
that ships only capability is not in its path.

## 6. What I would build, in order

Assuming the goal is a product rather than a demo:

1. **The ORTB field experiment layer** for one buyer, one field. Needs
   `services/ad-server` request shaping plus the existing experiment layer.
   Proves the core claim.
2. **Per-buyer behavioural profiles** — bid rate, timeout rate, field
   sensitivity, version tolerance. Falls out of (1) as a by-product and is the
   asset that compounds.
3. **The detectability certification** from §4, because nothing may act until
   changes are classified.
4. **The SRE agent**, whose first job is (3) rather than incident response.
5. *Then*, if there is still appetite, code generation — which by then has
   something worth generating, because (1) and (2) told it what to build.

Note that the SDLC idea is last and much smaller than it first appeared. Once
the system knows which fields matter per buyer, "write the integration" is
mostly templating.

## 7. Honest assessment

**The idea in §2 is genuinely good** — a real, expensive, unglamorous problem
with a measurable outcome, a defensible data moat, and no serious competition,
because it is too boring to attract any.

**The idea in §1 is not the wedge**, though it may be a feature later.

**The risk model in §4 is right in shape and needs the detectability gate** to
survive contact with an incident review.

And the thing that would kill it: **an agent that optimises bid rate learns that
sending more data always helps.** Every privacy and transparency constraint
looks, to a revenue-maximising optimiser, like a bug. That is why the protected
field set is hard-coded and why Integrity outranks Yield in the arbitration
order — and in a *product*, that ordering is not an implementation detail. It is
the thing being sold.
