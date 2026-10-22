package policy_test

import (
	"testing"

	"github.com/justrunme/gpu-admission-kueue/api/v1alpha1"
	"github.com/justrunme/gpu-admission-kueue/internal/policy"
	"github.com/justrunme/gpu-admission-kueue/internal/workloadinfo"
)

func basePolicy() *v1alpha1.GPUAdmissionPolicySpec {
	maxHours := 8.0
	price := int64(1000000) // 1 USD / GPU-hour in micro-USD
	budget := int64(4000000)
	return &v1alpha1.GPUAdmissionPolicySpec{
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
}

func baseInput() workloadinfo.Input {
	return workloadinfo.Input{
		Namespace:    "team-a",
		Name:         "job-1",
		Labels:       map[string]string{"cost-center": "cc-100", "owner": "alice", "team": "alpha"},
		Images:       []string{"registry.example.com/ml/train:1.0"},
		Flavors:      []string{"gpu-a100"},
		GPUModels:    []string{"A100"},
		GPURequest:   1,
		EstimatedHrs: 2,
		HasEstHours:  true,
	}
}

func TestEvaluateAllow(t *testing.T) {
	d := policy.Evaluate(basePolicy(), baseInput(), workloadinfo.NewUsageSnapshot())
	if d.Verdict != policy.VerdictAllow || d.Decision != policy.DecisionAllow {
		t.Fatalf("expected ALLOW, got verdict=%s decision=%s reason=%s", d.Verdict, d.Decision, d.Reason)
	}
}

func TestEvaluateNilPolicyFailClosed(t *testing.T) {
	d := policy.Evaluate(nil, baseInput(), workloadinfo.NewUsageSnapshot())
	if d.Decision != policy.DecisionDeny || d.Verdict != policy.VerdictBlock {
		t.Fatalf("expected DENY, got %+v", d)
	}
	if d.Reason == "" || d.Reason[:16] != "malformed_policy" {
		t.Fatalf("unexpected reason %q", d.Reason)
	}
}

func TestEvaluateMalformedEmptyVersion(t *testing.T) {
	p := basePolicy()
	p.Version = ""
	d := policy.Evaluate(p, baseInput(), workloadinfo.NewUsageSnapshot())
	if d.Decision != policy.DecisionDeny {
		t.Fatalf("expected DENY, got %+v", d)
	}
}

func TestEvaluateMissingLabels(t *testing.T) {
	in := baseInput()
	delete(in.Labels, "owner")
	d := policy.Evaluate(basePolicy(), in, workloadinfo.NewUsageSnapshot())
	if d.Decision != policy.DecisionDeny {
		t.Fatalf("expected DENY, got %+v", d)
	}
	if want := "missing_required_label"; len(d.Reason) < len(want) || d.Reason[:len(want)] != want {
		t.Fatalf("reason %q", d.Reason)
	}
}

func TestEvaluateDisallowedFlavor(t *testing.T) {
	in := baseInput()
	in.Flavors = []string{"gpu-h100"}
	d := policy.Evaluate(basePolicy(), in, workloadinfo.NewUsageSnapshot())
	if d.Decision != policy.DecisionDeny {
		t.Fatalf("expected DENY, got %+v", d)
	}
	if want := "disallowed_flavor"; d.Reason[:len(want)] != want {
		t.Fatalf("reason %q", d.Reason)
	}
}

func TestEvaluateOverNamespaceQuotaHold(t *testing.T) {
	usage := workloadinfo.NewUsageSnapshot()
	usage.ByNamespace["team-a"] = 4
	d := policy.Evaluate(basePolicy(), baseInput(), usage)
	if d.Decision != policy.DecisionHold || !d.Hold {
		t.Fatalf("expected HOLD, got %+v", d)
	}
}

func TestEvaluateOverNamespaceQuotaDeny(t *testing.T) {
	p := basePolicy()
	p.HoldOnQuotaExhausted = false
	usage := workloadinfo.NewUsageSnapshot()
	usage.ByNamespace["team-a"] = 4
	d := policy.Evaluate(p, baseInput(), usage)
	if d.Decision != policy.DecisionDeny {
		t.Fatalf("expected DENY, got %+v", d)
	}
}

func TestEvaluateDisallowedRegistry(t *testing.T) {
	in := baseInput()
	in.Images = []string{"evil.example/x:latest"}
	d := policy.Evaluate(basePolicy(), in, workloadinfo.NewUsageSnapshot())
	if d.Decision != policy.DecisionDeny {
		t.Fatalf("expected DENY, got %+v", d)
	}
	if want := "disallowed_image_registry"; d.Reason[:len(want)] != want {
		t.Fatalf("reason %q", d.Reason)
	}
}

func TestEvaluateDisallowedGHCREvilPrefix(t *testing.T) {
	in := baseInput()
	in.Images = []string{"ghcr.io/evil/x:latest"}
	d := policy.Evaluate(basePolicy(), in, workloadinfo.NewUsageSnapshot())
	if d.Decision != policy.DecisionDeny {
		t.Fatalf("expected DENY for ghcr.io/evil under ghcr.io/acme/ allowlist, got %+v", d)
	}
	if want := "disallowed_image_registry"; len(d.Reason) < len(want) || d.Reason[:len(want)] != want {
		t.Fatalf("reason %q", d.Reason)
	}
}

func TestEvaluateOverGPUHours(t *testing.T) {
	in := baseInput()
	in.EstimatedHrs = 20
	d := policy.Evaluate(basePolicy(), in, workloadinfo.NewUsageSnapshot())
	if d.Decision != policy.DecisionDeny {
		t.Fatalf("expected DENY, got %+v", d)
	}
	if want := "over_gpu_hours"; d.Reason[:len(want)] != want {
		t.Fatalf("reason %q", d.Reason)
	}
}

func TestEvaluateOverBudget(t *testing.T) {
	in := baseInput()
	in.GPURequest = 2
	in.EstimatedHrs = 3 // 2*3*1USD = 6USD > 4USD budget
	d := policy.Evaluate(basePolicy(), in, workloadinfo.NewUsageSnapshot())
	if d.Decision != policy.DecisionDeny {
		t.Fatalf("expected DENY, got %+v", d)
	}
	if want := "over_budget"; d.Reason[:len(want)] != want {
		t.Fatalf("reason %q", d.Reason)
	}
}

func TestEvaluateMissingEstimatedHoursFailClosed(t *testing.T) {
	in := baseInput()
	in.HasEstHours = false
	d := policy.Evaluate(basePolicy(), in, workloadinfo.NewUsageSnapshot())
	if d.Decision != policy.DecisionDeny {
		t.Fatalf("expected DENY, got %+v", d)
	}
}

func TestEvaluateNamespaceNotInMap(t *testing.T) {
	in := baseInput()
	in.Namespace = "unknown-ns"
	d := policy.Evaluate(basePolicy(), in, workloadinfo.NewUsageSnapshot())
	if d.Decision != policy.DecisionDeny {
		t.Fatalf("expected DENY, got %+v", d)
	}
}

func TestValidateSpec(t *testing.T) {
	if err := policy.ValidateSpec(basePolicy()); err != nil {
		t.Fatal(err)
	}
	if err := policy.ValidateSpec(nil); err == nil {
		t.Fatal("expected error")
	}
	p := basePolicy()
	p.GPUHourPriceMicros = nil
	if err := policy.ValidateSpec(p); err == nil {
		t.Fatal("expected budget/price error")
	}
}
