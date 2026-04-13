### 1. URLLC 切片 (以 Latency 延遲為導向)

URLLC 的核心在於極低的 **PDB (Packet Delay Budget)** 以及極低的 **PER (Packet Error Rate)**。你可以設計一個 `latency` 欄位，並做以下轉譯：

* **`latency: "ultra-low"` $\rightarrow$ 5QI: 85**
    * **背後規格**：PDB = 5ms, PER = $10^{-5}$
    * **對應場景**：高壓配電 (Electricity Distribution-high voltage)、極高精度工業控制。這是配置表中延遲最低且可靠度極高的選項。
* **`latency: "low"` $\rightarrow$ 5QI: 82**
    * **背後規格**：PDB = 10ms, PER = $10^{-4}$
    * **對應場景**：離散自動化 (Discrete Automation)。適合一般的工業機器人、無人機即時控制。
* **`latency: "medium"` $\rightarrow$ 5QI: 84**
    * **背後規格**：PDB = 30ms, PER = $10^{-5}$
    * **對應場景**：智慧交通系統 (Intelligent transport systems)。容許稍微多一點點延遲，但依舊保持極高的封包傳達率。

### 2. eMBB 切片 (以 Bandwidth / Throughput 為導向)

eMBB 的核心在於大頻寬與高吞吐量。在 3GPP 定義中，這通常透過 **GBR (Gu證比特率)** 與 **Non-GBR (非保證比特率)** 的 5QI 來區分優先級。你可以設計一個 `bandwidth` 欄位：

* **`bandwidth: "dedicated-high"` $\rightarrow$ 5QI: 4**
    * **背後規格**：PDB = 300ms, PER = $10^{-6}$, **Guaranteed Bitrate (GBR)**
    * **對應場景**：非對話型影像 (Non-conversational video)。這屬於 GBR 資源，當網路壅塞時，基地台必須優先保證這個 5QI 的頻寬，適合 4K/8K 高畫質專線串流。
* **`bandwidth: "high"` $\rightarrow$ 5QI: 6**
    * **背後規格**：PDB = 300ms, PER = $10^{-6}$, **Non-Guaranteed Bitrate**
    * **對應場景**：緩衝影音串流 (Buffered Streaming)。雖然是非保證頻寬，但在 Non-GBR 中具有較高的調度權重，適合一般順暢的高畫質影音應用。
* **`bandwidth: "standard"` $\rightarrow$ 5QI: 9**
    * **背後規格**：PDB = 300ms, PER = $10^{-6}$, **Non-Guaranteed Bitrate**
    * **對應場景**：預設網際網路流量。這是最標準的 Best-effort 預設值，當使用者沒有特別的高頻寬需求時，作為 eMBB 的打底設定。

---

如此一來，外部使用者或上層的 rApp 只需要發送 `{"sliceType": "URLLC", "latency": "ultra-low"}`，你的 Orchestrator 就會自動算出 `5QI: 85`。

接著：
1. **往 Core 走**：拿著這個 `85` 去 call free5GC 的 WebConsole API，設定 UE 的 QoS Profile。
2. **往 RAN 走**：拿著 `URLLC` 的類型，去生成你上一則訊息中提到的 `minPrbPolicyRatio` 與 `maxPrbPolicyRatio` 那些 srsRAN CRD。
