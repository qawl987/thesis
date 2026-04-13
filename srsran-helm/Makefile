build-image:
	cd docker && docker build -f Dockerfile.split-k8s -t srsran-split:latest ..
import:
	docker save srsran-split:latest | microk8s ctr image import -

build-ue-image:
	cd docker && docker build -f Dockerfile.srsue -t srsran-ue:latest ..
import-ue:
	docker save srsran-ue:latest | microk8s ctr image import -

build-gnu-breaker-image:
	cd docker && sudo docker build -f Dockerfile.gnu-breaker -t gnu-breaker:latest .

# Push locally-built images to Docker Hub (docker login required first).
DOCKERHUB_USER ?= qawl987
push:
	docker tag srsran-split:latest $(DOCKERHUB_USER)/srsran-split:latest
	docker push $(DOCKERHUB_USER)/srsran-split:latest
	docker tag srsran-ue:latest $(DOCKERHUB_USER)/srsran-ue:latest
	docker push $(DOCKERHUB_USER)/srsran-ue:latest
push-gnu:
	docker tag gnu-breaker:latest $(DOCKERHUB_USER)/gnu-breaker:latest
	docker push $(DOCKERHUB_USER)/gnu-breaker:latest

build-push: build-image build-ue-image push
build-push-gnu: build-gnu-breaker-image push-gnu

# Run command
.PHONY: free5gc cp up du ue ue1 ue2 gnu
free5gc:
	helm install free5gc-v1 -n free5gc /home/free5gc/free5gc-helm/charts/free5gc
cp:
	helm install srsran-cucp -n free5gc /home/free5gc/srsran-helm/charts/cucp
up:
	helm install srsran-cuup -n free5gc /home/free5gc/srsran-helm/charts/cuup
du:
	helm install srsran-du -n free5gc /home/free5gc/srsran-helm/charts/du
gnb:
	make cp
	sleep 3
	make up
	sleep 3
	make du
ue:
	KUBECONFIG=/home/free5gc/regional.kubeconfig kubectl get namespace srsran-ue 2>/dev/null || KUBECONFIG=/home/free5gc/regional.kubeconfig kubectl create namespace srsran-ue
	KUBECONFIG=/home/free5gc/regional.kubeconfig kubectl label namespace srsran-ue \
		pod-security.kubernetes.io/enforce=privileged \
		pod-security.kubernetes.io/enforce-version=latest \
		pod-security.kubernetes.io/audit=privileged \
		pod-security.kubernetes.io/audit-version=latest \
		pod-security.kubernetes.io/warn=privileged \
		pod-security.kubernetes.io/warn-version=latest --overwrite
	KUBECONFIG=/home/free5gc/regional.kubeconfig helm install srsran-ue -n srsran-ue /home/free5gc/srsran-helm/charts/ue
ue1:
	KUBECONFIG=/home/free5gc/regional.kubeconfig kubectl get namespace srsran-ue 2>/dev/null || KUBECONFIG=/home/free5gc/regional.kubeconfig kubectl create namespace srsran-ue
	KUBECONFIG=/home/free5gc/regional.kubeconfig kubectl label namespace srsran-ue \
		pod-security.kubernetes.io/enforce=privileged \
		pod-security.kubernetes.io/enforce-version=latest \
		pod-security.kubernetes.io/audit=privileged \
		pod-security.kubernetes.io/audit-version=latest \
		pod-security.kubernetes.io/warn=privileged \
		pod-security.kubernetes.io/warn-version=latest --overwrite
	KUBECONFIG=/home/free5gc/regional.kubeconfig helm upgrade --install ue1 -n srsran-ue /home/free5gc/srsran-helm/charts/ue -f /home/free5gc/srsran-helm/charts/ue/values_ue1.yaml
ue2:
	KUBECONFIG=/home/free5gc/regional.kubeconfig kubectl get namespace srsran-ue 2>/dev/null || KUBECONFIG=/home/free5gc/regional.kubeconfig kubectl create namespace srsran-ue
	KUBECONFIG=/home/free5gc/regional.kubeconfig kubectl label namespace srsran-ue \
		pod-security.kubernetes.io/enforce=privileged \
		pod-security.kubernetes.io/enforce-version=latest \
		pod-security.kubernetes.io/audit=privileged \
		pod-security.kubernetes.io/audit-version=latest \
		pod-security.kubernetes.io/warn=privileged \
		pod-security.kubernetes.io/warn-version=latest --overwrite
	KUBECONFIG=/home/free5gc/regional.kubeconfig helm upgrade --install ue2 -n srsran-ue /home/free5gc/srsran-helm/charts/ue -f /home/free5gc/srsran-helm/charts/ue/values_ue2.yaml
