package core

import (
	"context"
	"fmt"
	"reflect"

	"encoding/json"

	corev1alpha1 "github.com/etesami/skycluster-manager/api/core/v1alpha1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Object Functions //////////////////////

func GetUnstructuredObject(kubeClient client.Client, name, namespace string) (*unstructured.Unstructured, error) {
	unstructuredObj := &unstructured.Unstructured{}
	objKey := client.ObjectKey{Name: name, Namespace: namespace}
	if err := kubeClient.Get(context.Background(), objKey, unstructuredObj); err != nil {
		return nil, err
	}
	return unstructuredObj, nil
}

func ListUnstructuredObjectsByLabels(
	kubeClient client.Client, searchLabels map[string]string, refType map[string]string) (*unstructured.UnstructuredList, error) {
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

func ListUnstructuredObjectsByFieldList(
	kubeClient client.Client,
	searchSpec map[string]string,
	refType map[string]string,
	fields ...string) (*unstructured.UnstructuredList, error) {
	// Iterate over the list of objects with given group, version and kind
	// and search for the object with the given specs within the dependedBy list
	filteredObj := &unstructured.UnstructuredList{}
	unstructuredObjList := &unstructured.UnstructuredList{}
	unstructuredObjList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   refType["group"],
		Version: refType["version"],
		Kind:    refType["kind"],
	})
	if err := kubeClient.List(context.Background(), unstructuredObjList); err != nil {
		return nil, err
	}
	for _, obj := range unstructuredObjList.Items {
		if objSpec, err := GetNestedField(obj.Object, fields[:len(fields)-1]...); err != nil {
			return nil, err
		} else {
			descriptors := objSpec[fields[len(fields)-1]]
			switch d := descriptors.(type) {
			case []interface{}:
				for _, descriptor := range d {
					if descriptorMap, err := ConvertToMapString(descriptor); err != nil {
						return nil, err
					} else {
						if CompareStringMap(descriptorMap, searchSpec) {
							filteredObj.Items = append(filteredObj.Items, obj)
						}
					}
				}
			default:
				return nil, errors.New(fmt.Sprintf("field %s is not a list", fields[len(fields)-1]))
			}
		}
	}
	return filteredObj, nil
}

func ContainsLabels(objLabels map[string]string, labelKeys []string) bool {
	for _, key := range labelKeys {
		if _, exists := objLabels[key]; !exists {
			return false
		}
	}
	return true
}

// Update the labels of an object if they are different
// if the object labels are nil, they will be set to the labels
func UpdateLabelsIfDifferent(objLabels map[string]string, labels map[string]string) {
	if objLabels == nil {
		objLabels = labels
	}
	for key, value := range labels {
		vv, exists := (objLabels)[key]
		if !exists || vv != value {
			objLabels[key] = value
		}
	}
}

func CompareStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Returns true if two objects have the same labels and same values
func CompareStringMap(objLabels map[string]string, labels map[string]string) bool {
	keys1 := make([]string, 0, len(objLabels))
	keys2 := make([]string, 0, len(labels))
	if !CompareStringSlices(keys1, keys2) {
		return false
	}
	for key, value := range labels {
		if objLabels[key] != value {
			return false
		}
	}
	return true
}

func CompareObjectDescriptors(obj1, obj2 corev1alpha1.ObjectDescriptor) bool {
	return obj1.Name == obj2.Name &&
		obj1.Namespace == obj2.Namespace &&
		obj1.Group == obj2.Group &&
		obj1.Kind == obj2.Kind &&
		obj1.Version == obj2.Version
}

func AppendObjectDescriptor(objList *[]corev1alpha1.ObjectDescriptor, value corev1alpha1.ObjectDescriptor) {
	if objList == nil {
		// if the objList is nil we create a new object and therefore, should
		// assign its address to the objList, hecne, the objList should be a pointer
		objList = &[]corev1alpha1.ObjectDescriptor{value}
	} else {
		*objList = append(*objList, value)
	}
}

func ContainsObjectDescriptor(objList []corev1alpha1.ObjectDescriptor, value corev1alpha1.ObjectDescriptor) (bool, int) {
	exists, idx := false, -1
	for i, val := range objList {
		if CompareObjectDescriptors(val, value) {
			exists = true
			idx = i
			break
		}
	}
	return exists, idx
}

func RemoveObjectDescriptor(objList *[]corev1alpha1.ObjectDescriptor, idx int) {
	if objList == nil {
		return
	}
	*objList = append((*objList)[:idx], (*objList)[idx+1:]...)
}

