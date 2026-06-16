# Pre-release checklist (v1.x tags)

Run this before pushing a version tag (`v*`) so GitHub Actions can run [`workflows/release.yml`](workflows/release.yml) and GoReleaser cleanly.

## Automated gates (local)

1. **Go 1.24+** aligned with [`go.mod`](../go.mod) (`go 1.24.0`).
2. `go mod verify`
3. `go vet ./...`
4. `go test ./... -count=1`
5. `go build -o safecut ./cmd/safecut`

Optional: `go test -race ./...` on packages you care about.

## Release artifact dry-run

- Install [GoReleaser](https://goreleaser.com/) v2.
- `goreleaser release --snapshot --clean` (builds binaries without publishing; validates [`.goreleaser.yaml`](../.goreleaser.yaml)).

## Live Azure smoke (read-only)

Requires `az login` or equivalent and a **non-production** subscription or resource group.

1. Build or `go run ./cmd/safecut`.
2. Run `safecut doctor` first — confirms auth chain, pricing cache, subscription resolution.
3. Optionally set `SAFECUT_E2E=1` and run [`scripts/smoke.sh`](../scripts/smoke.sh) with `INFRAE2E_RG` pointing at a small RG.

Commands to spot-check:

- `safecut doctor`
- `safecut policy lint --resource-group <sandbox-rg>`
- `safecut quick-scan --resource-group <sandbox-rg>` (verify ROI SNAPSHOT block renders with real savings)
- `safecut quick-scan --cloud aws` (verify friendly stub)
- `safecut policy simulate --resource-group <sandbox-rg> --set criticality=high`
- `safecut apply --resource-group <sandbox-rg>` (read-only scan path only)
- `safecut upgrade` (tier table)
- `safecut upgrade --start-trial solo` (verify correct checkout URL)
- `safecut upgrade --book-demo`
- `safecut upgrade --partner`
- `safecut partner --brand "Acme" --client "Contoso"` (header preview)
- `safecut partner --apply`

## Conversion gate (new)

Before cutting `v1.0.0` final, confirm the freemium conversion path is
real and observable, not just printed text:

- [ ] `internal/pricing_tiers/tiers.go` URLs resolve (HTTP 200 or known
      pre-sell Typeform / landing page — not 404s).
- [ ] Solo / Team checkout URLs accept at least one successful
      test transaction **or** capture a verified lead (Stripe test mode or
      Typeform submission).
- [ ] `cta_shown` and `cta_clicked` events are visible in PostHog for
      `quick_scan_roi`, `upgrade_cmd`, and `partner_cmd` contexts with
      `SAFECUT_POSTHOG_KEY` set locally during the smoke test.
- [ ] `docs/pricing.html` FAQ and CTAs render without layout breakage on
      a 360px-wide viewport.
- [ ] Hero comparison table in `docs/index.html` is up to date (Azure
      Advisor / Cloudability feature rows).

## v1.0.0-rc1 focus

- [ ] `go vet ./...` clean
- [ ] `go test ./... -race -count=1` clean
- [ ] Record new demo (`demo.tape` → `demo.svg`) showcasing SAFETY line + ROI SNAPSHOT
- [ ] Update `README.md` headline screenshot if demo changed
- [ ] Validate `docs/index.html` (multi-cloud cards + waitlist form + founding-customer bar)
- [ ] Validate `docs/pricing.html` (four tiers, FAQ, Start trial)
- [ ] Publish tag `v1.0.0-rc1`, collect feedback for 5–7 days
- [ ] Review `cta_shown` / `cta_clicked` funnel in PostHog — tune CTA copy if CTR &lt; 15%
- [ ] Promote to `v1.0.0` after rc1 is stable **and** the conversion gate above is green

## GitHub secrets

- **`GITHUB_TOKEN`** — provided by Actions for the release job. Can
  write to `Rafaelhdsg/safecut` only.
- **`SAFECUT_POSTHOG_KEY`** — **required** for release telemetry.
  PostHog **Project API Key** (`phc_…`). GoReleaser embeds it into
  release binaries via `-ldflags`; without it, end-user installs send no
  events. Override locally with the same env var when testing a dev build.
- **`HOMEBREW_TAP_TOKEN`** — **required** for the Homebrew tap push.
  - Must be a dedicated PAT (fine-grained or classic), NOT the default
    `GITHUB_TOKEN` (which has no access to other repositories).
  - Fine-grained: resource owner `Rafaelhdsg`, repository access limited
    to `Rafaelhdsg/homebrew-tap`, permissions `Contents: Read and write`.
  - Classic: scope `public_repo` is enough for a public tap.
  - Symptom if the secret is missing / expired / under-scoped: the
    release step fails at the very end with
    `homebrew formula: could not get default branch: GET
    https://api.github.com/repos/Rafaelhdsg/homebrew-tap: 401 Bad
    credentials`. The GitHub release and checksums are still published
    (install.sh keeps working), only the tap is out of date.
  - Recovery: fix the PAT, re-run failed jobs from the Actions UI — the
    tag is preserved, GoReleaser will refresh the formula idempotently.

## Homebrew tap repository

Before the first release, create `Rafaelhdsg/homebrew-tap`:

- Must be **public** (private taps require extra `brew tap` flags).
- Name must be exactly `homebrew-<something>` (Homebrew convention).
- Seed it with a README so the default branch exists; GoReleaser writes
  `Formula/safecut.rb` automatically on each release.

## After the tag

- Verify release assets and checksums on GitHub.
- Smoke-install: Homebrew formula test runs `safecut --help`; optionally run a quick-scan from a test machine.
