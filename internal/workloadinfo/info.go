// Package workloadinfo extracts admission inputs from a Kueue Workload view.
package workloadinfo

import (
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
	return v, true, nil
}

// ImageRegistry returns the registry prefix before the first '/' after host,
// or the full image when no slash is present.
func ImageRegistry(image string) string {
	image = strings.TrimSpace(image)
	if image == "" {
		return ""
	}
	// strip digest/tag handling: registry/repo:tag or registry/repo@sha
	rest := image
	if i := strings.Index(rest, "/"); i > 0 {
		host := rest[:i]
		// dockerhub-style "library/ubuntu" has no dots/colon in first segment
		if !strings.Contains(host, ".") && !strings.Contains(host, ":") && host != "localhost" {
			return "docker.io/"
		}
		return host + "/"
	}
	return "docker.io/"
}
