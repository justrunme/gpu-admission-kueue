// Package controller implements the Kueue AdmissionCheck controller for GPU policy.
package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/justrunme/gpu-admission-kueue/api/v1alpha1"
	"github.com/justrunme/gpu-admission-kueue/internal/evidence"
	"github.com/justrunme/gpu-admission-kueue/internal/kueueapi"
	"github.com/justrunme/gpu-admission-kueue/internal/policy"
	"github.com/justrunme/gpu-admission-kueue/internal/workloadinfo"
)

const (
	evidenceNamespace = "gpu-admission-system"
	evidenceLabelKey  = "gpu-admission.justrunme.com/evidence"
)

var (
	gvkWorkload = schema.GroupVersionKind{Group: kueueapi.Group, Version: kueueapi.Version, Kind: "Workload"}
	gvkAC       = schema.GroupVersionKind{Group: kueueapi.Group, Version: kueueapi.Version, Kind: "AdmissionCheck"}
	gvkFlavor   = schema.GroupVersionKind{Group: kueueapi.Group, Version: kueueapi.Version, Kind: "ResourceFlavor"}
)

// WorkloadAdmissionReconciler evaluates Pending GPU AdmissionChecks on Workloads.
type WorkloadAdmissionReconciler struct {
	client.Client
	AdmissionCheckName string
}

// AdmissionCheckReconciler keeps the AdmissionCheck Active while this controller runs.
type AdmissionCheckReconciler struct {
	client.Client
	AdmissionCheckName string
}

