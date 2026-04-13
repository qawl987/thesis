#!/bin/bash
# E2E Pipeline Latency Measurement Script
# 使用本機時鐘測量各階段延遲

set -e

REGIONAL_KUBECONFIG="${REGIONAL_KUBECONFIG:-/home/free5gc/regional.kubeconfig}"
INTENT_FILE="${1:-config/samples/e2eqosintent_sample.yaml}"
POLL_INTERVAL=0.5  # 輪詢間隔（秒）
TIMEOUT=120        # 超時（秒）

# 顏色輸出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

timestamp() {
    date +%s.%N
}

elapsed() {
    echo "scale=2; $1 - $2" | bc
}

echo "=========================================="
echo "E2E Pipeline Latency Measurement"
echo "=========================================="
echo "Intent file: $INTENT_FILE"
echo "Regional kubeconfig: $REGIONAL_KUBECONFIG"
echo ""

# Step 0: 記錄初始狀態
echo -e "${YELLOW}[Step 0] Recording initial state...${NC}"
INITIAL_CM_VERSION=$(KUBECONFIG=$REGIONAL_KUBECONFIG kubectl get configmap gnb-regional-du-config -n srsran-gnb -o jsonpath='{.metadata.resourceVersion}' 2>/dev/null)
echo "  Initial ConfigMap version: $INITIAL_CM_VERSION"
echo ""

# Step 1: Apply intent
echo -e "${YELLOW}[Step 1] Applying intent...${NC}"
T0=$(timestamp)
kubectl apply -f "$INTENT_FILE"
echo "  T0 (Intent applied): $(date -d @$T0 '+%H:%M:%S.%3N')"
echo ""

# Step 2: 等待 server.log 出現 "Approved package"
echo -e "${YELLOW}[Step 2] Waiting for Porch workflow completion...${NC}"
echo "  (Watching server.log for 'Approved package')"
# 記錄 T0 時的 log 行數，只檢查新增的行
INITIAL_LOG_LINES=$(wc -l < server.log 2>/dev/null || echo "0")
START=$(timestamp)
while true; do
    if [ -f server.log ]; then
        # 只檢查 T0 之後新增的 log 行
        CURRENT_LINES=$(wc -l < server.log)
        if [ "$CURRENT_LINES" -gt "$INITIAL_LOG_LINES" ]; then
            NEW_LINES=$((CURRENT_LINES - INITIAL_LOG_LINES))
            LATEST=$(tail -$NEW_LINES server.log | grep -i "Approved package" | tail -1 || true)
            if [ -n "$LATEST" ]; then
                T1=$(timestamp)
                echo -e "  ${GREEN}✓ Found: $LATEST${NC}"
                break
            fi
        fi
    fi
    
    ELAPSED=$(elapsed $(timestamp) $START)
    if (( $(echo "$ELAPSED > $TIMEOUT" | bc -l) )); then
        echo -e "  ${RED}✗ Timeout waiting for Porch workflow${NC}"
        T1=$(timestamp)
        break
    fi
    sleep $POLL_INTERVAL
done
echo "  T1 (Porch complete): $(date -d @$T1 '+%H:%M:%S.%3N')"
echo ""

