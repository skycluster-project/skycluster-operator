package svc

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/time/rate"
	"gopkg.in/yaml.v2"

	corev1alpha1 "github.com/etesami/skycluster-manager/api/core/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
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
		if val, ok := m[field].(map[string]any); ok {
			m = val
		} else {
			return nil, fmt.Errorf("field [%s] not found in the object or its type is not map[string]interface{}", field)
		}
	}
	return m, nil // the last field is not found in the object
}

// GetNestedValue returns the nested value of a map[string]interface{} object
func GetNestedValue(obj map[string]any, fields ...string) (any, error) {
	f := fields[:len(fields)-1]
	value, err := GetNestedField(obj, f...)
	if err != nil {
		return nil, err
	}
	if val, ok := value[fields[len(fields)-1]]; ok {
		return val, nil
	}
	return nil, fmt.Errorf("field %s not found in the object", fields[len(fields)-1])
}

// GetMapString returns a map[string]string from a map[string]interface{}
func GetMapString(m map[string]any) map[string]string {
	res := map[string]string{}
	for k, v := range m {
		res[k] = fmt.Sprintf("%v", v)
	}
	return res
}

// RemoveFromList removes the element at the given index from the list
func RemoveFromList(list []string, idx int) []string {
	return append(list[:idx], list[idx+1:]...)
}

// RemoveFromConditionListByType removes the condition with the given type from the list
func RemoveFromConditionListByType(list []metav1.Condition, key string) []metav1.Condition {
	for i, item := range list {
		if item.Type == key {
			return append(list[:i], list[i+1:]...)
		}
	}
	return list
}

// FindInList finds the index of the given key in the list of maps
func FindInList(list []map[string]string, key string) int {
	for i, item := range list {
		if _, ok := item[key]; ok {
			return i
		}
	}
	return -1
}

// FindInListValue finds the index of the given key-value pair in the list of interfaces
// The given key-value pair should be convertible to map[string]string
func FindInListValue(list []any, key string, value string) int {
	for i, item := range list {
		if val, ok := item.(map[string]any)[key]; ok && val.(string) == value {
			return i
		}
	}
	return -1
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

func getMessage(msg any) string {
	if msg == nil {
		return ""
	}
	return msg.(string)
}
