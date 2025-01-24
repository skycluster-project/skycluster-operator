package core

import (
	"context"
	"fmt"

	"encoding/json"

	corev1alpha1 "github.com/etesami/skycluster-manager/api/core/v1alpha1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// /////////////////// Object Functions //////////////////////

func FilterObjectByLabels(
	kubeClient client.Client,
	searchLabels map[string]string, refType map[string]string) (*unstructured.UnstructuredList, error) {
	// namespace, depKind, depGroup, depVersion string) (*unstructured.UnstructuredList, error) {
	// Iterate over the list of objects with given group, version and kind
	// and search for the object with the given labels
	unstructuredObjList := &unstructured.UnstructuredList{}
	unstructuredObjList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   refType["group"],
		Version: refType["version"],
		Kind:    refType["kind"],
	})
	if err := kubeClient.List(context.Background(), unstructuredObjList, client.MatchingLabels(searchLabels)); err != nil {
		return nil, err
	}
	return unstructuredObjList, nil
}

func LabelsExist(objLabels map[string]string, labelKeys []string) bool {
	for _, key := range labelKeys {
		if _, exists := objLabels[key]; !exists {
			return false
		}
	}
	return true
}

func CompareAndUpdateLabels(objLabels map[string]string, labels map[string]string) bool {
	modified := false
	if objLabels == nil {
		objLabels = make(map[string]string)
	}
	for key, value := range labels {
		vv, exists := objLabels[key]
		if !exists || vv != value {
			objLabels[key] = value
			modified = true
		}
	}
	return modified
}

// /////////////////// Config Maps Functions //////////////////////

func GetConfigMapsByLabels(namespace string, searchLabels map[string]string, kubeClient client.Client) (*corev1.ConfigMapList, error) {
	cmList := &corev1.ConfigMapList{}
	listOptions := &client.ListOptions{
		Namespace:     namespace,
		LabelSelector: labels.SelectorFromSet(searchLabels),
	}
	if err := kubeClient.List(context.Background(), cmList, listOptions); err != nil {
		return nil, err
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

func GetProviderTypeFromConfigMap(kubeClient client.Client, providerLabels map[string]string) (string, error) {
	if configMaps, err := GetConfigMapsByLabels(corev1alpha1.SkyClusterNamespace, providerLabels, kubeClient); err != nil || configMaps == nil {
		return "", errors.Wrap(err, "failed to get ConfigMaps by labels")
	} else {
		// check the length of the configMaps
		if len(configMaps.Items) != 1 {
			return "", errors.New(fmt.Sprintf("expected 1 ConfigMap, got %d", len(configMaps.Items)))
		}
		for _, configMap := range configMaps.Items {
			if value, exists := configMap.Labels[corev1alpha1.SkyClusterProviderType]; exists {
				return value, nil
			}
		}
	}
	return "", errors.New("provider type not found from any ConfigMap")
}

// /////////////////// Unstructured Object Functions //////////////////////

// These functions are used to evaluate the content of dependsOn and dependents fields
// of the various objects.
// m.(type): map[string]interface{}
// m["spec"].(type): the type is interface{} but should be casted to map[string]interface{}
// e.g. m["spec"].(map[string]interface{}) then we can get dependsOn field
// m["spec"]["dependsOn"].(type): []interface{}

// The difference between this function and the metav1 SetNestedField function is that
// the obj["field1"]["field2"]...["fieldN"] is of type []interface{}
// So we are appending to the list
// func InsertNestedField(ctx context.Context, obj map[string]interface{}, value interface{}, fields ...string) error {
// 	return insertNestedFieldNoCpoy(ctx, obj, runtime.DeepCopyJSONValue(value), fields...)
// }

func InsertNestedField(obj map[string]interface{}, value interface{}, fields ...string) error {
	if len(fields) == 0 {
		return errors.New("no fields provided")
	}
	m := obj
	for _, field := range fields[:len(fields)-1] {
		if val, ok := m[field]; ok {
			if valMap, ok := val.(map[string]interface{}); ok {
				m = valMap
			} else {
				newMap := make(map[string]interface{})
				m[field] = newMap
				m = newMap
			}
		} else {
			newMap := make(map[string]interface{})
			m[field] = newMap
			m = newMap
		}
	}
	field := fields[len(fields)-1]
	if valList, ok := m[field].([]interface{}); ok {
		m[field] = append(valList, value)
	} else if m[field] == nil {
		m[field] = []interface{}{value}
	} else {
		return errors.New("field not found in the object")
	}
	return nil
}

// Return the map[string]interface{} of an object
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

func AppendToSliceField(obj map[string]interface{}, value interface{}, field ...string) error {
	m := obj
	for _, f := range field[:len(field)-1] {
		if val, ok := m[f]; ok {
			m = val.(map[string]interface{})
		} else if vList, ok := m[f].([]interface{}); ok {
			vList = append(vList, value)
			m[f] = vList
		} else {
			return errors.New("field not found in	the object")
		}
	}
	return nil
}
