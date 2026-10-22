#!/usr/bin/env bash
# Advertise simulated nvidia.com/gpu capacity on kind nodes.
# This does NOT attach real GPUs; it only patches Node status.allocatable/capacity.
set -euo pipefail

echo "Patching kind nodes to advertise nvidia.com/gpu (simulated extended resource)..."
for node in $(kubectl get nodes -o jsonpath='{.items[*].metadata.name}'); do
  flavor=$(kubectl get node "$node" -o jsonpath='{.metadata.labels.gpu-admission\.justrunme\.com/gpu-flavor}')
  count=2
  if [[ "$flavor" == "h100-sim" ]]; then
    count=1
  fi
  echo "  node=$node flavor=${flavor:-none} gpu=$count"
  kubectl patch node "$node" --subresource=status --type=merge -p "{\"status\":{\"capacity\":{\"nvidia.com/gpu\":\"${count}\"},\"allocatable\":{\"nvidia.com/gpu\":\"${count}\"}}}"
done
kubectl get nodes -o custom-columns=NAME:.metadata.name,GPU:.status.allocatable.nvidia\\.com/gpu,FLAVOR:.metadata.labels.gpu-admission\\.justrunme\\.com/gpu-flavor
