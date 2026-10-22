package controller

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/justrunme/gpu-admission-kueue/api/v1alpha1"
	"github.com/justrunme/gpu-admission-kueue/internal/evidence"
	"github.com/justrunme/gpu-admission-kueue/internal/kueueapi"
	"github.com/justrunme/gpu-admission-kueue/internal/policy"
)

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := v1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := kueueapi.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	return s
}

func baseAdmissionCheck() *unstructured.Unstructured {
	ac := &unstructured.Unstructured{}
	ac.SetGroupVersionKind(gvkAC)
	ac.SetName("gpu-admission")
	_ = unstructured.SetNestedField(ac.Object, v1alpha1.ControllerName, "spec", "controllerName")
	_ = unstructured.SetNestedMap(ac.Object, map[string]any{
		"apiGroup": v1alpha1.Group,
		"kind":     "GPUAdmissionPolicy",
		"name":     "default",
	}, "spec", "parameters")
	return ac
}

func basePolicy() *v1alpha1.GPUAdmissionPolicy {
	maxHours := 8.0
	price := int64(1000000)
	budget := int64(8000000)
	return &v1alpha1.GPUAdmissionPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "default"},
		Spec: v1alpha1.GPUAdmissionPolicySpec{
			Version:                "gpu-policy-1.0",
			RequiredLabels:         []string{"cost-center", "owner"},
			AllowedFlavors:         []string{"gpu-a100-sim"},
			AllowedGPUModels:       []string{"A100-SIM"},
			AllowedImageRegistries: []string{"registry.example.com/"},
			NamespaceGPUQuota:      map[string]int64{"team-a": 2},
			MaxGPUHoursPerWorkload: &maxHours,
			MaxBudgetUSDMicros:     &budget,
			GPUHourPriceMicros:     &price,
		},
	}
}

func baseWorkload() *unstructured.Unstructured {
	wl := &unstructured.Unstructured{}
	wl.SetGroupVersionKind(gvkWorkload)
	wl.SetNamespace("team-a")
	wl.SetName("job-1")
	wl.SetAnnotations(map[string]string{
		"gpu-admission.justrunme.com/estimated-hours": "1",
	})
	_ = unstructured.SetNestedSlice(wl.Object, []any{
		map[string]any{
			"name":  "main",
			"count": int64(1),
			"template": map[string]any{
				"metadata": map[string]any{
					"labels": map[string]any{
						"cost-center": "cc-100",
						"owner":       "alice",
						"team":        "alpha",
					},
					"annotations": map[string]any{
						"gpu-admission.justrunme.com/estimated-hours": "1",
					},
				},
				"spec": map[string]any{
					"containers": []any{
						map[string]any{
							"name":  "train",
							"image": "registry.example.com/ml/train:1.0",
							"resources": map[string]any{
								"requests": map[string]any{
									"nvidia.com/gpu": "1",
								},
							},
						},
					},
				},
			},
		},
	}, "spec", "podSets")
	_ = unstructured.SetNestedMap(wl.Object, map[string]any{
		"clusterQueue": "gpu-cluster-queue",
		"podSetAssignments": []any{
			map[string]any{
				"name": "main",
				"flavors": map[string]any{
					"nvidia.com/gpu": "gpu-a100-sim",
				},
			},
		},
	}, "status", "admission")
	_ = unstructured.SetNestedSlice(wl.Object, []any{
		map[string]any{
			"name":  "gpu-admission",
			"state": "Pending",
		},
	}, "status", "admissionChecks")
	return wl
}

func baseFlavor() *unstructured.Unstructured {
	rf := &unstructured.Unstructured{}
	rf.SetGroupVersionKind(gvkFlavor)
	rf.SetName("gpu-a100-sim")
	rf.SetAnnotations(map[string]string{
		"gpu-admission.justrunme.com/gpu-model": "A100-SIM",
	})
	_ = unstructured.SetNestedStringMap(rf.Object, map[string]string{
		"nvidia.com/gpu.product": "A100-SIM",
	}, "spec", "nodeLabels")
	return rf
}

