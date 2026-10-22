// Package policy implements fail-closed GPU admission evaluation.
package policy

import (
	"fmt"
	"sort"
	"strings"

	"github.com/justrunme/gpu-admission-kueue/api/v1alpha1"
	"github.com/justrunme/gpu-admission-kueue/internal/workloadinfo"
)

// Decision is the outcome of Evaluate. Verdict maps to ASC envelope verdict.
type Decision struct {
	// Verdict is ALLOW or BLOCK (ASC vocabulary). HOLD maps to BLOCK with Hold=true.
	Verdict string
	// Decision is the GPU-admission disposition: ALLOW, DENY, or HOLD.
	Decision string
	Reason   string
	Details  map[string]any
	// Hold asks the controller to set AdmissionCheck state Retry instead of Rejected.
	Hold bool
}

const (
	VerdictAllow = "ALLOW"
	VerdictBlock = "BLOCK"

	DecisionAllow = "ALLOW"
	DecisionDeny  = "DENY"
	DecisionHold  = "HOLD"
)

// Evaluate applies policy to a workload input. Fail-closed: any inability to
// decide yields DENY/BLOCK. nil policy is DENY.
func Evaluate(pol *v1alpha1.GPUAdmissionPolicySpec, in workloadinfo.Input, usage workloadinfo.UsageSnapshot) Decision {
	details := map[string]any{
		"namespace":   in.Namespace,
		"workload":    in.Name,
		"gpu_request": in.GPURequest,
		"flavors":     append([]string(nil), in.Flavors...),
		"images":      append([]string(nil), in.Images...),
	}

	if pol == nil {
		return deny("malformed_policy", "policy is nil", details)
	}
	if strings.TrimSpace(pol.Version) == "" {
		return deny("malformed_policy", "policy.spec.version is empty", details)
	}
	details["policy_version"] = pol.Version

	if in.GPURequest < 0 {
		return deny("undecidable", "negative GPU request", details)
	}

	// Required labels
	for _, key := range pol.RequiredLabels {
		key = strings.TrimSpace(key)
		if key == "" {
			return deny("malformed_policy", "empty requiredLabels entry", details)
		}
		val := ""
		if in.Labels != nil {
			val = strings.TrimSpace(in.Labels[key])
		}
		if val == "" {
			return deny("missing_required_label", fmt.Sprintf("missing required label %q", key), details)
		}
	}

	// Flavor allowlist
	if len(pol.AllowedFlavors) > 0 {
		allowed := toSet(pol.AllowedFlavors)
		if len(in.Flavors) == 0 {
			return deny("undecidable", "no resource flavor assigned; cannot evaluate flavor allowlist", details)
		}
		for _, f := range in.Flavors {
			if !allowed[f] {
				return deny("disallowed_flavor", fmt.Sprintf("flavor %q is not in allowedFlavors", f), details)
			}
		}
	}

	// GPU model allowlist
	if len(pol.AllowedGPUModels) > 0 {
		allowed := toSet(pol.AllowedGPUModels)
		if len(in.GPUModels) == 0 {
			return deny("undecidable", "GPU model unknown; cannot evaluate allowedGPUModels", details)
		}
		for _, m := range in.GPUModels {
			if !allowed[m] {
				return deny("disallowed_gpu_model", fmt.Sprintf("GPU model %q is not allowed", m), details)
			}
		}
	}

	// Image registry allowlist
	if len(pol.AllowedImageRegistries) > 0 {
		if len(in.Images) == 0 {
			return deny("undecidable", "no container images found; cannot evaluate registry allowlist", details)
		}
		for _, img := range in.Images {
			reg := workloadinfo.ImageRegistry(img)
			if !registryAllowed(reg, pol.AllowedImageRegistries) {
				details["rejected_image"] = img
				details["rejected_registry"] = reg
				return deny("disallowed_image_registry", fmt.Sprintf("image registry %q is not allowed", reg), details)
			}
		}
	}

	// Namespace GPU quota
	if len(pol.NamespaceGPUQuota) > 0 {
		limit, ok := pol.NamespaceGPUQuota[in.Namespace]
		if !ok {
			return deny("namespace_not_in_quota_map", fmt.Sprintf("namespace %q has no GPU quota entry", in.Namespace), details)
		}
		used := usage.ByNamespace[in.Namespace]
		details["namespace_gpu_used"] = used
		details["namespace_gpu_limit"] = limit
		if used+in.GPURequest > limit {
			msg := fmt.Sprintf("namespace %q GPU quota exceeded: used=%d request=%d limit=%d", in.Namespace, used, in.GPURequest, limit)
			if pol.HoldOnQuotaExhausted {
				return hold("over_namespace_quota", msg, details)
			}
			return deny("over_namespace_quota", msg, details)
		}
	}

	// Team GPU quota
	if len(pol.TeamGPUQuota) > 0 {
		teamKey := pol.TeamLabelKey
		if teamKey == "" {
			teamKey = "team"
		}
		team := ""
		if in.Labels != nil {
			team = strings.TrimSpace(in.Labels[teamKey])
		}
		if team == "" {
			return deny("missing_team_label", fmt.Sprintf("team quota configured but label %q is missing", teamKey), details)
		}
		limit, ok := pol.TeamGPUQuota[team]
		if !ok {
			return deny("team_not_in_quota_map", fmt.Sprintf("team %q has no GPU quota entry", team), details)
		}
		used := usage.ByTeam[team]
		details["team"] = team
		details["team_gpu_used"] = used
		details["team_gpu_limit"] = limit
		if used+in.GPURequest > limit {
			msg := fmt.Sprintf("team %q GPU quota exceeded: used=%d request=%d limit=%d", team, used, in.GPURequest, limit)
			if pol.HoldOnQuotaExhausted {
				return hold("over_team_quota", msg, details)
			}
			return deny("over_team_quota", msg, details)
		}
	}

	// GPU-hours
	if pol.MaxGPUHoursPerWorkload != nil {
		if !in.HasEstHours {
			return deny("undecidable", "maxGPUHoursPerWorkload set but estimated-hours annotation missing or invalid", details)
		}
		hours := float64(in.GPURequest) * in.EstimatedHrs
		details["gpu_hours"] = hours
		details["max_gpu_hours"] = *pol.MaxGPUHoursPerWorkload
		if hours > *pol.MaxGPUHoursPerWorkload {
			return deny("over_gpu_hours", fmt.Sprintf("GPU-hours %.2f exceed max %.2f", hours, *pol.MaxGPUHoursPerWorkload), details)
		}
	}

	// Budget
	if pol.MaxBudgetUSDMicros != nil {
		if pol.GPUHourPriceMicros == nil {
			return deny("malformed_policy", "maxBudgetUSDMicros set without gpuHourPriceMicros", details)
		}
		if !in.HasEstHours {
			return deny("undecidable", "budget check requires estimated-hours annotation", details)
		}
		cost := int64(float64(in.GPURequest) * in.EstimatedHrs * float64(*pol.GPUHourPriceMicros))
		details["estimated_cost_usd_micros"] = cost
		details["max_budget_usd_micros"] = *pol.MaxBudgetUSDMicros
		if cost > *pol.MaxBudgetUSDMicros {
			return deny("over_budget", fmt.Sprintf("estimated cost %d micro-USD exceeds budget %d", cost, *pol.MaxBudgetUSDMicros), details)
		}
	}

	return Decision{
		Verdict:  VerdictAllow,
		Decision: DecisionAllow,
		Reason:   "policy_allow",
		Details:  details,
	}
}