gnu:
	grcc multi_ue_scenario.grc -o ./charts/gnu-breaker/files/
	@test -f ./charts/gnu-breaker/files/multi_ue_scenario.py
	KUBECONFIG=/home/free5gc/regional.kubeconfig kubectl get namespace srsran-gnu 2>/dev/null || KUBECONFIG=/home/free5gc/regional.kubeconfig kubectl create namespace srsran-gnu
	KUBECONFIG=/home/free5gc/regional.kubeconfig kubectl label namespace srsran-gnu \
		pod-security.kubernetes.io/enforce=privileged \
		pod-security.kubernetes.io/enforce-version=latest \
		pod-security.kubernetes.io/audit=privileged \
		pod-security.kubernetes.io/audit-version=latest \
		pod-security.kubernetes.io/warn=privileged \
		pod-security.kubernetes.io/warn-version=latest --overwrite
	KUBECONFIG=/home/free5gc/regional.kubeconfig helm install srsran-gnu -n srsran-gnu /home/free5gc/srsran-helm/charts/gnu-breaker
gnb-ue:
	make cp
	sleep 3
	make up
	sleep 3
	make du
	sleep 20
	make ue
uninstall-free5gc:
	helm uninstall free5gc-v1 -n free5gc
uninstall-cp:
	helm uninstall srsran-cucp -n free5gc
uninstall-up:
	helm uninstall srsran-cuup -n free5gc
uninstall-du:
	helm uninstall srsran-du -n free5gc
uninstall-ue:
	KUBECONFIG=/home/free5gc/regional.kubeconfig helm uninstall srsran-ue -n srsran-ue
uninstall-ue1:
	KUBECONFIG=/home/free5gc/regional.kubeconfig helm uninstall ue1 -n srsran-ue
uninstall-ue2:
	KUBECONFIG=/home/free5gc/regional.kubeconfig helm uninstall ue2 -n srsran-ue
uninstall-gnb:
	make uninstall-du
	sleep 3
	make uninstall-up
	sleep 3
	make uninstall-cp
uninstall-all:
	helm uninstall srsran-ue -n free5gc && sleep 2 && helm uninstall srsran-du -n free5gc && sleep 2 && helm uninstall srsran-cuup -n free5gc && sleep 2 && helm uninstall srsran-cucp -n free5gc
uninstall-gnu:
	KUBECONFIG=/home/free5gc/regional.kubeconfig helm uninstall srsran-gnu -n srsran-gnu

# iperf3 client tests from UE pods
# Requires iperf3 installed in Kind worker: docker exec $KIND_WORKER apt-get install -y iperf3
KIND_WORKER ?= regional-md-0-7hcxb-z4qv6-q6r67

.PHONY: iperf-ue1 iperf-ue2
iperf-ue1: ## Run iperf3 client from UE1 network namespace to 10.0.1.254
	@UE1_CID=$$(sudo docker exec $(KIND_WORKER) crictl ps | grep 'srsran-ue1-' | awk '{print $$1}') && \
	UE1_PID=$$(sudo docker exec $(KIND_WORKER) crictl inspect $$UE1_CID | grep '"pid":' | head -1 | tr -dc '0-9') && \
	sudo docker exec $(KIND_WORKER) nsenter -t $$UE1_PID -n iperf3 --bind-dev uesimtun0 -c 10.0.1.254 -i 1 -t 60 -u -b 1.5M --get-server-output

iperf-ue2: ## Run iperf3 client from UE2 network namespace to 10.0.1.253
	@UE2_CID=$$(sudo docker exec $(KIND_WORKER) crictl ps | grep 'srsran-ue2-' | awk '{print $$1}') && \
	UE2_PID=$$(sudo docker exec $(KIND_WORKER) crictl inspect $$UE2_CID | grep '"pid":' | head -1 | tr -dc '0-9') && \
	sudo docker exec $(KIND_WORKER) nsenter -t $$UE2_PID -n iperf3 --bind-dev uesimtun0 -c 10.0.1.253 -i 1 -t 30 -u -b 1.5M --get-server-output

check-log:
	cd /home/free5gc/srsRAN_Project_helm && microk8s helm install srsran-cucp ./charts/cucp -n free5gc && sleep 2 && microk8s helm install srsran-cuup ./charts/cuup -n free5gc && sleep 2 && microk8s helm install srsran-du ./charts/du -n free5gc && sleep 10 && microk8s helm uninstall srsran-cucp srsran-cuup srsran-du -n free5gc
tmp:
	iperf3 -s -B 10.0.2.13
	ip link set dev tun_srsue mtu 1350
	iperf3 --bind-dev tun_srsue -c 10.0.2.13 -u -b 20M -t 10