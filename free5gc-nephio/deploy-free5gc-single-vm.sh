#!/bin/bash
# Single-VM Free5GC Deployment Script for Nephio
# Adapted for single Workload Cluster setup

set -e

echo "=========================================="
echo "Free5GC Single-VM Deployment Script"
echo "=========================================="

# Configuration - CHANGE THESE FOR YOUR CLUSTER
CLUSTER_NAME="${CLUSTER_NAME:-regional}"  # Use simple name without hyphens to avoid apply-replacements bug
CONTROL_PLANE_NODE="${CONTROL_PLANE_NODE:-regional-65ngw-fg6nq}"  # kind control-plane container name
WORKER_NODE="${WORKER_NODE:-regional-md-0-d55dw-lx5gd-sp69g}"  # Docker container name for the cluster node
KUBECONFIG_FILE="${KUBECONFIG_FILE:-regional.kubeconfig}"  # Worker cluster kubeconfig
# Resolve to absolute path so cwd changes later in the script cannot break it
[[ "${KUBECONFIG_FILE}" != /* ]] && KUBECONFIG_FILE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/../${KUBECONFIG_FILE}"
SITE_TYPE="${SITE_TYPE:-combined}"  # Cluster label: combined, regional, or edge
BRANCH="main"

echo "Using configuration:"
echo "  Cluster Name: $CLUSTER_NAME"
echo "  Worker Node: $WORKER_NODE"
echo "  Kubeconfig: $KUBECONFIG_FILE"
echo "  Site Type: $SITE_TYPE"

echo "=== Step 0: Fix kube-rbac-proxy Image (gcr.io removed) ==="
# gcr.io/kubebuilder/kube-rbac-proxy:v0.8.0 was removed from GCR.
# Pull from the upstream maintainer's registry (quay.io/brancz), re-tag,
# and load directly into both kind nodes via containerd so kubelet can use it
# without hitting the internet.
KRBP_IMAGE="gcr.io/kubebuilder/kube-rbac-proxy:v0.8.0"
KRBP_SRC="quay.io/brancz/kube-rbac-proxy:v0.8.0"

if sudo docker image inspect "${KRBP_IMAGE}" &>/dev/null; then
    echo "  kube-rbac-proxy image already present in Docker cache – skipping pull"
else
    echo "  Pulling ${KRBP_SRC}..."
    sudo docker pull "${KRBP_SRC}" \
        && sudo docker tag "${KRBP_SRC}" "${KRBP_IMAGE}" \
        && echo "  ✓ Pulled and tagged as ${KRBP_IMAGE}" \
        || echo "  Warning: pull failed – kube-rbac-proxy sidecar may stay in ImagePullBackOff"
fi

for node in "${CONTROL_PLANE_NODE}" "${WORKER_NODE}"; do
    if sudo docker exec "${node}" ctr -n k8s.io images ls 2>/dev/null | grep -q "kube-rbac-proxy:v0.8.0"; then
        echo "  ${node}: image already present in containerd – skipping"
    else
        echo "  Loading ${KRBP_IMAGE} into node ${node}..."
        sudo docker save "${KRBP_IMAGE}" \
            | sudo docker exec -i "${node}" ctr -n k8s.io images import - 2>/dev/null \
            && echo "  ✓ ${node}: loaded" \
            || echo "  Warning: failed to load into ${node}"
    fi
done
echo "✓ kube-rbac-proxy image pre-loaded into all nodes"

echo ""
echo "=== Step 1: Setup VLAN Interfaces in Worker Node ==="
echo "Creating dummy eth1 interface and VLANs..."
# NOTE: srsRAN already occupies VLANs 2-6 (eth1.2=E1/F1C/F1U, eth1.3=N2/N3)
# Free5GC reuses VLAN 3 (eth1.3) for N3/N2 co-location with gNB,
# and VLANs 4,5 (eth1.4, eth1.5) for UPF N4 and N6.
# VLANs 2-6 were created by deploy-srsran.sh; the loop below is idempotent.

# Create dummy interface eth1
sudo docker exec "$WORKER_NODE" ip link add eth1 type dummy 2>/dev/null || echo "  eth1 already exists"
sudo docker exec "$WORKER_NODE" ip link set eth1 up

# Ensure VLANs 2-6 exist (created by srsRAN script; safe to re-run)
for i in {2..6}; do
    echo "  Ensuring VLAN $i exists..."
    sudo docker exec "$WORKER_NODE" ip link add link eth1 name "eth1.$i" type vlan id "$i" 2>/dev/null || echo "  eth1.$i already exists"
    sudo docker exec "$WORKER_NODE" ip link set up "eth1.$i"
done

echo "✓ VLANs ready"
echo "Verifying VLAN interfaces:"
sudo docker exec "$WORKER_NODE" ip link show | grep -E "eth1(\.|:)" || true

echo ""
echo "=== Step 2: Create Network Topology (RawTopology) ==="

cat > /tmp/network-topo-single-vm.yaml <<EOF
---
apiVersion: topo.nephio.org/v1alpha1
kind: RawTopology
metadata:
  name: nephio
spec:
  nodes:
    ${CLUSTER_NAME}:
      provider: docker.io
      labels:
        nephio.org/cluster-name: ${CLUSTER_NAME}
  links: []
EOF

kubectl apply -f /tmp/network-topo-single-vm.yaml
echo "✓ Initial network topology created"

echo ""
echo "=== Step 3: Deploy Network Package ==="

if [ -f "/home/free5gc/test-infra/e2e/tests/free5gc/002-network.yaml" ]; then
    # Substitute variables in the network YAML
    envsubst < /home/free5gc/test-infra/e2e/tests/free5gc/002-network.yaml | kubectl apply -f -
    echo "Waiting for network PackageVariant to be ready..."
    kubectl wait --for=condition=Ready packagevariant/network --timeout=1m || echo "Warning: Network package may still be processing"
    echo "✓ Network package deployed"
else
    echo "Warning: 002-network.yaml not found at expected location"
fi

echo ""
echo "=== Step 3a: Apply Network Secret ==="
kubectl apply -f /home/free5gc/test-infra/e2e/tests/free5gc/002-secret.yaml
echo "✓ Network secret applied"

echo ""
echo "=== Step 3b: Update Network Topology with eth1 Master Interface ==="
# Update RawTopology to add the masterInterface configuration
cat > /tmp/network-topo-updated.yaml <<EOF
apiVersion: topo.nephio.org/v1alpha1
kind: RawTopology
metadata:
  name: nephio
spec:
  nodes:
    ${CLUSTER_NAME}:
      provider: docker.io
      labels:
        nephio.org/cluster-name: ${CLUSTER_NAME}
  links: []
EOF

kubectl apply -f /tmp/network-topo-updated.yaml
echo "✓ Network topology updated"
echo "Waiting for Network resources to be ready..."
sleep 5
kubectl wait --for=condition=Ready network --all --timeout=2m || echo "Warning: Networks may still be processing"

echo ""
echo "=== Step 3c: Add Cluster-Specific Routes to NetworkInstance Prefixes ==="
echo "Adding cluster-name labels to existing IPAM prefixes for: ${CLUSTER_NAME}"
# NOTE: srsRAN already created vpc-ran with 172.2.0.0/16 (n2) and 172.3.0.0/16 (n3).
# DO NOT add new /24 sub-prefixes — they would overlap and cause IPAM allocation
# conflicts between AMF/CU-CP (n2) and UPF/CU-UP (n3).
# Instead, add cluster-name labels to the existing /16 prefixes at index [1] and [3].

# Wait for NetworkInstances to be fully created
sleep 5

# vpc-ran: label existing 172.2.0.0/16 (idx 1) and 172.3.0.0/16 (idx 3)
echo "  Patching vpc-ran: adding cluster-name to existing n2 and n3 /16 prefixes..."
kubectl patch networkinstances.ipam.resource.nephio.org vpc-ran --type=json -p='[
  {"op": "add", "path": "/spec/prefixes/1/labels/nephio.org~1address-family", "value": "ipv4"},
  {"op": "add", "path": "/spec/prefixes/1/labels/nephio.org~1cluster-name",  "value": "'"${CLUSTER_NAME}"'"},
  {"op": "add", "path": "/spec/prefixes/3/labels/nephio.org~1address-family", "value": "ipv4"},
  {"op": "add", "path": "/spec/prefixes/3/labels/nephio.org~1cluster-name",  "value": "'"${CLUSTER_NAME}"'"}
]' 2>/dev/null || echo "    Warning: Could not patch vpc-ran (may already be labeled)"

# vpc-internal: label existing 172.1.0.0/16 (idx 1)
# NOTE: vpc-internal prefixes have NO labels field at all (unlike vpc-ran which
# was already labeled by srsRAN). JSON Patch "add .../labels/key" fails when the
# parent "labels" object doesn't exist. Must add the entire labels object at once.
echo "  Patching vpc-internal: adding cluster-name to existing /16 prefix..."
kubectl patch networkinstances.ipam.resource.nephio.org vpc-internal --type=json -p='[
  {"op": "add", "path": "/spec/prefixes/1/labels", "value": {
    "nephio.org/address-family": "ipv4",
    "nephio.org/cluster-name": "'"${CLUSTER_NAME}"'"
  }}
]' 2>/dev/null || echo "    Warning: Could not patch vpc-internal (may already be labeled)"

# vpc-internet: dynamically locate the 10.0.0.0/8 pool prefix and label it
echo "  Patching vpc-internet pool prefix with cluster labels..."
INTERNET_POOL_IDX=$(kubectl get networkinstances.ipam.resource.nephio.org vpc-internet \
    -o jsonpath='{range .spec.prefixes[*]}{.prefix}{"\n"}{end}' 2>/dev/null \
    | awk '/^10\./{print NR-1; exit}')
if [ -n "${INTERNET_POOL_IDX}" ]; then
    kubectl patch networkinstances.ipam.resource.nephio.org vpc-internet --type=json -p='[
      {"op": "add", "path": "/spec/prefixes/'"${INTERNET_POOL_IDX}"'/labels/nephio.org~1cluster-name",  "value": "'"${CLUSTER_NAME}"'"},
      {"op": "add", "path": "/spec/prefixes/'"${INTERNET_POOL_IDX}"'/labels/nephio.org~1address-family", "value": "ipv4"}
    ]' 2>/dev/null || echo "    Warning: Could not patch vpc-internet pool"
else
    echo "    Warning: 10.0.0.0/8 pool prefix not found in vpc-internet"
fi

echo "✓ Cluster-specific routes added to NetworkInstances"
echo "Waiting for IPAM to process routes..."
sleep 15

echo ""
echo "=== Step 4: Create Adapted PackageVariantSets for Combined Cluster ==="

# UPF PackageVariantSet
cat > /tmp/combined-free5gc-upf.yaml <<EOF
apiVersion: config.porch.kpt.dev/v1alpha2
kind: PackageVariantSet
metadata:
  name: combined-free5gc-upf
spec:
  upstream:
    repo: catalog-workloads-free5gc
    package: pkg-example-upf-bp
    workspaceName: main
  targets:
  - objectSelector:
      apiVersion: infra.nephio.org/v1alpha1
      kind: WorkloadCluster
      matchLabels:
        nephio.org/site-type: ${SITE_TYPE}
    template:
      downstream:
        package: free5gc-upf
      annotations:
        approval.nephio.org/policy: always
      injectors:
      - nameExpr: target.name
EOF

# AMF PackageVariantSet
cat > /tmp/combined-free5gc-amf.yaml <<EOF
apiVersion: config.porch.kpt.dev/v1alpha2
kind: PackageVariantSet
metadata:
  name: combined-free5gc-amf
spec:
  upstream:
    repo: catalog-workloads-free5gc
    package: pkg-example-amf-bp
    workspaceName: main
  targets:
  - objectSelector:
      apiVersion: infra.nephio.org/v1alpha1
      kind: WorkloadCluster
      matchLabels:
        nephio.org/site-type: ${SITE_TYPE}
    template:
      downstream:
        package: free5gc-amf
      annotations:
        approval.nephio.org/policy: always
      injectors:
      - nameExpr: target.name
EOF

# SMF PackageVariantSet
cat > /tmp/combined-free5gc-smf.yaml <<EOF
apiVersion: config.porch.kpt.dev/v1alpha2
kind: PackageVariantSet
metadata:
  name: combined-free5gc-smf
spec:
  upstream:
    repo: catalog-workloads-free5gc
    package: pkg-example-smf-bp
    workspaceName: main
  targets:
  - objectSelector:
      apiVersion: infra.nephio.org/v1alpha1
      kind: WorkloadCluster
      matchLabels:
        nephio.org/site-type: ${SITE_TYPE}
    template:
      downstream:
        package: free5gc-smf
      annotations:
        approval.nephio.org/policy: always
      injectors:
      - nameExpr: target.name
EOF

echo "✓ PackageVariantSet YAMLs created"

echo ""
echo "=== Step 5: Apply PackageVariantSets ==="

kubectl apply -f /tmp/combined-free5gc-upf.yaml
echo "✓ UPF PackageVariantSet applied"

kubectl apply -f /tmp/combined-free5gc-amf.yaml
echo "✓ AMF PackageVariantSet applied"

kubectl apply -f /tmp/combined-free5gc-smf.yaml
echo "✓ SMF PackageVariantSet applied"

echo ""
echo "=== Step 5b: Approve AMF and SMF PackageRevisions ==="
# Even with approval.nephio.org/policy: always, AMF/SMF can get stuck in Proposed if
# IPAM readiness gates are not satisfied in time (vpc-ran label propagation delay).
# This loop force-approves them as a fallback.
for NF in "free5gc-amf" "free5gc-smf"; do
    echo "  Waiting for ${NF} PackageRevision to be Proposed (up to 5 min)..."
    NF_PKG=""
    for attempt in $(seq 1 20); do
        NF_PKG=$(kubectl get packagerevision -n default 2>/dev/null \
            | grep "${CLUSTER_NAME}.${NF}.packagevariant" \
            | grep "Proposed" \
            | awk '{print $1}' | head -1)
        if [ -n "$NF_PKG" ]; then
            echo "  Found Proposed ${NF} package: ${NF_PKG}"
            break
        fi
        # Also check if already Published (policy:always worked)
        ALREADY_PUB=$(kubectl get packagerevision -n default 2>/dev/null \
            | grep "${CLUSTER_NAME}.${NF}.packagevariant" \
            | grep "Published" | head -1)
        if [ -n "$ALREADY_PUB" ]; then
            echo "  ${NF} already Published (auto-approved) – skipping"
            NF_PKG=""
            break
        fi
        echo "    Attempt ${attempt}/20: ${NF} not Proposed yet, waiting 15s..."
        sleep 15
    done
    if [ -n "$NF_PKG" ]; then
        porchctl rpkg approve "${NF_PKG}" -n default 2>/dev/null \
            && echo "  ✓ ${NF} approved" \
            || echo "  Warning: approve failed for ${NF} (may already be Published)"
    else
        echo "  Warning: ${NF} PackageRevision not found in Proposed state – check manually"
    fi
done
echo "✓ AMF/SMF approve step complete"

echo ""
echo "=== Step 6: Fix UPF Package to Include 3 Pools for SMF ==="
echo "The SMF operator template requires 3 pools for 3 network slices"
echo "Waiting for initial UPF PackageRevision to be Published (up to 5 min)..."

UPF_PKG=""
for attempt in $(seq 1 20); do
    UPF_PKG=$(kubectl get packagerevision -n default 2>/dev/null \
        | grep "${CLUSTER_NAME}.free5gc-upf.packagevariant" \
        | grep "Published" \
        | awk '{print $1}' | head -1)
    if [ -n "$UPF_PKG" ]; then
        echo "  Found Published UPF package: $UPF_PKG"
        break
    fi
    echo "  Attempt ${attempt}/20: UPF not Published yet, waiting 15s..."
    sleep 15
done

if [ -n "$UPF_PKG" ]; then
    echo "Found UPF package: $UPF_PKG"
    echo "Creating updated version with 3 pools..."
    
    # Find the next version number
    LATEST_VERSION=$(kubectl get packagerevision -n default 2>/dev/null | grep "${CLUSTER_NAME}.free5gc-upf" | grep "Published" | wc -l)
    NEXT_VERSION=$((LATEST_VERSION + 1))
    
    # Copy to new workspace
    porchctl rpkg copy "$UPF_PKG" --workspace "v${NEXT_VERSION}" -n default 2>/dev/null || echo "  Note: Using existing v${NEXT_VERSION}"
    
    # Pull and modify
    rm -rf /tmp/upf-3pools
    porchctl rpkg pull -n default "${CLUSTER_NAME}.free5gc-upf.v${NEXT_VERSION}" /tmp/upf-3pools 2>/dev/null
    
    if [ -d "/tmp/upf-3pools" ]; then
        # Update upfdeployment.yaml to have exactly 3 pools
        cat > /tmp/upf-3pools/upfdeployment.yaml << 'UPFEOF'
apiVersion: workload.nephio.org/v1alpha1
kind: NFDeployment
metadata:
  name: upf-${CLUSTER_NAME}
  namespace: free5gc-upf
  annotations:
    internal.kpt.dev/upstream-identifier: 'workload.nephio.org|NFDeployment|upf-example|upf-example'
spec:
  provider: upf.free5gc.io
  interfaces:
  - name: n3
    ipv4:
      address: 172.3.0.254/24
      gateway: ""
    vlanID: 3    # eth1.3 — same VLAN as srsRAN gnb-regional-n3 (CU-UP↔UPF GTP-U)
  - name: n4
    ipv4:
      address: 172.1.0.0/24
      gateway: ""
    vlanID: 4    # eth1.4 — free VLAN (PFCP: SMF↔UPF)
  - name: n6
    ipv4:
      address: 10.0.0.0/8
      gateway: ""
    vlanID: 5    # eth1.5 — free VLAN (internet breakout)
  networkInstances:
  - name: vpc-internal
    interfaces:
    - n4
  - name: vpc-internet
    dataNetworks:
    - name: internet
      pool:
      - prefix: 10.0.1.0/24
      - prefix: 10.0.2.0/24
      - prefix: 10.0.3.0/24
    interfaces:
    - n6
  - name: vpc-ran
    interfaces:
    - n3
  capacity:
    maxDownlinkThroughput: 5G
    maxUplinkThroughput: 5G
UPFEOF
        
        # Replace placeholder with actual cluster name
        sed -i "s/upf-\${CLUSTER_NAME}/upf-${CLUSTER_NAME}/g" /tmp/upf-3pools/upfdeployment.yaml
        
        # Push, propose, and approve
        ORIG_DIR="$(pwd)"
        cd /tmp/upf-3pools
        porchctl rpkg push -n default "${CLUSTER_NAME}.free5gc-upf.v${NEXT_VERSION}" . 2>/dev/null
        sleep 5
        porchctl rpkg propose "${CLUSTER_NAME}.free5gc-upf.v${NEXT_VERSION}" -n default 2>/dev/null
        porchctl rpkg approve "${CLUSTER_NAME}.free5gc-upf.v${NEXT_VERSION}" -n default 2>/dev/null
        cd "${ORIG_DIR}"   # MUST cd back: relative KUBECONFIG path in Step 7 depends on cwd

        echo "✓ UPF package updated with 3 pools"
        echo "  Waiting for ConfigSync to apply..."
        sleep 30
    fi
fi

echo ""
echo "=== Step 7: Create Config Resource for SMF ==="
echo "Creating Config with 3 pools from UPF NFDeployment..."

cat > /tmp/upf-config-3pools.yaml << 'CFGEOF'
apiVersion: ref.nephio.org/v1alpha1
kind: Config
metadata:
  name: smf-${CLUSTER_NAME}-upf-${CLUSTER_NAME}
  namespace: free5gc-cp
  annotations:
    config.kubernetes.io/local-config: "false"
spec:
  config:
    apiVersion: workload.nephio.org/v1alpha1
    kind: NFDeployment
    metadata:
      name: upf-${CLUSTER_NAME}
      namespace: free5gc-upf
    spec:
      capacity:
        maxDownlinkThroughput: 5G
        maxUplinkThroughput: 5G
      interfaces:
      - ipv4:
          address: 172.3.0.254/24
          gateway: ""
        name: n3
        vlanID: 3
      - ipv4:
          address: 172.1.0.0/24
          gateway: ""
        name: n4
        vlanID: 4
      - ipv4:
          address: 10.0.0.0/8
          gateway: ""
        name: n6
        vlanID: 5
      networkInstances:
      - interfaces:
        - n4
        name: vpc-internal
      - dataNetworks:
        - name: internet
          pool:
          - prefix: 10.0.1.0/24
          - prefix: 10.0.2.0/24
          - prefix: 10.0.3.0/24
        interfaces:
        - n6
        name: vpc-internet
      - interfaces:
        - n3
        name: vpc-ran
      provider: upf.free5gc.io
CFGEOF

# Replace placeholders
sed -i "s/\${CLUSTER_NAME}/${CLUSTER_NAME}/g" /tmp/upf-config-3pools.yaml

export KUBECONFIG="${KUBECONFIG_FILE}"
kubectl create -f /tmp/upf-config-3pools.yaml 2>/dev/null || kubectl apply -f /tmp/upf-config-3pools.yaml
echo "✓ Config resource created"

echo ""
echo "=== Step 5c: Fix N2/N3 IP conflicts via Porch Package Lifecycle ==="
# IPAM allocated the network base address (172.x.0.0) to both free5gc NFs and
# srsRAN pods.  Both share eth1.3 (macvlan bridge) → ARP collision → NGAP/GTP-U
# unreachable.  Fix by giving each pod a distinct host address:
#   AMF  N2: 172.2.0.254/24  (high end, "server" side)
#   UPF  N3: 172.3.0.254/24
#
# This uses Porch API (porchctl) to create new Draft revisions, modify the
# NFDeployment, then Propose/Approve — following Nephio best practices instead
# of directly modifying Git (which causes State Drift).

F5G_AMF_N2_CIDR="172.2.0.254/24"
F5G_UPF_N3_CIDR="172.3.0.254/24"

echo "  Waiting 30s for Porch/ConfigSync to settle after Steps 6-7..."
sleep 30

# Helper function to fix IP address in a PackageRevision via Porch lifecycle
fix_nf_ip_via_porch() {
    local NF_TYPE="$1"       # e.g., "free5gc-amf" or "free5gc-upf"
    local IFACE_NAME="$2"    # e.g., "n2" or "n3"
    local OLD_CIDR_PATTERN="$3"  # e.g., "172\.2\.0\.0/[0-9]*"
    local NEW_CIDR="$4"      # e.g., "172.2.0.254/24"

    echo "  Processing ${NF_TYPE} (${IFACE_NAME} → ${NEW_CIDR})..."

    # Find the latest Published PackageRevision for this NF
    local LATEST_PKG
    LATEST_PKG=$(kubectl get packagerevision -n default 2>/dev/null \
        | grep "${CLUSTER_NAME}\.${NF_TYPE}\." \
        | grep "Published" \
        | sort -t. -k3 -V \
        | tail -1 \
        | awk '{print $1}')

    if [[ -z "${LATEST_PKG}" ]]; then
        echo "    Warning: No Published ${NF_TYPE} package found – skipping"
        return 1
    fi
    echo "    Found latest package: ${LATEST_PKG}"

    # Pull the package to check if fix is needed
    local WORK_DIR="/tmp/porch-fix-${NF_TYPE}"
    rm -rf "${WORK_DIR}"
    if ! porchctl rpkg pull -n default "${LATEST_PKG}" "${WORK_DIR}" 2>/dev/null; then
        echo "    Warning: Failed to pull ${LATEST_PKG}"
        return 1
    fi

    # Find NFDeployment file
    local DEPLOY_FILE
    DEPLOY_FILE=$(find "${WORK_DIR}" -name "*deployment.yaml" -type f | head -1)
    if [[ -z "${DEPLOY_FILE}" || ! -f "${DEPLOY_FILE}" ]]; then
        echo "    Warning: NFDeployment file not found in ${LATEST_PKG}"
        rm -rf "${WORK_DIR}"
        return 1
    fi

    # Check if the file already has the correct IP
    if grep -q "address: ${NEW_CIDR}" "${DEPLOY_FILE}" 2>/dev/null; then
        echo "    ✓ ${NF_TYPE} already has correct IP (${NEW_CIDR}) – no change needed"
        rm -rf "${WORK_DIR}"
        return 0
    fi

    # Determine the next version number
    local LATEST_VERSION
    LATEST_VERSION=$(echo "${LATEST_PKG}" | sed 's/.*\.\(v[0-9]*\|packagevariant-[0-9]*\)$/\1/' | sed 's/[^0-9]//g')
    [[ -z "${LATEST_VERSION}" ]] && LATEST_VERSION=1
    local NEXT_VERSION=$((LATEST_VERSION + 1))
    local NEW_WORKSPACE="v${NEXT_VERSION}"
    local NEW_PKG="${CLUSTER_NAME}.${NF_TYPE}.${NEW_WORKSPACE}"

    echo "    Creating new revision: ${NEW_PKG}"

    # Copy to create a new Draft
    if ! porchctl rpkg copy "${LATEST_PKG}" --workspace "${NEW_WORKSPACE}" -n default 2>/dev/null; then
        echo "    Warning: Failed to copy package (may already exist)"
        # Try pulling the existing workspace if copy failed
        NEW_PKG="${CLUSTER_NAME}.${NF_TYPE}.${NEW_WORKSPACE}"
    fi

    # Pull the new Draft
    rm -rf "${WORK_DIR}"
    if ! porchctl rpkg pull -n default "${NEW_PKG}" "${WORK_DIR}" 2>/dev/null; then
        echo "    Warning: Failed to pull new Draft ${NEW_PKG}"
        return 1
    fi

    # Find and patch NFDeployment
    DEPLOY_FILE=$(find "${WORK_DIR}" -name "*deployment.yaml" -type f | head -1)
    if [[ -n "${DEPLOY_FILE}" && -f "${DEPLOY_FILE}" ]]; then
        sed -i "s|address: ${OLD_CIDR_PATTERN}|address: ${NEW_CIDR}|g" "${DEPLOY_FILE}"
        echo "    ✓ Patched NFDeployment: ${IFACE_NAME} → ${NEW_CIDR}"
    fi

    # Also patch NAD files (rendered under free5gc-cp or free5gc-upf subdirectory)
    local NAD_FILES
    NAD_FILES=$(find "${WORK_DIR}" -name "networkattachmentdefinition_*.yaml" -type f 2>/dev/null)
    for NAD_FILE in ${NAD_FILES}; do
        if grep -q "${OLD_CIDR_PATTERN}" "${NAD_FILE}" 2>/dev/null; then
            sed -i "s|${OLD_CIDR_PATTERN}|${NEW_CIDR}|g" "${NAD_FILE}"
            echo "    ✓ Patched NAD: $(basename "${NAD_FILE}")"
        fi
    done

    # Push the changes
    if ! porchctl rpkg push -n default "${NEW_PKG}" "${WORK_DIR}" 2>/dev/null; then
        echo "    Warning: Failed to push changes to ${NEW_PKG}"
        rm -rf "${WORK_DIR}"
        return 1
    fi

    # Propose and Approve
    sleep 2
    if porchctl rpkg propose "${NEW_PKG}" -n default 2>/dev/null; then
        echo "    ✓ Proposed ${NEW_PKG}"
    else
        echo "    Warning: Failed to propose ${NEW_PKG} (may already be proposed)"
    fi

    sleep 2
    if porchctl rpkg approve "${NEW_PKG}" -n default 2>/dev/null; then
        echo "    ✓ Approved ${NEW_PKG}"
    else
        echo "    Warning: Failed to approve ${NEW_PKG} (may already be approved)"
    fi

    rm -rf "${WORK_DIR}"
    echo "  ✓ ${NF_TYPE} IP fix complete via Porch lifecycle"
    return 0
}

# Fix AMF N2 interface
fix_nf_ip_via_porch "free5gc-amf" "n2" "172\.2\.0\.0/[0-9]*" "${F5G_AMF_N2_CIDR}"

# Fix UPF N3 interface
fix_nf_ip_via_porch "free5gc-upf" "n3" "172\.3\.0\.0/[0-9]*" "${F5G_UPF_N3_CIDR}"

# Fix SMF Config (reference to UPF N3 address)
echo "  Processing free5gc-smf (UPF N3 endpoint → ${F5G_UPF_N3_CIDR})..."
SMF_PKG=$(kubectl get packagerevision -n default 2>/dev/null \
    | grep "${CLUSTER_NAME}\.free5gc-smf\." \
    | grep "Published" \
    | sort -t. -k3 -V \
    | tail -1 \
    | awk '{print $1}')

if [[ -n "${SMF_PKG}" ]]; then
    SMF_WORK_DIR="/tmp/porch-fix-smf"
    rm -rf "${SMF_WORK_DIR}"
    if porchctl rpkg pull -n default "${SMF_PKG}" "${SMF_WORK_DIR}" 2>/dev/null; then
        SMF_CFG=$(find "${SMF_WORK_DIR}" -name "config_*.yaml" -type f 2>/dev/null | head -1)
        if [[ -n "${SMF_CFG}" && -f "${SMF_CFG}" ]]; then
            if grep -q "address: ${F5G_UPF_N3_CIDR}" "${SMF_CFG}" 2>/dev/null; then
                echo "    ✓ SMF Config already has correct UPF N3 endpoint"
            else
                # Need to create new revision for SMF
                SMF_VER=$(echo "${SMF_PKG}" | sed 's/.*\.\(v[0-9]*\|packagevariant-[0-9]*\)$/\1/' | sed 's/[^0-9]//g')
                [[ -z "${SMF_VER}" ]] && SMF_VER=1
                SMF_NEXT=$((SMF_VER + 1))
                SMF_NEW_PKG="${CLUSTER_NAME}.free5gc-smf.v${SMF_NEXT}"

                porchctl rpkg copy "${SMF_PKG}" --workspace "v${SMF_NEXT}" -n default 2>/dev/null || true
                rm -rf "${SMF_WORK_DIR}"
                if porchctl rpkg pull -n default "${SMF_NEW_PKG}" "${SMF_WORK_DIR}" 2>/dev/null; then
                    SMF_CFG=$(find "${SMF_WORK_DIR}" -name "config_*.yaml" -type f 2>/dev/null | head -1)
                    if [[ -n "${SMF_CFG}" ]]; then
                        sed -i "s|address: 172\.3\.0\.0/[0-9]*|address: ${F5G_UPF_N3_CIDR}|g" "${SMF_CFG}"
                        porchctl rpkg push -n default "${SMF_NEW_PKG}" "${SMF_WORK_DIR}" 2>/dev/null
                        sleep 2
                        porchctl rpkg propose "${SMF_NEW_PKG}" -n default 2>/dev/null || true
                        sleep 2
                        porchctl rpkg approve "${SMF_NEW_PKG}" -n default 2>/dev/null || true
                        echo "    ✓ Patched SMF Config UPF N3 endpoint via Porch"
                    fi
                fi
            fi
        fi
    fi
    rm -rf "${SMF_WORK_DIR}"
else
    echo "    Warning: SMF package not found – will be fixed on next run"
fi

echo "✓ IP conflict fixes applied via Porch Package Lifecycle (ConfigSync applies in ~60s)"

echo ""
echo "=== Step 8: Restart Free5GC Operator and Fix Images ==="
echo "Restarting operator to trigger SMF deployment..."
kubectl rollout restart deployment/free5gc-operator -n free5gc 2>/dev/null || echo "  Note: Operator restart failed, may need manual restart"
sleep 25

echo "Fixing Docker images to use official free5gc images..."
kubectl set image deployment/upf-${CLUSTER_NAME} -n free5gc-upf upf=free5gc/upf:v4.1.0 2>/dev/null || echo "  UPF image already set or not found"
kubectl set image deployment/amf-${CLUSTER_NAME} -n free5gc-cp amf=free5gc/amf:v4.1.0 2>/dev/null || echo "  AMF image already set or not found"
kubectl set image deployment/smf-${CLUSTER_NAME} -n free5gc-cp smf=free5gc/smf:v4.1.0 2>/dev/null || echo "  SMF image already set or not found"

sleep 15
echo "✓ Images updated"

echo ""
echo "=========================================="
echo "Deployment Complete!"
echo "=========================================="
echo ""
export KUBECONFIG="${KUBECONFIG_FILE}"
echo "Deployment Status:"
echo ""
echo "Control Plane (free5gc-cp):"
kubectl get pods -n free5gc-cp 2>/dev/null | grep -E "NAME|amf|smf" || echo "  Not ready yet"
echo ""
echo "User Plane (free5gc-upf):"
kubectl get pods -n free5gc-upf 2>/dev/null | grep -E "NAME|upf" || echo "  Not ready yet"
echo ""
echo "=========================================="
echo "Troubleshooting Commands:"
echo "=========================================="
echo ""
echo "1. Check all free5gc pods:"
echo "   export KUBECONFIG=./$KUBECONFIG_FILE"
echo "   kubectl get pods -A | grep free5gc"
echo ""
echo "2. Check operator logs:"
echo "   kubectl logs -n free5gc deployment/free5gc-operator -c operator --tail=50"
echo ""
echo "3. Check SMF configuration:"
echo "   kubectl get configmap smf-${CLUSTER_NAME} -n free5gc-cp -o yaml"
echo ""
echo "4. Check Config resource:"
echo "   kubectl get config smf-${CLUSTER_NAME}-upf-${CLUSTER_NAME} -n free5gc-cp -o yaml"
echo ""
echo "5. Verify UPF has 3 pools:"
echo "   kubectl get nfdeployment upf-${CLUSTER_NAME} -n free5gc-upf -o yaml | grep -A10 'pool:'"
echo ""
echo "=========================================="
echo "Key Fixes Applied:"
echo "=========================================="
echo "1. ✓ Added cluster-name labels to NetworkInstance prefixes"
echo "2. ✓ Updated UPF package with 3 pools (required by SMF template)"
echo "3. ✓ Created Config resource with 3 pools for SMF"
echo "4. ✓ Fixed Docker images to use official free5gc/* sources"
echo "5. ✓ Restarted operator to trigger SMF deployment"
echo ""
echo "Note: SMF template hardcodes 3 network slices:"
echo "  - sNssai sst:1, sd:010203 → Pool[0]: 10.0.1.0/24"
echo "  - sNssai sst:1, sd:112233 → Pool[1]: 10.0.2.0/24"
echo "  - sNssai sst:2, sd:112234 → Pool[2]: 10.0.3.0/24"
echo "=========================================="
