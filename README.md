# SafeCut CLI

**The cuts you'd defend at standup — read-only, safe, explainable.**
Find idle resources, simulate changes safely, and stop paying for what you're not using.

> One command. Real savings. Nothing is modified. Ever. Runtime scales with subscription size (often under a minute; large tenants can take several minutes). Use `--resource-group` to scope one RG for faster runs.

---

## Try it in 30 seconds

**Prerequisite:** an authenticated Azure session. Either run `az login`
(simplest) or export `AZURE_SUBSCRIPTION_ID` (or `ARM_SUBSCRIPTION_ID`).
Run `safecut doctor` first if unsure — it validates the credential
chain and pricing cache before any scan.

```bash
brew install --cask Rafaelhdsg/tap/safecut
```

or

```bash
curl -fsSL https://raw.githubusercontent.com/Rafaelhdsg/safecut/main/install.sh | bash
```

Then:

```bash
az login                     # or: export AZURE_SUBSCRIPTION_ID=<id>
safecut doctor             # optional: verify environment is ready
safecut quick-scan
```

That's it. No config files, no setup scripts, no flags. 100% read-only —
nothing is ever modified on your Azure subscription.

---

## Pricing

The CLI is **free forever** for read-only scans, policy simulate, policy
lint, history, and RI / rightsize suggestions — all shipping today.
SafeCut Cloud (automation, scheduled scans, Slack alerts, white-label
reports, SSO) ships with **v1.1**. Founding customers on the waitlist
lock in today's price for the lifetime of their subscription.

| Plan            | Price                                        | Status         | Who it's for                                       |
|-----------------|----------------------------------------------|----------------|----------------------------------------------------|
| **CLI Free**    | $0 forever · 1 sub · 7-day history            | **ships today**| Individual operators exploring a single sub        |
| **Solo**        | $29/mo                                        | **v1.1 waitlist** | Freelancer / single-sub CTO — 3 subs, auto-apply |
| **Team**        | $199/mo · up to 10 seats                      | **v1.1 waitlist** | Startup / scale-up — Slack alerts, SSO           |
| **Enterprise**  | from $799/mo **or 8% of verified savings**    | **v1.1 waitlist** | Mid-market / regulated — SAML (in design), audit |
| **Partner/MSP** | 20% recurring revshare                        | **v1.1 waitlist** | MSPs / consultants managing 2+ clients           |

