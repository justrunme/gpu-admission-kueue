// Package workloadinfo extracts admission inputs from a Kueue Workload view.
package workloadinfo

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

const (
	// AnnotationEstimatedHours is optional; used for GPU-hours and budget checks.
	AnnotationEstimatedHours = "gpu-admission.justrunme.com/estimated-hours"
	// AnnotationGPUModel may be set on a ResourceFlavor.
	AnnotationGPUModel = "gpu-admission.justrunme.com/gpu-model"
	// ResourceGPU is the extended resource name used for simulated and real GPUs.
	ResourceGPU = "nvidia.com/gpu"
)

// Input is the normalized view evaluated against a GPUAdmissionPolicy.
type Input struct {
	Namespace    string
	Name         string
	Labels       map[string]string
	Images       []string
	Flavors      []string
	GPUModels    []string
	GPURequest   int64
	EstimatedHrs float64
	HasEstHours  bool
}

// UsageSnapshot counts currently reserved (Ready-check) GPUs for quota math.
type UsageSnapshot struct {
	ByNamespace map[string]int64
	ByTeam      map[string]int64
}

// NewUsageSnapshot returns empty maps ready for accumulation.
func NewUsageSnapshot() UsageSnapshot {
	return UsageSnapshot{
		ByNamespace: map[string]int64{},
		ByTeam:      map[string]int64{},
	}
}

// ParseEstimatedHours reads the estimated-hours annotation; missing is ok.
// Present values must be finite and > 0 (fail-closed); negative, NaN, Inf, and
// zero are rejected so budget / GPU-hour checks cannot be bypassed.
func ParseEstimatedHours(annotations map[string]string) (float64, bool, error) {
	if annotations == nil {
		return 0, false, nil
	}
	raw, ok := annotations[AnnotationEstimatedHours]
	if !ok || strings.TrimSpace(raw) == "" {
		return 0, false, nil
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, true, err
	}
	if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
		return 0, true, fmt.Errorf("estimated-hours must be a finite number > 0, got %q", raw)
	}
	return v, true, nil
}

// ImageMatchesAllowlist reports whether the full image reference is allowed by
// any allowlist entry. Entries are prefixes of the full reference (not host-only),
// so "ghcr.io/acme/" matches "ghcr.io/acme/app:1" but not "ghcr.io/evil/x".
func ImageMatchesAllowlist(image string, allow []string) bool {
	image = strings.TrimSpace(image)
	if image == "" {
		return false
	}
	for _, a := range allow {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if strings.HasPrefix(image, a) {
			return true
		}
	}
	return false
}