// ValidateSpec returns an error if the policy cannot be used (fail-closed load).
func ValidateSpec(pol *v1alpha1.GPUAdmissionPolicySpec) error {
	if pol == nil {
		return fmt.Errorf("policy is nil")
	}
	if strings.TrimSpace(pol.Version) == "" {
		return fmt.Errorf("spec.version is required")
	}
	if pol.MaxBudgetUSDMicros != nil && pol.GPUHourPriceMicros == nil {
		return fmt.Errorf("gpuHourPriceMicros required when maxBudgetUSDMicros is set")
	}
	for _, k := range pol.RequiredLabels {
		if strings.TrimSpace(k) == "" {
			return fmt.Errorf("requiredLabels contains empty entry")
		}
	}
	return nil
}

func deny(reason, message string, details map[string]any) Decision {
	details = copyDetails(details)
	details["decision"] = DecisionDeny
	return Decision{
		Verdict:  VerdictBlock,
		Decision: DecisionDeny,
		Reason:   reason + ": " + message,
		Details:  details,
		Hold:     false,
	}
}

func hold(reason, message string, details map[string]any) Decision {
	details = copyDetails(details)
	details["decision"] = DecisionHold
	return Decision{
		Verdict:  VerdictBlock,
		Decision: DecisionHold,
		Reason:   reason + ": " + message,
		Details:  details,
		Hold:     true,
	}
}

func copyDetails(in map[string]any) map[string]any {
	out := make(map[string]any, len(in)+2)
	for k, v := range in {
		out[k] = v
	}
	return out
}

func toSet(items []string) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, i := range items {
		out[i] = true
	}
	return out
}

func registryAllowed(reg string, allow []string) bool {
	for _, a := range allow {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if strings.HasPrefix(reg, a) || strings.HasPrefix(a, reg) || reg == a {
			return true
		}
		// also allow exact host match without trailing slash asymmetry
		if strings.TrimSuffix(reg, "/") == strings.TrimSuffix(a, "/") {
			return true
		}
	}
	return false
}

// SortedKeys returns map keys sorted for stable digests.
func SortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
