package workloadinfo_test

import (
	"math"
	"testing"

	"github.com/justrunme/gpu-admission-kueue/internal/workloadinfo"
)

func TestParseEstimatedHoursOK(t *testing.T) {
	h, ok, err := workloadinfo.ParseEstimatedHours(map[string]string{
		workloadinfo.AnnotationEstimatedHours: "2.5",
	})
	if err != nil || !ok || h != 2.5 {
		t.Fatalf("got hours=%v ok=%v err=%v", h, ok, err)
	}
}

func TestParseEstimatedHoursMissing(t *testing.T) {
	_, ok, err := workloadinfo.ParseEstimatedHours(nil)
	if err != nil || ok {
		t.Fatalf("expected missing ok=false, got ok=%v err=%v", ok, err)
	}
}

func TestParseEstimatedHoursFailClosed(t *testing.T) {
	cases := []string{"-1", "0", "NaN", "+Inf", "-Inf", "abc"}
	for _, raw := range cases {
		_, ok, err := workloadinfo.ParseEstimatedHours(map[string]string{
			workloadinfo.AnnotationEstimatedHours: raw,
		})
		if err == nil {
			t.Fatalf("expected error for %q", raw)
		}
		if !ok {
			t.Fatalf("present-but-invalid should set ok=true for %q", raw)
		}
	}
	// Explicit Inf via formatted float
	_, _, err := workloadinfo.ParseEstimatedHours(map[string]string{
		workloadinfo.AnnotationEstimatedHours: stringsInf(),
	})
	if err == nil {
		t.Fatal("expected error for +Inf")
	}
	_ = math.Inf(1)
}

func stringsInf() string { return "+Inf" }

func TestImageMatchesAllowlistPrefixPath(t *testing.T) {
	allow := []string{"ghcr.io/acme/", "registry.example.com/"}
	if !workloadinfo.ImageMatchesAllowlist("ghcr.io/acme/app:1.0", allow) {
		t.Fatal("expected acme image allowed")
	}
	if workloadinfo.ImageMatchesAllowlist("ghcr.io/evil/x:latest", allow) {
		t.Fatal("ghcr.io/evil must NOT match ghcr.io/acme/ prefix")
	}
	if workloadinfo.ImageMatchesAllowlist("evil.example/x", allow) {
		t.Fatal("unrelated registry must be denied")
	}
	if !workloadinfo.ImageMatchesAllowlist("registry.example.com/ml/train:1.0", allow) {
		t.Fatal("expected registry.example.com image allowed")
	}
}

func TestImageMatchesAllowlistHostBoundaryRejectsEvilSubdomain(t *testing.T) {
	// Without trailing slash: exact host / path-boundary match only.
	allow := []string{"registry.example.com"}
	if !workloadinfo.ImageMatchesAllowlist("registry.example.com/ml/train:1.0", allow) {
		t.Fatal("expected exact-host allow for registry.example.com/...")
	}
	if workloadinfo.ImageMatchesAllowlist("registry.example.com.evil.io/x", allow) {
		t.Fatal("registry.example.com.evil.io must be rejected (no path boundary)")
	}
	// With trailing slash: still must not match evil subdomain.
	allowSlash := []string{"registry.example.com/"}
	if workloadinfo.ImageMatchesAllowlist("registry.example.com.evil.io/x", allowSlash) {
		t.Fatal("trailing-slash prefix must not match evil subdomain")
	}
}