func (r *AdmissionCheckReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if req.Name != r.AdmissionCheckName {
		return ctrl.Result{}, nil
	}
	logger := log.FromContext(ctx)
	ac := &unstructured.Unstructured{}
	ac.SetGroupVersionKind(gvkAC)
	if err := r.Get(ctx, req.NamespacedName, ac); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	controllerName, _, _ := unstructured.NestedString(ac.Object, "spec", "controllerName")
	if controllerName != v1alpha1.ControllerName {
		logger.Info("skipping AdmissionCheck with different controllerName", "got", controllerName)
		return ctrl.Result{}, nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	conditions, _, _ := unstructured.NestedSlice(ac.Object, "status", "conditions")
	found := false
	for i, raw := range conditions {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if m["type"] == kueueapi.AdmissionCheckActive {
			m["status"] = string(metav1.ConditionTrue)
			m["reason"] = "ControllerReady"
			m["message"] = "GPU admission controller is reconciling"
			m["lastTransitionTime"] = now
			conditions[i] = m
			found = true
			break
		}
	}
	if !found {
		conditions = append(conditions, map[string]any{
			"type":               kueueapi.AdmissionCheckActive,
			"status":             string(metav1.ConditionTrue),
			"reason":             "ControllerReady",
			"message":            "GPU admission controller is reconciling",
			"lastTransitionTime": now,
		})
	}
	if err := unstructured.SetNestedSlice(ac.Object, conditions, "status", "conditions"); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.Status().Update(ctx, ac); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *WorkloadAdmissionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	wl := &unstructured.Unstructured{}
	wl.SetGroupVersionKind(gvkWorkload)
	if err := r.Get(ctx, req.NamespacedName, wl); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	checks, found, err := unstructured.NestedSlice(wl.Object, "status", "admissionChecks")
	if err != nil || !found {
		return ctrl.Result{}, err
	}
	idx := -1
	var state string
	for i, raw := range checks {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if m["name"] == r.AdmissionCheckName {
			idx = i
			state, _ = m["state"].(string)
			break
		}
	}
	if idx < 0 {
		return ctrl.Result{}, nil
	}
	if state == string(kueueapi.CheckStateReady) || state == string(kueueapi.CheckStateRejected) {
		return ctrl.Result{}, nil
	}

	// Kueue runs AdmissionChecks after quota reservation. Wait until flavors exist.
	if !quotaReserved(wl) {
		logger.Info("waiting for quota reservation before evaluating GPU policy")
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}

	dec, env, err := r.decide(ctx, wl)
	if err != nil {
		logger.Error(err, "fail-closed reject")
		dec = policy.Decision{
			Verdict:  policy.VerdictBlock,
			Decision: policy.DecisionDeny,
			Reason:   "undecidable: " + err.Error(),
			Details:  map[string]any{"error": err.Error()},
		}
		env, _ = evidence.Build(evidence.BuildOptionsFromDecision(
			wl.GetNamespace()+"/"+wl.GetName(),
			r.AdmissionCheckName,
			"unknown",
			"sha256:0000000000000000000000000000000000000000000000000000000000000000",
			"sha256:0000000000000000000000000000000000000000000000000000000000000000",
			dec,
		))
	}

	newState := string(kueueapi.CheckStateRejected)
	switch dec.Decision {
	case policy.DecisionAllow:
		newState = string(kueueapi.CheckStateReady)
	case policy.DecisionHold:
		newState = string(kueueapi.CheckStateRetry)
	}

	message := truncate(dec.Reason, 1024)
	if env.EnvelopeSHA256 != "" {
		if err := r.persistEvidence(ctx, wl, env); err != nil {
			logger.Error(err, "persist evidence")
			if newState == string(kueueapi.CheckStateReady) {
				newState = string(kueueapi.CheckStateRejected)
				message = "undecidable: failed to persist evidence: " + err.Error()
			}
		}
	}

	m := checks[idx].(map[string]any)
	m["state"] = newState
	m["message"] = message
	m["lastTransitionTime"] = time.Now().UTC().Format(time.RFC3339)
	checks[idx] = m
	patch := []map[string]any{{
		"op":    "replace",
		"path":  fmt.Sprintf("/status/admissionChecks/%d/state", idx),
		"value": newState,
	}, {
		"op":    "add",
		"path":  fmt.Sprintf("/status/admissionChecks/%d/message", idx),
		"value": message,
	}, {
		"op":    "add",
		"path":  fmt.Sprintf("/status/admissionChecks/%d/lastTransitionTime", idx),
		"value": m["lastTransitionTime"],
	}}
	rawPatch, err := json.Marshal(patch)
	if err != nil {
		return ctrl.Result{}, err
	}
	if err := r.Status().Patch(ctx, wl, client.RawPatch(types.JSONPatchType, rawPatch)); err != nil {
		// Fallback: full status update of admissionChecks slice only via merge patch
		if err2 := unstructured.SetNestedSlice(wl.Object, checks, "status", "admissionChecks"); err2 != nil {
			return ctrl.Result{}, err
		}
		if err3 := r.Status().Update(ctx, wl); err3 != nil {
			return ctrl.Result{}, err3
		}
	}
	logger.Info("admission decision", "workload", req.String(), "decision", dec.Decision, "state", newState, "reason", dec.Reason)
	return ctrl.Result{}, nil
}

func (r *WorkloadAdmissionReconciler) decide(ctx context.Context, wl *unstructured.Unstructured) (policy.Decision, evidence.Envelope, error) {
	ac := &unstructured.Unstructured{}
	ac.SetGroupVersionKind(gvkAC)
	if err := r.Get(ctx, types.NamespacedName{Name: r.AdmissionCheckName}, ac); err != nil {
		return policy.Decision{}, evidence.Envelope{}, fmt.Errorf("get AdmissionCheck: %w", err)
	}
	controllerName, _, _ := unstructured.NestedString(ac.Object, "spec", "controllerName")
	if controllerName != v1alpha1.ControllerName {
		return policy.Decision{}, evidence.Envelope{}, fmt.Errorf("AdmissionCheck controllerName mismatch")
	}
	params, found, _ := unstructured.NestedMap(ac.Object, "spec", "parameters")
	if !found || params == nil {
		return policy.Decision{}, evidence.Envelope{}, fmt.Errorf("AdmissionCheck parameters missing")
	}
	apiGroup, _ := params["apiGroup"].(string)
	kind, _ := params["kind"].(string)
	polName, _ := params["name"].(string)
	if apiGroup != v1alpha1.Group || kind != "GPUAdmissionPolicy" {
		return policy.Decision{}, evidence.Envelope{}, fmt.Errorf("unsupported parameters ref %s/%s", apiGroup, kind)
	}

	var pol v1alpha1.GPUAdmissionPolicy
	if err := r.Get(ctx, types.NamespacedName{Name: polName}, &pol); err != nil {
		return policy.Decision{}, evidence.Envelope{}, fmt.Errorf("get GPUAdmissionPolicy %q: %w", polName, err)
	}
	if err := policy.ValidateSpec(&pol.Spec); err != nil {
		version := pol.Spec.Version
		if version == "" {
			version = "invalid"
		}
		dec := policy.Decision{
			Verdict:  policy.VerdictBlock,
			Decision: policy.DecisionDeny,
			Reason:   "malformed_policy: " + err.Error(),
			Details:  map[string]any{"policy": pol.Name},
		}
		raw, _ := json.Marshal(pol.Spec)
		env, berr := evidence.Build(evidence.BuildOptionsFromDecision(
			wl.GetNamespace()+"/"+wl.GetName(), r.AdmissionCheckName, version,
			evidence.PolicySHA256Bytes(raw),
			"sha256:0000000000000000000000000000000000000000000000000000000000000000",
			dec,
		))
		return dec, env, berr
	}

	in, err := extractInput(ctx, r.Client, wl)
	if err != nil {
		return policy.Decision{}, evidence.Envelope{}, err
	}
	usage, err := r.collectUsage(ctx, wl)
	if err != nil {
		return policy.Decision{}, evidence.Envelope{}, err
	}
	dec := policy.Evaluate(&pol.Spec, in, usage)
	inputsDigest, err := evidence.InputsDigest(in)
	if err != nil {
		return policy.Decision{}, evidence.Envelope{}, err
	}
	raw, err := json.Marshal(pol.Spec)
	if err != nil {
		return policy.Decision{}, evidence.Envelope{}, err
	}
	env, err := evidence.Build(evidence.BuildOptionsFromDecision(
		wl.GetNamespace()+"/"+wl.GetName(),
		r.AdmissionCheckName,
		pol.Spec.Version,
		evidence.PolicySHA256Bytes(raw),
		inputsDigest,
		dec,
	))
	return dec, env, err
}

func extractInput(ctx context.Context, c client.Client, wl *unstructured.Unstructured) (workloadinfo.Input, error) {
	in := workloadinfo.Input{
		Namespace: wl.GetNamespace(),
		Name:      wl.GetName(),
		Labels:    map[string]string{},
	}
	for k, v := range wl.GetLabels() {
		in.Labels[k] = v
	}
	hrs, has, err := workloadinfo.ParseEstimatedHours(wl.GetAnnotations())
	if err != nil {
		return in, fmt.Errorf("invalid estimated-hours: %w", err)
	}
	in.EstimatedHrs = hrs
	in.HasEstHours = has

	podSets, _, _ := unstructured.NestedSlice(wl.Object, "spec", "podSets")
	var gpuTotal int64
	for _, raw := range podSets {
		ps, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		count := int64(1)
		if v, ok := ps["count"].(int64); ok && v > 0 {
			count = v
		} else if v, ok := ps["count"].(float64); ok && v > 0 {
			count = int64(v)
		}
		template, _, _ := unstructured.NestedMap(ps, "template")
		meta, _, _ := unstructured.NestedMap(template, "metadata")
		if labels, ok := meta["labels"].(map[string]any); ok {
			for k, v := range labels {
				if s, ok := v.(string); ok {
					if _, exists := in.Labels[k]; !exists {
						in.Labels[k] = s
					}
				}
			}
		}
		if anns, ok := meta["annotations"].(map[string]any); ok {
			strAnns := map[string]string{}
			for k, v := range anns {
				if s, ok := v.(string); ok {
					strAnns[k] = s
				}
			}
			if !in.HasEstHours {
				h, okH, errH := workloadinfo.ParseEstimatedHours(strAnns)
				if errH != nil {
					return in, fmt.Errorf("invalid estimated-hours on pod template: %w", errH)
				}
				in.EstimatedHrs = h
				in.HasEstHours = okH
			}
		}
		spec, _, _ := unstructured.NestedMap(template, "spec")
		var psGPU int64
		for _, key := range []string{"containers", "initContainers"} {
			ctns, _, _ := unstructured.NestedSlice(spec, key)
			for _, cr := range ctns {
				ctn, ok := cr.(map[string]any)
				if !ok {
					continue
				}
				if img, _ := ctn["image"].(string); img != "" {
					in.Images = append(in.Images, img)
				}
				if key != "containers" {
					continue
				}
				req, _, _ := unstructured.NestedMap(ctn, "resources", "requests")
				if g, ok := req[workloadinfo.ResourceGPU]; ok {
					q, err := workloadinfo.ParseGPUQuantity(g)
					if err != nil {
						return in, fmt.Errorf("undecidable gpu quantity: %w", err)
					}
					psGPU += q
				}
			}
		}
		gpuTotal += psGPU * count
	}
	in.GPURequest = gpuTotal

	assignments, _, _ := unstructured.NestedSlice(wl.Object, "status", "admission", "podSetAssignments")
	seen := map[string]bool{}
	for _, raw := range assignments {
		as, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		flavors, _, _ := unstructured.NestedMap(as, "flavors")
		for _, v := range flavors {
			fname, _ := v.(string)
			if fname == "" || seen[fname] {
				continue
			}
			seen[fname] = true
			in.Flavors = append(in.Flavors, fname)
			rf := &unstructured.Unstructured{}
			rf.SetGroupVersionKind(gvkFlavor)
			if err := c.Get(ctx, types.NamespacedName{Name: fname}, rf); err != nil {
				if apierrors.IsNotFound(err) {
					continue
				}
				return in, err
			}
			model := rf.GetAnnotations()[workloadinfo.AnnotationGPUModel]
			if model == "" {
				labels, _, _ := unstructured.NestedStringMap(rf.Object, "spec", "nodeLabels")
				model = labels["nvidia.com/gpu.product"]
			}
			if model != "" {
				in.GPUModels = append(in.GPUModels, model)
			}
		}
	}
	return in, nil
}

func (r *WorkloadAdmissionReconciler) collectUsage(ctx context.Context, current *unstructured.Unstructured) (workloadinfo.UsageSnapshot, error) {
	usage := workloadinfo.NewUsageSnapshot()
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{Group: kueueapi.Group, Version: kueueapi.Version, Kind: "WorkloadList"})
	if err := r.List(ctx, list); err != nil {
		return usage, err
	}
	for i := range list.Items {
		wl := &list.Items[i]
		if wl.GetUID() == current.GetUID() {
			continue
		}
		checks, _, _ := unstructured.NestedSlice(wl.Object, "status", "admissionChecks")
		ready := false
		for _, raw := range checks {
			m, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if m["name"] == r.AdmissionCheckName && m["state"] == string(kueueapi.CheckStateReady) {
				ready = true
				break
			}
		}
		if !ready {
			continue
		}
		in, err := extractInput(ctx, r.Client, wl)
		if err != nil {
			return usage, err
		}
		usage.ByNamespace[wl.GetNamespace()] += in.GPURequest
		if team := in.Labels["team"]; team != "" {
			usage.ByTeam[team] += in.GPURequest
		}
	}
	return usage, nil
}

