package workloadinfo

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ParseGPUQuantity parses a GPU resource quantity from unstructured JSON.
// Fail-closed: unparseable or non-integer (fractional) values return an error.
// Callers must Reject / DENY rather than treating the value as zero.
func ParseGPUQuantity(v any) (int64, error) {
	switch t := v.(type) {
	case nil:
		return 0, fmt.Errorf("gpu quantity is nil")
	case int64:
		if t < 0 {
			return 0, fmt.Errorf("gpu quantity is negative: %d", t)
		}
		return t, nil
	case int:
		if t < 0 {
			return 0, fmt.Errorf("gpu quantity is negative: %d", t)
		}
		return int64(t), nil
	case int32:
		if t < 0 {
			return 0, fmt.Errorf("gpu quantity is negative: %d", t)
		}
		return int64(t), nil
	case float64:
		if math.IsNaN(t) || math.IsInf(t, 0) {
			return 0, fmt.Errorf("gpu quantity is not a finite number")
		}
		if t < 0 {
			return 0, fmt.Errorf("gpu quantity is negative: %v", t)
		}
		if t != math.Trunc(t) {
			return 0, fmt.Errorf("gpu quantity is fractional: %v", t)
		}
		return int64(t), nil
	case json.Number:
		s := t.String()
		if strings.ContainsAny(s, ".eE") {
			f, err := t.Float64()
			if err != nil {
				return 0, fmt.Errorf("gpu quantity unparseable: %w", err)
			}
			return ParseGPUQuantity(f)
		}
		i, err := t.Int64()
		if err != nil {
			return 0, fmt.Errorf("gpu quantity unparseable: %w", err)
		}
		if i < 0 {
			return 0, fmt.Errorf("gpu quantity is negative: %d", i)
		}
		return i, nil
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return 0, fmt.Errorf("gpu quantity is empty")
		}
		// Kubernetes quantities may use suffixes; for GPUs we require a plain integer.
		if strings.ContainsAny(s, ".eEmMkKgGtTpP") {
			// Allow only pure integer decimal form without suffix.
			if strings.ContainsAny(s, ".eE") || hasMetricSuffix(s) {
				return 0, fmt.Errorf("gpu quantity must be a whole integer without suffix: %q", s)
			}
		}
		i, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("gpu quantity unparseable: %q", s)
		}
		if i < 0 {
			return 0, fmt.Errorf("gpu quantity is negative: %d", i)
		}
		return i, nil
	default:
		return 0, fmt.Errorf("gpu quantity has unsupported type %T", v)
	}
}

func hasMetricSuffix(s string) bool {
	for _, suf := range []string{"m", "k", "M", "G", "T", "P", "Ki", "Mi", "Gi", "Ti", "Pi"} {
		if strings.HasSuffix(s, suf) && len(s) > len(suf) {
			return true
		}
	}
	return false
}
