#!/usr/bin/env bash
# E2E on kind with real upstream Kueue. GPUs are simulated extended resources.
#
# Default path (CI): demo/setup.sh + in-cluster Deployment from config/manager.
# Optional nested-VM workarounds: GPU_ADMISSION_KIND_WORKAROUNDS=1 (never set in CI).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
export PATH="${HOME}/bin:/usr/local/go/bin:${PATH}"

CLUSTER_NAME="${CLUSTER_NAME:-gpu-admission}"
RESULTS_E2E="results/e2e.json"
HELPER="$ROOT/test/e2e/workload_check_state.py"
NOTES="kind cluster; nvidia.com/gpu is a simulated extended resource, not a physical GPU"
mkdir -p results

log() { printf '%s\n' "$*"; }
TMP_PASSES="$(mktemp)"; TMP_FAILS="$(mktemp)"
trap 'rm -f "$TMP_PASSES" "$TMP_FAILS"' EXIT
pass() { log "PASS: $*"; echo "$1" >>"$TMP_PASSES"; }
fail() { log "FAIL: $*"; echo "$1" >>"$TMP_FAILS"; }

bash demo/setup.sh

if [[ "${GPU_ADMISSION_KIND_WORKAROUNDS:-}" == "1" ]]; then
  log "GPU_ADMISSION_KIND_WORKAROUNDS=1: applying opt-in nested-VM workarounds"
  bash hack/kind-network-workarounds.sh
  bash hack/run-controller-host.sh &
  HOST_CTRL_PID=$!
  trap 'rm -f "$TMP_PASSES" "$TMP_FAILS"; kill '"${HOST_CTRL_PID}"' 2>/dev/null || true' EXIT
  sleep 4
else
  kubectl -n gpu-admission-system wait --for=condition=available --timeout=180s deployment/gpu-admission-controller
fi

kubectl wait --for=condition=Active --timeout=120s admissioncheck/gpu-admission

dump_e2e_diagnostics() {
  local ns="${1:-team-a}"
  log "---- e2e diagnostics (ns=$ns) ----"
  kubectl -n gpu-admission-system get pods -o wide 2>/dev/null || true
  kubectl -n gpu-admission-system logs -l app.kubernetes.io/name=gpu-admission-controller --tail=80 2>/dev/null || true
  kubectl get admissioncheck gpu-admission -o yaml 2>/dev/null || true
  kubectl get clusterqueue gpu-cluster-queue -o yaml 2>/dev/null || true
  kubectl -n "$ns" get workloads -o yaml 2>/dev/null || true
  kubectl -n kueue-system get pods -o wide 2>/dev/null || true
  log "---- end diagnostics ----"
}

wait_workload_check_state() {
  local ns="$1" job="$2" want="$3" timeout="${4:-120}"
  local end=$((SECONDS + timeout)) state=""
  while (( SECONDS < end )); do
    state="$(kubectl -n "$ns" get workloads -o json 2>/dev/null | python3 "$HELPER" "$job" gpu-admission || true)"
    if [[ "$state" == "$want" ]]; then printf '%s\n' "$state"; return 0; fi
    if [[ "$want" == "Rejected" && "$state" == "REJECTED_EFFECTIVE" ]]; then printf '%s\n' "$state"; return 0; fi
    sleep 2
  done
  dump_e2e_diagnostics "$ns"
  printf 'timeout want=%s last=%s\n' "$want" "${state:-none}"
  return 1
}

cleanup_jobs() {
  kubectl -n team-a delete job --all --wait=false 2>/dev/null || true
  kubectl -n team-b delete job --all --wait=false 2>/dev/null || true
  sleep 3
}

cleanup_jobs
kubectl apply -f demo/jobs/allow-job.yaml
if wait_workload_check_state team-a allow-train Ready 120 >/tmp/e2e-state.txt; then pass allow_golden; else fail "allow_golden:$(cat /tmp/e2e-state.txt)"; fi

cleanup_jobs
kubectl apply -f demo/jobs/missing-labels-job.yaml
if wait_workload_check_state team-a missing-labels Rejected 120 >/tmp/e2e-state.txt; then pass missing_labels; else fail "missing_labels:$(cat /tmp/e2e-state.txt)"; fi

cleanup_jobs
kubectl apply -f demo/jobs/disallowed-flavor-job.yaml
if wait_workload_check_state team-a disallowed-flavor Rejected 120 >/tmp/e2e-state.txt; then pass disallowed_flavor; else fail "disallowed_flavor:$(cat /tmp/e2e-state.txt)"; fi

cleanup_jobs
kubectl apply -f demo/jobs/bad-registry-job.yaml
if wait_workload_check_state team-a bad-registry Rejected 120 >/tmp/e2e-state.txt; then pass bad_registry; else fail "bad_registry:$(cat /tmp/e2e-state.txt)"; fi

cleanup_jobs
cat <<'YAML' | kubectl apply -f -
apiVersion: batch/v1
kind: Job
metadata:
  name: prime-a
  namespace: team-a
  labels: {kueue.x-k8s.io/queue-name: gpu-lq, cost-center: cc-100, owner: alice, team: alpha}
  annotations: {gpu-admission.justrunme.com/estimated-hours: "1"}
