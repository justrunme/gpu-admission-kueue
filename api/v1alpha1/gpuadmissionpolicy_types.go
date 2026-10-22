package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ControllerName is the AdmissionCheck.spec.controllerName this project owns.
const ControllerName = "gpu-admission.justrunme.com/admission-check"

// GPUAdmissionPolicySpec declares fail-closed GPU admission rules evaluated by
// the AdmissionCheck controller.
type GPUAdmissionPolicySpec struct {
	// Version is recorded into evidence envelopes as policy_version.
	// +kubebuilder:validation:MinLength=1
	Version string `json:"version"`

	// RequiredLabels must be present (non-empty) on the workload job labels.
	// Typical values: cost-center, owner.
	// +optional
	RequiredLabels []string `json:"requiredLabels,omitempty"`

	// AllowedFlavors lists ResourceFlavor names a workload may reserve.
	// Empty means no flavor allowlist check (other rules still apply).
	// +optional
	AllowedFlavors []string `json:"allowedFlavors,omitempty"`

	// AllowedGPUModels lists accepted GPU model attribute values
	// (ResourceFlavor.spec.nodeLabels["nvidia.com/gpu.product"] or
	// annotation gpu-admission.justrunme.com/gpu-model on the flavor).
	// +optional
	AllowedGPUModels []string `json:"allowedGPUModels,omitempty"`

	// AllowedImageRegistries is a prefix allowlist for container images
	// (e.g. "registry.example.com/", "ghcr.io/org/"). Empty means no registry check.
	// +optional
	AllowedImageRegistries []string `json:"allowedImageRegistries,omitempty"`

	// NamespaceGPUQuota caps concurrent reserved nvidia.com/gpu units per namespace.
	// Missing namespaces are denied when this map is non-empty (fail-closed).
	// +optional
	NamespaceGPUQuota map[string]int64 `json:"namespaceGPUQuota,omitempty"`

	// TeamGPUQuota caps concurrent reserved GPUs per team label value
	// (label key defaults to "team").
	// +optional
	TeamGPUQuota map[string]int64 `json:"teamGPUQuota,omitempty"`

	// TeamLabelKey is the workload label used for TeamGPUQuota (default: team).
	// +optional
	TeamLabelKey string `json:"teamLabelKey,omitempty"`

	// MaxGPUHoursPerWorkload rejects workloads whose estimated GPU-hours
	// (gpus * durationHours annotation) exceed this value.
	// +optional
	MaxGPUHoursPerWorkload *float64 `json:"maxGPUHoursPerWorkload,omitempty"`

	// MaxBudgetUSDMicros rejects workloads whose estimated cost exceeds this
	// micro-USD budget when GPUHourPriceMicros is set.
	// +optional
	MaxBudgetUSDMicros *int64 `json:"maxBudgetUSDMicros,omitempty"`

	// GPUHourPriceMicros is the price in micro-USD per GPU-hour used for budget checks.
	// +optional
	GPUHourPriceMicros *int64 `json:"gpuHourPriceMicros,omitempty"`

	// HoldOnQuotaExhausted when true sets AdmissionCheck state Retry (hold)
	// instead of Rejected when namespace/team GPU quota is exhausted.
	// +optional
	HoldOnQuotaExhausted bool `json:"holdOnQuotaExhausted,omitempty"`
}

// GPUAdmissionPolicyStatus is observed state (optional bookkeeping).
type GPUAdmissionPolicyStatus struct {
	// ObservedGeneration mirrors metadata.generation when the controller last read the policy.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.spec.version`

// GPUAdmissionPolicy is a cluster-scoped policy referenced by an AdmissionCheck
// via spec.parameters.
type GPUAdmissionPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GPUAdmissionPolicySpec   `json:"spec,omitempty"`
	Status GPUAdmissionPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GPUAdmissionPolicyList contains a list of GPUAdmissionPolicy.
type GPUAdmissionPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GPUAdmissionPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GPUAdmissionPolicy{}, &GPUAdmissionPolicyList{})
}
