package workloadinfo_test

import (
	"encoding/json"
	"testing"

	"github.com/justrunme/gpu-admission-kueue/internal/workloadinfo"
)

func TestParseGPUQuantityOK(t *testing.T) {
	cases := []struct {
		in   any
		want int64
	}{
		{int64(1), 1},
		{int(2), 2},
		{float64(3), 3},
		{json.Number("4"), 4},
		{"5", 5},
		{"0", 0},
	}
	for _, tc := range cases {
		got, err := workloadinfo.ParseGPUQuantity(tc.in)
		if err != nil {
			t.Fatalf("input %#v: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("input %#v: got %d want %d", tc.in, got, tc.want)
		}
	}
}

func TestParseGPUQuantityFailClosed(t *testing.T) {
	bad := []any{
		nil,
		"abc",
		"",
		"1.5",
		float64(1.5),
		json.Number("2.5"),
		"-1",
		int64(-3),
		"1Gi",
		"500m",
		struct{}{},
	}
	for _, in := range bad {
		if _, err := workloadinfo.ParseGPUQuantity(in); err == nil {
			t.Fatalf("expected error for %#v", in)
		}
	}
}