# Step 3: 等待 ConfigSync 同步（監控 SrsRANCellConfig 變化）
echo -e "${YELLOW}[Step 3] Waiting for ConfigSync to sync SrsRANCellConfig...${NC}"
INITIAL_CELLCONFIG_VERSION=$(KUBECONFIG=$REGIONAL_KUBECONFIG kubectl get srsrancellconfig gnb-cell-config -n srsran-gnb -o jsonpath='{.metadata.resourceVersion}' 2>/dev/null)
echo "  Initial SrsRANCellConfig version: $INITIAL_CELLCONFIG_VERSION"
START=$(timestamp)
while true; do
    CURRENT_CELLCONFIG_VERSION=$(KUBECONFIG=$REGIONAL_KUBECONFIG kubectl get srsrancellconfig gnb-cell-config -n srsran-gnb -o jsonpath='{.metadata.resourceVersion}' 2>/dev/null)
    if [ "$CURRENT_CELLCONFIG_VERSION" != "$INITIAL_CELLCONFIG_VERSION" ]; then
        T2=$(timestamp)
        echo -e "  ${GREEN}✓ SrsRANCellConfig updated: version $CURRENT_CELLCONFIG_VERSION${NC}"
        break
    fi
    
    ELAPSED=$(elapsed $(timestamp) $START)
    if (( $(echo "$ELAPSED > $TIMEOUT" | bc -l) )); then
        echo -e "  ${RED}✗ Timeout waiting for ConfigSync${NC}"
        T2=$(timestamp)
        break
    fi
    sleep $POLL_INTERVAL
done
echo "  T2 (ConfigSync complete): $(date -d @$T2 '+%H:%M:%S.%3N')"
echo ""

# Step 4: 等待 ConfigMap 更新
echo -e "${YELLOW}[Step 4] Waiting for ConfigMap update...${NC}"
START=$(timestamp)
while true; do
    CURRENT_CM_VERSION=$(KUBECONFIG=$REGIONAL_KUBECONFIG kubectl get configmap gnb-regional-du-config -n srsran-gnb -o jsonpath='{.metadata.resourceVersion}' 2>/dev/null)
    if [ "$CURRENT_CM_VERSION" != "$INITIAL_CM_VERSION" ]; then
        T3=$(timestamp)
        echo -e "  ${GREEN}✓ ConfigMap updated: version $CURRENT_CM_VERSION${NC}"
        break
    fi
    
    ELAPSED=$(elapsed $(timestamp) $START)
    if (( $(echo "$ELAPSED > $TIMEOUT" | bc -l) )); then
        echo -e "  ${RED}✗ Timeout waiting for ConfigMap (may not have changed)${NC}"
        T3=$(timestamp)
        break
    fi
    sleep $POLL_INTERVAL
done
echo "  T3 (ConfigMap updated): $(date -d @$T3 '+%H:%M:%S.%3N')"
echo ""

# 計算結果
echo "=========================================="
echo "Results"
echo "=========================================="
D1=$(elapsed $T1 $T0)
D2=$(elapsed $T2 $T1)
D3=$(elapsed $T3 $T2)
TOTAL=$(elapsed $T3 $T0)

printf "| %-30s | %8s |\n" "Stage" "Duration"
printf "|%-32s|%10s|\n" "--------------------------------" "----------"
printf "| %-30s | %6.2fs |\n" "E2E Orchestrator + Porch" "$D1"
printf "| %-30s | %6.2fs |\n" "ConfigSync (Git → Cluster)" "$D2"
printf "| %-30s | %6.2fs |\n" "srsran-operator (ConfigMap)" "$D3"
printf "|%-32s|%10s|\n" "--------------------------------" "----------"
printf "| %-30s | %6.2fs |\n" "TOTAL" "$TOTAL"
echo ""

# 保存結果
RESULT_FILE="./exp/latency-result-$(date +%Y%m%d-%H%M%S).txt"
cat > "$RESULT_FILE" << EOF
E2E Pipeline Latency Measurement
Date: $(date)
Intent: $INTENT_FILE

T0 (Intent applied):      $(date -d @$T0 '+%H:%M:%S.%3N')
T1 (Porch complete):      $(date -d @$T1 '+%H:%M:%S.%3N')
T2 (ConfigSync complete): $(date -d @$T2 '+%H:%M:%S.%3N')
T3 (ConfigMap updated):   $(date -d @$T3 '+%H:%M:%S.%3N')

Durations:
  E2E Orchestrator + Porch:     ${D1}s
  ConfigSync (Git → Cluster):   ${D2}s
  srsran-operator (ConfigMap):  ${D3}s
  TOTAL:                        ${TOTAL}s
EOF

echo "Results saved to: $RESULT_FILE"