func TestDecideMissingPolicyFailClosed(t *testing.T) {
	s := testScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(baseAdmissionCheck(), baseFlavor()).Build()
	r := &WorkloadAdmissionReconciler{Client: c, AdmissionCheckName: "gpu-admission"}
	_, _, err := r.decide(context.Background(), baseWorkload())
	if err == nil {
		t.Fatal("expected missing policy error")
	}
	if !apierrors.IsNotFound(err) && !strings.Contains(err.Error(), "get GPUAdmissionPolicy") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDecideAdmissionCheckLookupError(t *testing.T) {
	s := testScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(basePolicy(), baseFlavor()).Build()
	r := &WorkloadAdmissionReconciler{Client: c, AdmissionCheckName: "gpu-admission"}
	_, _, err := r.decide(context.Background(), baseWorkload())
	if err == nil {
		t.Fatal("expected AdmissionCheck lookup error")
	}
	if !strings.Contains(err.Error(), "get AdmissionCheck") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReconcileEvidencePersistFailureDowngradesReady(t *testing.T) {
	s := testScheme(t)
	wl := baseWorkload()
	ac := baseAdmissionCheck()
	pol := basePolicy()
	rf := baseFlavor()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: evidenceNamespace}}

	c := fake.NewClientBuilder().WithScheme(s).WithObjects(wl, ac, pol, rf, ns).
		WithStatusSubresource(wl, ac).
		WithInterceptorFuncs(interceptor.Funcs{
			Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				if _, ok := obj.(*corev1.ConfigMap); ok {
					return errors.New("persist boom")
				}
				return c.Create(ctx, obj, opts...)
			},
		}).Build()

	r := &WorkloadAdmissionReconciler{Client: c, AdmissionCheckName: "gpu-admission"}
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: "team-a", Name: "job-1"},
	})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(gvkWorkload)
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "team-a", Name: "job-1"}, got); err != nil {
		t.Fatal(err)
	}
	checks, _, _ := unstructured.NestedSlice(got.Object, "status", "admissionChecks")
	m := checks[0].(map[string]any)
	if m["state"] != string(kueueapi.CheckStateRejected) {
		t.Fatalf("expected Rejected after evidence persist failure, got %v msg=%v", m["state"], m["message"])
	}
	if !strings.Contains(m["message"].(string), "failed to persist evidence") {
		t.Fatalf("message=%v", m["message"])
	}
}

func TestReconcileMissingPolicyRejects(t *testing.T) {
	s := testScheme(t)
	wl := baseWorkload()
	ac := baseAdmissionCheck()
	rf := baseFlavor()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: evidenceNamespace}}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(wl, ac, rf, ns).
		WithStatusSubresource(wl, ac).Build()
	r := &WorkloadAdmissionReconciler{Client: c, AdmissionCheckName: "gpu-admission"}
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: "team-a", Name: "job-1"},
	})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(gvkWorkload)
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "team-a", Name: "job-1"}, got); err != nil {
		t.Fatal(err)
	}
	checks, _, _ := unstructured.NestedSlice(got.Object, "status", "admissionChecks")
	m := checks[0].(map[string]any)
	if m["state"] != string(kueueapi.CheckStateRejected) {
		t.Fatalf("expected Rejected, got %v (%v)", m["state"], m["message"])
	}
	msg := m["message"].(string)
	if !strings.Contains(msg, "GPUAdmissionPolicy") && !strings.Contains(msg, "undecidable") {
		t.Fatalf("message=%v", msg)
	}
}

func TestDecideAllowGolden(t *testing.T) {
	s := testScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(
		baseAdmissionCheck(), basePolicy(), baseFlavor(),
	).Build()
	r := &WorkloadAdmissionReconciler{Client: c, AdmissionCheckName: "gpu-admission"}
	dec, _, err := r.decide(context.Background(), baseWorkload())
	if err != nil {
		t.Fatal(err)
	}
	if dec.Decision != policy.DecisionAllow {
		t.Fatalf("got %+v", dec)
	}
}

func workloadWithGPUQuantity(gpu any) *unstructured.Unstructured {
	wl := baseWorkload()
	_ = unstructured.SetNestedSlice(wl.Object, []any{
		map[string]any{
			"name":  "main",
			"count": int64(1),
			"template": map[string]any{
				"metadata": map[string]any{
					"labels": map[string]any{
						"cost-center": "cc-100",
						"owner":       "alice",
						"team":        "alpha",
					},
					"annotations": map[string]any{
						"gpu-admission.justrunme.com/estimated-hours": "1",
					},
				},
				"spec": map[string]any{
					"containers": []any{
						map[string]any{
							"name":  "train",
							"image": "registry.example.com/ml/train:1.0",
							"resources": map[string]any{
								"requests": map[string]any{
									"nvidia.com/gpu": gpu,
								},
							},
						},
					},
				},
			},
		},
	}, "spec", "podSets")
	return wl
}

