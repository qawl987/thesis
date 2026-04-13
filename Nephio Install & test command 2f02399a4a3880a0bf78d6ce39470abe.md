# Nephio Install & test command

Ubuntu 22.04

官方的gcr, kube-rbac-proxy v0.8沒維護了，改用bitnami latest，要修改init.sh，手動匯入檔案

### Install command

1. `echo "$USER ALL=(ALL) NOPASSWD:ALL" | sudo tee /etc/sudoers.d/$USER`

```bash
cd ~
git clone https://github.com/nephio-project/test-infra.git
```

`~/test-infra/e2e/provision/init.sh` 中修改sandbox creation part

`cd ~/test-infra/e2e/provision/`

`sudo NEPHIO_DEBUG=false \
NEPHIO_BRANCH=main \
NEPHIO_USER=$USER \
bash ./init.sh`

```bash
# Sandbox Creation
int_start=$(date +%s)
cd "$REPO_DIR/e2e/provision"

# === [極簡直入版：直接串流匯入，不存檔案] ===
cat << 'EOF' > /tmp/nephio-image-fix.sh
#!/bin/bash
set -e # 遇到錯誤直接中斷，不掩蓋問題
exec > /tmp/nephio-image-fix.log 2>&1
echo "=== Starting Image Pre-load ==="

KRBP_SRC="bitnami/kube-rbac-proxy:latest"
KRBP_IMAGE="gcr.io/kubebuilder/kube-rbac-proxy:v0.8.0"

echo "1. Pulling and tagging from Bitnami..."
sudo docker pull "$KRBP_SRC"
sudo docker tag "$KRBP_SRC" "$KRBP_IMAGE"

echo "2. Waiting for kind node to appear..."
while ! sudo docker ps --format '{{.Names}}' | grep -q "kind-control-plane"; do
    sleep 2
done

echo "3. Kind node detected. Waiting 5 seconds for containerd..."
sleep 5

echo "4. Streaming image directly into containerd (no temp files)..."
# 使用你最初驗證成功的 Pipe 寫法，直接餵進 containerd
sudo docker save "$KRBP_IMAGE" | sudo docker exec -i kind-control-plane ctr -n k8s.io images import -

echo "5. Verification:"
sudo docker exec kind-control-plane ctr -n k8s.io images ls | grep "kube-rbac-proxy"
echo "=== Image Fix Completed Successfully ==="
EOF

chmod +x /tmp/nephio-image-fix.sh
/tmp/nephio-image-fix.sh &
# === [極簡直入版結束] ===

export DEBUG DOCKERHUB_USERNAME DOCKERHUB_TOKEN FAIL_FAST MGMT_CLUSTER_TYPE K8S_VERSION
```

刪掉失敗的cluster

```bash
sudo /usr/local/bin/kind delete cluster --name kind 2>/dev/null
sudo docker rm -f kind-control-plane 2>/dev/null
```

### Nephio Cluster created

1. `porchctl rpkg clone catalog-infra-capi.nephio-workload-cluster.main regional --repository=mgmt -n default`
    1. 透過`catalog-infra-capi`package在最初的repo`mgmt` 建立一個draft cluster package`regional`
    2. `mgmt.regional.v1 created`
    3. free5gc@Nephio:~$ porchctl rpkg get --name free5gc-worker -n default
    NAME                     PACKAGE          WORKSPACENAME   REVISION   LATEST   LIFECYCLE   REPOSITORY
    mgmt.free5gc-worker.v1   free5gc-worker   v1              0          false    Draft       mgm
2. `porchctl rpkg propose mgmt.regional.v1 -n default`
    1. Porpose
3. `porchctl rpkg approve mgmt.regional.v1 -n default`
    1. Approve
4. `porchctl rpkg get --name regional -n default`
    1. 兩個package代表revision 1是目前運行的版本，merge到main裡面，而main才是真正被監聽的版本，之後的revision有merge到main的話才會真正改動
    2. NAME                       PACKAGE          WORKSPACENAME   REVISION   LATEST   LIFECYCLE   REPOSITORY
    mgmt.free5gc-worker.main   free5gc-worker   main            -1         false    Published   mgmt
    mgmt.free5gc-worker.v1     free5gc-worker   v1              1          true     Published   mgmt
5. `kubectl get secret regional-kubeconfig -n default -o jsonpath='{.data.value}' | base64 -d > regional.kubeconfig`
    1. 抓取新建立的cluster secret
