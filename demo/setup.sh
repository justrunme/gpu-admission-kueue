#!/usr/bin/env bash
# Reproducible kind + upstream Kueue + GPU admission controller demo.
# GPUs are simulated as the nvidia.com/gpu extended resource on kind nodes.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

KUEUE_VERSION="${KUEUE_VERSION:-v0.11.6}"
CLUSTER_NAME="${CLUSTER_NAME:-gpu-admission}"
export PATH="${HOME}/bin:/usr/local/go/bin:${PATH}"

echo "==> Creating kind cluster ${CLUSTER_NAME} (if needed)"
if ! kind get clusters 2>/dev/null | grep -qx "${CLUSTER_NAME}"; then
  kind create cluster --config demo/kind-config.yaml
else
  echo "    cluster already exists"
  kubectl cluster-info --context "kind-${CLUSTER_NAME}" >/dev/null
fi
kubectl config use-context "kind-${CLUSTER_NAME}"

echo "==> Simulating GPU capacity on nodes"
bash demo/patch-gpu-capacity.sh

echo "==> Installing upstream Kueue ${KUEUE_VERSION}"
KUEUE_IMAGE="registry.k8s.io/kueue/kueue:${KUEUE_VERSION}"
# kind nodes often cannot reach registry.k8s.io; pull on the host and import via ctr.
docker pull "${KUEUE_IMAGE}"
KUEUE_TAR="$(mktemp /tmp/kueue-XXXXXX.tar)"
docker save -o "${KUEUE_TAR}" "${KUEUE_IMAGE}"
for node in $(kind get nodes --name "${CLUSTER_NAME}"); do
  echo "    importing ${KUEUE_IMAGE} into ${node}"
  docker exec -i "${node}" ctr -n k8s.io images import - <"${KUEUE_TAR}"
done
rm -f "${KUEUE_TAR}"
kubectl apply --server-side -f "https://github.com/kubernetes-sigs/kueue/releases/download/${KUEUE_VERSION}/manifests.yaml"
kubectl -n kueue-system patch deploy kueue-controller-manager --type=json \
  -p='[{"op":"replace","path":"/spec/template/spec/containers/0/imagePullPolicy","value":"IfNotPresent"}]' || true
kubectl -n kueue-system wait --for=condition=available --timeout=300s deployment/kueue-controller-manager

echo "==> Installing GPU admission CRD/RBAC/policy"
kubectl apply -f config/manager/namespace.yaml
kubectl apply -f config/crd/bases/gpu-admission.justrunme.com_gpuadmissionpolicies.yaml
kubectl apply -f config/rbac/role.yaml
kubectl apply -f config/samples/gpuadmissionpolicy.yaml

echo "==> Building and loading controller image"
docker pull gcr.io/distroless/static:nonroot || true
docker build -t gpu-admission-controller:e2e .
CTRL_TAR="$(mktemp /tmp/gactrl-XXXXXX.tar)"
docker save -o "${CTRL_TAR}" gpu-admission-controller:e2e
for node in $(kind get nodes --name "${CLUSTER_NAME}"); do
  echo "    importing gpu-admission-controller:e2e into ${node}"
  docker exec -i "${node}" ctr -n k8s.io images import - <"${CTRL_TAR}"
done
rm -f "${CTRL_TAR}"
kubectl apply -f config/manager/deployment.yaml
kubectl -n gpu-admission-system wait --for=condition=available --timeout=180s deployment/gpu-admission-controller

echo "==> Applying Kueue queues / flavors / AdmissionCheck"
kubectl apply -f config/kueue/resourceflavor.yaml
kubectl apply -f config/kueue/admissioncheck.yaml
kubectl apply -f config/kueue/clusterqueue.yaml
kubectl apply -f config/kueue/localqueue.yaml

echo "==> Waiting for AdmissionCheck Active"
kubectl wait --for=condition=Active --timeout=120s admissioncheck/gpu-admission

echo "Demo cluster ready. Submit jobs from demo/jobs/ (see README)."
