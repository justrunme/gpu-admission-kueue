// Package evidence builds ASC-compatible evidence envelopes for GPU admission decisions.
// Schema aligned with github.com/justrunme/agent-security-contract schemas/evidence-envelope.schema.json.
package evidence

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/justrunme/gpu-admission-kueue/internal/policy"
)

const (
	SchemaVersion = "1.0.0"
	DigestField   = "envelope_sha256"
)

// Envelope is the shared evidence JSON record.
type Envelope struct {
	SchemaVersion  string         `json:"schema_version"`
	PolicyVersion  string         `json:"policy_version"`
	PolicySHA256   string         `json:"policy_sha256"`
	ScenarioID     string         `json:"scenario_id"`
	Verdict        string         `json:"verdict"`
	Reason         string         `json:"reason"`
	TraceID        string         `json:"trace_id"`
	CommitSHA      string         `json:"commit_sha"`
	Timestamp      string         `json:"timestamp"`
	BlockedCallID  *string        `json:"blocked_call_id"`
	Details        map[string]any `json:"details"`
	ACSDisposition string         `json:"acs_disposition"`
	EnvelopeSHA256 string         `json:"envelope_sha256"`
}

// Build constructs an envelope from a policy decision.
// details must include decision and inputs_digest when available.
func Build(opts BuildOptions) (Envelope, error) {
	if opts.PolicyVersion == "" {
		return Envelope{}, fmt.Errorf("policy_version required")
	}
	if opts.ScenarioID == "" {
		return Envelope{}, fmt.Errorf("scenario_id required")
	}
	if opts.Verdict == "" {
		return Envelope{}, fmt.Errorf("verdict required")
	}
	if opts.Reason == "" {
		return Envelope{}, fmt.Errorf("reason required")
	}
	policyDigest := opts.PolicySHA256
	if policyDigest == "" {
		policyDigest = "sha256:" + fmt.Sprintf("%064x", 0)
	}
	if opts.Details == nil {
		opts.Details = map[string]any{}
	}
	ts := opts.Timestamp
	if ts == "" {
		ts = time.Now().UTC().Truncate(time.Second).Format("2006-01-02T15:04:05Z")
	}
	commit := opts.CommitSHA
	if commit == "" {
		commit = ResolveCommitSHA()
	}
	trace := opts.TraceID
	if trace == "" {
		trace = "unknown"
	}
	disposition := "DENY"
	switch opts.Verdict {
	case policy.VerdictAllow:
		disposition = "ALLOW"
	case "REQUIRE_APPROVAL":
		disposition = "ASK"
	}

	env := Envelope{
		SchemaVersion:  SchemaVersion,
		PolicyVersion:  opts.PolicyVersion,
		PolicySHA256:   policyDigest,
		ScenarioID:     opts.ScenarioID,
		Verdict:        opts.Verdict,
		Reason:         opts.Reason,
		TraceID:        trace,
		CommitSHA:      commit,
		Timestamp:      ts,
		BlockedCallID:  opts.BlockedCallID,
		Details:        opts.Details,
		ACSDisposition: disposition,
	}
	digest, err := EnvelopeDigest(env)
	if err != nil {
		return Envelope{}, err
	}
	env.EnvelopeSHA256 = digest
	return env, nil
}

// BuildOptions configures envelope construction.
type BuildOptions struct {
	PolicyVersion string
	PolicySHA256  string
	ScenarioID    string
	Verdict       string
	Reason        string
	TraceID       string
	CommitSHA     string
	Timestamp     string
	BlockedCallID *string
	Details       map[string]any
}

