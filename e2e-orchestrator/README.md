# E2E Intent Orchestrator

基於 Kubernetes Operator 模式實作的 6G Intent-driven 端到端網路切片編排器。

## 概述

E2E Orchestrator 是一個 Kubernetes Controller，負責監聽 `E2EQoSIntent` CR，將高階的 SLA 意圖轉譯為領域特定參數，並透過混合南向介面協調底層的 RAN 和 Core 網路域。

### 架構圖

```
┌─────────────────────────────────────────────────────────────────┐
│                         rApp Layer                              │
│                  (Submit E2EQoSIntent CR)                       │
└─────────────────────────┬───────────────────────────────────────┘
                          │ kubectl apply
                          ▼
┌─────────────────────────────────────────────────────────────────┐
│                    Kubernetes API Server                        │
│                   (E2EQoSIntent CRD)                            │
└─────────────────────────┬───────────────────────────────────────┘
                          │ Watch
                          ▼
┌─────────────────────────────────────────────────────────────────┐
│                   E2E Intent Orchestrator                       │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │              E2EQoSIntentReconciler                      │   │
│  │  1. Fetch Intent                                         │   │
│  │  2. Translate (sliceType/latency/bandwidth → 5QI/PRB)    │   │
│  │  3. Apply to Core Domain (free5GC REST API)              │   │
│  │  4. Apply to RAN Domain (Nephio Porch Workflow)          │   │
│  └──────────────────────────────────────────────────────────┘   │
└───────────────┬─────────────────────────────┬───────────────────┘
                │                             │
    ┌───────────▼───────────┐     ┌───────────▼───────────┐
    │   Core Domain         │     │   RAN Domain          │
    │   (free5GC)           │     │   (srsRAN via Porch)  │
    │                       │     │                       │
    │ • UE Registration     │     │ • SrsRANCellConfig    │
    │ • 5QI/QFI Config      │     │ • PRB Ratio           │
    │ • Session Management  │     │ • Slice Priority      │
    └───────────────────────┘     └───────────────────────┘
```

## 功能特性

### 1. E2EQoSIntent CRD

定義高階端到端 QoS 意圖的自訂資源，支援多個意圖群組：

```yaml
apiVersion: e2e.intent.domain/v1alpha1
kind: E2EQoSIntent
metadata:
  name: slices-intent
spec:
  intentGroups:
    - id: "embb"
      contexts:
        targetUEs: ["208930000000001"]
      expectations:
        sliceType: "eMBB"
        bandwidth: "high"
        resourceShare: "Partial"
    - id: "urllc"
      contexts:
        targetUEs: ["208930000000002"]
      expectations:
        sliceType: "URLLC"
        latency: "ultra-low"
        resourceShare: "Full"
```

### 2. Intent 轉譯引擎

使用 switch/case 進行簡單明確的意圖轉譯：

#### Core Domain (5QI 映射)

| SliceType | 參數 | 值 | 5QI |
|-----------|------|-----|-----|
| URLLC | latency: ultra-low | → | 85 |
| URLLC | latency: low | → | 82 |
| URLLC | latency: medium | → | 84 |
| eMBB | bandwidth: dedicated-high | → | 4 |
| eMBB | bandwidth: high | → | 6 |
| eMBB | bandwidth: standard | → | 9 |

#### RAN Domain (PRB 與優先權映射)

| SliceType | SST | SD (十進位) | SD (十六進位) | Priority |
|-----------|-----|-------------|---------------|----------|
| URLLC | 1 | 1122867 | 0x112233 | 200 |
| eMBB | 1 | 66051 | 0x010203 | 10 |

| ResourceShare | minPrbPolicyRatio | maxPrbPolicyRatio |
|---------------|-------------------|-------------------|
| Full | 0 | 100 |
| Partial | 0 | 50 |

### 3. Core Domain 南向介面 (Hybrid REST)

提供 `free5gc_client.go` 骨架程式碼，包含：

- `registerUEToFree5GCWebConsole(ue, qfi)` - UE 註冊佔位函數
- `Free5GCClient` struct - WebConsole REST API 客戶端骨架
- 詳細的 TODO 註解說明預期的 API payload 格式

```go
// 預期實作的 REST API payload 結構
{
  "plmnID": "20893",
  "ueId": "imsi-208930000000001",
  "SessionManagementSubscriptionData": [{
    "singleNssai": {"sst": 1, "sd": "010203"},
    "dnnConfigurations": {
      "internet": {
        "5gQosProfile": {
          "5qi": <qfi>,
          "arp": {"priorityLevel": 8}
        }
      }
    }
  }]
}
```

