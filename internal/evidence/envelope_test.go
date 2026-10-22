package evidence_test

import (
	"testing"

	"github.com/justrunme/gpu-admission-kueue/internal/evidence"
	"github.com/justrunme/gpu-admission-kueue/internal/policy"
)

func TestEnvelopeRoundTrip(t *testing.T) {
	d := policy.Decision{
		Verdict:  policy.VerdictBlock,
		Decision: policy.DecisionDeny,
		Reason:   "missing_required_label: missing required label \"owner\"",
		Details:  map[string]any{"namespace": "team-a"},
	}
	opts := evidence.BuildOptionsFromDecision(
		"team-a/job-1",
		"gpu-admission-check",
		"gpu-policy-1.0",
		"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		d,
	)
	opts.Timestamp = "2026-10-22T07:00:00Z"
	opts.CommitSHA = "deadbeef"
	env, err := evidence.Build(opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := evidence.Verify(env); err != nil {
		t.Fatal(err)
	}
	dec, _ := env.Details["decision"].(string)
	if dec != policy.DecisionDeny {
		t.Fatalf("details.decision=%v", env.Details["decision"])
	}
	if _, ok := env.Details["inputs_digest"]; !ok {
		t.Fatal("missing inputs_digest")
	}
}

func TestEnvelopeTamperDetected(t *testing.T) {
	env, err := evidence.Build(evidence.BuildOptions{
		PolicyVersion: "v1",
		PolicySHA256:  "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ScenarioID:    "s1",
		Verdict:       policy.VerdictAllow,
		Reason:        "policy_allow",
		TraceID:       "t1",
		CommitSHA:     "c1",
		Timestamp:     "2026-10-22T07:00:00Z",
		Details:       map[string]any{"decision": "ALLOW"},
	})
	if err != nil {
		t.Fatal(err)
	}
	env.Reason = "tampered"
	if err := evidence.Verify(env); err == nil {
		t.Fatal("expected verify failure")
	}
}

func TestInputsDigestStable(t *testing.T) {
	a, err := evidence.InputsDigest(map[string]any{"b": 1, "a": 2})
	if err != nil {
		t.Fatal(err)
	}
	b, err := evidence.InputsDigest(map[string]any{"a": 2, "b": 1})
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("digests differ: %s vs %s", a, b)
	}
}
