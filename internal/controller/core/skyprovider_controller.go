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

package core

import (
	"context"
	"fmt"
	"reflect"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	corev1alpha1 "github.com/etesami/skycluster-manager/api/core/v1alpha1"
	"github.com/google/uuid"
	"github.com/pkg/errors"
)

// SkyProviderReconciler reconciles a SkyProvider object
type SkyProviderReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core.skycluster.io,resources=skyproviders,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.skycluster.io,resources=skyproviders/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.skycluster.io,resources=skyproviders/finalizers,verbs=update

func (r *SkyProviderReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	modified := false

	// Fetch the object
	var obj corev1alpha1.SkyProvider
	if err := r.Get(ctx, req.NamespacedName, &obj); err != nil {
		logger.Error(err, "unable to fetch object, maybe it is deleted?")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	labelKeys := []string{
		corev1alpha1.SkyClusterProviderName,
		corev1alpha1.SkyClusterProviderRegion,
		corev1alpha1.SkyClusterProviderZone,
		corev1alpha1.SkyClusterProviderType,
		corev1alpha1.SkyClusterProjectID,
	}
	if labelExists := LabelsExist(obj.Labels, labelKeys); !labelExists {
		logger.Info("default labels do not exist, adding...")
		// Add labels based on the fields
		modified = r.compareAndUpdateLabels(&obj, map[string]string{
			"skycluster.io/provider-name":   obj.Spec.ProviderRef.ProviderName,
			"skycluster.io/provider-region": obj.Spec.ProviderRef.ProviderRegion,
			"skycluster.io/provider-zone":   obj.Spec.ProviderRef.ProviderZone,
			"skycluster.io/project-id":      uuid.New().String(),
		})
	}

	providerLabels := map[string]string{
		corev1alpha1.SkyClusterProviderName:   obj.Spec.ProviderRef.ProviderName,
		corev1alpha1.SkyClusterProviderRegion: obj.Spec.ProviderRef.ProviderRegion,
		corev1alpha1.SkyClusterProviderZone:   obj.Spec.ProviderRef.ProviderZone,
	}
	if providerType, err := r.getProviderTypeFromConfigMap(ctx, &obj, providerLabels); err != nil {
		logger.Error(err, "failed to get provider type from ConfigMap")
		return ctrl.Result{}, err
	} else {
		logger.Info("Adding provider type label...")
		obj.Spec.ProviderRef.ProviderType = providerType
		obj.Labels[corev1alpha1.SkyClusterProviderType] = providerType
		modified = true
	}

	// Namespaced dependencies
	// Dependencies: [ProviderSetup (XProviderSetup)]
	searchLabels := map[string]string{
		corev1alpha1.SkyClusterProviderName:   obj.Spec.ProviderRef.ProviderName,
		corev1alpha1.SkyClusterProviderRegion: obj.Spec.ProviderRef.ProviderRegion,
		corev1alpha1.SkyClusterProviderZone:   obj.Spec.ProviderRef.ProviderZone,
	}
	dependenciesObj, err := GetDependencies(ctx, r.Client, searchLabels, obj.Namespace, "ProviderSetup", "xrds.skycluster.io", "v1alpha1")
	if err != nil || len(dependenciesObj.Items) == 0 {
		logger.Info("ProviderSetup does not exists")
		if err := r.createProviderSetup(ctx, &obj); err != nil {
			logger.Error(err, "failed to create ProviderSetup")
			return ctrl.Result{}, err
		}
		logger.Info("ProviderSetup created")
	} else {
		logger.Info("ProviderSetup exists, skipping creating...")
	}

	// if the SkyProvider obejct is modified, update it
	if modified {
		logger.Info("SkyProvider modified")
		if err := r.Update(ctx, &obj); err != nil {
			logger.Error(err, "failed to update object with project-id")
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *SkyProviderReconciler) createProviderSetup(ctx context.Context, obj *corev1alpha1.SkyProvider) error {
	providerRef := obj.Spec.ProviderRef
	gvk := schema.GroupVersionKind{
		Group:   "xrds.skycluster.io",
		Version: "v1alpha1",
		Kind:    "ProviderSetup",
	}
	unstructuredObj := &unstructured.Unstructured{}
	unstructuredObj.SetGroupVersionKind(gvk)
	unstructuredObj.SetNamespace(obj.Namespace)
	unstructuredObj.SetName(obj.Name)

	// Public Key
	// Retrive the secret value and use publicKey field for the xProviderSetup
	secretName := obj.Spec.KeypairRef.Name
	var secretNamespace string
	if obj.Spec.KeypairRef.Namespace != "" {
		secretNamespace = obj.Spec.KeypairRef.Namespace
	} else {
		secretNamespace = obj.Namespace
	}
	secret := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: secretNamespace, Name: secretName}, secret); err != nil {
		return errors.Wrap(err, "failed to get secret")
	}
	secretData := secret.Data
	stringMap := map[string]string{
		"publicKey": string(secretData["publicKey"]),
	}
	if err := unstructured.SetNestedStringMap(unstructuredObj.Object, stringMap, "spec", "forProvider"); err != nil {
		return errors.Wrap(err, "failed to set publicKey")
	}
	ipCidrRange, _ := r.getIPCidrRange(ctx, obj)
	stringMap = map[string]string{
		"ipCidrRange": ipCidrRange,
	}
	if err := unstructured.SetNestedStringMap(unstructuredObj.Object, stringMap, "spec", "forProvider"); err != nil {
		return errors.Wrap(err, "failed to set ipCidrRange")
	}
	secGroup := obj.Spec.SecGroup
	secGroupMap, err := DeepCopyField(secGroup)
	if err != nil {
		return errors.Wrap(err, "failed to marshal/unmarshal secGroup")
	}
	if err := unstructured.SetNestedMap(unstructuredObj.Object, secGroupMap, "spec", "forProvider", "secGroup"); err != nil {
		return errors.Wrap(err, "failed to set secGroup")
	}
	// Set the providerRef field
	providerRefMap, err := DeepCopyField(providerRef)
	if err != nil {
		return errors.Wrap(err, "failed to marshal/unmarshal providerRef")
	}
	if err := unstructured.SetNestedMap(unstructuredObj.Object, providerRefMap, "spec", "providerRef"); err != nil {
		return errors.Wrap(err, "failed to set providerRef")
	}
	// This object is namespaced so let's set the namespace
	if err := unstructured.SetNestedField(unstructuredObj.Object, obj.Namespace, "metadata", "namespace"); err != nil {
		return errors.Wrap(err, "failed to set namespace")
	}

	// Instead of setting owner reference, we set fields "dependsOn" and "dependents"
	// Because we may want to keep this object even if the SkyProvider object is deleted.
	// We need to set dependent field in ProviderSetup and dependsOn field in the SkyProvider object
	// SkyProvider -- depends on --> ProviderSetup
	// skyProviderObj := corev1alpha1.ObjectSpec{Name: obj.Name, Namespace: obj.Namespace}
	currentObj := corev1alpha1.ObjectSpec{
		Name:      obj.Name,
		Namespace: obj.Namespace,
	}
	// check if there are any existing dependents, when we create an object for the first time,
	// this is obviously empty

	skyObj := corev1alpha1.ObjectGroupSpec{
		Group: obj.GroupVersionKind().Group,
		Kind:  reflect.TypeOf(obj).Elem().Name(),
		Items: []corev1alpha1.ObjectSpec{currentObj},
	}

	logger := log.FromContext(ctx)
	logger.Info("Setting the dependents field set field")

	m := unstructuredObj.Object["spec"]
	if valMap, ok := m.(map[string][]interface{}); ok {
		if err := setNestedFieldNoCopy(valMap, skyObj, "dependents"); err != nil {
			return errors.Wrap(err, "failed to set dependents")
		}
	}

	annot := map[string]string{
		"crossplane.io/paused": "true",
	}
	if err := unstructured.SetNestedStringMap(unstructuredObj.Object, annot, "metadata", "annotations"); err != nil {
		return errors.Wrap(err, "failed to set annotations")
	}
	providerLabels := map[string]string{
		corev1alpha1.SkyClusterProviderName:   obj.Spec.ProviderRef.ProviderName,
		corev1alpha1.SkyClusterProviderRegion: obj.Spec.ProviderRef.ProviderRegion,
		corev1alpha1.SkyClusterProviderZone:   obj.Spec.ProviderRef.ProviderZone,
		corev1alpha1.SkyClusterProviderType:   obj.Spec.ProviderRef.ProviderType,
		corev1alpha1.SkyClusterProjectID:      obj.Labels[corev1alpha1.SkyClusterProjectID],
	}
	if err := unstructured.SetNestedStringMap(unstructuredObj.Object, providerLabels, "metadata", "labels"); err != nil {
		return errors.Wrap(err, "failed to set labels")
	}

	// Create the XProviderSetup
	if err := r.Create(ctx, unstructuredObj); err != nil {
		return errors.Wrap(err, "failed to create XProviderSetup")
	}
	return nil
}

func (r *SkyProviderReconciler) getIPCidrRange(ctx context.Context, obj *corev1alpha1.SkyProvider) (string, error) {
	logger := log.FromContext(ctx)

	ipGroup, currentIpSubnet, configMap, err := r.getIpCidrParts(ctx, obj)
	if err != nil {
		logger.Error(err, "failed to get IP CIDR parts")
		return "", err
	}
	err = r.updateIPCidrConfigMap(ctx, configMap, currentIpSubnet)
	if err != nil {
		logger.Error(err, "failed to update ConfigMap")
		return "", err
	}

	return fmt.Sprintf("10.%s.%s.0/24", ipGroup, currentIpSubnet), nil
}

func (r *SkyProviderReconciler) getIpCidrParts(ctx context.Context, obj *corev1alpha1.SkyProvider) (string, string, *corev1.ConfigMap, error) {
	logger := log.FromContext(ctx)

	providerName := obj.Spec.ProviderRef.ProviderName

	// get a config map with label config-type: ip-cidr-ranges
	configMaps := &corev1.ConfigMapList{}
	listOptions := &client.MatchingLabels{
		"skycluster.io/config-type":   "ip-cidr-ranges",
		"skycluster.io/provider-name": providerName,
	}
	if err := r.List(ctx, configMaps, listOptions); err != nil {
		logger.Error(err, "failed to list ConfigMaps for ip-cidr-ranges")
		return "", "", nil, err
	}

	if len(configMaps.Items) == 0 {
		logger.Info("No ConfigMap found with label config-type: ip-cidr-ranges")
		return "", "", nil, nil
	}
	// There should be only one config map matching the labels
	configMap := &configMaps.Items[0]
	// get the data and based on the values of the fields returns their values
	data := configMap.Data
	// check if any fields is equal to 'providerName'
	ipGroup, ok1 := data["ipGroup"]
	currentIpSubnet, ok2 := data["currentIpSubnet"]

	if !ok1 || !ok2 {
		logger.Info("No IP CIDR range found for the provider", "provider", providerName)
		return "", "", nil, nil
	}
	return ipGroup, currentIpSubnet, configMap, nil
}

func (r *SkyProviderReconciler) updateIPCidrConfigMap(
	ctx context.Context, configMap *corev1.ConfigMap, currentIpSubnet string) error {
	logger := log.FromContext(ctx)

	data := configMap.Data
	currentIpSubnetInt, err := strconv.Atoi(currentIpSubnet)
	if err != nil {
		logger.Error(err, "failed to convert currentIpSubnet to int")
	}
	currentIpSubnetInt++
	data["currentIpSubnet"] = strconv.Itoa(currentIpSubnetInt)

	if err := r.Update(ctx, configMap); err != nil {
		logger.Error(err, "failed to update ConfigMap")
		return err
	}
	return nil
}

func (r *SkyProviderReconciler) compareAndUpdateLabels(obj *corev1alpha1.SkyProvider, labels map[string]string) bool {
	modified := false
	if obj.Labels == nil {
		obj.Labels = make(map[string]string)
	}
	for key, value := range labels {
		vv, exists := obj.Labels[key]
		if !exists || vv != value {
			obj.Labels[key] = value
			modified = true
		}
	}
	return modified
}

func (r *SkyProviderReconciler) getProviderTypeFromConfigMap(
	ctx context.Context, obj *corev1alpha1.SkyProvider, providerLabels map[string]string) (string, error) {
	if configMaps, err := GetConfigMapsByLabels(ctx, corev1alpha1.SkyClusterNamespace, providerLabels, r.Client); err != nil || configMaps == nil {
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

// SetupWithManager sets up the controller with the Manager.
func (r *SkyProviderReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1alpha1.SkyProvider{}).
		Named("core-skyprovider").
		Complete(r)
}
