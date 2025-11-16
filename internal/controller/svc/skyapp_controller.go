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

package svc

import (
	"context"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	hv1a1 "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"
)

// SkyAppReconciler reconciles a SkyApp object
type SkyAppReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=svc.skycluster.io,resources=skyapps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=svc.skycluster.io,resources=skyapps/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=svc.skycluster.io,resources=skyapps/finalizers,verbs=update

func (r *SkyAppReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	
	return ctrl.Result{}, nil
}

func generateDeployObjectManifests(manifests []hv1a1.SkyService, providerCfgName string) map[string]unstructured.Unstructured {
	objs := map[string]unstructured.Unstructured{}
	// Generate deployment and service manifests
	for _, manifest := range manifests {
		// for deployment and services we only wrap them in "Object"
		if strings.ToLower(manifest.ComponentRef.Kind) == "deployment" || strings.ToLower(manifest.ComponentRef.Kind) == "service" {
			obj := &unstructured.Unstructured{}
			obj.SetAPIVersion("kubernetes.crossplane.io/v1alpha2")
			obj.SetKind("Object")
			obj.SetName(manifest.ComponentRef.Name)
			obj.SetLabels(map[string]string{
				hv1a1.SKYCLUSTER_MANAGEDBY_LABEL: hv1a1.SKYCLUSTER_MANAGEDBY_VALUE,
			})
			// "Object" type requires type <object> for manifest
			// we marshal the manifest to map[string]interface{}
			// and set it as the manifest
			yamlManifest := map[string]any{}
			err := yaml.Unmarshal([]byte(manifest.Manifest), &yamlManifest)
			if err != nil {
				fmt.Println("Error unmarshalling manifest yaml")
			}
			obj.Object["spec"] = map[string]any{
				"forProvider": map[string]any{
					"manifest": yamlManifest,
				},
				"providerConfigRef": map[string]string{
					"name": providerCfgName,
				},
			}
			objs[manifest.ComponentRef.Name] = *obj
		}
	}
	return objs
}

func generateIstioConfig(manifests []hv1a1.SkyService, providerCfgName string) map[string]unstructured.Unstructured {

	objs := map[string]unstructured.Unstructured{}
	// Generate Istio configuration
	// As a general rule, we create a DestinaRule ofject that enforces
	// priorities for failover, and we prioritize the local provider.
	// More specifically, we priotize a destination where it adopts
	// as many labels as the client that send the request.
	// These set of labels are introduced in the DestinationRule object
	// and include provider region alias, region, and zone.
	for _, manifest := range manifests {
		if strings.ToLower(manifest.ComponentRef.Kind) == "service" {
			yamlManifest := map[string]any{}
			err := yaml.Unmarshal([]byte(manifest.Manifest), &yamlManifest)
			if err != nil {
				fmt.Println("Error unmarshalling manifest yaml")
			}

			svcTypeLabel := hv1a1.SKYCLUSTER_SVCTYPE_LABEL
			labels, err := GetNestedField(yamlManifest, "metadata", "labels")
			if err != nil {
				// This service is not eligible for istio configuration
				continue
			}
			// this label is added to the service indicating this is the main endpoint
			// for the service and should be used for istio configuration
			if v, ok := labels[svcTypeLabel]; !ok || v != "app-face" {
				continue
			}

			failovers := []string{
				hv1a1.SKYCLUSTER_PROVIDERREGIONALIAS_LABEL,
				hv1a1.SKYCLUSTER_PROVIDERREGION_LABEL,
				hv1a1.SKYCLUSTER_PROVIDERZONE_LABEL,
			}

			istioObj := map[string]interface{}{
				"apiVersion": "networking.istio.io/v1",
				"kind":       "DestinationRule",
				"metadata": map[string]interface{}{
					"name": manifest.ComponentRef.Name,
				},
				"spec": map[string]interface{}{
					"host": manifest.ComponentRef.Name,
					"trafficPolicy": map[string]interface{}{
						"loadBalancer": map[string]interface{}{
							"simple":           "LEAST_REQUEST",
							"failoverPriority": failovers,
						},
						"outlierDetection": map[string]interface{}{
							"consecutiveErrors":  5,
							"interval":           "5s",
							"baseEjectionTime":   "30s",
							"maxEjectionPercent": 100,
						},
					},
				},
			}

			obj := &unstructured.Unstructured{}
			obj.SetAPIVersion("kubernetes.crossplane.io/v1alpha2")
			obj.SetKind("Object")
			obj.SetName(manifest.ComponentRef.Name)
			obj.SetLabels(map[string]string{
				hv1a1.SKYCLUSTER_MANAGEDBY_LABEL: hv1a1.SKYCLUSTER_MANAGEDBY_VALUE,
			})
			obj.Object["spec"] = map[string]interface{}{
				"forProvider": map[string]interface{}{
					"manifest": istioObj,
				},
				"providerConfigRef": map[string]string{
					"name": providerCfgName,
				},
			}

			objs[manifest.ComponentRef.Name] = *obj
		}
	}
	return objs
}