func (r *WorkloadAdmissionReconciler) persistEvidence(ctx context.Context, wl *unstructured.Unstructured, env evidence.Envelope) error {
	if err := evidence.Verify(env); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return err
	}
	name := fmt.Sprintf("evidence-%s-%s", wl.GetNamespace(), wl.GetName())
	if len(name) > 63 {
		name = name[:63]
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: evidenceNamespace,
			Labels: map[string]string{
				evidenceLabelKey: "true",
				"workload":       wl.GetName(),
				"workload-ns":    wl.GetNamespace(),
			},
		},
		Data: map[string]string{
			"envelope.json": string(raw),
		},
	}
	var existing corev1.ConfigMap
	err = r.Get(ctx, types.NamespacedName{Name: name, Namespace: evidenceNamespace}, &existing)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, cm)
	}
	if err != nil {
		return err
	}
	existing.Data = cm.Data
	existing.Labels = cm.Labels
	return r.Update(ctx, &existing)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func quotaReserved(wl *unstructured.Unstructured) bool {
	if _, found, _ := unstructured.NestedMap(wl.Object, "status", "admission"); found {
		return true
	}
	conds, _, _ := unstructured.NestedSlice(wl.Object, "status", "conditions")
	for _, raw := range conds {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if m["type"] == "QuotaReserved" && m["status"] == "True" {
			return true
		}
	}
	return false
}

// SetupWithManager registers controllers.
func SetupWithManager(mgr ctrl.Manager, admissionCheckName string) error {
	if err := (&AdmissionCheckReconciler{
		Client:             mgr.GetClient(),
		AdmissionCheckName: admissionCheckName,
	}).SetupWithManager(mgr); err != nil {
		return err
	}
	return (&WorkloadAdmissionReconciler{
		Client:             mgr.GetClient(),
		AdmissionCheckName: admissionCheckName,
	}).SetupWithManager(mgr)
}

func (r *AdmissionCheckReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kueueapi.AdmissionCheck{}).
		Complete(r)
}

func (r *WorkloadAdmissionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kueueapi.Workload{}).
		Complete(r)
}
