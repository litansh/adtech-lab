# ADR 0001 — The Publisher lives in this repo until Phase 7

**Status:** accepted · 2026-08-28

## Context

`apps/publisher` is the mini-games site. It is a **different economic entity**
from the ad platform: separate ledger, separate P&L, separate reason to change.
Keeping both in one repository risks eroding a boundary the project exists to
teach — it makes it easy to share a helper, import a type, and stop noticing
that these are two businesses.

At Phase 7 an external Publisher appears. At that point our O&O site is one
publisher among several and has no business living inside the ad platform's repo.

## Decision

Keep `apps/publisher` in this repository **through Phase 6. Split at Phase 7.**

Two constraints make the split cheap when it comes:

1. **`apps/publisher` may never import from `services/` or `packages/`.**
   It communicates with the ad server over HTTP only, exactly as an external
   publisher would. Wanting to break this rule is the signal to split.
2. **Separate Terraform stacks and deploy paths** for publisher and ad-server,
   so the split is a `git filter-repo`, not an untangling.

## Alternatives

- **Split now into `litansh/game-room`.** Conceptually cleaner. Rejected: Phase 1
  is one vertical slice whose whole value is end-to-end traceability from a click
  on a board square to a row in Athena. Two repos means two PRs, two CI runs and
  two deploys on exactly the workflow the phase is about.
- **Never split.** Rejected: it becomes actively wrong once a second publisher exists.

## Why

The Architecture Honesty Rule: *what concrete problem do we have today that
requires this?* Today, none. The load-bearing separation is already real —
`adtag.js` reaches the ad server over HTTP like any third party, and
`game-room.js` and `adtag.js` do not import each other. A repo boundary would
enforce that harder, but it is not what makes it true.

## Cost impact

None. Both options are $0.

## When to reconsider

**The trigger is concrete: the moment a second Publisher exists** (Phase 7).
Also reconsider if anyone proposes importing across the `apps/publisher`
boundary, or if publisher and ad-server release cadences start conflicting.
