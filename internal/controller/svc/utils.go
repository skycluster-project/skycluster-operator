package svc

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/time/rate"
	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	corev1alpha1 "github.com/etesami/skycluster-manager/api/core/v1alpha1"
	svcv1alpha1 "github.com/etesami/skycluster-manager/api/svc/v1alpha1"
	ctrlutils "github.com/etesami/skycluster-manager/internal/controller"
)

func newCustomRateLimiter() workqueue.TypedRateLimiter[reconcile.Request] {
	return workqueue.NewTypedMaxOfRateLimiter(
		workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](5*time.Second, 30*time.Second),
		&workqueue.TypedBucketRateLimiter[reconcile.Request]{Limiter: rate.NewLimiter(rate.Limit(10), 100)},
	)
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

// GetProviderTypeFromConfigMap returns the provider type from the ConfigMap with the given providerLabels labels
func getProviderTypeFromConfigMap(c client.Client, providerLabels map[string]string) (string, error) {
	if configMaps, err := ctrlutils.GetConfigMapsByLabels(c, corev1alpha1.SKYCLUSTER_NAMESPACE, providerLabels); err != nil || configMaps == nil {
		return "", errors.Wrap(err, "failed to get ConfigMaps by labels")
	} else {
		// check the length of the configMaps
		if len(configMaps.Items) != 1 {
			return "", errors.New(fmt.Sprintf("expected 1 ConfigMap, got %d", len(configMaps.Items)))
		}
		for _, configMap := range configMaps.Items {
			if value, exists := configMap.Labels[corev1alpha1.SKYCLUSTER_PROVIDERTYPE_LABEL]; exists {
				return value, nil
			}
		}
	}
	return "", errors.New("provider type not found from any ConfigMap")
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
	if spec.ProviderRef.ProviderName != "" {
		providerRef["providerName"] = spec.ProviderRef.ProviderName
	}
	if spec.ProviderRef.ProviderType != "" {
		providerRef["providerType"] = spec.ProviderRef.ProviderType
	}
	if spec.ProviderRef.ProviderZone != "" {
		providerRef["providerZone"] = spec.ProviderRef.ProviderZone
	}
	if spec.ProviderRef.ProviderRegion != "" {
		providerRef["providerRegion"] = spec.ProviderRef.ProviderRegion
	}

	return map[string]any{
		"forProvider": forProvider,
		"providerRef": providerRef,
	}
}

// addDefaultLabels return the default skycluster labels
func addDefaultLabels(providerSpec corev1alpha1.ProviderRefSpec) map[string]string {
	labels := make(map[string]string)
	if providerSpec.ProviderName != "" {
		labels[corev1alpha1.SKYCLUSTER_PROVIDERNAME_LABEL] = providerSpec.ProviderName
	}
	if providerSpec.ProviderRegion != "" {
		labels[corev1alpha1.SKYCLUSTER_PROVIDERREGION_LABEL] = providerSpec.ProviderRegion
	}
	if providerSpec.ProviderZone != "" {
		labels[corev1alpha1.SKYCLUSTER_PROVIDERZONE_LABEL] = providerSpec.ProviderZone
	}
	if providerSpec.ProviderType != "" {
		labels[corev1alpha1.SKYCLUSTER_PROVIDERTYPE_LABEL] = providerSpec.ProviderType
	}
	labels[corev1alpha1.SKYCLUSTER_MANAGEDBY_LABEL] = corev1alpha1.SKYCLUSTER_MANAGEDBY_VALUE
	return labels
}
