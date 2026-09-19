# Standards Status — Verified August 2026

Classification: `LEARN NOW` · `IMPLEMENT NOW` · `SIMULATE` · `LEARN LATER` · `NOT RELEVANT`

Re-verify this table at the start of each phase. Several entries changed between the first and second drafts of this document.

## The Decision Table

| Standard | Current status | Classification | Reasoning |
|---|---|---|---|
| **OpenRTB 2.6** | `2.6-202606` (June 2026). Monthly releases; version increments only on breaking changes | LEARN NOW -> IMPLEMENT Phase 5 | The working standard of the industry |
| **OpenRTB 3.0** | Published 2018; did not achieve broad adoption | NOT RELEVANT | Useful as an industry lesson: a technically sound standard without adoption does not become the standard |
| **AdCOM 1.0** | `1.0-202607` (July 2026) | LEARN LATER | The enumeration/object library designed alongside OpenRTB 3.0; OpenRTB 2.6 references it for some enums. Needed when we need specific enum values |
| **ads.txt** | **1.1** (August 2022), current | **IMPLEMENT Phase 1** (placeholder form only — see below) | Teaches authorisation, `DIRECT`/`RESELLER`, `OWNERDOMAIN`, `MANAGERDOMAIN` |
| **app-ads.txt** | **1.1**, the same specification document as ads.txt, extended to apps | LEARN LATER | We have no app. Listed correctly rather than dismissed |
| **sellers.json** | 1.0 | IMPLEMENT Phase 5 | Meaningful only once we represent inventory to external buyers |
| **SupplyChain (schain)** | 1.0. In OpenRTB 2.6 it is `source.schain`; in 2.5 it was `source.ext.schain` | LEARN NOW -> IMPLEMENT Phase 5 | |
| **Prebid.js** | **Current 11.x generation as of Phase 0.** Weekly release cadence — a patch version pinned here would be stale within days. **Verify the current stable release immediately before Phase 8 integration** | LEARN NOW -> Phase 8 | De-facto header-bidding standard |
| **Prebid Server / S2S** | Active | LEARN LATER | Phase 8+ |
| **GPP — Global Privacy Protocol** | Spec v1.0. **IAB Tech Lab renamed it from "Global Privacy Platform" to "Global Privacy Protocol."** MSPA-alignment update in public comment through 2026-09-11 | LEARN NOW -> pass-through Phase 5 | The container that carries jurisdiction-specific privacy strings |
| **TCF (IAB Europe)** | **v2.3 is the mandatory operational baseline** — full support for writing, reading and processing required as of 2026-02-28; strings created afterwards without the required segment are invalid. **v2.4 is in progress**: policy v5.0.b amendments and a technical public-comment period opened 2026-05-29, GVL specification version increment and GVL/translations followed mid-2026 | LEARN NOW -> IMPLEMENT when EU monetisation begins | Getting this wrong in production degrades ad requests to limited ads |
| **VAST** | **4.3** (December 2022) | LEARN LATER | Video is a separate domain; display first |
| **OMID / OM SDK** | **1.5** (June 2024) | LEARN LATER | We will measure viewability directly with `IntersectionObserver` against the MRC rule — same concept, no SDK |
| **First-price auctions** | Prevailing model in open-web programmatic | IMPLEMENT Phase 3 | |
| **Bid floors** | — | IMPLEMENT Phase 2 | The publisher's primary price lever |
| **PMP / Deals** | Part of OpenRTB | LEARN LATER | Phase 8 |
| **SPO / Curation** | Market structure, not a specification | LEARN NOW | The dominant structural trend of 2025-2026 |
| **Identity** (UID2, ID5, RampID and others) | Active | LEARN LATER | Phase 8. Carried in `user.eids` |
| **Privacy Sandbox** | Substantially wound down — see below | LEARN NOW (as context) | |
| **Third-party cookies in Chrome** | Broad deprecation plan abandoned — see below | LEARN NOW | |
| **Contextual targeting** | — | IMPLEMENT Phase 1 | Our Phase 1 targeting is contextual + coarse geo + device type |
| **Frequency cap / Pacing** | — | Pacing: IMPLEMENT Phase 2. Frequency cap: **Lab only** in Phase 2 | See [privacy-baseline.md](privacy-baseline.md) |
| **Attribution** | — | SIMULATE Phase 4 | |
| **IVT / Fraud** | MRC guidelines | SIMULATE Phase 2+ | Lab only, against our own systems |
| **Brand safety** | — | LEARN LATER | |
| **Bidstream minimisation** | — | LEARN LATER | Becomes real when request volume has real cost |
| **ads.cert 2.0** | Limited adoption | LEARN LATER | |

---