spec:
  suspend: true
  template:
    metadata:
      labels: {cost-center: cc-100, owner: alice, team: alpha}
      annotations: {gpu-admission.justrunme.com/estimated-hours: "1"}
    spec:
      restartPolicy: Never
      nodeSelector: {gpu-admission.justrunme.com/gpu-flavor: a100-sim}
      containers:
      - name: train
        image: registry.example.com/ml/train:1.0
        command: ["sh","-c","sleep 600"]
        resources:
          requests: {cpu: 50m, memory: 64Mi, nvidia.com/gpu: "1"}
          limits: {nvidia.com/gpu: "1"}
---
apiVersion: batch/v1
kind: Job
metadata:
  name: prime-b
  namespace: team-a
  labels: {kueue.x-k8s.io/queue-name: gpu-lq, cost-center: cc-100, owner: bob, team: alpha}
  annotations: {gpu-admission.justrunme.com/estimated-hours: "1"}
spec:
  suspend: true
  template:
    metadata:
      labels: {cost-center: cc-100, owner: bob, team: alpha}
      annotations: {gpu-admission.justrunme.com/estimated-hours: "1"}
    spec:
      restartPolicy: Never
      nodeSelector: {gpu-admission.justrunme.com/gpu-flavor: a100-sim}
      containers:
      - name: train
        image: registry.example.com/ml/train:1.0
        command: ["sh","-c","sleep 600"]
        resources:
          requests: {cpu: 50m, memory: 64Mi, nvidia.com/gpu: "1"}
          limits: {nvidia.com/gpu: "1"}
YAML
wait_workload_check_state team-a prime-a Ready 120 >/dev/null || true
wait_workload_check_state team-a prime-b Ready 120 >/dev/null || true
kubectl apply -f demo/jobs/over-quota-job.yaml
if wait_workload_check_state team-a over-quota Retry 120 >/tmp/e2e-state.txt; then pass over_quota_hold; else fail "over_quota_hold:$(cat /tmp/e2e-state.txt)"; fi

cleanup_jobs
kubectl apply -f - <<'YAML'
apiVersion: gpu-admission.justrunme.com/v1alpha1
kind: GPUAdmissionPolicy
metadata: {name: default}
spec:
  version: broken-budget-1
  requiredLabels: ["cost-center", "owner"]
  allowedFlavors: ["gpu-a100-sim"]
  allowedGPUModels: ["A100-SIM"]
  allowedImageRegistries: ["registry.example.com/"]
  namespaceGPUQuota: {team-a: 2}
  maxBudgetUSDMicros: 1000
YAML
kubectl apply -f demo/jobs/allow-job.yaml
if wait_workload_check_state team-a allow-train Rejected 120 >/tmp/e2e-state.txt; then pass malformed_policy; else fail "malformed_policy:$(cat /tmp/e2e-state.txt)"; fi
kubectl apply -f config/samples/gpuadmissionpolicy.yaml

# Fail-closed when the in-cluster controller is down (standard path).
cleanup_jobs
if [[ "${GPU_ADMISSION_KIND_WORKAROUNDS:-}" == "1" ]]; then
  kill "${HOST_CTRL_PID}" 2>/dev/null || true
  wait "${HOST_CTRL_PID}" 2>/dev/null || true
else
  kubectl -n gpu-admission-system scale deployment/gpu-admission-controller --replicas=0
  kubectl -n gpu-admission-system wait --for=delete pod -l app.kubernetes.io/name=gpu-admission-controller --timeout=60s || true
fi
kubectl apply -f demo/jobs/allow-job.yaml
sleep 20
state="$(kubectl -n team-a get workloads -o json | python3 "$HELPER" allow-train gpu-admission || true)"
admitted="$(kubectl -n team-a get workloads -o json | python3 -c 'import json,sys; d=json.load(sys.stdin); print(any(c.get("type")=="Admitted" and c.get("status")=="True" for w in d.get("items",[]) for c in w.get("status",{}).get("conditions") or []))')"
if [[ "$admitted" == "True" || "$state" == "Ready" ]]; then
  fail "controller_down_fail_closed:state=$state admitted=$admitted"
else
  pass controller_down_fail_closed
fi

pass_n=$(wc -l <"$TMP_PASSES" | tr -d ' ')
fail_n=$(wc -l <"$TMP_FAILS" | tr -d ' ')
e2e_passed=true
[[ "$fail_n" == "0" ]] || e2e_passed=false

# Evidence SHA: GITHUB_SHA in CI, otherwise unknown (or dirty via ResolveCommitSHA semantics in Go tools).
# This file is written only by this runner — never hand-edit.
python3 - "$RESULTS_E2E" "$e2e_passed" "$pass_n" "$fail_n" "$TMP_PASSES" "$TMP_FAILS" "$NOTES" <<'PY'
import json, sys, os, datetime
out, passed, pass_n, fail_n, pf, ff, notes = sys.argv[1:8]
passes = open(pf).read().splitlines()
fails = open(ff).read().splitlines()
commit = os.environ.get("GITHUB_SHA") or "unknown"
doc = {
    "generated_at": datetime.datetime.now(datetime.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z"),
    "commit_sha": commit,
    "passed": passed in ("True", "true"),
    "pass_count": int(pass_n),
    "fail_count": int(fail_n),
    "passes": passes,
    "fails": fails,
    "notes": notes,
}
open(out, "w").write(json.dumps(doc, indent=2) + "\n")
print(json.dumps(doc, indent=2))
PY

[[ "$e2e_passed" == "true" ]] || { log "E2E failed ($fail_n failures)"; exit 1; }
log "E2E passed ($pass_n checks)"