6. `kubectl --kubeconfig regional.kubeconfig get nodes`
    1. 透過secret存取新建立的cluster資訊
    2. NAME                                    STATUS   ROLES           AGE     VERSION
    free5gc-worker-4xjdn-bpvvr              Ready    control-plane   6m22s   v1.31.0
    free5gc-worker-md-0-5rxvt-fd4pd-7d86b   Ready    <none>          5m54s   v1.31.0
7. 或者用環境變數
    1. `export KUBECONFIG=./regional.kubeconfig`
    2. `unset KUBECONFIG`

docker ps中帶md的是worker node，沒帶md的regional是cluster node。修改兩個.sh以及helm makefile中的worker node

### free5GC-cp

`porchctl rpkg clone catalog-workloads-free5gc.free5gc-cp.main free5gc-cp --repository=regional -n default`

`porchctl rpkg propose regional.free5gc-cp.v1 -n default`

`porchctl rpkg approve regional.free5gc-cp.v1 -n default`

### free5GC operator

`porchctl rpkg clone catalog-workloads-free5gc.free5gc-operator.main free5gc-operator --repository=regional -n default`

`porchctl rpkg propose regional.free5gc-operator.v1 -n default`

`porchctl rpkg approve regional.free5gc-operator.v1 -n default`

### Mark label && run script && reconcile to trigger ip modify

`kubectl label workloadcluster regional nephio.org/site-type=combined --overwrite -n default`

`./deploy-free5gc-single-vm.sh`

`make reconcile-nfs`

### srsran operator

1. `install go`
2. `./deploy-srsran.sh`

### srsUE

1. install helm

```bash
bash <<EOF
curl -fsSL -o get_helm.sh https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3
chmod 700 get_helm.sh
./get_helm.sh
EOF
```

關鍵! du的f1u: 172.6.0.2 以及zmq: 172.6.0.0，但是問題不在網段，在masterif。

1. 先看du f1u實際上的masterif分配到哪個vlan
    1. kubectl exec -it gnb-regional-du-84b54fb7bc-rgxm5 -n srsran-gnb -- ip a
    2. 假設f1u@if23
2. 代表對應到worker node的ip a第23個if
    1. sudo docker exec -it regional-md-0-7hcxb-z4qv6-q6r67 ip a
    2. 23: eth1.3@eth1
3. 所以到ue_zmq_nad.yaml中修改對應的masterif "master": "eth1.3",

### Test Procedure

worker node裡面安裝iperf3

註冊第二個slice如圖，default s-nssai一定要勾，不然mongodb會少UE rule

![image.png](image.png)

### Install

1. srsran-operator: make gnb-up
2. srsran-helm: make ue1
3. srsran-helm: make ue2
4. srsran-helm: make gnu

### Test

1. srsran-operator: make upf-iperf-setup
2. srsran-operator: make iperf-server-ue1
3. srsran-operator: make iperf-server-ue2
4. srsran-helm: make iperf-ue1
5. srsran-helm: make iperf-ue2

清除iperf占用: iperf-clean

### Intent testing

`make run-webconsole 2>&1 | tee server.log`

`./hack/measure-latency.sh config/samples/e2eqosintent_sample.yaml`

e2e-orchestrator，底下有指令可以檢查e2e的latency

`make generate` 

`make install`

`make build`

`make run`

`make apply-sample`

| 命令 | 作用 | 備註 |
| --- | --- | --- |
| `make manifests` | 生成 CRD YAML | `make install` 會自動呼叫 |
| `make generate` | 生成 DeepCopy 方法 | 只有修改 `api/` 時才需要 |
| `make install` | 安裝 CRD 到 cluster | 包含 manifests |
| `make build` | 編譯 binary | `make run` 直接用 `go run`，不需要 |
| `make run` | 啟動 controller | 包含 fmt + vet |
| `make apply-sample` | 套用測試 CR |  |

| 階段 | 時間戳 | 來源 | 命令 |
| --- | --- | --- | --- |
| Intent 收到 | 14:41:10 | server.log | `grep 'Spec changed' server.log` |
| Porch 完成 | 14:41:11 | PackageRevision | `kubectl get packagerevision ... -o jsonpath='{.metadata.creationTimestamp}'` |
| Config Sync | 14:46:47 | SrsRANCellConfig | `kubectl get srsrancellconfig ... -o jsonpath='{.metadata.managedFields[0].time}'` |
| ConfigMap 更新 | ~14:46:47+ | 無直接時間戳 | ConfigMap metadata 沒有 lastUpdateTime |

Webconsole expose

`KUBECONFIG=/home/free5gc/regional.kubeconfig kubectl port-forward -n free5gc-cp svc/webui-service 30500:5000 --address 0.0.0.0`