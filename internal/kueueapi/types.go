// Package kueueapi contains minimal Kueue API types used by the admission controller.
// The cluster still runs upstream Kueue from release manifests; this package avoids
// pulling the full kueue Go module into the build graph.
package kueueapi

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	Group   = "kueue.x-k8s.io"
	Version = "v1beta1"
)

var (
	GroupVersion = schema.GroupVersion{Group: Group, Version: Version}
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)
	AddToScheme   = SchemeBuilder.AddToScheme
)

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion,
		&Workload{}, &WorkloadList{},
		&AdmissionCheck{}, &AdmissionCheckList{},
		&ResourceFlavor{}, &ResourceFlavorList{},
	)
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}

type CheckState string

const (
	CheckStateRetry    CheckState = "Retry"
	CheckStateRejected CheckState = "Rejected"
	CheckStatePending  CheckState = "Pending"
	CheckStateReady    CheckState = "Ready"
)

const AdmissionCheckActive = "Active"

// Workload is a partial mirror of kueue Workload.
type Workload struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              WorkloadSpec   `json:"spec,omitempty"`
	Status            WorkloadStatus `json:"status,omitempty"`
}

type WorkloadList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Workload `json:"items"`
}

type WorkloadSpec struct {
	PodSets []PodSet `json:"podSets,omitempty"`
	Active  *bool    `json:"active,omitempty"`
}

type PodSet struct {
	Name     string                   `json:"name,omitempty"`
	Template corev1.PodTemplateSpec   `json:"template"`
	Count    int32                    `json:"count"`
}

type WorkloadStatus struct {
	Admission       *Admission                `json:"admission,omitempty"`
	AdmissionChecks []AdmissionCheckState     `json:"admissionChecks,omitempty"`
	Conditions      []metav1.Condition        `json:"conditions,omitempty"`
}

type Admission struct {
	ClusterQueue      string             `json:"clusterQueue,omitempty"`
	PodSetAssignments []PodSetAssignment `json:"podSetAssignments,omitempty"`
}

type PodSetAssignment struct {
	Name          string                                   `json:"name,omitempty"`
	Flavors       map[corev1.ResourceName]string           `json:"flavors,omitempty"`
	ResourceUsage corev1.ResourceList                      `json:"resourceUsage,omitempty"`
}

type AdmissionCheckState struct {
	Name               string      `json:"name"`
	State              CheckState  `json:"state"`
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
	Message            string      `json:"message,omitempty"`
}

type AdmissionCheck struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              AdmissionCheckSpec   `json:"spec,omitempty"`
	Status            AdmissionCheckStatus `json:"status,omitempty"`
}

type AdmissionCheckList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AdmissionCheck `json:"items"`
}

type AdmissionCheckSpec struct {
	ControllerName string                            `json:"controllerName"`
	Parameters     *AdmissionCheckParametersReference `json:"parameters,omitempty"`
}

type AdmissionCheckParametersReference struct {
	APIGroup string `json:"apiGroup"`
	Kind     string `json:"kind"`
	Name     string `json:"name"`
}

type AdmissionCheckStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

type ResourceFlavor struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ResourceFlavorSpec `json:"spec,omitempty"`
}

type ResourceFlavorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ResourceFlavor `json:"items"`
}

type ResourceFlavorSpec struct {
	NodeLabels map[string]string `json:"nodeLabels,omitempty"`
}

