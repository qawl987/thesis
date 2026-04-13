### 🇬🇧 English Prompt for Code Generation
**Related file**
srsran-operator: /home/free5gc/srsran-operator
free5gc-operator: /home/free5gc/free5gc
**Role & Context:**
You are an expert Golang developer specializing in Kubernetes Operators, Kubebuilder, and Cloud-Native Telecommunication architectures (specifically Nephio and O-RAN). I am building a 6G Intent-driven End-to-End (E2E) Network Slicing testbed using srsRAN (RAN domain) and free5GC (Core domain). 

**Architecture Overview:**
Instead of a traditional RESTful API for the O-RAN R1 interface, we are implementing a **Cloud-Native, KRM-based Declarative R1 Interface**. The rApp submits an E2E Intent by applying a Custom Resource (CR) named `E2EQoSIntent` to the Kubernetes API server. 

**Task:**
I need you to implement the **E2E Orchestrator** (a Kubernetes Controller/Operator). Its primary job is to watch the `E2EQoSIntent` CR, translate the high-level SLAs into domain-specific parameters, and orchestrate the underlying domains via a hybrid southbound interface.
First, please review the architecture of the srsran-operator. You can refer to the srsran-operator for the file directory structure, as well as how it defines and installs CRDs. For any other uncertain operator implementation details, you can also use the srsran-operator as a reference.
Implement place: /home/free5gc/e2e-orchestrator
**Input CRD Structure (`E2EQoSIntent`):**
```yaml
apiVersion: e2e.intent.domain/v1alpha1
kind: E2EQoSIntent
metadata:
  name: slices-intent
spec:
  # 定義一個陣列 (Array)，讓 Operator 可以迴圈處理每一個切片意圖
  intentGroups:
    # ---------------- 第一個意圖：eMBB ----------------
    - id: "embb"
      contexts:
        targetUEs: ["208930000000001"]
      expectations:
        sliceType: "eMBB"
        bandwidth: "high"        # 轉譯器對應：5QI=6
        resourceShare: "Partial" # 轉譯器對應：maxPrb=50, priority=10
    # ---------------- 第二個意圖：URLLC 無人機 ----------------
    - id: "urllc"
      # 對應 3GPP: Contexts (條件/範圍，針對哪個 UE)
      contexts:
        targetUEs: ["208930000000002"]
      # 對應 3GPP: Expectations & Targets (期望與目標 SLA)
      expectations:
        sliceType: "URLLC"
        latency: "ultra-low"     # 轉譯器對應：5QI=85
        resourceShare: "Full"    # 轉譯器對應：maxPrb=100, priority=200
```


**Controller Responsibilities & Strict Mapping Rules:**
1. **Fetch Intent:** Retrieve the `E2EQoSIntent` object in the Reconcile loop.
2. **Intent Translation (Keep it simple using switch/case):** Iterate through `spec.intentGroups`. 
   - **For Core Domain (5QI Mapping):**
     - If `sliceType` == "URLLC": Map `latency` to 5QI. ("ultra-low" -> 85, "low" -> 82, "medium" -> 84). Default is 82.
     - If `sliceType` == "eMBB": Map `bandwidth` to 5QI. ("dedicated-high" -> 4, "high" -> 6, "standard" -> 9). Default is 9.
   - **For RAN Domain (PRB Ratio & Priority Mapping):**
     - `resourceShare`: "Full" maps to `minPrbPolicyRatio: 0, maxPrbPolicyRatio: 100`. "Partial" maps to `minPrbPolicyRatio: 0, maxPrbPolicyRatio: 50`.
     - `sliceType` == "URLLC": Assign `sst: 1, sd: 1122867, priority: 200`.
     - `sliceType` == "eMBB": Assign `sst: 1, sd: 66051, priority: 10`.
3. **Southbound - Core Domain (Hybrid REST):**
   - Create a placeholder/dummy function (e.g., `registerUEToFree5GCWebConsole(ue string, qfi int)`). 
   - *Note: Do not implement the actual HTTP client logic for now. Just leave a TODO comment. I will implement the exact free5GC WebConsole REST API payload later.*
4. **Southbound - RAN Domain (Nephio Porch Workflow):**
   - The orchestrator needs to act as a Nephio Porch client.
   - Write the logic or pseudo-code on how to programmatically interact with the Porch API to mutate the `srscellconfig` KRM package. The workflow should represent: cloning a draft -> mutating the PRB ratios -> proposing -> approving the package.
   - nephio workflow best practice reference: /home/free5gc/nephio-intent-workflow-0330.md
   - srscellconfig.yaml reference: /home/free5gc/srsran-operator/blueprint/srsrancellconfig.yaml
   - please follow the best practice to implement the above workflow

**Deliverables:**
1. The Golang struct definitions for the `E2EQoSIntent` CR (`api/v1alpha1/e2eqosintent_types.go`).
2. The core Reconcile loop logic (`internal/controller/e2eqosintent_controller.go`), including the translation mapping functions.
3. A skeleton/example of the programmatic Porch API interaction function for the RAN domain.

