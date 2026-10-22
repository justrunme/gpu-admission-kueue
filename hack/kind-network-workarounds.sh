#!/usr/bin/env bash
# OPTIONAL nested-VM workarounds when kind Service ClusterIP routing is broken.
# Opt-in only: set GPU_ADMISSION_KIND_WORKAROUNDS=1. CI must never enable this.
# Does not change Kueue webhook failurePolicy (remains Fail / upstream default).
set -euo pipefail

if [[ "${GPU_ADMISSION_KIND_WORKAROUNDS:-}" != "1" ]]; then
  echo "refusing to run: set GPU_ADMISSION_KIND_WORKAROUNDS=1 to opt in" >&2
  exit 2
fi

CLUSTER_NAME="${CLUSTER_NAME:-gpu-admission}"
CP_NODE="${CLUSTER_NAME}-control-plane"
CP_IP="$(docker inspect -f '{{range.NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "${CP_NODE}")"

echo "Applying opt-in kind network workarounds (control-plane IP ${CP_IP})"

kubectl -n kueue-system patch deploy kueue-controller-manager --type=strategic -p "{
  \"spec\": {
    \"replicas\": 1,
    \"template\": {
      \"spec\": {
        \"hostNetwork\": true,
        \"dnsPolicy\": \"ClusterFirstWithHostNet\",
        \"tolerations\": [{\"key\":\"node-role.kubernetes.io/control-plane\",\"operator\":\"Exists\",\"effect\":\"NoSchedule\"}],
        \"nodeSelector\": {\"node-role.kubernetes.io/control-plane\": \"\"},
        \"containers\": [{\"name\": \"manager\", \"imagePullPolicy\": \"IfNotPresent\"}]
      }
    }
  }
}" >/dev/null
kubectl -n kueue-system rollout status deploy/kueue-controller-manager --timeout=180s

# Retarget webhook clientConfigs to the hostNetwork listener. Do NOT set failurePolicy: Ignore.
python3 - <<PY
import json, subprocess
cp = "${CP_IP}"
for kind in ["mutatingwebhookconfiguration", "validatingwebhookconfiguration"]:
    doc = json.loads(subprocess.check_output(["kubectl", "get", kind, "-o", "json"]))
    for item in doc.get("items", []):
        name = item["metadata"]["name"]
        if "kueue" not in name:
            continue
        for wh in item.get("webhooks", []):
            cc = wh.setdefault("clientConfig", {})
            svc = cc.pop("service", None)
            path = "/"
            if svc and svc.get("path"):
                path = svc["path"]
            cc["url"] = f"https://{cp}:9443{path}"
            # Preserve upstream failurePolicy (do not force Ignore).
        meta = item["metadata"]
        for k in ("managedFields", "resourceVersion", "uid", "creationTimestamp"):
            meta.pop(k, None)
        subprocess.run(["kubectl", "delete", kind, name, "--ignore-not-found"], check=False, stdout=subprocess.DEVNULL)
        subprocess.run(["kubectl", "create", "-f", "-"], input=json.dumps(item).encode(), check=True)
        print(f"recreated {kind}/{name} -> https://{cp}:9443")
PY
