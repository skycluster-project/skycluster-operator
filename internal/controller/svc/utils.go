package svc

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/time/rate"
	"gopkg.in/yaml.v2"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	corev1alpha1 "github.com/etesami/skycluster-manager/api/core/v1alpha1"
	ctrlutils "github.com/etesami/skycluster-manager/internal/controller"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// getStatus returns the metav1.ConditionStatus based on the given status string
func getStatus(status any) metav1.ConditionStatus {
	if status == nil {
		return metav1.ConditionUnknown
	}
	switch status {
	case "True":
		return metav1.ConditionTrue
	case "False":
		return metav1.ConditionFalse
	default:
		return metav1.ConditionUnknown
	}
}