Full pricing and FAQ: [pricing page](https://safecut.dev/pricing.html)
or run `safecut upgrade` for the in-terminal table.

Optional:

```bash
safecut quick-scan --resource-group <name>   # faster: one resource group only
safecut quick-scan -o json --progress        # JSON on stdout; stage lines on stderr
```

### What you'll see

> This is real output from a live Azure subscription — not a mock.

<details>
<summary>Text output (click to expand)</summary>

```
  [DISCOVERY]  Scanning subscription a1b2c3d4...       Done. 96 resources found
  [PRICING]    Loading Azure Retail Prices...........   Done. 5 regions, all real-time
  [GRAPH]      Building dependency tree..............   Done. 42 linked, 54 isolated
  [ENGINE]     Analyzing 14-day telemetry signals....   Done. 38 idle detected
  [SAFETY]     Simulating blast radius...............   Done. 2 risks found
  [FORECAST]   Projecting 12-month savings...........   Done. $4,212/yr recoverable

  ─── DASHBOARD ──────────────────────────────────────────────

  +----------------------------------------------------------+
  |  SAFECUT  |  $4,212/yr waste across 3 RGs — 95% safe   |
  +----------------------------------------------------------+
  |  ANNUAL SAVINGS    $4,212 / yr                           |
  |  SAFETY SCORE      36/38 safe  [HIGH CONFIDENCE]         |
  |  AUTO-EXECUTE      28 actions ready to automate          |
  +----------------------------------------------------------+

  ─── TARGETS ────────────────────────────────────────────────

  TOP TARGETS

  1. 🖥️  self-host-agent-vm (VM)                           STOP
     ├─ SIGNALS:  Ghost VM — zero activity across all signals for 14 days
     ├─ BLAST:    Standalone VM — no attached resources depend on it
     └─ SAVING:   $70.08/mo  ($2.34/day burning idle)

  2. 🌐 12 public IPs                                 DEALLOCATE
     ├─ PATTERN:  All from NAT gateways — leftover from deployments
     ├─ BLAST:    All orphaned. No NICs reference these IPs.
     └─ SAVING:   $43.80/mo  ($1.46/day wasted)

  3. 💾 19 unattached disks                               DELETE
     ├─ PATTERN:  Disks in "Unattached" state with no VM reference
     ├─ BLAST:    All detached. No VMs depend on these disks.
     └─ SAVING:   $189.24/mo  ($6.31/day wasted)

  🔒  Full SIGNALS + BLAST analysis for 4 more targets (23 resources)
     →  SafeCut Cloud  safecut.dev

  ─── INSIGHT ────────────────────────────────────────────────

  [!] WASTE HOTSPOT DETECTED
  68% of recoverable cost is concentrated in resource group
  'lab-testing'. Apply the 'development' policy template.

  ──────────────────────────────────────────────────────────────
  NEXT  safecut policy simulate --resource-group <rg>  what-if analysis

  🔒 Full evidence report for all 96 resources, exportable
  dashboards & Slack alerts → SafeCut Cloud  safecut.dev

  ROI SNAPSHOT
  ============
  → $351.00/mo identified in safe recommendations.
  • Pay Cloud $29/mo  →  $322.00 net savings/mo (11x ROI)
  • Automate 23 recommendation(s) in one click  →  safecut upgrade --start-trial solo
  • Managing 3 resource groups / clients?  →  safecut upgrade --partner
  • Enterprise alternative: pay 8% of verified savings (greater applies).  safecut upgrade --book-demo
```

</details>

---

## Command flow

```
quick-scan  →  policy simulate  →  apply [Cloud]
 (discover)      (what-if)          (automate)
```

### `safecut quick-scan`

Zero-config instant scan. Scans 10 Azure resource types (canonical list: [`internal/defaults/defaults.go`](internal/defaults/defaults.go)) across your subscription.

```bash
safecut quick-scan
safecut quick-scan --subscription <ID>
safecut quick-scan --resource-group <name>
safecut quick-scan --export report.md
safecut quick-scan -o json
safecut quick-scan -o json --progress
```

### `safecut policy simulate`

What-if analysis: see the blast radius of a policy change before applying it.

```bash
safecut policy simulate --resource-group prod-sap --set criticality=high
safecut policy simulate --resource-group prod-sap --set mode=protect --export report.html
```

### `safecut policy lint`

Fast metadata-only validation of `safecut-*` governance tags. Runs
discovery + policy resolution but skips metrics, rules, and simulation,
so it's cheap enough to gate in CI. Flags unsupported tag values and
drift between resource-level tags and their RG / subscription parent.

```bash
safecut policy lint
safecut policy lint --resource-group prod-sap
safecut policy lint -o json
```

### `safecut history`

Prints a compact table of local scan records for a subscription (7-day
local window). Useful for smoke-testing that scans are persisting and
spotting short-term trends without re-hitting Azure.

```bash
safecut history
safecut history --subscription <ID>
safecut history -o json
```

> Long-window trends (30 / 60 / 90 days) and anomaly alerting ship with
> SafeCut Cloud.

### `safecut apply` [Cloud]

Runs the same full read-only scan as `quick-scan` and lists every
recommendation that is safe to auto-execute. **In v1.0 the CLI does
not mutate Azure** — actual execution, rollback windows, and audit
trail ship with SafeCut Cloud (Solo tier and above). The CLI
output makes the split explicit so you never confuse "listed" with
"applied".

```bash
safecut apply
safecut apply --subscription <ID>
safecut apply --resource-group <name>
```

### `safecut config`

Manage CLI settings and telemetry preferences.

```bash
safecut config --telemetry status
safecut config --telemetry disable
```

### `safecut doctor`

Read-only environment check before a scan: runtime, subscription resolution,
Azure credentials, pricing cache freshness, telemetry status.

```bash
safecut doctor
safecut doctor --subscription <ID>
```

### `safecut upgrade`

Compares SafeCut Cloud plans and jumps to the right conversion path.

```bash
safecut upgrade                       # show the pricing table
safecut upgrade --start-trial solo    # Solo $29/mo trial
safecut upgrade --start-trial team    # Team $199/mo trial
safecut upgrade --book-demo           # Enterprise (from $799/mo or 8% of savings)
safecut upgrade --partner             # MSP / consultancy track (20% revshare)
safecut upgrade --open                # open the relevant URL in your browser
```

### `safecut partner`

Previews the MSP / white-label track. Shows the partner pitch, lets you try a
`--brand` / `--client` header, and jumps to the application form with
`--apply`.

```bash
safecut partner
safecut partner --brand "Acme Consulting" --client "Contoso Ltd"
safecut partner --apply
```

### Multi-cloud (coming soon)

`quick-scan` and `apply` accept `--cloud azure|aws|gcp`. v1.0 ships Azure-first;
the engine, rules, and policy model are cloud-agnostic. Passing `--cloud aws`
or `--cloud gcp` prints the waitlist CTA — adapters land after v1.0.

---

## What SafeCut does

SafeCut correlates CPU, network, and disk signals using a weighted geometric mean to detect truly idle resources — not just "low CPU" false positives. It runs a 6-layer pipeline before recommending anything:

```
Discovery → Pricing → Dependency Graph → Decision Engine → Simulation → Forecast
```

| Step | What happens |
|------|-------------|
| **Discovery** | Collects resources and usage metrics from your cloud provider (10 types) |
| **Pricing** | Fetches real-time retail prices from Azure Retail Prices API with local cache |
| **Dependency Graph** | Maps relationships (IP → NIC → VM → Disk) to prevent breaking dependencies |
| **Decision Engine** | Applies rules, computes idle scores, respects criticality and safe-locks |
| **Simulation** | Dry-runs every recommendation against the dependency graph |
| **Forecast** | Projects monthly and yearly savings with ROI |

Every recommendation comes with an idle score, confidence level, risk classification, and a full explanation of *why*.

### Resource types scanned

VMs, Managed Disks, Public IPs, Network Interfaces, App Services, SQL Databases, Storage Accounts, Load Balancers, NAT Gateways, Container Instances.

---

## Governance Tags

SafeCut reads resource tags to understand business context. No agents, no sidecars — just tags.

### Mode (`safecut-mode`)

| Value | Analyzed? | Recommendations? | Auto-Execute? |
|-------|-----------|-------------------|---------------|
| *(no tag)* | Yes | Yes | Yes |
| `observe` | Yes | No | No |
| `protect` | Yes | Yes | **No** |
| `ignore` | No | No | No |

### Criticality (`safecut-criticality`)

| Value | Thresholds | Risk | Auto-Execute? |
|-------|-----------|------|---------------|
| `high` | 2x stricter | +1 level | **Blocked** |
| `medium` | Standard | No change | Allowed |
| `low` | 2x aggressive | No change | Allowed |

### External Dependencies (`safecut-external`)

Flag resources with dependencies outside the cloud graph (VPN, ExpressRoute, on-prem). Confidence is halved, risk increases, auto-execution blocked. If a policy simulation touches external resources, impact auto-escalates to **CRITICAL**.

### Templates (`safecut-template`)

Apply presets at scale:

| Template | Mode | Criticality | External |
|----------|------|-------------|----------|
| `production` | protect | high | — |
| `staging` | observe | medium | — |
| `development` | *(default)* | low | — |
| `legacy` | protect | high | true |

```bash
az tag update --resource-id <RG_ID> --operation merge --tags safecut-template=production
```

### Policy Inheritance

Policies resolve by walking the cloud hierarchy:

```
Resource → Resource Group → Subscription → Default
```

Each field resolves independently (first-match wins). Use `safecut-policy=override` to block inheritance on a specific resource.

---

## Architecture

```
safecut/
├── cmd/
│   ├── safecut/main.go           # Entry point
│   ├── root.go                     # Cobra root + global flags
│   ├── quick_scan.go               # safecut quick-scan
│   ├── apply.go                    # safecut apply [Cloud]
│   ├── policy.go                   # safecut policy simulate
│   └── config.go                   # safecut config
├── internal/
│   ├── discovery/                  # Resource & metrics collection
│   ├── engine/                     # Analyzer, decision, policy, safe-lock
│   ├── graph/                      # Dependency graph (IP → NIC → VM → Disk)
│   ├── simulation/                 # Dry-run with dependency safety
│   ├── forecast/                   # Cost projections & ROI
│   ├── rules/                      # Pluggable rules (idle, orphan, rightsize, RI)
│   ├── pipeline/                   # Orchestrates all layers
│   ├── providers/                  # Cloud adapters (Azure, future: AWS, GCP)
│   ├── pricing/                    # Azure Retail Prices API + local cache
│   ├── history/                    # Scan history & trend tracking
│   └── telemetry/                  # Anonymous usage analytics
├── pkg/report/                     # Output: colors, formatting, CTA
├── .goreleaser.yaml                # Cross-compile + Homebrew + release
└── install.sh                      # curl installer
```

## Install (all options)

```bash
# Homebrew
brew install --cask Rafaelhdsg/tap/safecut

# curl
curl -fsSL https://raw.githubusercontent.com/Rafaelhdsg/safecut/main/install.sh | bash

# Go
go install github.com/Rafaelhdsg/safecut/cmd/safecut@latest

# Build from source
git clone https://github.com/Rafaelhdsg/safecut.git
cd safecut
go build -o safecut ./cmd/safecut
```

## Development

- Run tests: `go test ./...`
- Pre-release checklist (CI, GoReleaser snapshot, Azure smoke): [`.github/RELEASE-CHECKLIST.md`](.github/RELEASE-CHECKLIST.md)

## License

See [LICENSE](LICENSE) for details.
