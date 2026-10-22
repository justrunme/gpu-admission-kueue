package policy

import (
	"github.com/justrunme/gpu-admission-kueue/api/v1alpha1"
	"github.com/justrunme/gpu-admission-kueue/internal/workloadinfo"
)

// Scenario is one corpus case used by unit tests and evidence generation.
type Scenario struct {
	ID       string
	Policy   *v1alpha1.GPUAdmissionPolicySpec
	Input    workloadinfo.Input
	Usage    workloadinfo.UsageSnapshot
	Expected string // ALLOW | DENY | HOLD
}

// Corpus returns the fixed scenario set for this repository.
func Corpus() []Scenario {
	maxHours := 8.0
	price := int64(1000000)
	budget := int64(4000000)
	base := &v1alpha1.GPUAdmissionPolicySpec{
		Version:                "gpu-policy-1.0",
		RequiredLabels:         []string{"cost-center", "owner"},
		AllowedFlavors:         []string{"gpu-a100", "gpu-t4"},
		AllowedGPUModels:       []string{"A100", "T4"},
		AllowedImageRegistries: []string{"registry.example.com/", "ghcr.io/acme/"},
		NamespaceGPUQuota:      map[string]int64{"team-a": 4, "team-b": 2},
		TeamGPUQuota:           map[string]int64{"alpha": 4, "beta": 2},
		TeamLabelKey:           "team",
		MaxGPUHoursPerWorkload: &maxHours,
		MaxBudgetUSDMicros:     &budget,
		GPUHourPriceMicros:     &price,
		HoldOnQuotaExhausted:   true,
	}
	allowIn := workloadinfo.Input{
		Namespace: "team-a", Name: "job-ok",
		Labels:       map[string]string{"cost-center": "cc-100", "owner": "alice", "team": "alpha"},
		Images:       []string{"registry.example.com/ml/train:1.0"},
		Flavors:      []string{"gpu-a100"},
		GPUModels:    []string{"A100"},
		GPURequest:   1,
		EstimatedHrs: 2, HasEstHours: true,
	}
	malformed := *base
	malformed.Version = ""
	overQuotaUsage := workloadinfo.NewUsageSnapshot()
	overQuotaUsage.ByNamespace["team-a"] = 4
	noHold := *base
	noHold.HoldOnQuotaExhausted = false

	missingOwner := allowIn
	missingOwner.Name = "job-missing-owner"
	missingOwner.Labels = map[string]string{"cost-center": "cc-100", "team": "alpha"}

	badFlavor := allowIn
	badFlavor.Name = "job-bad-flavor"
	badFlavor.Flavors = []string{"gpu-h100"}

	badReg := allowIn
	badReg.Name = "job-bad-registry"
	badReg.Images = []string{"evil.example/x:latest"}

	// Prefix path escape: allowlist "ghcr.io/acme/" must not admit "ghcr.io/evil/...".
	ghcrEvil := allowIn
	ghcrEvil.Name = "job-ghcr-evil"
	ghcrEvil.Images = []string{"ghcr.io/evil/x:latest"}

	overHours := allowIn
	overHours.Name = "job-over-hours"
	overHours.EstimatedHrs = 20

	overBudget := allowIn
	overBudget.Name = "job-over-budget"
	overBudget.GPURequest = 2
	overBudget.EstimatedHrs = 3

	noHours := allowIn
	noHours.Name = "job-no-hours"
	noHours.HasEstHours = false

	unknownNS := allowIn
	unknownNS.Name = "job-unknown-ns"
	unknownNS.Namespace = "unknown-ns"

	return []Scenario{
		{ID: "allow_golden", Policy: base, Input: allowIn, Usage: workloadinfo.NewUsageSnapshot(), Expected: DecisionAllow},
		{ID: "nil_policy_fail_closed", Policy: nil, Input: allowIn, Usage: workloadinfo.NewUsageSnapshot(), Expected: DecisionDeny},
		{ID: "malformed_policy_empty_version", Policy: &malformed, Input: allowIn, Usage: workloadinfo.NewUsageSnapshot(), Expected: DecisionDeny},
		{ID: "missing_required_label_owner", Policy: base, Input: missingOwner, Usage: workloadinfo.NewUsageSnapshot(), Expected: DecisionDeny},
		{ID: "disallowed_flavor", Policy: base, Input: badFlavor, Usage: workloadinfo.NewUsageSnapshot(), Expected: DecisionDeny},
		{ID: "over_namespace_quota_hold", Policy: base, Input: allowIn, Usage: overQuotaUsage, Expected: DecisionHold},
		{ID: "over_namespace_quota_deny", Policy: &noHold, Input: allowIn, Usage: overQuotaUsage, Expected: DecisionDeny},
		{ID: "disallowed_image_registry", Policy: base, Input: badReg, Usage: workloadinfo.NewUsageSnapshot(), Expected: DecisionDeny},
		{ID: "disallowed_ghcr_evil_prefix", Policy: base, Input: ghcrEvil, Usage: workloadinfo.NewUsageSnapshot(), Expected: DecisionDeny},
		{ID: "over_gpu_hours", Policy: base, Input: overHours, Usage: workloadinfo.NewUsageSnapshot(), Expected: DecisionDeny},
		{ID: "over_budget", Policy: base, Input: overBudget, Usage: workloadinfo.NewUsageSnapshot(), Expected: DecisionDeny},
		{ID: "missing_estimated_hours_fail_closed", Policy: base, Input: noHours, Usage: workloadinfo.NewUsageSnapshot(), Expected: DecisionDeny},
		{ID: "namespace_not_in_quota_map", Policy: base, Input: unknownNS, Usage: workloadinfo.NewUsageSnapshot(), Expected: DecisionDeny},
	}
}