## Privacy Sandbox and third-party cookies — stated precisely

The first draft of this document said "Privacy Sandbox is dead" and "3P cookie deprecation is cancelled." Those shorthands are misleading in both directions. The accurate account:

**What Google did:**
* In **April 2025**, Google announced it would **not proceed with its plan to deprecate third-party cookies broadly in Chrome**. Third-party cookies remain, with no announced removal timeline, and users retain the ability to control them in settings.
* On **2025-10-17**, Google **retired ten of the remaining Privacy Sandbox APIs** across Chrome and Android, citing low adoption. These included **Topics**, **Protected Audience**, and **Attribution Reporting**.
* **Several technologies continue**, including **CHIPS** (partitioned third-party cookies), **FedCM** (federated identity), and **Private State Tokens**.

**What this does not mean:**

Signal loss remains a central AdTech topic, for reasons independent of Google's roadmap:

1. **Safari, Firefox and Brave continue to block third-party cookies by default.** A substantial share of web traffic has been cookieless for years and remains so.
2. **Mobile app identifiers** are governed by platform policy (ATT and equivalents), not by Chrome.
3. **Regulation and consent** constrain data use regardless of technical availability. A cookie that exists but cannot lawfully be used for a purpose is not a usable signal for that purpose.
4. The first-party data, contextual, and identity-graph approaches developed during the deprecation period **remain in active use**, because they addressed problems that did not disappear.

**Implication for this project:** we build contextual and first-party approaches first — because they are the simplest to implement, because a meaningful share of traffic requires them, and because they teach the durable concepts. Not because cookies are going away.

---

## ads.txt in Phase 1 — placeholder form only

Phase 1 has **no external authorised seller**. Publishing an ads.txt file that names a selling system would misrepresent a commercial relationship that does not exist, and would be a false statement to any buyer that reads it.

**What we will publish:** the specification's own placeholder form, which signals "this file is intentionally spec-compliant and declares no authorised advertising systems":

```text
placeholder.example.com, placeholder, DIRECT, placeholder
```

The literal strings `placeholder.example.com` and `placeholder` are the actual required values for this case — they are not variables to substitute. The spec recommends this rather than an empty file so that consuming systems parse it correctly.

**What we will teach now, without publishing false entries:**

| Field / concept | Meaning |
|---|---|
| `DIRECT` | The publisher directly controls the account with this advertising system |
| `RESELLER` | The publisher has authorised another entity to sell this inventory on its behalf |
| `OWNERDOMAIN` | The domain of the entity that ultimately owns the inventory and receives payment. Introduced in 1.1 |
| `MANAGERDOMAIN` | The domain of the entity that manages monetisation for this inventory. Introduced in 1.1 |
| Why 1.1 added them | To let a buyer connect an ads.txt entry to a `sellers.json` record and validate the `schain` end to end |

Real seller entries are added **only when a real external monetisation relationship exists** — Phase 6 at the earliest.

---

## What Phase 1 Actually Implements from This List

1. **ads.txt 1.1**, placeholder form, as a static file.
2. **Contextual + coarse geo + device-type targeting.** No cookies, no identifiers, no consent string.

Everything else is deferred, with the phase noted above.

## Sources

- [OpenRTB 2.x releases](https://github.com/InteractiveAdvertisingBureau/openrtb2.x/releases) · [AdCOM releases](https://github.com/InteractiveAdvertisingBureau/AdCOM/releases) · [SupplyChain object](https://github.com/InteractiveAdvertisingBureau/openrtb/blob/main/supplychainobject.md)
- [IAB Tech Lab — ads.txt](https://iabtechlab.com/ads-txt/) · [ads.txt 1.1 specification (PDF)](https://iabtechlab.com/wp-content/uploads/2022/04/Ads.txt-1.1.pdf) · [sellers.json](https://dev.iabtechlab.com/sellers-json/)
- [IAB Tech Lab — Global Privacy Protocol](https://iabtechlab.com/gpp/)
- [IAB Europe — transition to TCF v2.3](https://iabeurope.eu/all-you-need-to-know-about-the-transition-to-tcf-v2-3/) · [Google Ad Manager — TCF publisher integration and the Feb 2026 deadline](https://support.google.com/admanager/answer/9805023)
- [Prebid.js releases](https://github.com/prebid/Prebid.js/releases)
- [VAST 4.3 (PDF)](https://iabtechlab.com/wp-content/uploads/2022/09/VAST_4.3.pdf) · [Open Measurement SDK](https://iabtechlab.com/standards/open-measurement-sdk/)
- [Google — Next steps for Privacy Sandbox and tracking protections in Chrome](https://privacysandbox.google.com/blog/privacy-sandbox-next-steps)
