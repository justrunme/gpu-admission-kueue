#!/usr/bin/env bash
# OPTIONAL: run the GPU admission controller as a host process against kind kubeconfig.
# Opt-in only with GPU_ADMISSION_KIND_WORKAROUNDS=1. CI uses the in-cluster Deployment.
set -euo pipefail

if [[ "${GPU_ADMISSION_KIND_WORKAROUNDS:-}" != "1" ]]; then
  echo "refusing to run: set GPU_ADMISSION_KIND_WORKAROUNDS=1 to opt in" >&2
  exit 2
fi

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
export PATH="${HOME}/bin:/usr/local/go/bin:${PATH}"
CLUSTER_NAME="${CLUSTER_NAME:-gpu-admission}"
kind export kubeconfig --name "${CLUSTER_NAME}"

if kubectl -n gpu-admission-system get deploy gpu-admission-controller >/dev/null 2>&1; then
  kubectl -n gpu-admission-system scale deploy/gpu-admission-controller --replicas=0 || true
fi

mkdir -p bin
CGO_ENABLED=0 go build -o bin/controller ./cmd/controller
exec ./bin/controller \
  --admission-check-name=gpu-admission \
  --health-probe-bind-address=:18081 \
  --metrics-bind-address=:18080
