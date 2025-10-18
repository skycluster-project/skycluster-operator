package svc

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/time/rate"
	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	hv1a1 "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"
	svcv1alpha1 "github.com/skycluster-project/skycluster-operator/api/svc/v1alpha1"
	ctrlutils "github.com/skycluster-project/skycluster-operator/internal/controller"
)

func newCustomRateLimiter() workqueue.TypedRateLimiter[reconcile.Request] {
	return workqueue.NewTypedMaxOfRateLimiter(
		workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](5*time.Second, 30*time.Second),
		&workqueue.TypedBucketRateLimiter[reconcile.Request]{Limiter: rate.NewLimiter(rate.Limit(10), 100)},
	)
}

// GetNestedField returns the nested field of a map[string]interface{} object
// It returns the nested field if it exists, and an error if it doesn't
// The fields are the keys to access the nested field
func GetNestedField(obj map[string]any, fields ...string) (map[string]any, error) {
	if len(fields) == 0 {
		return nil, errors.New("no fields provided")
	}
	m := obj
	for _, field := range fields {
		if val, ok := m[field].(map[string]interface{}); ok {
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
// func getUniqueProviders(manifests []hv1a1.SkyService) []hv1a1.ProviderRefSpec {
// 	pExists := map[string]any{}
// 	providers := []hv1a1.ProviderRefSpec{}
// 	for _, manifest := range manifests {
// 		// ProviderName uniquely identifies a provider
// 		// Only deployments are considered as the services do not tie to a provider
// 		if strings.ToLower(manifest.ComponentRef.Kind) != "deployment" {
// 			continue
// 		}
// 		pID := manifest.ProviderRef.Name
// 		// TODO: Remove this check
// 		if pID == "os" {
// 			fmt.Println("ProviderName cannot be 'os' [SkyApp]")
// 		}
// 		if _, ok := pExists[pID]; !ok {
// 			providers = append(providers, manifest.ProviderRef)
// 			pExists[pID] = struct{}{}
// 		}
// 	}
// 	return providers
// }

// // GetProviderTypeFromConfigMap returns the provider type from the ConfigMap with the given providerLabels labels
// func getProviderTypeFromConfigMap(c client.Client, providerLabels map[string]string) (string, error) {
// 	if configMaps, err := ctrlutils.GetConfigMapsByLabels(c, hv1a1.SKYCLUSTER_NAMESPACE, providerLabels); err != nil || configMaps == nil {
// 		return "", errors.Wrap(err, "failed to get ConfigMaps by labels")
// 	} else {
// 		// check the length of the configMaps
// 		if len(configMaps.Items) != 1 {
// 			return "", errors.New(fmt.Sprintf("expected 1 ConfigMap, got %d", len(configMaps.Items)))
// 		}
// 		for _, configMap := range configMaps.Items {
// 			if value, exists := configMap.Labels[hv1a1.SKYCLUSTER_PROVIDERTYPE_LABEL]; exists {
// 				return value, nil
// 			}
// 		}
// 	}
// 	return "", errors.New("provider type not found from any ConfigMap")
// }

// // sameProviders returns true if the two providers are the same
// // based on the provider name, region and zone
// func sameProviders(p1, p2 hv1a1.ProviderRefSpec) bool {
// 	return p1.Name == p2.Name &&
// 		p1.Region == p2.Region &&
// 		p1.Zone == p2.Zone
// }

// // getProviderId returns a unique identifier for the provider
// // based on the provider name, region, zone and type
// func getProviderId(p hv1a1.ProviderRefSpec) string {
// 	var parts []string
// 	if p.Name != "" {
// 		parts = append(parts, p.Name)
// 	}
// 	if p.Region != "" {
// 		parts = append(parts, p.Region)
// 	}
// 	if p.Zone != "" {
// 		parts = append(parts, p.Zone)
// 	}
// 	if p.Type != "" {
// 		parts = append(parts, p.Type)
// 	}
// 	return strings.Join(parts, "-")
// }

// getProviderCfgName returns the providerConfig name from the given object
func getProviderCfgName(obj *unstructured.Unstructured) (string, error) {
	pConfigName, err := ctrlutils.GetNestedValue(obj.Object, "status", "gateway", "providerConfig")
	if err != nil {
		return "", err
	}
	if _, ok := pConfigName.(string); !ok {
		return "", fmt.Errorf("providerConfig name is not of type string")
	}
	return pConfigName.(string), nil
}

// generateProviderGwSpec generates the provider gateway spec for the given provider spec
func generateProviderGwSpec(spec svcv1alpha1.SkyProviderSpec) map[string]any {
	forProvider := make(map[string]any)

	// Gateway
	gateway := make(map[string]any)
	if spec.ProviderGateway.Flavor != "" {
		gateway["flavor"] = spec.ProviderGateway.Flavor
	}
	if spec.ProviderGateway.PublicKey != "" {
		gateway["publicKey"] = spec.ProviderGateway.PublicKey
	}
	if len(gateway) > 0 {
		forProvider["gateway"] = gateway
	}

	// Overlay
	overlay := make(map[string]any)
	if spec.ProviderGateway.Overlay.Host != "" {
		overlay["host"] = spec.ProviderGateway.Overlay.Host
	}
	if spec.ProviderGateway.Overlay.Port != 0 {
		overlay["port"] = spec.ProviderGateway.Overlay.Port
	}
	if spec.ProviderGateway.Overlay.Token != "" {
		overlay["token"] = spec.ProviderGateway.Overlay.Token
	}
	if len(overlay) > 0 {
		forProvider["overlay"] = overlay
	}

	// ProviderRef
	providerRef := make(map[string]any)
	if spec.ProviderRef.Platform != "" {
		providerRef["platform"] = spec.ProviderRef.Platform
	}
	if spec.ProviderRef.Name != "" {
		providerRef["name"] = spec.ProviderRef.Name
	}
	if spec.ProviderRef.Type != "" {
		providerRef["type"] = spec.ProviderRef.Type
	}
	if spec.ProviderRef.Zone != "" {
		providerRef["zone"] = spec.ProviderRef.Zone
	}
	if spec.ProviderRef.Region != "" {
		providerRef["region"] = spec.ProviderRef.Region
	}
	if spec.ProviderRef.RegionAlias != "" {
		providerRef["regionAlias"] = spec.ProviderRef.RegionAlias
	}

	return map[string]any{
		"forProvider": forProvider,
		"providerRef": providerRef,
	}
}

// addDefaultLabels return the default skycluster labels
func addDefaultLabels(providerSpec hv1a1.ProviderRefSpec) map[string]string {
	labels := make(map[string]string)
	if providerSpec.Name != "" {
		labels[hv1a1.SKYCLUSTER_PROVIDERNAME_LABEL] = providerSpec.Name
	}
	if providerSpec.Region != "" {
		labels[hv1a1.SKYCLUSTER_PROVIDERREGION_LABEL] = providerSpec.Region
	}
	if providerSpec.Zone != "" {
		labels[hv1a1.SKYCLUSTER_PROVIDERZONE_LABEL] = providerSpec.Zone
	}
	if providerSpec.Type != "" {
		labels[hv1a1.SKYCLUSTER_PROVIDERTYPE_LABEL] = providerSpec.Type
	}
	labels[hv1a1.SKYCLUSTER_MANAGEDBY_LABEL] = hv1a1.SKYCLUSTER_MANAGEDBY_VALUE
	return labels
}