func (in *Workload) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}
func (in *Workload) DeepCopy() *Workload {
	if in == nil {
		return nil
	}
	out := new(Workload)
	in.DeepCopyInto(out)
	return out
}
func (in *Workload) DeepCopyInto(out *Workload) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}
func (in *WorkloadSpec) DeepCopyInto(out *WorkloadSpec) {
	*out = *in
	if in.Active != nil {
		v := *in.Active
		out.Active = &v
	}
	if in.PodSets != nil {
		out.PodSets = make([]PodSet, len(in.PodSets))
		for i := range in.PodSets {
			in.PodSets[i].DeepCopyInto(&out.PodSets[i])
		}
	}
}
func (in *PodSet) DeepCopyInto(out *PodSet) {
	*out = *in
	in.Template.DeepCopyInto(&out.Template)
}
func (in *WorkloadStatus) DeepCopyInto(out *WorkloadStatus) {
	*out = *in
	if in.Admission != nil {
		a := new(Admission)
		in.Admission.DeepCopyInto(a)
		out.Admission = a
	}
	if in.AdmissionChecks != nil {
		out.AdmissionChecks = make([]AdmissionCheckState, len(in.AdmissionChecks))
		for i := range in.AdmissionChecks {
			in.AdmissionChecks[i].DeepCopyInto(&out.AdmissionChecks[i])
		}
	}
	if in.Conditions != nil {
		out.Conditions = make([]metav1.Condition, len(in.Conditions))
		for i := range in.Conditions {
			in.Conditions[i].DeepCopyInto(&out.Conditions[i])
		}
	}
}
func (in *Admission) DeepCopyInto(out *Admission) {
	*out = *in
	if in.PodSetAssignments != nil {
		out.PodSetAssignments = make([]PodSetAssignment, len(in.PodSetAssignments))
		for i := range in.PodSetAssignments {
			in.PodSetAssignments[i].DeepCopyInto(&out.PodSetAssignments[i])
		}
	}
}
func (in *PodSetAssignment) DeepCopyInto(out *PodSetAssignment) {
	*out = *in
	if in.Flavors != nil {
		out.Flavors = make(map[corev1.ResourceName]string, len(in.Flavors))
		for k, v := range in.Flavors {
			out.Flavors[k] = v
		}
	}
	if in.ResourceUsage != nil {
		out.ResourceUsage = in.ResourceUsage.DeepCopy()
	}
}
func (in *AdmissionCheckState) DeepCopyInto(out *AdmissionCheckState) {
	*out = *in
	in.LastTransitionTime.DeepCopyInto(&out.LastTransitionTime)
}

func (in *WorkloadList) DeepCopyObject() runtime.Object { return in.DeepCopy() }
func (in *WorkloadList) DeepCopy() *WorkloadList {
	if in == nil {
		return nil
	}
	out := new(WorkloadList)
	in.DeepCopyInto(out)
	return out
}
func (in *WorkloadList) DeepCopyInto(out *WorkloadList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]Workload, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

func (in *AdmissionCheck) DeepCopyObject() runtime.Object { return in.DeepCopy() }
func (in *AdmissionCheck) DeepCopy() *AdmissionCheck {
	if in == nil {
		return nil
	}
	out := new(AdmissionCheck)
	in.DeepCopyInto(out)
	return out
}
func (in *AdmissionCheck) DeepCopyInto(out *AdmissionCheck) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}
func (in *AdmissionCheckSpec) DeepCopyInto(out *AdmissionCheckSpec) {
	*out = *in
	if in.Parameters != nil {
		p := *in.Parameters
		out.Parameters = &p
	}
}
func (in *AdmissionCheckStatus) DeepCopyInto(out *AdmissionCheckStatus) {
	*out = *in
	if in.Conditions != nil {
		out.Conditions = make([]metav1.Condition, len(in.Conditions))
		for i := range in.Conditions {
			in.Conditions[i].DeepCopyInto(&out.Conditions[i])
		}
	}
}
func (in *AdmissionCheckList) DeepCopyObject() runtime.Object { return in.DeepCopy() }
func (in *AdmissionCheckList) DeepCopy() *AdmissionCheckList {
	if in == nil {
		return nil
	}
	out := new(AdmissionCheckList)
	in.DeepCopyInto(out)
	return out
}
func (in *AdmissionCheckList) DeepCopyInto(out *AdmissionCheckList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]AdmissionCheck, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

func (in *ResourceFlavor) DeepCopyObject() runtime.Object { return in.DeepCopy() }
func (in *ResourceFlavor) DeepCopy() *ResourceFlavor {
	if in == nil {
		return nil
	}
	out := new(ResourceFlavor)
	in.DeepCopyInto(out)
	return out
}
func (in *ResourceFlavor) DeepCopyInto(out *ResourceFlavor) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
}
func (in *ResourceFlavorSpec) DeepCopyInto(out *ResourceFlavorSpec) {
	*out = *in
	if in.NodeLabels != nil {
		out.NodeLabels = make(map[string]string, len(in.NodeLabels))
		for k, v := range in.NodeLabels {
			out.NodeLabels[k] = v
		}
	}
}
func (in *ResourceFlavorList) DeepCopyObject() runtime.Object { return in.DeepCopy() }
func (in *ResourceFlavorList) DeepCopy() *ResourceFlavorList {
	if in == nil {
		return nil
	}
	out := new(ResourceFlavorList)
	in.DeepCopyInto(out)
	return out
}
func (in *ResourceFlavorList) DeepCopyInto(out *ResourceFlavorList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]ResourceFlavor, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}
