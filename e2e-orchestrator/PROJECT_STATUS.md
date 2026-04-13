# E2E Intent Orchestrator - Project Status

## 專案概述

本專案實作了一個基於 Kubernetes Operator 的 **E2E Intent Orchestrator**，用於 6G 網路切片的意圖驅動管理。遵循 **3GPP TS 28.312** Intent-driven Management 規範，實現從高階 QoS 意圖到底層網路配置的端到端閉環控制。

### 實驗名稱
**"E2E Intent-driven Network Slicing Configuration Pipeline"**

---

## 架構圖

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          Management Cluster                                  │
│  ┌──────────────┐    ┌───────────────────┐    ┌──────────────────────────┐ │
│  │   rApp/CLI   │───▶│  E2EQoSIntent CR  │───▶│  E2E Intent Orchestrator │ │
│  │ (kubectl)    │    │  (CRD)            │    │  (Operator)              │ │
│  └──────────────┘    └───────────────────┘    └───────────┬──────────────┘ │
│                                                           │                 │
│                      ┌────────────────────────────────────┼─────────────┐   │
│                      │            Nephio Porch            │             │   │
│                      │  ┌─────────┐  ┌─────────┐  ┌──────▼────────┐    │   │
│                      │  │  copy   │─▶│  mutate │─▶│ propose/approve│    │   │
│                      │  └─────────┘  └─────────┘  └───────────────┘    │   │
│                      └─────────────────────────────────────────────────┘   │
│                                          │                                  │
│                                          ▼                                  │
│                                   ┌─────────────┐                          │
│                                   │  Git Repo   │                          │
│                                   │ (regional)  │                          │
│                                   └──────┬──────┘                          │
└──────────────────────────────────────────┼──────────────────────────────────┘
                                           │ Config Sync
                                           ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│                           Worker Cluster (regional)                          │
