package policy_test

import (
	"testing"

	"github.com/justrunme/gpu-admission-kueue/internal/policy"
)

func TestCorpus(t *testing.T) {
	for _, sc := range policy.Corpus() {
		sc := sc
		t.Run(sc.ID, func(t *testing.T) {
			d := policy.Evaluate(sc.Policy, sc.Input, sc.Usage)
			if d.Decision != sc.Expected {
				t.Fatalf("expected %s, got %s (%s)", sc.Expected, d.Decision, d.Reason)
			}
		})
	}
}
