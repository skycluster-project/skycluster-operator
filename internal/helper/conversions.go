package helper

import (
	"fmt"
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