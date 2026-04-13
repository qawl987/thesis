#!/bin/bash
# server command: iperf3 -s -B n6if
# client command: iperf3 --bind-dev uesimtun0 -c 10.1.195.193 -u -b 20M -t 10
# --- 設定區 ---
# 你的 UE Pod 的 Namespace
NAMESPACE="free5gc" 
# 透過 Label 篩選 UE Pod (請修改成你 helm chart 中 UE 的 label)
# 例如: app=ue, component=srsue 等等
POD_LABEL_SELECTOR="app=ue"

TARGET_IP="10.0.2.13"
START_BW=1   # 起始頻寬 (Mbps)
STEP_BW=1    # 每次增加 (Mbps)
DURATION=5   # 每次測試持續時間 (秒)
# ----------------

# 自動抓取 UE Pod 的名稱
UE_POD=$(microk8s kubectl get pods -n $NAMESPACE -l $POD_LABEL_SELECTOR -o jsonpath="{.items[0].metadata.name}")

if [ -z "$UE_POD" ]; then
    echo "錯誤: 找不到符合 Label '$POD_LABEL_SELECTOR' 的 Pod。"
    exit 1
fi

echo "目標 UE Pod: $UE_POD"
echo "目標 Server IP: $TARGET_IP"
echo "----------------------------------------"

CURRENT_BW=$START_BW

while true; do
    echo "正在執行測試: 頻寬限制 = ${CURRENT_BW} Mbps, 持續 $DURATION 秒..."
    
    # 透過 kubectl exec 讓 UE 執行 iperf3
    # -u: 使用 UDP (比較好控制頻寬)
    # -b: 設定頻寬
    # -t: 時間
    # -R: Reverse mode (如果需要測 下行 下載速度，加上 -R；如果測 上行 上傳，則不用加)
    microk8s kubectl exec -n $NAMESPACE "$UE_POD" -- iperf3 --bind-dev uesimtun0 -c $TARGET_IP -u -b ${CURRENT_BW}M -t $DURATION
    
    echo "完成 ${CURRENT_BW} Mbps 測試。"
    echo "休息 2 秒準備下一輪..."
    sleep 2
    
    # 增加頻寬
    CURRENT_BW=$((CURRENT_BW + STEP_BW))
done