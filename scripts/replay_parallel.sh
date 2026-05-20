#!/usr/bin/env bash
# Runs all 4 bot replays in parallel.
# Each provider's output is prefixed with [provider] so interleaved logs stay readable.
# One provider failing does not kill the others. Exits 1 if any failed.
#
# Uses indexed arrays (no `declare -A`) so it works on macOS's stock Bash 3.2.

set -uo pipefail

DAYS=${1:-90}
PROVIDERS=(claude gpt gemini grok)
PIDS=()

echo "→ parallel replay: ${PROVIDERS[*]} · ${DAYS} days"
echo ""

for p in "${PROVIDERS[@]}"; do
    (
        set -o pipefail
        python3 -u -m "bots.${p}_bot" --replay "$DAYS" --verbose 2>&1 \
            | awk -v tag="[$p]" '{ print tag " " $0; fflush() }'
    ) &
    PIDS+=($!)
done

FAILED=()
for i in "${!PROVIDERS[@]}"; do
    p="${PROVIDERS[$i]}"
    pid="${PIDS[$i]}"
    if wait "$pid"; then
        echo ""
        echo "[$p] ✓ done"
    else
        echo ""
        echo "[$p] ✗ failed — check output above"
        FAILED+=("$p")
    fi
done

echo ""
if [ ${#FAILED[@]} -gt 0 ]; then
    printf "✗ finished with failures: %s\n" "${FAILED[*]}"
    exit 1
fi
echo "✓ all 4 providers replayed"
