package core

import (
	"context"

	"encoding/json"

	corev1 "k8s.io/api/core/v1"

	corev1alpha1 "github.com/etesami/skycluster-manager/api/core/v1alpha1"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Unified function using the interface
func GeneratetLabelsforProvider(providerRef corev1alpha1.ProviderRefSpec) map[string]string {
	return map[string]string{
		"skycluster.io/provider-name":   providerRef.ProviderName,
		"skycluster.io/provider-region": providerRef.ProviderRegion,
		"skycluster.io/provider-zone":   providerRef.ProviderZone,
		"skycluster.io/project-id":      uuid.New().String(),
	}
}

func ListByGroupVersionKind(
	ctx context.Context, kubeClient client.Client, namespace, depKind, depGroup, depAPIVersion string) (*unstructured.UnstructuredList, error) {
	gvk := schema.GroupVersionKind{
		Group:   depGroup,
		Version: depAPIVersion,
		Kind:    depKind,
	}
	unstructuredObj := &unstructured.Unstructured{}
	unstructuredObj.SetGroupVersionKind(gvk)
	unstructuredObj.SetNamespace(namespace)
	// Iterate over the list of XProviderSetups
	unstructuredObjList := &unstructured.UnstructuredList{}
	unstructuredObjList.SetGroupVersionKind(gvk)
	if err := kubeClient.List(ctx, unstructuredObjList, &client.ListOptions{
		Namespace: namespace,
	}); err != nil {
		return nil, err
	}
	return unstructuredObjList, nil
}

func DependencyExists(unstructuredObjList unstructured.UnstructuredList, searchLabels map[string]string) (bool, error) {
	for _, foundObj := range unstructuredObjList.Items {
		foundObjLabels, found, err := unstructured.NestedMap(foundObj.Object, "metadata", "labels")
		if !found || err != nil {
			return false, errors.Wrap(err, "cannot get nested labels")
		}
		matched := true
		for key, value := range searchLabels {
			foundValue, exists := foundObjLabels[key]
			if !exists || foundValue != value {
				matched = false
				break
			}
		}
		if matched {
			return true, nil
		}
	}
	return false, nil
}

func DeepCopyField(field interface{}) (map[string]interface{}, error) {
	fieldBytes, err := json.Marshal(field)
	if err != nil {
		return nil, err
	}
	// Unmarshal JSON into a map
	var fieldMap map[string]interface{}
	if err := json.Unmarshal(fieldBytes, &fieldMap); err != nil {
		return nil, err
	}
	return fieldMap, nil
}

func LabelsExist(objLabels map[string]string, labelKeys []string) bool {
	for _, key := range labelKeys {
		if _, exists := objLabels[key]; !exists {
			return false
		}
	}
	return true
}

func GetConfigMapsByLabels(ctx context.Context, namespace string, searchLabels map[string]string, kubeClient client.Client) (*corev1.ConfigMapList, error) {
	cmList := &corev1.ConfigMapList{}
	listOptions := &client.ListOptions{
		Namespace:     namespace,
		LabelSelector: labels.SelectorFromSet(searchLabels),
	}
	if err := kubeClient.List(ctx, cmList, listOptions); err != nil {
		return nil, err
	}
	if len(cmList.Items) == 0 {
		return nil, nil
	}
	return cmList, nil
}

func GetConfigMap(ctx context.Context, name, namespace string, kubeClient client.Client) (*corev1.ConfigMap, error) {
	cm := &corev1.ConfigMap{}
	key := client.ObjectKey{Name: name, Namespace: namespace}
	if err := kubeClient.Get(ctx, key, cm); err != nil {
		return nil, err
	}
	return cm, nil
}
