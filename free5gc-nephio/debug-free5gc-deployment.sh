#!/bin/bash
# Debug script for Free5GC deployment after running deploy-free5gc-single-vm.sh
# This script collects all diagnostic information to troubleshoot deployment issues

echo "=========================================="
echo "Free5GC Deployment Diagnostic Script"
echo "=========================================="
echo ""

# Configuration - UPDATE THESE FOR YOUR CLUSTER
CLUSTER_NAME="${CLUSTER_NAME:-regional}"
KUBECONFIG_FILE="${KUBECONFIG_FILE:-regional.kubeconfig}"

echo "Using configuration:"
echo "  Cluster Name: $CLUSTER_NAME"
echo "  Kubeconfig File: $KUBECONFIG_FILE"
echo ""

# Make sure we're in management cluster context
unset KUBECONFIG

echo "=========================================="
echo "1. MANAGEMENT CLUSTER - PackageVariant Status"
echo "=========================================="
echo ""
echo "--- All PackageVariants ---"
kubectl get packagevariant -o wide
echo ""
echo "--- Free5GC PackageVariants Details ---"
kubectl get packagevariant | grep -E "free5gc|NAME"
echo ""

echo "=========================================="
echo "2. MANAGEMENT CLUSTER - PackageRevision Status"
echo "=========================================="
echo ""
echo "--- All PackageRevisions for ${CLUSTER_NAME} ---"
kubectl get packagerevision | grep -E "${CLUSTER_NAME}|NAME"
echo ""
echo "--- Check for Draft vs Published ---"
kubectl get packagerevision | grep -E "${CLUSTER_NAME}" | grep -E "Draft|Published"
echo ""

echo "=========================================="
echo "3. MANAGEMENT CLUSTER - Check for Name Recursion Bug"
echo "=========================================="
echo ""
echo "Checking NFDeployment names in PackageRevisions..."
echo ""

# Check UPF
UPF_PKG=$(kubectl get packagerevision | grep "${CLUSTER_NAME}.*free5gc-upf" | awk '{print $1}' | head -1)
if [ -n "$UPF_PKG" ]; then
    echo "--- UPF NFDeployment name ---"
    kubectl get packagerevisionresources "$UPF_PKG" -o jsonpath='{.spec.resources.upfdeployment\.yaml}' 2>/dev/null | grep -E "^\s+name:" | head -1
    echo ""
    echo "Expected: name: upf-${CLUSTER_NAME}"
    echo "If you see: name: upf-${CLUSTER_NAME}-${CLUSTER_NAME}-${CLUSTER_NAME}... then there's a BUG!"
    echo ""
fi

# Check AMF
AMF_PKG=$(kubectl get packagerevision | grep "${CLUSTER_NAME}.*free5gc-amf" | awk '{print $1}' | head -1)
if [ -n "$AMF_PKG" ]; then
    echo "--- AMF NFDeployment name ---"
    kubectl get packagerevisionresources "$AMF_PKG" -o jsonpath='{.spec.resources.amfdeployment\.yaml}' 2>/dev/null | grep -E "^\s+name:" | head -1
    echo ""
fi

# Check SMF
SMF_PKG=$(kubectl get packagerevision | grep "${CLUSTER_NAME}.*free5gc-smf" | awk '{print $1}' | head -1)
if [ -n "$SMF_PKG" ]; then
    echo "--- SMF NFDeployment name ---"
    kubectl get packagerevisionresources "$SMF_PKG" -o jsonpath='{.spec.resources.smfdeployment\.yaml}' 2>/dev/null | grep -E "^\s+name:" | head -1
    echo ""
fi

echo "=========================================="
echo "4. MANAGEMENT CLUSTER - Network Resources"
echo "=========================================="
echo ""
echo "--- RawTopology ---"
kubectl get rawtopology nephio -o yaml 2>/dev/null | grep -A20 "spec:"
echo ""
echo "--- NetworkInstances ---"
kubectl get networkinstance -o wide
echo ""
echo "--- Check NetworkInstance SYNC status ---"
kubectl get networkinstance -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.status.conditions[?(@.type=="Synced")].status}{"\n"}{end}'
echo ""

echo "=========================================="
echo "5. MANAGEMENT CLUSTER - WorkloadCluster"
echo "=========================================="
echo ""
kubectl get workloadcluster "$CLUSTER_NAME" -o yaml 2>/dev/null || echo "WorkloadCluster $CLUSTER_NAME not found!"
echo ""

echo "=========================================="
echo "6. MANAGEMENT CLUSTER - PackageVariantSet Status"
echo "=========================================="
echo ""
kubectl get packagevariantset -o wide
echo ""

echo "=========================================="
echo "7. WORKER CLUSTER - Namespaces"
echo "=========================================="
echo ""
if [ -f "$KUBECONFIG_FILE" ]; then
    export KUBECONFIG="$KUBECONFIG_FILE"
    echo "--- All namespaces ---"
    kubectl get ns
    echo ""
    echo "--- Free5GC related namespaces ---"
    kubectl get ns | grep free5gc
    echo ""
