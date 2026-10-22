// Command corpus evaluates the fixed unit corpus and writes results/results.json.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/justrunme/gpu-admission-kueue/internal/evidence"
	"github.com/justrunme/gpu-admission-kueue/internal/policy"
)

func main() {
	start := time.Now()
	scenarios := policy.Corpus()
	type row struct {
		ScenarioID string         `json:"scenario_id"`
		Expected   string         `json:"expected"`
		Actual     string         `json:"actual"`
		Match      bool           `json:"match"`
		Reason     string         `json:"reason"`
		Envelope   map[string]any `json:"envelope"`
	}
	out := make([]row, 0, len(scenarios))
	allow, deny, hold := 0, 0, 0
	falseAllow, falseDeny := 0, 0
	passed := true
	for _, sc := range scenarios {
		d := policy.Evaluate(sc.Policy, sc.Input, sc.Usage)
		match := d.Decision == sc.Expected
		if !match {
			passed = false
		}
		switch d.Decision {
		case policy.DecisionAllow:
			allow++
		case policy.DecisionHold:
			hold++
		default:
			deny++
		}
		if sc.Expected != policy.DecisionAllow && d.Decision == policy.DecisionAllow {
			falseAllow++
		}
		if sc.Expected == policy.DecisionAllow && d.Decision != policy.DecisionAllow {
			falseDeny++
		}
		polVersion := "none"
		polDigest := "sha256:0000000000000000000000000000000000000000000000000000000000000000"
		if sc.Policy != nil {
			polVersion = sc.Policy.Version
			if polVersion == "" {
				polVersion = "invalid"
			}
			raw, _ := json.Marshal(sc.Policy)
			polDigest = evidence.PolicySHA256Bytes(raw)
		}
		inputsDigest, _ := evidence.InputsDigest(sc.Input)
		env, err := evidence.Build(evidence.BuildOptionsFromDecision(
			sc.ID, "unit-corpus", polVersion, polDigest, inputsDigest, d,
		))
		if err != nil {
			fmt.Fprintf(os.Stderr, "envelope %s: %v\n", sc.ID, err)
			os.Exit(1)
		}
		var envMap map[string]any
		b, _ := json.Marshal(env)
		_ = json.Unmarshal(b, &envMap)
		out = append(out, row{
			ScenarioID: sc.ID,
			Expected:   sc.Expected,
			Actual:     d.Decision,
			Match:      match,
			Reason:     d.Reason,
			Envelope:   envMap,
		})
	}

	unitPassed, unitFailed := runUnitTests()
	if unitFailed > 0 {
		passed = false
	}

	commit := evidence.ResolveCommitSHA()
	e2eSkipped := true
	e2eReason := "unit corpus only; e2e recorded separately by test/e2e when run"
	if v := os.Getenv("E2E_PASSED"); v != "" {
		e2eSkipped = false
		e2eReason = ""
	}
	var e2ePassed *bool
	if !e2eSkipped {
		v := os.Getenv("E2E_PASSED") == "1" || strings.EqualFold(os.Getenv("E2E_PASSED"), "true")
		e2ePassed = &v
		if !v {
			passed = false
		}
	}

	report := map[string]any{
		"generated_at":  time.Now().UTC().Format(time.RFC3339),
		"commit_sha":    commit,
		"passed":        passed,
		"suite_time_ms": time.Since(start).Milliseconds(),
		"metrics": map[string]any{
			"claims_scope":           fmt.Sprintf("on this corpus of %d scenarios", len(scenarios)),
			"scenario_count":         len(scenarios),
			"allow_count":            allow,
			"deny_count":             deny,
			"hold_count":             hold,
			"false_allow_count":      falseAllow,
			"false_deny_legitimate":  falseDeny,
			"unit_tests_passed":      unitPassed,
			"unit_tests_failed":      unitFailed,
			"e2e_skipped":            e2eSkipped,
			"e2e_skip_reason":        e2eReason,
			"e2e_passed":             e2ePassed,
		},
		"scenarios": out,
	}
	if err := os.MkdirAll("results", 0o755); err != nil {
		fatal(err)
	}
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fatal(err)
	}
	if err := os.WriteFile("results/results.json", raw, 0o644); err != nil {
		fatal(err)
	}
	fmt.Printf("wrote results/results.json scenarios=%d passed=%v\n", len(scenarios), passed)
	if !passed {
		os.Exit(1)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func runUnitTests() (passed, failed int) {
	cmd := exec.Command("go", "test", "./internal/...", "-count=1", "-v")
	out, err := cmd.CombinedOutput()
	text := string(out)
	fmt.Print(text)
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "--- PASS:") {
			passed++
		}
		if strings.HasPrefix(line, "--- FAIL:") {
			failed++
		}
	}
	if err != nil && failed == 0 {
		failed = 1
	}
	return passed, failed
}
