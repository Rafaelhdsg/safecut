# InfraMind CLI

Decision engine for cloud infrastructure. Analyze, simulate, and optimize costs with safety and explainability.

## Architecture

InfraMind operates as a layered pipeline:

```
Discovery → Policy Resolution (with inheritance) → Graph → Analysis → Decision → Simulation → Forecast
```

| Layer | Package | Purpose |
|-------|---------|---------|
| Discovery | `internal/discovery/` | Collects raw resources and metrics from cloud providers |
| Governance | `internal/engine/policy.go` + `safelock.go` | Resolves policies with inheritance, templates, and drift detection |
| Dependency Graph | `internal/graph/` | Maps relationships (IP → NIC → VM → Disk) |
| Analysis | `internal/engine/analyzer.go` | Computes idle scores from correlated signals |
| Decision Engine | `internal/engine/` + `internal/rules/` | Applies rules and generates recommendations |
| Simulation Engine | `internal/simulation/` | Predicts impact and checks dependency safety |
| Forecast Engine | `internal/forecast/` | Projects savings and ROI |

## Project Structure

```
inframind-cli/
├── cmd/
│   ├── inframind/              # Main binary entry point
│   │   └── main.go
│   ├── root.go                 # Cobra root command
│   ├── scan.go                 # 'inframind scan'
│   ├── simulate.go             # 'inframind simulate'
│   ├── forecast.go             # 'inframind forecast'
│   └── policy.go              # 'inframind policy simulate'
├── internal/
│   ├── discovery/              # Layer 1 — Resource & metrics collection
│   │   ├── collector.go
│   │   └── metrics.go
│   ├── graph/                  # Layer 2 — Dependency graph engine
│   │   └── mapper.go
│   ├── engine/                 # Layer 3 — Decision engine (core)
│   │   ├── analyzer.go
│   │   ├── decision.go
│   │   ├── policy.go
│   │   └── safelock.go
│   ├── simulation/             # Layer 4 — Simulation engine (differentiator)
│   │   └── simulation.go
│   ├── forecast/               # Layer 5 — Cost savings & ROI
│   │   └── forecast.go
│   │   └── policy_simulator.go
│   ├── rules/                  # Pluggable rules for the decision engine
│   │   ├── rule.go
│   │   ├── orphan_disk.go
│   │   └── idle_resource.go
│   ├── providers/              # Cloud provider adapters
│   │   ├── azure/
│   │   └── provider.go
│   ├── pipeline/               # Orchestrates all layers
│   │   ├── pipeline.go
│   │   └── policy_sim.go
│   └── store/                  # Persistence (history, future dashboard)
│       └── store.go
├── pkg/
│   └── report/                 # Output formatting (JSON, Table, ASCII)
│       ├── report.go
│       └── policy_sim.go
├── go.mod
└── README.md
```

## Getting Started

### Prerequisites

- Go 1.23+

### Build

```bash
go build -o inframind ./cmd/inframind
```

### Usage

```bash
# Scan infrastructure for optimization opportunities
inframind scan --subscription <SUBSCRIPTION_ID>

# Simulate the impact of proposed changes
inframind simulate --dry-run

# Forecast cost savings
inframind forecast --months 12

# Simulate a policy change before applying (blast radius analysis)
inframind policy simulate --resource-group prod-sap --set criticality=high
inframind policy simulate --subscription my-sub --set mode=protect --set external=true
inframind policy simulate --resource-group dev-test --set template=development
```

## Resource Governance Tags

InfraMind reads Azure resource tags to understand business context and governance constraints. Three independent tag dimensions control how each resource is treated.

### 1. Mode (`inframind-mode`)

Controls **what InfraMind is allowed to do** with the resource.

| Value | Analyzed? | Recommendations? | Auto-Execute? | Use Case |
|-------|-----------|-------------------|---------------|----------|
| *(no tag)* | Yes | Yes | Yes | Default — full pipeline |
| `observe` | Yes | No | No | Monitor without acting (new resources, evaluation period) |
| `protect` | Yes | Yes | **No** | Show recommendations, but require manual approval |
| `ignore` | No | No | No | Completely invisible to InfraMind |

