# Evidence

This file is **generated** from `results/results.json` by `go run ./cmd/evidencegen`.
Do not edit numbers by hand; re-run unit/e2e and regenerate.

All claims below are **on this corpus of 12 scenarios** only. They do not imply general cluster safety.

## Run metrics

- Scope: on this corpus of 12 scenarios
- Generated at: `2026-10-22T09:02:39Z`
- Commit: `unknown`
- Corpus passed: `true`
- Suite time (ms): `1417`
- Scenario count: `12`
- ALLOW: `1` · DENY: `10` · HOLD: `1`
- False-ALLOW count: `0` (must be 0)
- False-DENY (legitimate) count: `0`
- Unit tests: passed `26`, failed `0`
- E2E: skipped (`unit corpus only; e2e recorded separately by test/e2e when run`)

## Per-scenario table

| Scenario | Expected | Actual | Match | Reason |
|---|---|---|---|---|
| `allow_golden` | ALLOW | ALLOW | yes | policy_allow |
| `nil_policy_fail_closed` | DENY | DENY | yes | malformed_policy: policy is nil |
| `malformed_policy_empty_version` | DENY | DENY | yes | malformed_policy: policy.spec.version is empty |
| `missing_required_label_owner` | DENY | DENY | yes | missing_required_label: missing required label "owner" |
| `disallowed_flavor` | DENY | DENY | yes | disallowed_flavor: flavor "gpu-h100" is not in allowedFlavors |
| `over_namespace_quota_hold` | HOLD | HOLD | yes | over_namespace_quota: namespace "team-a" GPU quota exceeded: used=4 request=1 limit=4 |
| `over_namespace_quota_deny` | DENY | DENY | yes | over_namespace_quota: namespace "team-a" GPU quota exceeded: used=4 request=1 limit=4 |
| `disallowed_image_registry` | DENY | DENY | yes | disallowed_image_registry: image registry "evil.example/" is not allowed |
| `over_gpu_hours` | DENY | DENY | yes | over_gpu_hours: GPU-hours 20.00 exceed max 8.00 |
| `over_budget` | DENY | DENY | yes | over_budget: estimated cost 6000000 micro-USD exceeds budget 4000000 |
| `missing_estimated_hours_fail_closed` | DENY | DENY | yes | undecidable: maxGPUHoursPerWorkload set but estimated-hours annotation missing or invalid |
| `namespace_not_in_quota_map` | DENY | DENY | yes | namespace_not_in_quota_map: namespace "unknown-ns" has no GPU quota entry |

## Notes

- Claims apply **on this corpus** only.
- Kind demo advertises `nvidia.com/gpu` as an extended resource; it does not require physical GPUs.
- Evidence envelopes use ASC-compatible `schema_version` `1.0.0` with `details.decision` and `details.inputs_digest`.
- Renderer wall clock: `2026-10-22T09:02:39Z`

