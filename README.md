# InfraMind CLI

Decision engine for cloud infrastructure. Analyze, simulate, and optimize costs with safety and explainability.

## Project Structure

```
inframind-cli/
├── cmd/
│   ├── inframind/          # Main binary entry point
│   │   └── main.go
│   ├── root.go             # Cobra root command
│   ├── scan.go             # 'inframind scan' command
│   ├── simulate.go         # 'inframind simulate' command
│   └── forecast.go         # 'inframind forecast' command
├── internal/
│   ├── engine/             # Core decision and simulation logic
│   │   ├── decision.go
│   │   └── simulation.go
│   ├── graph/              # Dependency graph engine
│   │   └── mapper.go
│   ├── providers/          # Cloud provider abstraction layer
│   │   ├── azure/
│   │   └── provider.go
│   ├── forecast/           # Cost savings and ROI calculations
│   └── rules/              # Optimization rules
│       └── orphan_disk.go
├── pkg/
│   └── report/             # Output formatting (JSON, Table, ASCII)
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
./inframind scan --subscription <SUBSCRIPTION_ID>

# Simulate the impact of proposed changes
./inframind simulate --dry-run

# Forecast cost savings
./inframind forecast --months 12
```

## License

See [LICENSE](LICENSE) for details.