│  ┌────────────────────┐    ┌────────────────────┐    ┌──────────────────┐   │
│  │ SrsRANCellConfig   │───▶│  srsran-operator   │───▶│  DU ConfigMap    │   │
│  │ (CR with slicing)  │    │  (Reconcile)       │    │  (gnb-config.yml)│   │
│  └────────────────────┘    └────────────────────┘    └──────────────────┘   │
└──────────────────────────────────────────────────────────────────────────────┘
```

---

## 已完成功能 ✅

### 1. CRD 定義 (`E2EQoSIntent`)

- **Spec 結構**：支援多個 `intentGroups`，每個包含：
  - `id`: 意圖群組識別碼 (e.g., "embb", "urllc")
  - `contexts.targetUEs`: 目標 UE 的 IMSI/SUPI 列表
  - `expectations`: QoS 期望參數
    - `sliceType`: eMBB / URLLC / MIoT
    - `latency`: ultra-low / low / medium (URLLC 用)
    - `bandwidth`: dedicated-high / high / standard (eMBB 用)
    - `resourceShare`: Full / Partial

- **Status 結構** (3GPP TS 28.312 閉環控制)：
  - `phase`: Processing / Applied / Failed
  - `fulfillmentState`: NOT_FULFILLED / PARTIALLY_FULFILLED / FULFILLED / DEGRADED
  - `observedGeneration`: 追蹤 spec 變更
  - `intentGroupStatuses[]`:
    - `translatedParams`: 翻譯後的 Core/RAN 參數
    - `achievedTargets`: latency/bandwidth/resourceShare 達成狀態
    - `domainStatus`: Core/RAN 域配置狀態

### 2. Intent Translation Engine

將高階意圖翻譯為底層參數：

| Slice Type | 意圖參數 | 5QI | SST | SD | Priority |
|------------|----------|-----|-----|---------|----------|
| URLLC | latency: ultra-low | 85 | 1 | 1122867 | 200 |
| URLLC | latency: low | 82 | 1 | 1122867 | 200 |
| URLLC | latency: medium | 84 | 1 | 1122867 | 200 |
| eMBB | bandwidth: dedicated-high | 4 | 1 | 66051 | 10 |
| eMBB | bandwidth: high | 6 | 1 | 66051 | 10 |
| eMBB | bandwidth: standard | 9 | 1 | 66051 | 10 |

| resourceShare | maxPrbPolicyRatio |
|---------------|-------------------|
| Full | 100 |
| Partial | 50 |

### 3. Core Domain - free5GC UE 註冊

透過 free5GC WebConsole REST API 自動註冊 UE：

**實作內容** (`internal/controller/free5gc_client.go`)：
- `Login()`: 透過 POST /api/login 取得 JWT Token
- `RegisterSubscriber()`: 透過 POST /api/subscriber/{ueId}/{plmnId} 註冊 UE
- `UpdateSubscriberQoS()`: 透過 PUT /api/subscriber/{ueId}/{plmnId} 更新 QoS
- `DeleteSubscriber()`: 透過 DELETE /api/subscriber/{ueId}/{plmnId} 刪除 UE

**預設 UE 配置**：
- PLMN ID: `20893`
- DNN: `internet`
- PDU Session Type: IPv4
- SSC Mode: SSC_MODE_1
- 認證方式: 5G_AKA (使用標準測試金鑰)

**5QI 映射**：根據 intent 中的 sliceType 和 latency/bandwidth 自動設定 5QI
- 預設 5QI=9，根據 intent 動態調整

**啟動參數**：
```bash
--free5gc-url http://localhost:5000    # WebConsole URL
--free5gc-username admin               # 登入帳號
--free5gc-password free5gc             # 登入密碼
```

### 4. RAN Domain - Nephio Porch GitOps Workflow

完整實作 Porch 工作流程：
```
copy → pull → mutate → push → propose → approve
```

- 使用 `porchctl` CLI 操作 PackageRevisions
- 修改 `srscellconfig.yaml` 中的 `slicing[]` 配置
- 支援**多個 slice 同時配置**（單次 Porch workflow 處理所有 intentGroups）
- 自動生成 workspace 名稱：`intent-YYYYMMDD-HHMMSS`

### 5. Spec Change Detection

- 使用 `metadata.generation` vs `status.observedGeneration` 比較
- Intent spec 修改後自動重新處理
- 避免重複處理已完成的 intent

### 6. 3GPP TS 28.312 Closed-loop Status Reporting

rApp 可透過 K8s Watch API 監聽 status 變化：
```yaml
status:
  fulfillmentState: FULFILLED
  intentGroupStatuses:
    - id: "embb"
      fulfillmentState: FULFILLED
      achievedTargets:
        bandwidth: achieved
        resourceShare: achieved
      domainStatus:
        coreDomain:
          state: CONFIGURED
          message: "UEs registered with 5QI=6"
        ranDomain:
          state: CONFIGURED
          message: "Slice configured: SST=1, SD=66051, maxPRB=50"
```

---

## 未完成功能 ❌

### 1. 真實 QoS 監控 (True Closed-loop)

**目前狀態**：`achievedTargets` 在配置成功後直接標記為 `achieved`

**預計實作**：
- 整合 Prometheus/監控系統
- 定期檢查實際 latency/throughput
- 若未達標則更新 `fulfillmentState` 為 `DEGRADED`

### 2. Porch Go SDK Migration

**目前狀態**：使用 `porchctl` CLI (exec.Command)

**預計實作**：
- 改用 Porch Go SDK / 直接操作 PackageRevision CR
- 減少外部依賴，提升效能

---

## 快速開始

### 前置需求

- Kubernetes cluster (Nephio management cluster)
- Nephio Porch 已部署
- `porchctl` CLI 可用
- regional workload cluster 已連接 (Config Sync)
- free5GC WebConsole 運行中 (若需自動 UE 註冊)

### 執行步驟

```bash
cd /home/free5gc/e2e-orchestrator

# 1. 安裝 CRD 到 cluster
make install

# 2. 啟動 controller (終端 1)
# 不啟用 free5GC UE 註冊
make run

