#!/usr/bin/env bash
# Optional live Azure smoke test (read-only). Does nothing destructive.
# Usage:
#   export INFRAMIND_E2E=1
#   export INFRAE2E_RG=my-sandbox-rg
#   ./scripts/smoke.sh
set -euo pipefail

if [[ "${INFRAMIND_E2E:-0}" != "1" ]]; then
  echo "Skipping: set INFRAMIND_E2E=1 to run live quick-scan (requires az login / subscription access)."
  exit 0
fi

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

RG="${INFRAE2E_RG:?set INFRAE2E_RG to a sandbox resource group name}"

echo "Running: go run ./cmd/inframind quick-scan --resource-group $RG"
go run ./cmd/inframind quick-scan --resource-group "$RG"
