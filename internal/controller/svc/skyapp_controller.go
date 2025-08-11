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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"

	corev1alpha1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	svcv1alpha1 "github.com/skycluster-project/skycluster-operator/api/svc/v1alpha1"
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
	logger := log.FromContext(ctx)
	loggerName := "SkyApp"
	logger.Info(fmt.Sprintf("[%s]\t Reconciling SkyApp for [%s]", loggerName, req.Name))

	skyApp := &svcv1alpha1.SkyApp{}
	if err := r.Get(ctx, req.NamespacedName, skyApp); err != nil {
		logger.Info(fmt.Sprintf("[%s]\t SkyApp not found.", loggerName))
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	skyApp.SetCondition("Synced", metav1.ConditionTrue, "ReconcileSuccess", "Reconcile successfully.")

	// if the providerConfigName is not set, we don't proceed
	if skyApp.Status.ProviderConfigRef == "" {
		skyApp.Status.ProviderConfigRef = "NotReady"
		logger.Info(fmt.Sprintf("[%s]\t ProviderConfig is not set yet.", loggerName))
		// TODO: Uncomment this line
		// r.setConditionUnreadyAndUpdate(skyApp, "ProviderConfig is not set yet.")
		// return ctrl.Result{}, nil
	}

	// if both objects and providerConfigName are set, we don't proceed
	if len(skyApp.Status.Objects) > 0 {
		logger.Info(fmt.Sprintf("[%s]\t Objects are already set.", loggerName))
		return ctrl.Result{}, nil
	}

	// We need to create deployment and services for the remote cluster.
	// We have the base manifest in spec.manifest and we need to create
	// the Kubernetes Provider "objects" for the remote cluster.
	// Additionally istio configuration should be generated.

	// Creating Kubernetes Provider Objects for deployments and services
	// We need to watch for providerCfgName and when it is available,
	// proceed with the creation of the deployment and services and etc.
	providerCfgName := skyApp.Status.ProviderConfigRef
	manifests := generateDeployObjectManifests(skyApp.Spec.Manifests, providerCfgName)

	// Creating Istio configuration
	// manifestCfg := generateIstioConfig(skyApp.Spec.Manifests, providerCfgName)

	// Update the status with the objects that will be created
	for _, obj := range manifests {
		yamlObj, err := generateYAMLManifest(obj.Object)
		if err != nil {
			logger.Error(err, fmt.Sprintf("[%s]\t Error creating YAML manifest.", loggerName))
			r.setConditionUnreadyAndUpdate(skyApp, "Error creating YAML manifest.")
			return ctrl.Result{}, err
		}
		skyApp.Status.Objects = append(skyApp.Status.Objects, corev1alpha1.SkyService{
			ComponentRef: corev1.ObjectReference{
				Name:       obj.GetName(),
				Kind:       obj.GetKind(),
				APIVersion: obj.GetAPIVersion(),
			},
			Manifest: yamlObj,
		})
	}

	// Istio configuration
	manifests = generateIstioConfig(skyApp.Spec.Manifests, providerCfgName)
	// Update the status with the objects that will be created
	for _, obj := range manifests {
		yamlObj, err := generateYAMLManifest(obj.Object)
		if err != nil {
			logger.Error(err, fmt.Sprintf("[%s]\t Error creating YAML manifest.", loggerName))
			r.setConditionUnreadyAndUpdate(skyApp, "Error creating YAML manifest.")
			return ctrl.Result{}, err
		}
		skyApp.Status.Objects = append(skyApp.Status.Objects, corev1alpha1.SkyService{
			ComponentRef: corev1.ObjectReference{
				Name:       obj.GetName(),
				Kind:       obj.GetKind(),
				APIVersion: obj.GetAPIVersion(),
			},
			Manifest: yamlObj,
		})
	}

	// Update the status with the provider configuration name
	logger.Info(fmt.Sprintf("[%s]\t Updating status with provider configuration name.", loggerName))
	skyApp.SetConditionReady()
	if err := r.Status().Update(ctx, skyApp); err != nil {
		logger.Error(err, fmt.Sprintf("[%s]\t Error updating status.", loggerName))
		return ctrl.Result{}, err
	}

	// if the status is updated, we create the objects
	// for _, obj := range manifests {
	// 	if err := r.Create(ctx, &obj); err != nil {
	// 		logger.Error(err, fmt.Sprintf("[%s]\t Error creating object.", loggerName))
	// 		return ctrl.Result{}, err
	// 	}
	// }

	return ctrl.Result{}, nil
}

func generateDeployObjectManifests(manifests []corev1alpha1.SkyService, providerCfgName string) map[string]unstructured.Unstructured {
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
				corev1alpha1.SKYCLUSTER_MANAGEDBY_LABEL: corev1alpha1.SKYCLUSTER_MANAGEDBY_VALUE,
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

func generateIstioConfig(manifests []corev1alpha1.SkyService, providerCfgName string) map[string]unstructured.Unstructured {

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

			svcTypeLabel := corev1alpha1.SKYCLUSTER_SVCTYPE_LABEL
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
				corev1alpha1.SKYCLUSTER_PROVIDERREGIONALIAS_LABEL,
				corev1alpha1.SKYCLUSTER_PROVIDERREGION_LABEL,
				corev1alpha1.SKYCLUSTER_PROVIDERZONE_LABEL,
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
				corev1alpha1.SKYCLUSTER_MANAGEDBY_LABEL: corev1alpha1.SKYCLUSTER_MANAGEDBY_VALUE,
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

// SetupWithManager sets up the controller with the Manager.
func (r *SkyAppReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&svcv1alpha1.SkyApp{}).
		Named("svc-skyapp").
		WithOptions(controller.Options{
			RateLimiter: newCustomRateLimiter(),
		}).
		Complete(r)
}

func (r *SkyAppReconciler) setConditionReadyAndUpdate(s *svcv1alpha1.SkyApp) {
	s.SetCondition("Ready", metav1.ConditionTrue, "Available", "SkyCluster is ready.")
	if err := r.Status().Update(context.Background(), s); err != nil {
		panic(fmt.Sprintf("failed to update SkyCluster status: %v", err))
	}
}

func (r *SkyAppReconciler) setConditionUnreadyAndUpdate(s *svcv1alpha1.SkyApp, m string) {
	s.SetCondition("Ready", metav1.ConditionFalse, "Unavailable", m)
	if err := r.Status().Update(context.Background(), s); err != nil {
		panic(fmt.Sprintf("failed to update SkyCluster status: %v", err))
	}
}