# 或者，啟用 free5GC UE 註冊 (使用 regional cluster 的 NodePort)
# WebConsole NodePort: webui-service 在 free5gc-cp namespace, port 30500
go run ./cmd/main.go \
  --free5gc-url http://172.18.0.4:30500 \
  --free5gc-username admin \
  --free5gc-password free5gc

# 3. 套用測試 Intent (終端 2)
make apply-sample

# 4. 查看 Intent 狀態
kubectl get e2eqosintent slices-intent -o yaml
```

### 驗證結果

```bash
# 檢查 Porch package
kubectl get packagerevisions -n default | grep intent

# 檢查 worker cluster 的 SrsRANCellConfig
KUBECONFIG=/home/free5gc/regional.kubeconfig \
  kubectl get srsrancellconfig -n srsran-gnb -o yaml

# 檢查 DU ConfigMap
KUBECONFIG=/home/free5gc/regional.kubeconfig \
  kubectl get configmap gnb-regional-du-config -n srsran-gnb -o yaml

# 檢查 free5GC WebConsole 中的 UE 註冊 (若已啟用)
curl -X POST http://localhost:5000/api/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"free5gc"}'
# 使用回傳的 token 查詢 subscribers
curl http://localhost:5000/api/subscriber -H "Token: <access_token>"
```

---

## E2E Pipeline 延遲分析

### 理想情況（無 ConfigSync wait 阻塞）

| 階段 | 耗時 | 說明 |
|------|------|------|
| E2E Orchestrator 翻譯 | < 1 sec | Intent → RAN/Core 配置 |
| Porch workflow | ~7 sec | Clone → Edit → Propose → Approve |
| ConfigSync 同步 | ~15-20 sec | Git poll (15s) + apply |
| srsran-operator | < 0.1 sec | ConfigMap 生成 |
| **總計** | **~25-30 sec** | |

### ✅ 已解決：NFDeployment Status 導致 ConfigSync Wait Timeout

**問題描述**：

ConfigSync 使用 **kstatus** 來判斷資源是否達到 "Current" 狀態。kstatus 會檢查：
1. `status.conditions` 中是否有 `Ready` condition 且 `status: True`
2. `status.observedGeneration` 是否等於 `metadata.generation`

原本 4 個 NFDeployment 資源都有問題，導致 ConfigSync 每次同步都要等待 **2 分鐘** timeout：

1. `free5gc-cp/NFDeployment/amf-regional`
2. `free5gc-cp/NFDeployment/smf-regional`
3. `free5gc-upf/NFDeployment/upf-regional`
4. `srsran-gnb/NFDeployment/gnb-regional`

**根本原因**：

1. **free5gc-operator**：使用底層 Deployment 的 `generation` 而非 NFDeployment 的 `generation` 來設定 `observedGeneration`
   ```
   amf-regional: gen=2, observedGeneration=3 (不匹配)
   smf-regional: gen=1, observedGeneration=2238 (不匹配)
   upf-regional: gen=3, observedGeneration=2242 (不匹配)
   ```

2. **srsran-operator**：沒有更新 `observedGeneration`，導致值始終為 0
   ```
   gnb-regional: gen=1, observedGeneration=0 (不匹配)
   ```

**解決方案（已實作）**：

1. **free5gc-operator** (`controllers/nf/*/status.go`)：
   - 修改 `createNfDeploymentStatus()` 使用 `nfDeployment.Generation` 而非 `deployment.Generation`
   - 在 deployment Available 時同時加入 `Ready` condition

2. **srsran-operator** (`internal/controller/randeployment_controller.go`)：
   - 修改 `updateStatusIfRequired()` 設定 `nfDeploy.Status.ObservedGeneration = int32(nfDeploy.Generation)`
   - 加入 `Ready` condition

**修復後狀態**：
```
free5gc-cp/amf-regional: gen=2, observedGeneration=2 ✓, Ready=True
free5gc-cp/smf-regional: gen=1, observedGeneration=1 ✓, Ready=True
free5gc-upf/upf-regional: gen=3, observedGeneration=3 ✓, Ready=True
srsran-gnb/gnb-regional: gen=1, observedGeneration=1 ✓, Ready=True
```

**效能改善**：

| 測試 | ConfigSync 時間 | 總時間 |
|------|----------------|--------|
| 修復前（timeout） | 120s | 249s |
| 修復後 | ~20-27s | ~30-36s |

### 時間戳檢查命令

```bash
# Step 1-2: E2E Orchestrator log
grep 'Spec changed\|Approved package' server.log

# Step 3: Config Sync 同步狀態
KUBECONFIG=/home/free5gc/regional.kubeconfig \
  kubectl get rootsync regional -n config-management-system \
  -o jsonpath='{.status.sync.lastUpdate}'

# Step 4: ConfigMap 版本
KUBECONFIG=/home/free5gc/regional.kubeconfig \
  kubectl get configmap gnb-regional-du-config -n srsran-gnb \
  -o jsonpath='{.metadata.resourceVersion}'

# 檢查 ConfigSync wait timeout 資源
KUBECONFIG=/home/free5gc/regional.kubeconfig \
  kubectl logs -n config-management-system deploy/root-reconciler-regional \
  --tail=100 | grep "Timeout"
```

---

## Operator 待修復問題

### 1. srsran-operator: SrsRANCellConfig Watch 問題（已修復）

**問題**：`findNFDeploymentsForConfig` 使用 `obj.GetObjectKind().GroupVersionKind().Kind` 取得資源類型，但 controller-runtime 從 cache 讀取的對象不會自動設置 GVK，導致 watch handler 無法正確觸發 reconcile。

**修復**：使用 Go type switch 判斷對象類型（已在 2026-04-02 修復）。

### 2. NFDeployment Status 導致 ConfigSync Timeout（已修復）

**問題**：srsran-operator 和 free5gc-operator reconcile NFDeployment 後 status 設定不正確：
- `observedGeneration` 值錯誤（free5gc 使用底層 Deployment 的 generation，srsran 不更新）
- 缺少 `Ready` condition

**修復**（2026-04-03）：
- **srsran-operator**：`updateStatusIfRequired()` 加入 `ObservedGeneration` 設定和 `Ready` condition
- **free5gc-operator**：`createNfDeploymentStatus()` 改用 `nfDeployment.Generation`，加入 `Ready` condition

**修改的檔案**：
- `/home/free5gc/srsran-operator/internal/controller/randeployment_controller.go`
- `/home/free5gc/free5gc/controllers/nf/amf/status.go`
- `/home/free5gc/free5gc/controllers/nf/smf/status.go`
- `/home/free5gc/free5gc/controllers/nf/upf/status.go`

---

## 檔案結構

```
e2e-orchestrator/
├── api/v1alpha1/
│   ├── e2eqosintent_types.go     # CRD struct 定義
│   ├── groupversion_info.go      # API group 註冊
│   └── zz_generated.deepcopy.go  # 自動生成
├── cmd/
│   └── main.go                   # Manager 入口
├── config/
│   ├── crd/bases/                # 生成的 CRD YAML
│   ├── rbac/                     # RBAC 設定
│   └── samples/
│       └── e2eqosintent_sample.yaml  # 測試用 CR
├── internal/controller/
│   ├── e2eqosintent_controller.go    # 主要 Reconcile 邏輯
│   ├── porch_client.go               # Porch workflow 實作
│   └── free5gc_client.go             # free5GC client (placeholder)
├── Makefile
├── Dockerfile
└── README.md
```

---

## 相關規範

- **3GPP TS 28.312**: Intent-driven Management Services for Mobile Networks
- **3GPP TS 28.541**: 5G NRM (Network Resource Model)
- **O-RAN WG2**: Near-RT RIC and xApp Architecture

---

## 版本資訊

- **API Version**: `e2e.intent.domain/v1alpha1`
- **Go Version**: 1.22+
- **controller-runtime**: v0.18.5
- **Nephio Porch**: v2.0+

---

## 貢獻者

- E2E Orchestrator 由 Nephio 社群協作開發
- 遵循 Apache License 2.0
