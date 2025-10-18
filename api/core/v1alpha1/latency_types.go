/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"errors"
	"math"
	"math/rand/v2"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LatencySpec defines the desired state of Latency
type LatencySpec struct {
	// ProviderA and ProviderB are canonical sorted provider names (A < B)
	ProviderRefA corev1.LocalObjectReference `json:"providerRefA"`
	ProviderRefB corev1.LocalObjectReference `json:"providerRefB"`
	// FixedLatencyMs is the seeded latency (from your fixed table)
	FixedLatencyMs string `json:"fixedLatencyMs,omitempty"`
}

// LatencyStatus defines the observed state of Latency.
type LatencyStatus struct {
	LastMeasuredMs  string           `json:"lastMeasuredMs,omitempty"`
	LastMeasuredAt  *metav1.Time   `json:"lastMeasuredAt,omitempty"`
	P50             string           `json:"p50,omitempty"`
	P95             string           `json:"p95,omitempty"`
	P99             string           `json:"p99,omitempty"`
	MeasurementCount int           `json:"measurementCount,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Last_Latency(ms)",type=string,JSONPath=".status.lastMeasuredMs"
// +kubebuilder:printcolumn:name="P99(ms)",type=string,JSONPath=".status.p99"
// +kubebuilder:printcolumn:name="Last_Measured",type=string,JSONPath=".status.lastMeasuredAt"

// Latency is the Schema for the latencies API
type Latency struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of Latency
	// +required
	Spec LatencySpec `json:"spec"`

	// status defines the observed state of Latency
	// +optional
	Status LatencyStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// LatencyList contains a list of Latency
type LatencyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Latency `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Latency{}, &LatencyList{})
}


// generateLatency returns latency in ms rounded to 2 decimals.
func generateLatency(srcRegion, dstRegion, srcType, dstType string, rng *rand.Rand) (float64, error) {
	// helper: sample from lognormal given desired mean and std (of the lognormal)
	lognormal := func(mean, std float64) float64 {
		if mean <= 0 {
			return 0
		}
		mu := math.Log((mean*mean) / math.Sqrt(std*std+mean*mean))
		sigma := math.Sqrt(math.Log(1 + (std/mean)*(std/mean)))

		// sample standard normal via Box-Muller
		u1 := rng.Float64()
		u2 := rng.Float64()
		z := math.Sqrt(-2*math.Log(u1)) * math.Cos(2*math.Pi*u2)

		return math.Exp(mu + sigma*z)
	}

	cloudCloudLatency := func(sameContinent bool) float64 {
		if sameContinent {
			return lognormal(100, 10)
		}
		return lognormal(200, 50)
	}
	cloudEdgeLatency := func() float64 {
		return lognormal(15.44, 7)
	}
	cloudNteLatency := func() float64 {
		return lognormal(25, 15)
	}
	edgeEdgeLatency := func() float64 {
		return lognormal(6, 4)
	}
	nteNteLatency := func() float64 {
		return lognormal(10, 1)
	}
	nteEdgeLatency := func() float64 {
		return lognormal(8, 3)
	}

	// Extract continent from region (part before first '-')
	srcContinent := srcRegion
	if parts := strings.SplitN(srcRegion, "-", 2); len(parts) >= 1 {
		srcContinent = parts[0]
	}
	dstContinent := dstRegion
	if parts := strings.SplitN(dstRegion, "-", 2); len(parts) >= 1 {
		dstContinent = parts[0]
	}

	sameRegion := srcRegion == dstRegion
	sameContinent := srcContinent == dstContinent

	var totalLatency float64

	switch {
	case srcType == "cloud" && dstType == "cloud":
		totalLatency = cloudCloudLatency(sameContinent)

	case (srcType == "cloud" && dstType == "nte") || (srcType == "nte" && dstType == "cloud"):
		totalLatency = cloudNteLatency()

	case (srcType == "cloud" && dstType == "edge") || (srcType == "edge" && dstType == "cloud"):
		totalLatency = cloudEdgeLatency()

	case srcType == "edge" && dstType == "edge":
		// optionally you could check sameRegion or sameContinent if needed
		totalLatency = edgeEdgeLatency()

	case srcType == "nte" && dstType == "nte":
		// only supported for same region / zone in the original; keep same behavior if needed:
		if !sameRegion {
			return 0, errors.New("nte-nte communication only supported within same region/zone")
		}
		totalLatency = nteNteLatency()

	case (srcType == "nte" && dstType == "edge") || (srcType == "edge" && dstType == "nte"):
		totalLatency = nteEdgeLatency()

	default:
		return 0, errors.New("unsupported communication type")
	}

	// round to 2 decimals
	rounded := math.Round(totalLatency*100) / 100
	return rounded, nil
}
