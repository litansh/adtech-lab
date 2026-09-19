# adtech-lab — Project Guardrails

Permanent constraints for this repository. These override default behaviour.

## Mission

**Learning-first.** This project exists so that AdTech concepts become natural
through having built and operated them. Optimise for understanding, not for
impressive architecture. Revenue is a secondary goal and must never be pursued
at the cost of the learning objective.

## Teach before implementing

Before implementing any meaningful AdTech component, explain: what problem it
solves, who needs it, where it sits in the industry, its inputs and outputs, how
money relates to it, how it affects Publisher or Advertiser economics, its
failure modes, how large platforms implement it, and what we are simplifying.
Then implement. Then walk through one concrete request with real values.

## Language

Responses and documentation in **English**.

## Accounts

Account ids, CLI profiles and identities are never written in this repository.
They live in gitignored files: `infrastructure/*/terraform.tfvars`, `local.mk`
and `CLAUDE.local.md`. **Always pass the AWS profile explicitly** and never rely
on the machine's default profile or global git identity — on a machine with
more than one account, the default is the wrong one.

## Cost

**$100/month is the ceiling, not the target.** Aim for $0-20.

There is **no hard monetary cap** on this stack — AWS provides none for
usage-based services, and CloudFront flat-rate plans are unavailable on the Free
plan. Protection is layered, not guaranteed: route-aware Cost Fuse, API Gateway
throttling, Lambda reserved concurrency, budgets, anomaly detection.

Stop and ask before any change projected to exceed $50/month. Never state a cost
figure as a guaranteed ceiling — say "estimated" until it has been measured.

Account is on the AWS **Free plan**: credits expire **2027-02-08**.

## Architecture

Serverless, managed, pay-per-use, scale-to-zero. Prefer the simplest thing that
teaches the concept. Before adding any component, answer: *what concrete problem
do we have today that requires this?*

Never: Kubernetes, Kafka, Redis clusters, service mesh, multi-region.
Where the industry does something bigger, document it as
"at 50B events/day this would be X; at our scale it is Y".

## Repository boundaries

`apps/publisher` is a **different economic entity** from the platform, not a
module of it. Therefore:

- **`apps/publisher` may never import from `services/` or `packages/`.**
  It talks to the ad server over HTTP only, exactly as an external Publisher
  would. Wanting to break this rule is the signal that it is time to split the
  repository — not a reason to make an exception.
- `game-room.js` and `adtag.js` do not import each other. They share only a
  `session_id` and the analytics transport. That boundary is what makes
  "does advertising hurt the product?" answerable rather than an opinion.
- **Separate Terraform stacks and deploy paths** per component:
  `infrastructure/guardrails`, `infrastructure/publisher`, `infrastructure/ad-server`.
  No stack reaches into another's resources except through documented outputs.
- The Publisher splits into its own repository at **Phase 7**, when a second
  Publisher exists. See [ADR 0001](docs/adr/0001-publisher-lives-in-the-monorepo-until-phase-7.md).

## Agent authority

Agents change **parameters**, never **architecture**.

The test: *can the running system consume this change without a deploy?*
Yes → a parameter, an acting agent may change it. No → architecture, and the
agent opens a **PR** for human review.

No stage of the agent maturity ladder permits an agent to merge its own
architectural change. Full autonomy means autonomy inside the parameter space,
never over the system's shape. See [docs/agent-fleet.md](docs/agent-fleet.md)
and [docs/portable-agents.md](docs/portable-agents.md).

## Process

- Small commits, feature branches, PRs. No direct pushes of meaningful changes to `main`.
- Before merging, show: what changed, why, tests, Terraform plan, cost impact, risk, rollback.
- Terraform for all persistent infrastructure. Document any manual step.
- Terraform manages **infrastructure only** — never campaigns, line items or creatives.

## Attribution

All commits, PRs and repository content are authored by the repository owner
alone.

- No `Co-Authored-By` trailers, no session links, no "Generated with" footers,
  in commit messages or PR descriptions.
- Use the repo-local git identity, and check which `gh` account is active
  before any push or PR. `.githooks/pre-push` enforces both.

## Never commit

AWS credentials, GitHub tokens, private keys, API secrets, partner secrets,
Terraform state.

## Traffic integrity

Two strictly separate worlds:

- **Lab** — synthetic traffic, bots, fraud simulation, load tests. Against our own systems only.
- **Production** — real users only.

Never generate fake impressions, clicks, users, conversions or engagement against
any real monetisation platform. No traffic laundering, no policy circumvention.
Synthetic events must never reach a real monetisation partner or appear in any
figure reported to an advertiser or publisher.

## Product quality

The Publisher must provide real value with advertising removed. Not an MFA site,
not an ad-refresh machine. Never create an ad opportunity merely because we
technically can; every monetisation change is measured against product metrics
in the same experiment.

## Privacy

No persistent advertising identifier in Phase 1 production. Data minimisation by
default. Introducing an identifier is a gated decision requiring a written
purpose, scope, lifetime, notice update and consent analysis — not a feature toggle.

## Research current specifications

Do not rely on built-in knowledge for standards. Verify against IAB Tech Lab,
Prebid, AWS docs and primary sources at the start of each phase, and cite them.
Standards move: OpenRTB ships monthly, Prebid weekly.

## Approval gates

Explicit approval required before: first AWS deployment, any new paid service
with non-trivial fixed cost, projected spend over $50 or $100/month, connecting
real monetisation, connecting external Publisher traffic, buying traffic, sending
real OpenRTB traffic to an external partner, creating paid third-party accounts,
or changing fundamental architecture.

Enthusiasm is not approval.
