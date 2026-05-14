#!/usr/bin/env bash
# Optional live Azure smoke test (read-only). Does nothing destructive.
# Usage:
#   export SAFECUT_E2E=1
#   export INFRAE2E_RG=my-sandbox-rg
#   ./scripts/smoke.sh
set -euo pipefail

if [[ "${SAFECUT_E2E:-0}" != "1" ]]; then
  echo "Skipping: set SAFECUT_E2E=1 to run live quick-scan (requires az login / subscription access)."
  exit 0
fi

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

RG="${INFRAE2E_RG:?set INFRAE2E_RG to a sandbox resource group name}"

echo "Running: go run ./cmd/safecut quick-scan --resource-group $RG"
go run ./cmd/safecut quick-scan --resource-group "$RG"