### 4. RAN Domain 南向介面 (Nephio Porch Workflow)

完整實作 Nephio Porch 工作流程：

```
┌─────────────────────────────────────────────────────────────┐
│                  Porch Workflow                             │
│                                                             │
│  1. Copy     ─→  porchctl rpkg copy (建立 Draft)           │
│  2. Pull     ─→  porchctl rpkg pull (拉取到本地)           │
│  3. Mutate   ─→  修改 srscellconfig.yaml 中的 slicing      │
│  4. Push     ─→  porchctl rpkg push (推送變更)             │
│  5. Propose  ─→  porchctl rpkg propose (提議發佈)          │
│  6. Approve  ─→  porchctl rpkg approve (批准發佈)          │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

`porch_client.go` 包含：

- `PorchClient` struct - Porch CLI 包裝器
- `UpdateRANSliceConfig()` - 完整的工作流程實作
- `mutateSliceConfig()` - YAML 解析與修改邏輯
- `PorchAPIClient` struct - 未來使用 K8s API 直接操作的骨架

### 5. Status 回報

Controller 會更新 CR 的 status 欄位，包含：

- 整體處理階段 (Pending/Processing/Applied/Failed)
- 每個 intentGroup 的處理狀態
- 轉譯後的參數值 (用於除錯和驗證)
- 最後 Reconcile 時間戳

```yaml
status:
  phase: Applied
  lastReconcileTime: "2024-03-31T05:30:00Z"
  conditions:
    - type: Ready
      status: "True"
      reason: AllIntentsApplied
  intentGroupStatuses:
    - id: embb
      phase: Applied
      translatedParams:
        coreParams:
          fiveQI: 6
          qfi: 6
        ranParams:
          sst: 1
          sd: 66051
          minPrbPolicyRatio: 0
          maxPrbPolicyRatio: 50
          priority: 10
```

## 專案結構

```
e2e-orchestrator/
├── api/
│   └── v1alpha1/
│       ├── groupversion_info.go      # API Group 定義
│       ├── e2eqosintent_types.go     # CRD struct 定義
│       └── zz_generated.deepcopy.go  # 自動產生的 DeepCopy
├── cmd/
│   └── main.go                       # Manager 進入點
├── internal/
│   └── controller/
│       ├── e2eqosintent_controller.go  # 主 Reconcile 邏輯
│       ├── porch_client.go             # Nephio Porch 工作流程
│       └── free5gc_client.go           # free5GC REST API 佔位
├── config/
│   ├── crd/
│   │   └── bases/
│   │       └── e2e.intent.domain_e2eqosintents.yaml  # 產生的 CRD
│   ├── rbac/
│   │   ├── role.yaml
│   │   ├── role_binding.yaml
│   │   └── service_account.yaml
│   ├── manager/
│   │   └── manager.yaml              # Deployment manifest
│   └── samples/
│       └── e2eqosintent_sample.yaml  # 範例 CR
├── hack/
│   └── boilerplate.go.txt
├── Dockerfile
├── Makefile
├── go.mod
└── go.sum
```

## 使用方式

### 本地開發

```bash
# 安裝 CRD 到 cluster
make install

# 本地執行 controller
make run

# 套用範例 Intent
make apply-sample

# 查看 Intent 狀態
kubectl get e2eqosintent slices-intent -o yaml
```

### 部署到 Cluster

```bash
# 建置 Docker image
make docker-build

# 推送到 registry
make docker-push

# 部署 controller
make deploy
```

### 命令列參數

```bash
./manager \
  --porch-namespace=default \
  --porch-published-package=regional.srsran-gnb.packagevariant-1 \
  --leader-elect=false
```

## 相依性

- Go 1.23+
- Kubernetes 1.30+
- controller-runtime v0.18.5
- Nephio Porch (用於 RAN domain 編排)
- porchctl CLI (目前使用 CLI，未來可改為直接 API)

## 待實作項目

1. **free5GC WebConsole REST API 整合**
   - 實作實際的 HTTP client 邏輯
   - 處理認證和錯誤回應

2. **Porch Go SDK 整合**
   - 將 porchctl CLI 呼叫改為直接使用 Kubernetes API
   - 操作 PackageRevision/PackageRevisionResources CR

3. **ConfigSync 同步等待**
   - 等待 ConfigSync 將變更同步到 workload cluster
   - 驗證 ConfigMap 已更新

4. **事件記錄**
   - 發送 Kubernetes Events 以便追蹤
   - 整合 Prometheus metrics

## 授權

Apache License 2.0