Legacy tags `inframind-ignore` and `inframind-lock` with truthy values map to `ignore` mode.

### 2. Criticality (`inframind-criticality`)

Declares **how important** the resource is. Directly affects analysis behavior.

| Value | Idle Thresholds | Risk Adjustment | Auto-Execute? |
|-------|-----------------|-----------------|---------------|
| `high` | 2x stricter (needs stronger evidence) | +1 level | **Blocked** |
| `medium` | Standard | No change | Allowed |
| `low` | 2x more aggressive (easier to flag) | No change | Allowed |

Example: a `high` criticality resource with CPU at 3% won't be flagged idle because the threshold drops from 5% to 2.5%.

### 3. External Dependencies (`inframind-external`)

Flags resources with **dependencies outside the cloud provider's visibility** (VPN, ExpressRoute, on-prem integrations, third-party services).

| Effect | Impact |
|--------|--------|
| Confidence | Halved (×0.5) — the analyzer's view is incomplete |
| Risk | +1 level — unknown dependencies increase danger |
| Auto-Execute | **Blocked** — manual review required |

### 4. Policy Templates (`inframind-template`)

Named presets that apply mode + criticality + external in one tag. Scales governance across thousands of resources.

| Template | Mode | Criticality | External |
|----------|------|-------------|----------|
| `production` | protect | high | — |
| `staging` | observe | medium | — |
| `development` | *(default)* | low | — |
| `legacy` | protect | high | true |

```bash
# Apply a template to an entire resource group
az tag update --resource-id <RG_ID> --operation merge --tags inframind-template=production
```

Explicit tags on the same entity override template values.

### 5. Policy Override (`inframind-policy: override`)

Blocks inheritance from parent scopes. A resource with this tag only uses its own tags — RG and subscription tags are ignored.

```bash
az tag update --resource-id <ID> --operation merge --tags inframind-policy=override inframind-criticality=low
```

Also accepts `inframind-inherit: false`.

## Policy Inheritance

In a 10,000-resource environment, nobody tags resources one by one. InfraMind resolves policies by walking the cloud hierarchy:

```
Resource tags  →  Resource Group tags  →  Subscription tags  →  Default
  (highest)           (inherited)            (inherited)         (lowest)
```

Each field (mode, criticality, external) is resolved independently using **first-match wins**: if the resource has `criticality=low` but the RG has `criticality=high`, the resource's own value wins.

### How It Works

1. Tag the RG `prod-sap` with `inframind-criticality=high`
2. All 500 resources inside automatically inherit `criticality=high`
3. A specific resource can override with `inframind-criticality=low` if needed
4. Or use `inframind-policy=override` to block all inheritance

### Source Tracking

The report shows exactly where each policy value came from:

```
POLICY SOURCES
==============
/subscriptions/.../myVM:
  mode:           protect  ← resource_group "prod-sap"
  criticality:    high     ← template "production" (via resource_group "prod-sap")
  external:       false    ← default
```

### Drift Detection

When a resource explicitly diverges from its parent, InfraMind flags it:

```
POLICY DRIFT WARNINGS
=====================
  criticality: resource=low, but resource_group "prod-sap"=high
```

This helps catch misconfigurations and ensures governance consistency.

### Applying Tags (Azure)

```bash
# Tag a resource group (all children inherit)
az tag update --resource-id <RG_ID> --operation merge --tags inframind-template=production

# Tag a subscription (all RGs and resources inherit)
az tag update --resource-id <SUB_ID> --operation merge --tags inframind-criticality=medium

# Override on a specific resource
az tag update --resource-id <ID> --operation merge \
  --tags inframind-policy=override inframind-criticality=low
```

### Report Sections

The output shows five sections:

- **Recommendations** — actionable findings with idle score, confidence, and auto-execute status
- **Policy Notes** — explains why auto-execution is blocked for specific resources
- **Signal Breakdown** — per-signal idle analysis (CPU, network, disk)
- **Policy Sources** — shows inheritance chain for each resolved policy
- **Drift Warnings** — flags resources that diverge from their parent's policy
- **Observed** — observe-mode resources with their scores (no action taken)
- **Ignored** — ignore-mode resources excluded from all analysis

## License

See [LICENSE](LICENSE) for details.