func TestDecideUnparseableGPUQuantityFailClosed(t *testing.T) {
	s := testScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(
		baseAdmissionCheck(), basePolicy(), baseFlavor(),
	).Build()
	r := &WorkloadAdmissionReconciler{Client: c, AdmissionCheckName: "gpu-admission"}
	_, _, err := r.decide(context.Background(), workloadWithGPUQuantity("1.5"))
	if err == nil {
		t.Fatal("expected undecidable gpu quantity error")
	}
	if !strings.Contains(err.Error(), "undecidable gpu quantity") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReconcileUnparseableGPUQuantityRejects(t *testing.T) {
	s := testScheme(t)
	// "1.5" is a valid Kubernetes quantity but fractional — must Reject / undecidable.
	wl := workloadWithGPUQuantity("1.5")
	ac := baseAdmissionCheck()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: evidenceNamespace}}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(
		wl, ac, basePolicy(), baseFlavor(), ns,
	).WithStatusSubresource(wl, ac).Build()
	r := &WorkloadAdmissionReconciler{Client: c, AdmissionCheckName: "gpu-admission"}
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: "team-a", Name: "job-1"},
	})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(gvkWorkload)
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "team-a", Name: "job-1"}, got); err != nil {
		t.Fatal(err)
	}
	checks, _, _ := unstructured.NestedSlice(got.Object, "status", "admissionChecks")
	m := checks[0].(map[string]any)
	if m["state"] != string(kueueapi.CheckStateRejected) {
		t.Fatalf("expected Rejected, got %v (%v)", m["state"], m["message"])
	}
	msg := m["message"].(string)
	if !strings.Contains(msg, "undecidable") {
		t.Fatalf("message=%v", msg)
	}
}

func readyWorkload(name, teamLabelKey, teamVal string) *unstructured.Unstructured {
	wl := baseWorkload()
	wl.SetName(name)
	wl.SetUID(types.UID("uid-" + name))
	_ = unstructured.SetNestedSlice(wl.Object, []any{
		map[string]any{"name": "gpu-admission", "state": "Ready"},
	}, "status", "admissionChecks")
	podSets, _, _ := unstructured.NestedSlice(wl.Object, "spec", "podSets")
	ps := podSets[0].(map[string]any)
	template := ps["template"].(map[string]any)
	meta := template["metadata"].(map[string]any)
	labels := meta["labels"].(map[string]any)
	delete(labels, "team")
	labels[teamLabelKey] = teamVal
	_ = unstructured.SetNestedSlice(wl.Object, podSets, "spec", "podSets")
	return wl
}

func TestCollectUsageUsesPolicyTeamLabelKey(t *testing.T) {
	s := testScheme(t)
	other := readyWorkload("job-other", "org", "platform")
	current := baseWorkload()
	current.SetUID("uid-current")
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(other, current, baseFlavor()).Build()
	r := &WorkloadAdmissionReconciler{Client: c, AdmissionCheckName: "gpu-admission"}
	usage, err := r.collectUsage(context.Background(), current, "org")
	if err != nil {
		t.Fatal(err)
	}
	if usage.ByTeam["platform"] != 1 {
		t.Fatalf("expected team key org=platform usage 1, got %+v", usage.ByTeam)
	}
	if usage.ByTeam["alpha"] != 0 {
		t.Fatalf("must not use hardcoded 'team' label, got %+v", usage.ByTeam)
	}
}

func TestCollectUsageExcludesFinishedAndInactive(t *testing.T) {
	s := testScheme(t)
	finished := readyWorkload("job-finished", "team", "alpha")
	_ = unstructured.SetNestedSlice(finished.Object, []any{
		map[string]any{"type": "Finished", "status": "True"},
	}, "status", "conditions")
	inactive := readyWorkload("job-inactive", "team", "alpha")
	active := false
	_ = unstructured.SetNestedField(inactive.Object, active, "spec", "active")
	live := readyWorkload("job-live", "team", "alpha")
	current := baseWorkload()
	current.SetUID("uid-current")
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(finished, inactive, live, current, baseFlavor()).Build()
	r := &WorkloadAdmissionReconciler{Client: c, AdmissionCheckName: "gpu-admission"}
	usage, err := r.collectUsage(context.Background(), current, "team")
	if err != nil {
		t.Fatal(err)
	}
	if usage.ByNamespace["team-a"] != 1 {
		t.Fatalf("expected only live workload counted, got ns=%v team=%v", usage.ByNamespace, usage.ByTeam)
	}
}

func TestEvidenceConfigMapNameNoTrailingDash(t *testing.T) {
	wl := baseWorkload()
	wl.SetUID("uid-1")
	env := evidence.Envelope{EnvelopeSHA256: "sha256:abc", Timestamp: "2026-01-01T00:00:00Z"}
	name := evidenceConfigMapName(wl, env)
	if strings.HasSuffix(name, "-") {
		t.Fatalf("name must not end with dash: %q", name)
	}
	if !strings.HasPrefix(name, "ev-") || len(name) > 63 {
		t.Fatalf("unexpected name %q", name)
	}
	other := evidenceConfigMapName(wl, evidence.Envelope{EnvelopeSHA256: "sha256:def", Timestamp: "2026-01-01T00:00:01Z"})
	if name == other {
		t.Fatal("distinct envelopes must produce distinct ConfigMap names")
	}
}