else
    echo "ERROR: Kubeconfig file $KUBECONFIG_FILE not found!"
    echo "Skipping worker cluster checks."
    exit 1
fi

echo "=========================================="
echo "8. WORKER CLUSTER - NFDeployment Resources"
echo "=========================================="
echo ""
echo "--- UPF NFDeployment ---"
kubectl get nfdeployment -n free5gc-upf -o wide 2>/dev/null || echo "No NFDeployment in free5gc-upf namespace"
echo ""
if kubectl get nfdeployment -n free5gc-upf 2>/dev/null | grep -q "upf-"; then
    UPF_DEPLOY=$(kubectl get nfdeployment -n free5gc-upf -o jsonpath='{.items[0].metadata.name}')
    echo "--- UPF NFDeployment Details ---"
    kubectl describe nfdeployment "$UPF_DEPLOY" -n free5gc-upf | tail -40
    echo ""
fi

echo "--- AMF NFDeployment ---"
kubectl get nfdeployment -n free5gc-cp -o wide 2>/dev/null | grep amf || echo "No AMF NFDeployment found"
echo ""

echo "--- SMF NFDeployment ---"
kubectl get nfdeployment -n free5gc-cp -o wide 2>/dev/null | grep smf || echo "No SMF NFDeployment found"
echo ""

echo "=========================================="
echo "9. WORKER CLUSTER - Pods Status"
echo "=========================================="
echo ""
echo "--- UPF Pods ---"
kubectl get pods -n free5gc-upf 2>/dev/null || echo "Namespace free5gc-upf not found or no pods"
echo ""
echo "--- Control Plane Pods ---"
kubectl get pods -n free5gc-cp 2>/dev/null || echo "Namespace free5gc-cp not found or no pods"
echo ""
echo "--- All Free5GC related pods ---"
kubectl get pods -A | grep free5gc
echo ""

echo "=========================================="
echo "10. WORKER CLUSTER - Free5GC Operator Logs"
echo "=========================================="
echo ""
if kubectl get deployment -n free5gc free5gc-operator 2>/dev/null; then
    echo "--- Operator logs (last 50 lines) ---"
    kubectl logs -n free5gc deployment/free5gc-operator -c operator --tail=50 2>/dev/null || \
    kubectl logs -n free5gc deployment/free5gc-operator --tail=50 2>/dev/null
    echo ""
    echo "--- Operator logs filtered for errors ---"
    kubectl logs -n free5gc deployment/free5gc-operator -c operator --tail=100 2>/dev/null | grep -i error || \
    kubectl logs -n free5gc deployment/free5gc-operator --tail=100 2>/dev/null | grep -i error
    echo ""
else
    echo "Free5GC operator not found in namespace 'free5gc'"
fi

echo "=========================================="
echo "11. WORKER CLUSTER - Check for Interface Resources"
echo "=========================================="
echo ""
echo "--- Checking if Interface CRD exists ---"
kubectl get crd | grep interface
echo ""
echo "--- Checking Interface resources in free5gc-upf namespace ---"
kubectl get interface -n free5gc-upf 2>/dev/null || echo "No Interface resources or CRD not found"
echo ""

echo "=========================================="
echo "12. WORKER CLUSTER - Events in free5gc-upf namespace"
echo "=========================================="
echo ""
kubectl get events -n free5gc-upf --sort-by='.lastTimestamp' 2>/dev/null | tail -20 || echo "No events in free5gc-upf"
echo ""

echo "=========================================="
echo "13. WORKER CLUSTER - ConfigMaps in free5gc-upf"
echo "=========================================="
echo ""
kubectl get configmap -n free5gc-upf 2>/dev/null || echo "No configmaps in free5gc-upf"
echo ""

echo "=========================================="
echo "SUMMARY - Common Issues to Check"
echo "=========================================="
echo ""
echo "✓ Check #3: If NFDeployment name shows recursion (upf-regional-regional-regional...)"
echo "  → This is the apply-replacements bug with cluster names containing hyphens"
echo "  → Solution: Use simple cluster name without hyphens (e.g., 'regional' not 'free5gc-worker')"
echo ""
echo "✓ Check #2: If PackageRevision stuck in Draft status"
echo "  → Check the conditions in PackageRevision details"
echo "  → May be due to name recursion bug or missing WorkloadCluster info"
echo ""
echo "✓ Check #8: If NFDeployment exists but no pods"
echo "  → Check operator logs (#10) for errors"
echo "  → Common error: 'Interface N4 not found in UPF NFDeployment Spec'"
echo "  → This means nfdeploy-fn didn't inject interface info correctly"
echo ""
echo "✓ Check #4: If NetworkInstances not Synced"
echo "  → Network package may not be ready"
echo "  → Check network PackageVariant status"
echo ""
echo "=========================================="
echo "Script completed. Review output above."
echo "=========================================="
