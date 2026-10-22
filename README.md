# GPU Admission on Kueue

Fail-closed GPU admission for AI workloads on **upstream** [Kueue](https://kueue.sigs.k8s.io/) (ClusterQueue / LocalQueue / ResourceFlavor + AdmissionChecks). Not a Kueue fork.

Part of the **Secure and Auditable AI Infrastructure on Kubernetes** track.

Evidence: [docs/EVIDENCE.md](docs/EVIDENCE.md) (claims **on this corpus** only)

Maintainer site: [https://justrunme.com](https://justrunme.com)

## What

A controller that owns an AdmissionCheck (`controllerName: gpu-admission.justrunme.com/admission-check`). After Kueue reserves quota, this controller evaluates a cluster-scoped `GPUAdmissionPolicy` and sets the Workload AdmissionCheck state to:

| Decision | Kueue check state | When |
| --- | --- | --- |
| ALLOW | `Ready` | Policy satisfied |
| HOLD | `Retry` | Soft quota pressure (`holdOnQuotaExhausted`) |
| DENY | `Rejected` | Hard violation or undecidable input |

Every decision emits an ASC-compatible evidence envelope (`schema_version: 1.0.0`) with `verdict`, `reason`, `policy_version`, `timestamp`, plus `details.decision` and `details.inputs_digest`. Envelopes are stored as ConfigMaps in `gpu-admission-system`.

## Why

GPU AI jobs need auditable, fail-closed gates beyond raw ClusterQueue quota: team/namespace caps, allowed flavors/models, required FinOps labels, GPU-hour / budget ceilings, and image registry allowlists. Using Kueue's AdmissionCheck API keeps the control plane upstream-friendly and leaves scheduling to Kueue.

**Fail-closed defaults:**

- Controller down → checks stay `Pending` → Workloads are not Admitted
- Malformed / missing policy → `Rejected`
- Cannot decide (missing annotations required by a rule, unknown flavor/model, etc.) → `Rejected` / DENY

## Policy examples

See [`config/samples/gpuadmissionpolicy.yaml`](config/samples/gpuadmissionpolicy.yaml):

- GPU quota per namespace and per team label
- Allowed ResourceFlavor / GPU model
- Required labels (`cost-center`, `owner`)
- Max GPU-hours and micro-USD budget (needs `gpu-admission.justrunme.com/estimated-hours`)
- Allowed image registry prefixes

## Quickstart

Requirements: Go 1.23+, Docker, [kind](https://kind.sigs.k8s.io/), kubectl.

```bash
# Unit tests + regenerate corpus evidence
make corpus

# Kind demo with real upstream Kueue (GPUs are simulated — see below)
make demo

# Submit an allowed job
kubectl apply -f demo/jobs/allow-job.yaml

# Inspect AdmissionCheck state on the Kueue Workload
kubectl -n team-a get workloads -o yaml

# Inspect evidence envelope
kubectl -n gpu-admission-system get configmaps -l gpu-admission.justrunme.com/evidence -o yaml
```

Full suite (`allow_golden` plus negatives: controller down, malformed policy, over quota, missing labels, disallowed flavor, bad registry, `ghcr.io/evil` prefix escape):

```bash
make e2e
```

Default e2e path is `demo/setup.sh` plus the in-cluster Deployment from `config/manager` (what CI runs). Nested-VM hostNetwork / host-process workarounds are opt-in only via `GPU_ADMISSION_KIND_WORKAROUNDS=1` and must never be set in CI.

### Simulated GPUs (honest)

The kind demo **does not use physical GPUs**. Script [`demo/patch-gpu-capacity.sh`](demo/patch-gpu-capacity.sh) patches Node `status.capacity` / `allocatable` so `nvidia.com/gpu` appears as an extended resource, and ResourceFlavors carry simulated model labels (`A100-SIM`). Admission and queuing behavior are real (upstream Kueue + this controller); device plugins, drivers, and CUDA are out of scope.

Allowed demo images (`registry.example.com/...`) will typically `ImagePullBackOff` if a Pod is unsuspended — admission checks are validated on Workload AdmissionCheck state, not on successful image pull.

## Architecture

```text
Job (suspend) → Kueue Workload → ClusterQueue quota reservation
                                      ↓
                         AdmissionCheck gpu-admission (Pending)
                                      ↓
                         GPU admission controller + GPUAdmissionPolicy
                                      ↓
                         Ready | Retry | Rejected + evidence ConfigMap
```

Pinned demo Kueue release: **v0.11.6** (override with `KUEUE_VERSION`).

## Development

```bash
make tidy vet test
make build
go run ./cmd/corpus && go run ./cmd/evidencegen
```

## License

Apache-2.0. See [LICENSE](LICENSE).

## Security

See [SECURITY.md](SECURITY.md).

## Limitations (honest)

- Claims in [`docs/EVIDENCE.md`](docs/EVIDENCE.md) apply **on this corpus** only.
- Kind demo simulates `nvidia.com/gpu` via Node capacity patches — no device plugin, driver, or CUDA.
- Allowed demo images will typically `ImagePullBackOff`; admission is asserted on Workload AdmissionCheck state.
- After a `Rejected` check, upstream Kueue deactivates the Workload and may reset the check message to `Previously: Rejected` while `spec.active=false`. E2E treats that as an effective reject.
- On some nested kind environments, Service ClusterIP routing from pods/apiserver fails. Opt in with `GPU_ADMISSION_KIND_WORKAROUNDS=1` to run `hack/kind-network-workarounds.sh` + `hack/run-controller-host.sh`. CI never sets that flag and always uses the in-cluster Deployment.
