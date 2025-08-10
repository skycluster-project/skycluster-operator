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
	"encoding/json"
	"fmt"
	"strings"
	"time"

	errors "github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	lo "github.com/samber/lo"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	"github.com/skycluster-project/skycluster-operator/internal/controller/core/helper"
)

// ImageReconciler reconciles a Image object
type ImageReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core.skycluster.io,resources=images,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.skycluster.io,resources=images/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.skycluster.io,resources=images/finalizers,verbs=update

// Reconcile behavior:
//
// 1) If spec unchanged AND last update < 1h ago => return without requeue.
// 2) Otherwise ensure a runner Pod exists; poll (requeueAfter) until Pod ends.
// 3) When Pod finishes, write results to .status, set .status.lastUpdateTime, and stop requeuing.
// 4) If spec changes while a Pod is running, mark .status.needsRerun=true; finish current run,
//    then immediately launch a fresh Pod for the new spec.

func (r *ImageReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := zap.New(helper.CustomLogger()).WithName("[Images]")
	logger.Info("Reconciler started.", "name", req.Name)

	const updateThreshold = time.Duration(cv1a1.DefaultRefreshHourImages) * time.Hour
	const requeuePoll = 10 * time.Second

	// Fetch the Images resource
	img := &cv1a1.Image{}
	if err := r.Get(ctx, req.NamespacedName, img); err != nil {
		logger.Info("unable to fetch Image")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	// quick exit for deletions
	if !img.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	// convenience handles
	st := &img.Status
	now := time.Now()

	// spec changed: requires to track ObservedGeneration in status
	// True means the spec has changed since the last run.
	specChanged := img.Generation != img.Status.Generation

	// Find the provider by name and if it does not exist, return an error
	provider := &cv1a1.ProviderProfile{}
	if err := r.Get(ctx, client.ObjectKey{Name: img.Spec.ProviderRef, Namespace: img.Namespace}, provider); err != nil {
		logger.Info("unable to fetch ProviderProfile for image", "name", img.Spec.ProviderRef)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// // Update the Images resource with the new labels
	// img.Labels = r.AddUpdateDefaultLabels(img, provider)
	// if err := r.Update(ctx, img); err != nil {
	// 	logger.Error(err, "unable to update Images labels")
	// 	return ctrl.Result{}, err
	// }

	// If the last update is within the threshold, skip reconciliation
	if !specChanged &&
		!st.LastUpdateTime.IsZero() &&
		now.Sub(st.LastUpdateTime.Time) < updateThreshold &&
		st.RunnerPodName == "" && !st.NeedsRerun {
		logger.Info("Skipping reconciliation", "name", img.Name,
			"lastUpdateTime", st.LastUpdateTime.Time,
			"runnerPodName", st.RunnerPodName,
			"needsRerun", st.NeedsRerun)
		return ctrl.Result{RequeueAfter: updateThreshold - now.Sub(st.LastUpdateTime.Time)}, nil
	}

	if specChanged && st.RunnerPodName != "" {
		// do NOT kill the in-flight Pod (keeps behavior simple and idempotent).
		// Mark that we need another run right after this one completes.
		if !st.NeedsRerun {
			st.NeedsRerun = true
			st.SetCondition(cv1a1.ImgCondReady, corev1.ConditionFalse,
				cv1a1.ImgCondReasonStale, "Spec changed; will re-run after current Pod finishes")
			if err := r.Status().Update(ctx, img); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{RequeueAfter: requeuePoll}, nil
	}

	// 2) ensure a runner Pod exists
	if st.RunnerPodName == "" {
		// create the Pod for the current spec
		jsonData, err := r.generateInputJson(provider.Spec.Region, img.Spec.Zones)
		if err != nil {
			logger.Error(err, "failed to generate input JSON for image-finder Pod")
			return ctrl.Result{}, err
		}
		pod, err := r.buildRunnerPod(img, jsonData)
		if err != nil {
			logger.Error(err, "failed to build runner Pod for image-finder")
			return ctrl.Result{}, err
		}
		if err := ctrl.SetControllerReference(img, pod, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Create(ctx, pod); err != nil && !apierrors.IsAlreadyExists(err) {
			return ctrl.Result{}, err
		}
		// record Pod name (GeneratedName may be used)
		st.RunnerPodName = pod.Name
		st.Generation = img.GetGeneration()
		st.SetCondition(cv1a1.ImgCondReady, corev1.ConditionFalse, cv1a1.ImgCondReasonRun, "Runner Pod created")
		if err := r.Status().Update(ctx, img); err != nil {
			return ctrl.Result{}, err
		}
		// start polling for completion
		return ctrl.Result{RequeueAfter: requeuePoll}, nil
	}

	// 3) a Pod name is recorded: check its phase
	var pod corev1.Pod
	if err := r.Get(ctx, client.ObjectKey{Namespace: img.Namespace, Name: st.RunnerPodName}, &pod); err != nil {
		if apierrors.IsNotFound(err) {
			// Pod disappeared; clear and try again next loop
			st.RunnerPodName = ""
			if err2 := r.Status().Update(ctx, img); err2 != nil {
				return ctrl.Result{}, err2
			}
			return ctrl.Result{RequeueAfter: requeuePoll}, nil
		}
		return ctrl.Result{}, err
	}

	switch pod.Status.Phase {
	case corev1.PodPending, corev1.PodRunning:
		// keep polling
		return ctrl.Result{RequeueAfter: requeuePoll}, nil

	case corev1.PodSucceeded, corev1.PodFailed, corev1.PodUnknown:
		// 4) collect result, update status, clear runner, possibly start another run
		st.LastRunPhase = string(pod.Status.Phase)
		st.LastRunReason = helper.ContainerTerminatedReason(&pod)
		st.LastRunMessage = helper.ContainerTerminatedMessage(&pod)
		st.LastUpdateTime = metav1.NewTime(now)
		st.SetCondition(cv1a1.ImgCondReady, corev1.ConditionTrue, cv1a1.ImgCondReasonDone, "Runner Pod finished")
		// clear the runner
		st.RunnerPodName = ""

		// If spec changed during the run, immediately start a fresh run by requeueing soon.
		if st.NeedsRerun {
			st.NeedsRerun = false
			if err := r.Status().Update(ctx, img); err != nil {
				return ctrl.Result{}, err
			}
			// requeue quickly to create the next Pod for the new spec
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}

		// If the pod termination reason is completed,
		// we set the Zones based on the POD message
		if pod.Status.Phase == corev1.PodSucceeded && st.LastRunReason == "Completed" {
			zones, err := r.DecodeJSONToImageOffering(st.LastRunMessage)
			if err != nil {
				logger.Error(err, "failed to decode image offerings from JSON")
				return ctrl.Result{}, err
			}
			img.Status.Zones = zones
		}

		// no more work
		if err := r.Status().Update(ctx, img); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil

	default:
		// unexpected; check again soon
		return ctrl.Result{RequeueAfter: requeuePoll}, nil
	}

}

// SetupWithManager sets up the controller with the Manager.
func (r *ImageReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cv1a1.Image{}).
		Named("core-image").
		Complete(r)
}

func (r *ImageReconciler) updateConfigMap(ctx context.Context, cm *corev1.ConfigMap, yamlData, p, rg, z string) error {
	if cm.Data == nil {
		cm.Data = make(map[string]string)
	}
	cm.Data["images.yaml"] = yamlData
	if cm.Labels == nil {
		cm.Labels = make(map[string]string)
	}
	cm.Labels["skycluster.io/managed-by"] = "skycluster"
	cm.Labels["skycluster.io/provider-platform"] = p
	cm.Labels["skycluster.io/provider-region"] = rg
	cm.Labels["skycluster.io/provider-zone"] = z
	return nil
}

func (r *ImageReconciler) updateOrCreate(ctx context.Context, cm *corev1.ConfigMap, cmFound bool, cmName string) error {
	if !cmFound {
		cm.Name = cmName
		cm.Namespace = helper.SKYCLUSTER_NAMESPACE
		if err := r.Create(ctx, cm); err != nil {
			return fmt.Errorf("unable to create ConfigMap: %w", err)
		}
	} else {
		if err := r.Update(ctx, cm); err != nil {
			return fmt.Errorf("unable to update ConfigMap: %w", err)
		}
	}
	return nil
}

func (r *ImageReconciler) AddUpdateDefaultLabels(img *cv1a1.Image, provider *cv1a1.ProviderProfile) map[string]string {
	if img.Labels == nil {
		img.Labels = make(map[string]string)
	}

	defaultZone, ok := lo.Find(provider.Spec.Zones, func(zone cv1a1.ZoneSpec) bool {
		return zone.DefaultZone
	})
	if ok {
		img.Labels["skycluster.io/provider-zone"] = defaultZone.Name
	}
	img.Labels["skycluster.io/provider-platform"] = provider.Spec.Platform
	img.Labels["skycluster.io/provider-region"] = provider.Spec.Region
	return img.Labels
}

// buildRunnerPod creates a image-finder Pod that finds images based on the spec.
// Keep it idempotent; use GenerateName + ownerRef.
func (r *ImageReconciler) buildRunnerPod(img *cv1a1.Image, jsonData string) (*corev1.Pod, error) {
	// fetch AWS credentials from the secret
	cred, err := r.fetchAWSCredentials(context.Background())
	if err != nil {
		return nil, errors.Wrap(err, "failed to create image-finder Pod")
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    img.Namespace,
			GenerateName: img.Name + "-image-finder",
			Labels: map[string]string{
				"skycluster.io/managed-by":        "skycluster",
				"skycluster.io/provider-platform": img.Spec.ProviderRef,
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{{
				Name:  "worker",
				Image: "etesami/image-finder:latest",
				Env: []corev1.EnvVar{
					{Name: "AWS_ACCESS_KEY_ID", Value: cred[strings.ToLower("AWS_ACCESS_KEY_ID")]},
					{Name: "AWS_SECRET_ACCESS_KEY", Value: cred[strings.ToLower("AWS_SECRET_ACCESS_KEY")]},
					{Name: "PROVIDER", Value: "aws"},
					{Name: "INPUT_JSON", Value: jsonData},
				},
			}},
		},
	}, nil
}

func (r *ImageReconciler) fetchAWSCredentials(ctx context.Context) (map[string]string, error) {
	// Fetch the AWS credentials from the secret
	secrets := &corev1.SecretList{}
	if err := r.List(ctx, secrets, client.InNamespace(helper.SKYCLUSTER_NAMESPACE), client.MatchingLabels{
		"skycluster.io/managed-by":        "skycluster",
		"skycluster.io/provider-platform": "aws",
		"skycluster.io/secret-role":       "credentials",
	}); err != nil {
		return nil, fmt.Errorf("unable to list AWS credentials secret: %w", err)
	}
	if len(secrets.Items) == 0 {
		return nil, fmt.Errorf("no AWS credentials secret found")
	}
	// Assuming the first secret is the one we want
	secret := secrets.Items[0]

	if secret.Data == nil {
		return nil, fmt.Errorf("no data found in AWS credentials secret")
	}
	if len(secret.Data) == 0 {
		return nil, fmt.Errorf("AWS credentials secret is empty")
	}

	// Convert the secret data to a map
	cred := make(map[string]string)
	for k, v := range secret.Data {
		cred[strings.ToLower(k)] = string(v)
	}
	return cred, nil
}

func (r *ImageReconciler) generateInputJson(region string, zones []cv1a1.ImageOffering) (string, error) {
	type inputJsonType struct {
		Region string                `json:"region"`
		Zones  []cv1a1.ImageOffering `json:"zones"`
	}

	inputJson := inputJsonType{
		Region: region,
		Zones:  zones,
	}
	jsonData, err := json.Marshal(inputJson)
	if err != nil {
		return "", fmt.Errorf("failed to marshal input JSON: %w", err)
	}
	// Convert to string and return
	return string(jsonData), nil
}

func (r *ImageReconciler) handleConfigMap(ctx context.Context, provider *cv1a1.ProviderProfile, jsonData string) error {

	defaultZone, ok := lo.Find(provider.Spec.Zones, func(zone cv1a1.ZoneSpec) bool {
		return zone.DefaultZone
	})
	if !ok {
		return fmt.Errorf("no default zone found for provider %s", provider.Name)
	}

	cmName := fmt.Sprintf("%s.%s.%s", provider.Spec.Platform, provider.Spec.Region, defaultZone.Name)
	cmList := &corev1.ConfigMapList{}
	if err := r.List(ctx, cmList, client.MatchingLabels{
		"skycluster.io/managed-by":        "skycluster",
		"skycluster.io/provider-platform": provider.Spec.Platform,
		"skycluster.io/provider-region":   provider.Spec.Region,
		"skycluster.io/config-type":       "provider-profile",
	},
		client.InNamespace(helper.SKYCLUSTER_NAMESPACE),
	); err != nil {
		return fmt.Errorf("unable to list ConfigMaps for images: %w", err)
	}
	if len(cmList.Items) > 1 {
		return fmt.Errorf("multiple ConfigMaps found for images, expected only one")
	}
	cm := cmList.Items[0]

	p := provider.Spec.Platform
	reg := provider.Spec.Region
	z := defaultZone.Name
	// Update the configMap with the new instance types
	type imagesDataType struct {
		Zones []cv1a1.ImageOffering `json:"zones"`
	}
	yamlData, err := helper.EncodeJSONStringToYAML(jsonData)
	if err != nil {
		return fmt.Errorf("failed to encode instance types to YAML: %w", err)
	}

	if err := r.updateConfigMap(ctx, &cm, yamlData, p, reg, z); err != nil {
		return fmt.Errorf("unable to update ConfigMap for images: %w", err)
	}

	cmFound := len(cmList.Items) > 0
	if err := r.updateOrCreate(ctx, &cm, cmFound, cmName); err != nil {
		return fmt.Errorf("unable to create/update ConfigMap for images: %w", err)
	}
	return nil
}

func (r *ImageReconciler) DecodeJSONToImageOffering(jsonData string) ([]cv1a1.ImageOffering, error) {
	var payload struct {
		Zones []cv1a1.ImageOffering `json:"zones"`
	}
	if err := json.Unmarshal([]byte(jsonData), &payload); err != nil {
		return nil, err
	}
	if payload.Zones == nil {
		return nil, fmt.Errorf("missing or invalid zones field")
	}
	return payload.Zones, nil
}
