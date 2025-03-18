package svc

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pkg/errors"

	corev1alpha1 "github.com/etesami/skycluster-manager/api/core/v1alpha1"
	"gopkg.in/yaml.v2"
)

// GetNestedField returns the nested field of a map[string]interface{} object
// It returns the nested field if it exists, and an error if it doesn't
// The fields are the keys to access the nested field
func GetNestedField(obj map[string]any, fields ...string) (map[string]any, error) {
	if len(fields) == 0 {
		return nil, errors.New("no fields provided")
	}
	m := obj
	for _, field := range fields {
		if val, ok := m[field].(map[string]any); ok {
			m = val
		} else {
			return nil, fmt.Errorf("field %s not found in the object or its type is not map[string]interface{}", field)
		}
	}
	return m, nil // the last field is not found in the object
}

// generateYAMLManifest generates a string YAML manifest from the given object
func generateYAMLManifest(obj any) (string, error) {
	var inInterface map[string]interface{}
	inrec, _ := json.Marshal(obj)
	json.Unmarshal(inrec, &inInterface)
	objYAML, err := yaml.Marshal(&inInterface)
	if err != nil {
		return "", errors.Wrap(err, "Error marshalling obj manifests.")
	}
	return string(objYAML), nil
}

// getUniqueProviders returns a list of unique providers from the given manifests
func getUniqueProviders(manifests []corev1alpha1.SkyService) []corev1alpha1.ProviderRefSpec {
	pExists := map[string]any{}
	providers := []corev1alpha1.ProviderRefSpec{}
	for _, manifest := range manifests {
		// ProviderName uniquely identifies a provider
		// Only deployments are considered as the services do not tie to a provider
		if strings.ToLower(manifest.ComponentRef.Kind) != "deployment" {
			continue
		}
		pID := manifest.ProviderRef.ProviderName
		// TODO: Remove this check
		if pID == "os" {
			fmt.Println("ProviderName cannot be 'os' [SkyApp]")
		}
		if _, ok := pExists[pID]; !ok {
			providers = append(providers, manifest.ProviderRef)
			pExists[pID] = struct{}{}
		}
	}
	return providers
}

// sameProviders returns true if the two providers are the same
// based on the provider name, region and zone
func sameProviders(p1, p2 corev1alpha1.ProviderRefSpec) bool {
	return p1.ProviderName == p2.ProviderName &&
		p1.ProviderRegion == p2.ProviderRegion &&
		p1.ProviderZone == p2.ProviderZone
}

// getProviderId returns a unique identifier for the provider
// based on the provider name, region, zone and type
func getProviderId(p corev1alpha1.ProviderRefSpec) string {
	var parts []string
	if p.ProviderName != "" {
		parts = append(parts, p.ProviderName)
	}
	if p.ProviderRegion != "" {
		parts = append(parts, p.ProviderRegion)
	}
	if p.ProviderZone != "" {
		parts = append(parts, p.ProviderZone)
	}
	if p.ProviderType != "" {
		parts = append(parts, p.ProviderType)
	}
	return strings.Join(parts, "-")
}