// BuildOptionsFromDecision maps a policy.Decision into BuildOptions details.
func BuildOptionsFromDecision(
	scenarioID, traceID, policyVersion, policySHA256, inputsDigest string,
	d policy.Decision,
) BuildOptions {
	details := map[string]any{}
	for k, v := range d.Details {
		details[k] = v
	}
	details["decision"] = d.Decision
	details["inputs_digest"] = inputsDigest
	var blocked *string
	if d.Verdict != policy.VerdictAllow {
		id := scenarioID
		blocked = &id
	}
	return BuildOptions{
		PolicyVersion: policyVersion,
		PolicySHA256:  policySHA256,
		ScenarioID:    scenarioID,
		Verdict:       d.Verdict,
		Reason:        d.Reason,
		TraceID:       traceID,
		Details:       details,
		BlockedCallID: blocked,
	}
}

// EnvelopeDigest is SHA-256 over canonical JSON of all fields except envelope_sha256.
func EnvelopeDigest(env Envelope) (string, error) {
	body := map[string]any{
		"schema_version":  env.SchemaVersion,
		"policy_version":  env.PolicyVersion,
		"policy_sha256":   env.PolicySHA256,
		"scenario_id":     env.ScenarioID,
		"verdict":         env.Verdict,
		"reason":          env.Reason,
		"trace_id":        env.TraceID,
		"commit_sha":      env.CommitSHA,
		"timestamp":       env.Timestamp,
		"blocked_call_id": env.BlockedCallID,
		"details":         env.Details,
		"acs_disposition": env.ACSDisposition,
	}
	canonical, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	// Re-marshal via map sorted by encoding/json (map keys sorted)
	var tmp any
	if err := json.Unmarshal(canonical, &tmp); err != nil {
		return "", err
	}
	sorted, err := json.Marshal(tmp)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(sorted)
	return fmt.Sprintf("sha256:%x", sum), nil
}

// Verify checks required fields and envelope_sha256 integrity.
func Verify(env Envelope) error {
	if env.SchemaVersion != SchemaVersion {
		return fmt.Errorf("schema_version %q != %q", env.SchemaVersion, SchemaVersion)
	}
	for _, pair := range []struct {
		name, val string
	}{
		{"policy_version", env.PolicyVersion},
		{"policy_sha256", env.PolicySHA256},
		{"scenario_id", env.ScenarioID},
		{"verdict", env.Verdict},
		{"reason", env.Reason},
		{"trace_id", env.TraceID},
		{"commit_sha", env.CommitSHA},
		{"timestamp", env.Timestamp},
		{"envelope_sha256", env.EnvelopeSHA256},
	} {
		if pair.val == "" {
			return fmt.Errorf("missing %s", pair.name)
		}
	}
	got, err := EnvelopeDigest(env)
	if err != nil {
		return err
	}
	if got != env.EnvelopeSHA256 {
		return fmt.Errorf("envelope_sha256 mismatch")
	}
	return nil
}

// ResolveCommitSHA records provenance for evidence envelopes.
//
// Rules (CI is the source of the real SHA):
//   - If GITHUB_SHA is set (CI), use it.
//   - Else if the working tree has uncommitted changes, return "dirty".
//   - Else return "unknown" (do not invent a local git SHA).
func ResolveCommitSHA() string {
	if v := os.Getenv("GITHUB_SHA"); v != "" {
		return v
	}
	if workingTreeDirty() {
		return "dirty"
	}
	return "unknown"
}

func workingTreeDirty() bool {
	if _, err := os.Stat(".git"); err != nil {
		return false
	}
	out, err := exec.Command("git", "status", "--porcelain").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}

// PolicySHA256Bytes hashes raw policy document bytes.
func PolicySHA256Bytes(raw []byte) string {
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("sha256:%x", sum)
}

// InputsDigest hashes canonical JSON of admission inputs.
func InputsDigest(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	var tmp any
	if err := json.Unmarshal(b, &tmp); err != nil {
		return "", err
	}
	sorted, err := json.Marshal(tmp)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(sorted)
	return fmt.Sprintf("sha256:%x", sum), nil
}

// MustJSON returns compact JSON or panics (tests/tools only).
func MustJSON(env Envelope) []byte {
	b, err := json.Marshal(env)
	if err != nil {
		panic(err)
	}
	return b
}
