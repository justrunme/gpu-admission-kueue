// Command evidencegen renders docs/EVIDENCE.md from results/results.json.
// Numbers are never invented: they are copied from the results file only.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type resultsFile struct {
	GeneratedAt string         `json:"generated_at"`
	CommitSHA   string         `json:"commit_sha"`
	Passed      bool           `json:"passed"`
	SuiteTimeMs int64          `json:"suite_time_ms"`
	Metrics     resultsMetrics `json:"metrics"`
	Scenarios   []scenarioRow  `json:"scenarios"`
	E2E         map[string]any `json:"e2e"`
}

type resultsMetrics struct {
	ClaimsScope         string `json:"claims_scope"`
	ScenarioCount       int    `json:"scenario_count"`
	AllowCount          int    `json:"allow_count"`
	DenyCount           int    `json:"deny_count"`
	HoldCount           int    `json:"hold_count"`
	FalseAllowCount     int    `json:"false_allow_count"`
	FalseDenyLegitimate int    `json:"false_deny_legitimate"`
	UnitTestsPassed     int    `json:"unit_tests_passed"`
	UnitTestsFailed     int    `json:"unit_tests_failed"`
	E2EPassed           *bool  `json:"e2e_passed,omitempty"`
	E2ESkipped          bool   `json:"e2e_skipped"`
	E2ESkipReason       string `json:"e2e_skip_reason,omitempty"`
}

type scenarioRow struct {
	ScenarioID string `json:"scenario_id"`
	Expected   string `json:"expected"`
	Actual     string `json:"actual"`
	Match      bool   `json:"match"`
	Reason     string `json:"reason"`
}

func main() {
	inPath := "results/results.json"
	outPath := "docs/EVIDENCE.md"
	if len(os.Args) > 1 {
		inPath = os.Args[1]
	}
	if len(os.Args) > 2 {
		outPath = os.Args[2]
	}
	raw, err := os.ReadFile(inPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read %s: %v\n", inPath, err)
		os.Exit(1)
	}
	var report resultsFile
	if err := json.Unmarshal(raw, &report); err != nil {
		fmt.Fprintf(os.Stderr, "parse %s: %v\n", inPath, err)
		os.Exit(1)
	}
	md := render(report)
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(outPath, []byte(md), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", outPath, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s from %s\n", outPath, inPath)
}

func render(r resultsFile) string {
	scope := r.Metrics.ClaimsScope
	if scope == "" {
		scope = fmt.Sprintf("on this corpus of %d scenarios", r.Metrics.ScenarioCount)
	}
	var b strings.Builder
	b.WriteString("# Evidence\n\n")
	b.WriteString("This file is **generated** from `results/results.json` by `go run ./cmd/evidencegen`.\n")
	b.WriteString("Do not edit numbers by hand; re-run unit/e2e and regenerate.\n\n")
	b.WriteString(fmt.Sprintf("All claims below are **%s** only. They do not imply general cluster safety.\n\n", scope))
	b.WriteString("## Run metrics\n\n")
	b.WriteString(fmt.Sprintf("- Scope: %s\n", scope))
	b.WriteString(fmt.Sprintf("- Generated at: `%s`\n", r.GeneratedAt))
	b.WriteString(fmt.Sprintf("- Commit: `%s`\n", r.CommitSHA))
	b.WriteString(fmt.Sprintf("- Corpus passed: `%t`\n", r.Passed))
	b.WriteString(fmt.Sprintf("- Suite time (ms): `%d`\n", r.SuiteTimeMs))
	b.WriteString(fmt.Sprintf("- Scenario count: `%d`\n", r.Metrics.ScenarioCount))
	b.WriteString(fmt.Sprintf("- ALLOW: `%d` · DENY: `%d` · HOLD: `%d`\n", r.Metrics.AllowCount, r.Metrics.DenyCount, r.Metrics.HoldCount))
	b.WriteString(fmt.Sprintf("- False-ALLOW count: `%d` (must be 0)\n", r.Metrics.FalseAllowCount))
	b.WriteString(fmt.Sprintf("- False-DENY (legitimate) count: `%d`\n", r.Metrics.FalseDenyLegitimate))
	b.WriteString(fmt.Sprintf("- Unit tests: passed `%d`, failed `%d`\n", r.Metrics.UnitTestsPassed, r.Metrics.UnitTestsFailed))
	if r.Metrics.E2ESkipped {
		b.WriteString(fmt.Sprintf("- E2E: skipped (`%s`)\n", r.Metrics.E2ESkipReason))
	} else if r.Metrics.E2EPassed != nil {
		b.WriteString(fmt.Sprintf("- E2E passed: `%t`\n", *r.Metrics.E2EPassed))
	}
	b.WriteString("\n## Per-scenario table\n\n")
	b.WriteString("| Scenario | Expected | Actual | Match | Reason |\n")
	b.WriteString("|---|---|---|---|---|\n")
	for _, s := range r.Scenarios {
		match := "yes"
		if !s.Match {
			match = "NO"
		}
		b.WriteString(fmt.Sprintf("| `%s` | %s | %s | %s | %s |\n",
			s.ScenarioID, s.Expected, s.Actual, match, escapePipe(s.Reason)))
	}
	b.WriteString("\n## Notes\n\n")
	b.WriteString("- Claims apply **on this corpus** only.\n")
	b.WriteString("- Kind demo advertises `nvidia.com/gpu` as an extended resource; it does not require physical GPUs.\n")
	b.WriteString("- Evidence envelopes use ASC-compatible `schema_version` `1.0.0` with `details.decision` and `details.inputs_digest`.\n")
	b.WriteString(fmt.Sprintf("- Renderer wall clock: `%s`\n", time.Now().UTC().Format(time.RFC3339)))
	if len(r.E2E) > 0 {
		b.WriteString("\n## Kind e2e (this run)\n\n")
		b.WriteString("- Scope: **on this e2e suite**\n")
		b.WriteString(fmt.Sprintf("- Passed: `%v`\n", r.E2E["passed"]))
		b.WriteString(fmt.Sprintf("- Pass count: `%v`\n", r.E2E["pass_count"]))
		b.WriteString(fmt.Sprintf("- Fail count: `%v`\n", r.E2E["fail_count"]))
		if passes, ok := r.E2E["passes"].([]any); ok {
			names := make([]string, 0, len(passes))
			for _, p := range passes {
				names = append(names, fmt.Sprintf("`%v`", p))
			}
			b.WriteString("- Cases: " + strings.Join(names, ", ") + "\n")
		}
		if n, ok := r.E2E["notes"].(string); ok && n != "" {
			b.WriteString("- Notes: " + n + "\n")
		}
	}
	b.WriteString("\n")
	return b.String()
}

func escapePipe(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}
