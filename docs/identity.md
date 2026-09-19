# User verification and identity

> "Publishers today want to verify users — see how you do it in the best way.
> Maybe another agent, or a role for an agent."

"Verify users" is four different problems wearing one phrase. They have
different owners, different legal weight, and one of them is not an AdTech
problem at all.

---

## The four things people mean

| # | Question | Really about | Who owns it here |
|---|---|---|---|
| 1 | Is this a human? | fraud / IVT | **Integrity** — already built |
| 2 | Is this person allowed to be here? | age assurance, consent | **compliance** — not an agent decision |
| 3 | Who is this person, across sessions? | identity for targeting and frequency | **Integrity**, new mandate |
| 4 | Can we prove they are who they claim? | authentication | **Product** — it is a login |

Conflating 1 and 3 is the common error, and it matters because they pull in
opposite directions: #1 wants to *reject* traffic, #3 wants to *enrich* it.

## The finding that reorders everything

**Alternative IDs are downstream of authentication.**

UID2, EUID and ID5 are all built on a hashed email with consent. A publisher
with no logged-in users **cannot supply any of them** — there is no email to
hash. So for most publishers, "adopt UID2" is not an integration project, it is
a *login* project wearing an integration's clothes.

Which means: for the large majority of publishers, **"verify users" is a product
problem, not an AdTech one.** The AdTech work is three days. Getting people to
log in is the actual work, and it is Product's.

This is why the ranking below starts where it does.

## The ranking, for a publisher in 2026

**1. Authenticated first-party identity — the only durable answer.**

Highest CPMs, survives every deprecation, and it is *yours*. The hard part is
not technical: it is having a reason for someone to log in that benefits them
rather than you.

The honest reason has to be a feature they want. For a games site the obvious
one is **scores and streaks that follow you across devices** — which is a real
benefit, not a pretext. "Log in to continue" is a pretext, and users can tell.

**2. Seller-defined audiences (IAB Tech Lab SDA) — the underrated one.**

The publisher declares audience cohorts from a standard taxonomy, sent in the
bid request. **No identifier, no cross-site tracking, no consent for personal
data required**, because none is processed.

Chronically underused because it earns less per impression than an ID-based
audience — but it applies to *100% of traffic*, including the unauthenticated
majority that alternative IDs can never reach. Total uplift usually beats the
higher-CPM option that covers 5% of inventory.

**3. Alternative IDs (UID2/EUID, ID5, RampID) — only after #1.**

Real demand, real CPM uplift, and entirely dependent on #1 existing. Adopting
them before you have logins is building a pipe with nothing to put in it.

**4. Privacy Sandbox (Topics, Protected Audience) — browser-mediated.**

The publisher holds no identifier; the browser does the work. Worth
understanding, low effort to support, and not something to build a strategy on.

## Age assurance is not an advertising decision

Item #2 deserves separating because people file it under identity and it does
not belong there.

Age assurance is a **legal precondition**, not an optimisation. Where it applies
it is binary: you comply or you do not operate. It has nothing to do with CPMs,
it cannot be traded against revenue, and it must never be an agent's decision —
in either direction.

**It is a precondition on serving, checked before any agent runs.** An agent
that could weigh compliance against revenue is an agent that will eventually
weigh it wrongly.

## Agent or role?

**A role, inside Integrity — not an eighth agent.**

Integrity already exists to say *no* on non-revenue grounds, and it is
structurally superior to Yield for exactly that reason. Identity and consent are
the same kind of constraint:

| | Existing mandate | New mandate |
|---|---|---|
| Question | should anyone be billed for this? | what may we know about this user, and is it lawful? |
| Answers to | correctness and law | correctness and law |
| Overridable by revenue? | **no** | **no** |
| Data it reads | session, traffic class, timing | session, consent string, auth state |

Same input, same veto, same failure mode — *revenue pressure erodes it*. A
separate agent would duplicate the machinery and, worse, create the possibility
of the two disagreeing, which is exactly the seam a revenue argument gets pushed
through.

So Integrity's mandate becomes: **"nothing is billed that should not be, and
nothing is known that should not be."**

The one argument for separating them — that age assurance has a different
escalation path — is handled by taking age assurance out of the agent layer
entirely, as above.

### What the Integrity agent gains

| Metric | Question |
|---|---|
| **consented share** | what fraction of traffic carries a valid TCF/GPP signal |
| **addressable share** | what fraction carries a usable audience signal, by type |
| **authenticated share** | what fraction is logged in — the ceiling on every ID strategy |
| **CPM by signal type** | what each identity tier is actually worth |

That last row is the one that makes the argument. Without it, "should we adopt
UID2?" is a debate. With it, it is a number.

## Where this project stands

`CLAUDE.md` forbids a persistent advertising identifier in Phase 1 production,
and introducing one is a gated decision requiring a written purpose, scope,
lifetime, notice update and consent analysis.

That position is **unchanged by this document**, and worth restating: our
records table is `localStorage` — a score, not an identifier. It never leaves
the device, it identifies nobody, and it needs no consent.

If we ever wanted scores to follow a player across devices, that is a login, and
a login is the gated decision. It is also, per the ranking above, **the single
highest-value identity move available** — which is a useful thing to know and
not a reason to rush it.

For the learning goal, the valuable exercise is **seller-defined audiences**: it
teaches how audience signal reaches a bid request, costs nothing, requires no
identifier, and does not touch the gate at all.
