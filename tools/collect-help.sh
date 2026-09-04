#!/usr/bin/env bash
# Collects a corpus of raw `az ... --help` outputs into testdata/help/.
# The metadata parser is written and tested against these fixtures, never
# against assumptions about the format (spec section 3.1).
#
# Usage: tools/collect-help.sh [extra az command path ...]
#
# Re-running overwrites existing fixtures. Failed invocations are reported
# at the end and do not abort the collection.

set -u

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="$ROOT/testdata/help"
mkdir -p "$OUT_DIR"

# Curated corpus. Criteria (spec sections 3.1 and 11.1):
#   - the commands named explicitly in the spec
#   - ~5 commands per large service (group, storage, vm, network, aks,
#     keyvault, monitor, webapp, sql, cosmosdb, acr, role/ad)
#   - at least one command from an installed extension
#   - group pages (no executable command) for navigation parsing
#   - format edge cases: positional args (rest), repeatable --parameters
#     (deployment group create), deprecated commands (ad sp create-for-rbac)
COMMANDS=(
    # --- explicitly required by the spec ---
    "group create"
    "storage account create"
    "vm create"
    "aks create"
    "network vnet create"
    "keyvault create"
    "login"

    # --- group pages (navigation) ---
    "storage account"
    "vm"
    "network"
    "aks"
    "keyvault"

    # --- resource groups ---
    "group list"
    "group show"
    "group delete"
    "group update"

    # --- storage ---
    "storage account list"
    "storage account show"
    "storage account update"
    "storage account delete"
    "storage account keys list"
    "storage account check-name"
    "storage container create"
    "storage blob upload"

    # --- compute ---
    "vm list"
    "vm show"
    "vm delete"
    "vm start"
    "vm stop"

    # --- network ---
    "network vnet list"
    "network vnet subnet create"
    "network nsg create"
    "network public-ip create"
    "network lb create"
    "network application-gateway create"

    # --- aks ---
    "aks scale"
    "aks upgrade"
    "aks show"
    "aks list"

    # --- keyvault ---
    "keyvault list"
    "keyvault show"
    "keyvault secret set"

    # --- monitor ---
    "monitor metrics alert create"
    "monitor log-analytics workspace create"

    # --- web ---
    "webapp create"
    "appservice plan create"
    "functionapp create"

    # --- databases ---
    "sql server create"
    "sql db create"
    "cosmosdb create"
    "redis create"
    "postgres flexible-server create"

    # --- registry / identity / containers ---
    "acr create"
    "acr login"
    "role assignment create"
    "ad app create"
    "ad sp create-for-rbac"
    "container create"

    # --- messaging ---
    "eventhubs namespace create"
    "servicebus queue create"

    # --- edge cases ---
    "rest"
    "configure"
    "deployment group create"
    "account list-locations"
    "account show"

    # --- extension commands (bastion is installed on this machine) ---
    "network bastion create"
    "network bastion ssh"
    "network bastion tunnel"
)

FAILED=()

collect() {
    local cmd="$1"
    local file
    file="$(echo "$cmd" | tr ' ' '-').txt"
    printf 'collecting: az %s --help\n' "$cmd"
    # Word-splitting of $cmd is intentional: each entry is a space-separated
    # command path.
    if ! az $cmd --help --only-show-errors >"$OUT_DIR/$file" 2>/dev/null; then
        rm -f "$OUT_DIR/$file"
        FAILED+=("$cmd")
    fi
}

for cmd in "${COMMANDS[@]}" "$@"; do
    [ -n "$cmd" ] && collect "$cmd"
done

az version >"$OUT_DIR/az-version.txt" 2>/dev/null || FAILED+=("version")

echo
echo "collected: $(find "$OUT_DIR" -name '*.txt' ! -name 'az-version.txt' | wc -l | tr -d ' ') fixtures -> $OUT_DIR"
if [ "${#FAILED[@]}" -gt 0 ]; then
    echo "FAILED (${#FAILED[@]}):"
    printf '  az %s\n' "${FAILED[@]}"
fi