func StructToMap(obj interface{}) map[string]string {
	result := make(map[string]string)
	val := reflect.ValueOf(obj)
	typ := reflect.TypeOf(obj)
	for i := 0; i < val.NumField(); i++ {
		field := typ.Field(i)
		value := val.Field(i).Interface()
		result[field.Name] = fmt.Sprintf("%v", value)
	}
	return result
}

// Config Maps Functions //////////////////////

func GetConfigMapsByLabels(kubeClient client.Client, namespace string, searchLabels map[string]string) (*corev1.ConfigMapList, error) {
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
	if configMaps, err := GetConfigMapsByLabels(kubeClient, corev1alpha1.SkyClusterNamespace, providerLabels); err != nil || configMaps == nil {
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

// Unstructured Object Functions //////////////////////

// The GetNestedField function is used to get the value of a nested field in a map[string]interface{}
// Call this function like this: m, err := GetNestedField(obj, "spec")
// then use m["dependsOn"] to get the value of the dependsOn field
// The functions return nil if the field is not found in the object
func GetNestedField(obj map[string]interface{}, fields ...string) (map[string]interface{}, error) {
	if len(fields) == 0 {
		return nil, errors.New("no fields provided")
	}
	m := obj
	for _, field := range fields {
		if val, ok := m[field].(map[string]interface{}); ok {
			m = val
		} else {
			return nil, errors.New(fmt.Sprintf("field %s not found in the object or its type is not map[string]interface{}", field))
		}
	}
	return m, nil // the last field is not found in the object
}

func RemoveFromNestedField(obj map[string]interface{}, idx int, fields ...string) error {
	m, err := GetNestedField(obj, fields[:len(fields)-1]...)
	if err != nil {
		return err
	}
	field := fields[len(fields)-1]
	switch m[field].(type) {
	case []interface{}:
		valList := m[field].([]interface{})
		m[field] = append(valList[:idx], valList[idx+1:]...)
	default:
		return errors.New(fmt.Sprintf("field %s not found in the object or its not a list", field))
	}
	return nil
}

// Append a value to a nested field in a map[string]interface{}
// if the field is nil, it will be set to a list containing the value
func AppendToNestedField(obj map[string]interface{}, value interface{}, fields ...string) error {
	m, err := GetNestedField(obj, fields[:len(fields)-1]...)
	if err != nil {
		return err
	}
	field := fields[len(fields)-1]
	switch m[field].(type) {
	case []interface{}:
		m[field] = append(m[field].([]interface{}), value)
	case nil:
		m[field] = []interface{}{value}
	default:
		return errors.New(fmt.Sprintf("field %s not found in the object or its not either nil or a list", field))
	}
	return nil
}

// Set the value of a nested field in a map[string]interface{}
// if the latest field is nil, it will be set to the value
// if the latest field is not nil, it will be overwritten
func SetNestedField(obj map[string]interface{}, value interface{}, fields ...string) error {
	m := obj
	for _, field := range fields[:len(fields)-1] {
		if val, ok := m[field]; ok {
			if valMap, ok := val.(map[string]interface{}); ok {
				m = valMap
			} else {
				return errors.New(fmt.Sprintf("field %s is not a map[string]interface{}", field))
			}
		} else {
			newVal := make(map[string]interface{})
			m[field] = newVal
			m = newVal
		}
	}
	field := fields[len(fields)-1]
	m[field] = value
	return nil
}

func ContainsNestedMap(obj map[string]interface{}, value map[string]string, fields ...string) (bool, int, error) {
	foundIdx, exists := -1, false
	m, err := GetNestedField(obj, fields[:len(fields)-1]...)
	if err != nil {
		return false, foundIdx, err
	}
	field := fields[len(fields)-1]
	switch m[field].(type) {
	case []interface{}:
		valList := m[field].([]interface{})
		for idx, val := range valList {
			if mapString, err := ConvertToMapString(val); err != nil {
				return false, foundIdx, err
			} else {
				if CompareStringMap(mapString, value) {
					exists = true
					foundIdx = idx
					break
				}
			}
		}
		return exists, foundIdx, nil
	case nil:
		return false, foundIdx, nil
	default:
		return false, foundIdx, errors.New(fmt.Sprintf("the field %s is not a list", field))
	}
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

// Return the map[string]interface{} of an object
func ConvertToMapString(field interface{}) (map[string]string, error) {
	fieldBytes, err := json.Marshal(field)
	if err != nil {
		return nil, err
	}
	// Unmarshal JSON into a map
	var fieldMap map[string]string
	if err := json.Unmarshal(fieldBytes, &fieldMap); err != nil {
		return nil, err
	}
	return fieldMap, nil
}
