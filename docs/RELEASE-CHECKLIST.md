# Pre-release checklist (v1.x tags)

Run this before pushing a version tag (`v*`) so GitHub Actions can run [`.github/workflows/release.yml`](../.github/workflows/release.yml) and GoReleaser cleanly.

## Automated gates (local)

1. **Go 1.24+** aligned with [`go.mod`](../go.mod) (`go 1.24.0`).
2. `go mod verify`
3. `go vet ./...`
4. `go test ./... -count=1`
5. `go build -o inframind ./cmd/inframind`

Optional: `go test -race ./...` on packages you care about.

## Release artifact dry-run

- Install [GoReleaser](https://goreleaser.com/) v2.
- `goreleaser release --snapshot --clean` (builds binaries without publishing; validates [`.goreleaser.yaml`](../.goreleaser.yaml)).

## Live Azure smoke (read-only)

Requires `az login` or equivalent and a **non-production** subscription or resource group.

1. Build or `go run ./cmd/inframind`.
2. Run `inframind doctor` first — confirms auth chain, pricing cache, subscription resolution.
3. Optionally set `INFRAMIND_E2E=1` and run [`scripts/smoke.sh`](../scripts/smoke.sh) with `INFRAE2E_RG` pointing at a small RG.

Commands to spot-check:

- `inframind doctor`
- `inframind policy lint --resource-group <sandbox-rg>`
- `inframind quick-scan --resource-group <sandbox-rg>` (verify ROI SNAPSHOT block renders with real savings)
- `inframind quick-scan --cloud aws` (verify friendly stub)
- `inframind policy simulate --resource-group <sandbox-rg> --set criticality=high`
- `inframind apply --resource-group <sandbox-rg>` (read-only scan path only)
- `inframind upgrade` (tier table)
- `inframind upgrade --start-trial solo` (verify correct checkout URL)
- `inframind upgrade --book-demo`
- `inframind upgrade --partner`
- `inframind partner --brand "Acme" --client "Contoso"` (header preview)
- `inframind partner --apply`

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
      `INFRAMIND_POSTHOG_KEY` set locally during the smoke test.
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

- **`GITHUB_TOKEN`** — provided by Actions for the release job.
- **`HOMEBREW_TAP_TOKEN`** — required for the Homebrew tap push in [`.goreleaser.yaml`](../.goreleaser.yaml). If unset, confirm whether you still want the brews block or adjust the config.

## After the tag

- Verify release assets and checksums on GitHub.
- Smoke-install: Homebrew formula test runs `inframind --help`; optionally run a quick-scan from a test machine.
