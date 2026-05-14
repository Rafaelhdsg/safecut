# Conversion roadmap — Ondas 1, 2, 3

This document tracks the freemium conversion work that ships after the
plan in [`.cursor/plans/freemium_conversion_upgrade_*.plan.md`](../). Wave 1
lands with `v1.0.0-rc1`; Waves 2 and 3 are explicitly **out of scope** for
`v1.0.0` but must be scheduled before we raise prices.

## Onda 1 — Public pricing + ROI CTA (shipped in rc1)

Status: done. Captured so we don't regress.

- Single source of truth in [`internal/pricing_tiers/tiers.go`](../internal/pricing_tiers/tiers.go).
- `CloudCTA(monthlySavings)` in [`pkg/report/color.go`](../pkg/report/color.go)
  swaps the generic "join waitlist" for a concrete `Solo $29/mo pays back
  in N days` footer whenever we have real savings to anchor against.
- `safecut upgrade` renders a 4-tier table with `--start-trial`,
  `--book-demo`, `--partner` flags (see [`cmd/upgrade.go`](../cmd/upgrade.go)).
- `safecut partner` preview command for the MSP track.
- [`docs/pricing.html`](pricing.html) page with FAQ and Start-trial CTA.
- `README.md` has a dedicated Pricing section linking to it.
- RELEASE-CHECKLIST has a new Conversion Gate block.

Success metric: >15% CTR on the ROI SNAPSHOT block (measured via
`cta_shown` / `cta_clicked` in PostHog).

## Onda 2 — Social proof + urgency + comparison (v1.0 final)

Goal: close the gap between "interesting tool" and "I'll buy today".
Target landing window: v1.0.0 final, ~5–7 days after rc1.

1. **Founding customer counter**
   - Replace the static `47 / 100 spots remaining` in
     [`docs/index.html`](index.html) hero with a real counter fed by a
     `/api/founding-count` endpoint (static JSON is fine while signups are
     low).
   - Surface the same counter in `safecut upgrade` when terminal is
     interactive.

2. **Verified spend badge**
   - Ship an aggregate `$X M+ under management` badge driven by
     telemetry (sum of `monthly_savings_bucket` midpoints, rolling 30-day).
   - Gate it behind `SAFECUT_TELEMETRY` opt-in so we only aggregate
     consenting installs.

3. **Comparison table v2**
   - Extend the mini-table in [`docs/index.html`](index.html) with
     Infracost Cloud, Vantage, and Azure Advisor columns.
   - Add a dedicated `docs/compare.html` for SEO, one page per
     competitor (Apptio, Cloudability, Finout).

4. **Conversion telemetry review**
   - At day 7 after rc1, pull the `cta_shown` → `cta_clicked` funnel from
     PostHog and tune copy. Targets:
     - `quick_scan_roi` CTR ≥ 15%
     - `upgrade_cmd` → `start_trial` CTR ≥ 25%
     - `partner_cmd` → `partner_apply` CTR ≥ 10%

5. **First 10 paid case studies**
   - For every Solo / Team signup, schedule a 15-min async Loom follow-up
     asking for a quote. Publish on pricing.html when we hit 5.

Exit criteria for Onda 2: we can show real signups, real savings
aggregate, and at least three testimonials on pricing.html.

## Onda 3 — Partner / MSP GA (v1.1)

Goal: turn the Partner preview into a revenue line on its own.

1. **White-label PDF / HTML export**
   - Promote `safecut partner --brand --client` preview to full
     output: write PDF with brand header and hide the `Powered by
     SafeCut` footer for Partner plan.
   - Add `safecut quick-scan --brand --client --export pdf`.

2. **Per-client subscription management**
   - Ship a partner dashboard in SafeCut Cloud: one account, many
     client subscriptions, per-client billing rollup.
   - CLI command `safecut partner clients list|add|remove`.

3. **Revshare portal**
   - Stripe Connect for 20% recurring revshare payouts.
   - Public `docs/partner.html` listing benefits, tiers
     (Silver/Gold/Platinum MSP), and onboarding flow.

4. **Co-marketing kit**
   - Template LinkedIn / blog posts and a shared case-study page for
     Platinum partners.

Exit criteria for Onda 3: at least 3 active MSPs with paying clients
routed through Stripe Connect, and v1.1 is tagged with the Partner
commands documented in the main README.

## Risks that force a schedule slip

- **Checkout URLs remain fake-doors beyond Onda 1.** If Stripe isn't
  live by Onda 2, swap to Typeform capture and push Solo/Team paid
  activation to v1.0.1. Don't ship real prices without a real payment
  path.
- **Telemetry opt-in rate &lt; 20%.** Social proof numbers would be too
  thin to display; in that case we keep the founding-customer bar but
  drop the `$X M+ under management` claim until the sample is big
  enough.
- **Partner PDF pipeline slips past v1.1.** We keep the `partner`
  preview command honest (it already says "ships in v1.1") and roll the
  feature into v1.2 rather than half-shipping it.
