#!/bin/bash
# E2E Pipeline Latency Benchmark
# Runs multiple tests and calculates statistics

set -e

ITERATIONS=${1:-10}
INTENT_FILE=${2:-config/samples/e2eqosintent_sample.yaml}
INTERVAL=${3:-30}  # Wait between tests (seconds)

RESULTS_DIR="benchmark-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$RESULTS_DIR"

echo "=========================================="
echo "E2E Pipeline Latency Benchmark"
echo "=========================================="
echo "Iterations: $ITERATIONS"
echo "Intent: $INTENT_FILE"
echo "Interval: ${INTERVAL}s between tests"
echo "Results dir: $RESULTS_DIR"
echo ""

# Arrays to store results
declare -a D1_RESULTS D2_RESULTS D3_RESULTS TOTAL_RESULTS

for i in $(seq 1 $ITERATIONS); do
    echo "=========================================="
    echo "Test $i of $ITERATIONS"
    echo "=========================================="
    
    # Run measurement
    OUTPUT=$(./hack/measure-latency.sh "$INTENT_FILE" 2>&1)
    echo "$OUTPUT"
    
    # Extract durations from output
    D1=$(echo "$OUTPUT" | grep "E2E Orchestrator + Porch" | awk '{print $NF}' | sed 's/s$//')
    D2=$(echo "$OUTPUT" | grep "ConfigSync" | awk '{print $NF}' | sed 's/s$//')
    D3=$(echo "$OUTPUT" | grep "srsran-operator" | awk '{print $NF}' | sed 's/s$//')
    TOTAL=$(echo "$OUTPUT" | grep "TOTAL" | awk '{print $NF}' | sed 's/s$//')
    
    D1_RESULTS+=($D1)
    D2_RESULTS+=($D2)
    D3_RESULTS+=($D3)
    TOTAL_RESULTS+=($TOTAL)
    
    # Save individual result
    mv latency-result-*.txt "$RESULTS_DIR/test-$i.txt" 2>/dev/null || true
    
    if [ $i -lt $ITERATIONS ]; then
        echo ""
        echo "Waiting ${INTERVAL}s before next test..."
        sleep $INTERVAL
    fi
done

# Calculate statistics
calc_stats() {
    local name=$1
    shift
    local arr=("$@")
    local n=${#arr[@]}
    
    # Mean
    local sum=0
    for v in "${arr[@]}"; do
        sum=$(echo "$sum + $v" | bc -l)
    done
    local mean=$(echo "scale=2; $sum / $n" | bc -l)
    
    # Variance and StdDev
    local var_sum=0
    for v in "${arr[@]}"; do
        local diff=$(echo "$v - $mean" | bc -l)
        var_sum=$(echo "$var_sum + ($diff * $diff)" | bc -l)
    done
    local variance=$(echo "scale=4; $var_sum / $n" | bc -l)
    local stddev=$(echo "scale=2; sqrt($variance)" | bc -l)
    
    # Min/Max
    local min=${arr[0]}
    local max=${arr[0]}
    for v in "${arr[@]}"; do
        if (( $(echo "$v < $min" | bc -l) )); then min=$v; fi
        if (( $(echo "$v > $max" | bc -l) )); then max=$v; fi
    done
    
    printf "| %-28s | %7.2f | %7.2f | %7.2f | %7.2f |\n" "$name" "$mean" "$stddev" "$min" "$max"
}

echo ""
echo "=========================================="
echo "Benchmark Results (n=$ITERATIONS)"
echo "=========================================="
printf "| %-28s | %7s | %7s | %7s | %7s |\n" "Stage" "Mean" "StdDev" "Min" "Max"
printf "|%-30s|%9s|%9s|%9s|%9s|\n" "------------------------------" "---------" "---------" "---------" "---------"
calc_stats "E2E Orchestrator + Porch" "${D1_RESULTS[@]}"
calc_stats "ConfigSync (Git → Cluster)" "${D2_RESULTS[@]}"
calc_stats "srsran-operator (ConfigMap)" "${D3_RESULTS[@]}"
calc_stats "TOTAL" "${TOTAL_RESULTS[@]}"
echo ""

# Save summary
SUMMARY_FILE="$RESULTS_DIR/summary.txt"
{
    echo "E2E Pipeline Latency Benchmark Summary"
    echo "Date: $(date)"
    echo "Iterations: $ITERATIONS"
    echo "Intent: $INTENT_FILE"
    echo ""
    echo "Raw data (seconds):"
    echo "D1 (Orchestrator+Porch): ${D1_RESULTS[*]}"
    echo "D2 (ConfigSync):         ${D2_RESULTS[*]}"
    echo "D3 (srsran-operator):    ${D3_RESULTS[*]}"
    echo "Total:                   ${TOTAL_RESULTS[*]}"
} > "$SUMMARY_FILE"

echo "Results saved to: $RESULTS_DIR/"
echo "Summary: $SUMMARY_FILE"
