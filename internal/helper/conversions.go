package helper

import (
	"fmt"
	"strconv"
	"strings"
)

// NormalizeToTOPS converts a compute value (with unit) into TOPS
func NormalizeToTOPS(value float64, unit string) float64 {
	unit = strings.ToUpper(strings.TrimSpace(unit))

	switch unit {
	case "GFLOPS":
		// 1 GFLOP = 0.001 TFLOP ≈ 0.002 TOPS
		return value * 0.002
	case "TFLOPS":
		// 1 TFLOP ≈ 2 TOPS (rough heuristic for FP32->INT8 conversion)
		return value * 2.0
	case "TOPS":
		return value
	default:
		fmt.Printf("Warning: Unknown unit '%s', returning raw value\n", unit)
		return value
	}
}

func ParseAmount(s string) (float64, error) {
	s = strings.TrimSpace(s)

	// handle parentheses as negative: (123.45)
	neg := false
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		neg = true
		s = s[1 : len(s)-1]
	}

	// remove dollar sign(s), commas and surrounding spaces
	s = strings.ReplaceAll(s, "$", "")
	s = strings.ReplaceAll(s, ",", "")
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("no numeric content")
	}

	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	if neg {
		v = -v
	}
	return v, nil
}
