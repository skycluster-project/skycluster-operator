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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	lo "github.com/samber/lo"
	"gopkg.in/yaml.v3"

	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	h "github.com/skycluster-project/skycluster-operator/internal/controller/core/helper"
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
	var imgLogger = zap.New(h.CustomLogger()).WithName("[Images]")
	imgLogger.Info("Reconciler started.", "name", req.Name)

	// Fetch the Images resource
	img := &cv1a1.Image{}
	if err := r.Get(ctx, req.NamespacedName, img); err != nil {
		imgLogger.Info("unable to fetch Image")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	// quick exit for deletions
	if !img.DeletionTimestamp.IsZero() {
		imgLogger.Info("Image is being deleted, skipping reconciliation", "name", img.Name)
		return ctrl.Result{}, nil
	}

	// convenience handles
	st, now := &img.Status, time.Now()

	// spec changed: requires to track ObservedGeneration in status
	// True means the spec has changed since the last run.
	specChanged := img.Generation != img.Status.Generation

	// Find the pp profile by name and if it does not exist, return an error
	pp := &cv1a1.ProviderProfile{}
	if err := r.Get(ctx, client.ObjectKey{Name: img.Spec.ProviderRef, Namespace: img.Namespace}, pp); err != nil {
		err := fmt.Errorf("unable to fetch ProviderProfile for image: %s", img.Spec.ProviderRef)
		return ctrl.Result{}, err
	}

	// If the last update is within the threshold, skip reconciliation
	if !specChanged &&
		!st.LastUpdateTime.IsZero() &&
		now.Sub(st.LastUpdateTime.Time) < h.UpdateThreshold &&
		st.RunnerPodName == "" && !st.NeedsRerun {
		imgLogger.Info("Skipping reconciliation", "name", img.Name,
			"lastUpdateTime", st.LastUpdateTime.Time,
			"runnerPodName", st.RunnerPodName,
			"needsRerun", st.NeedsRerun)
		return ctrl.Result{RequeueAfter: h.UpdateThreshold - now.Sub(st.LastUpdateTime.Time)}, nil
	}

	if specChanged && st.RunnerPodName != "" {
		// do NOT kill the in-flight Pod (keeps behavior simple and idempotent).
		// Mark that we need another run right after this one completes.
		imgLogger.Info("Spec changed, marking for re-run")
		if !st.NeedsRerun { // if re-run is not needed
			st.NeedsRerun = true
			imgLogger.Info("Image spec changed, marking for re-run and returning")
			st.SetCondition(cv1a1.CondReady, corev1.ConditionFalse,
				cv1a1.CondReasonStale, "Spec changed; will re-run after current Pod finishes")
			if err := r.Status().Update(ctx, img); err != nil {
				return ctrl.Result{}, err
			}
		}
		imgLogger.Info("Requeuing for re-run...")
		return ctrl.Result{RequeueAfter: h.RequeuePollThreshold}, nil
	}

	// 2) ensure a runner Pod exists
	if st.RunnerPodName == "" {
		imgLogger.Info("No RunnerPodName is set, checking if we need to create a new Pod")
		// create the Pod for the current spec
		jsonData, err := r.generateJSON(img.Spec.Zones)
		if err != nil {
			imgLogger.Error(err, "failed to generate input JSON for image-finder Pod")
			return ctrl.Result{}, err
		}
		imgLogger.Info("Generated JSON for image-finder Pod", "jsonData", jsonData)
		pod, err := r.buildRunnerPod(img, pp, jsonData)
		if err != nil {
			imgLogger.Error(err, "failed to build runner Pod for image-finder")
			return ctrl.Result{}, err
		}
		if err := ctrl.SetControllerReference(img, pod, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}

		// Check if the pod already exists
		existingPods := &corev1.PodList{}
		err = r.List(ctx, existingPods,
			client.InNamespace(img.Namespace), client.MatchingLabels(h.DefaultPodLabels(pp.Spec.Platform, pp.Spec.Region)))
		if err == nil && len(existingPods.Items) > 0 {
			// Pod already exists, no need to create a new one
			existingPod := &existingPods.Items[0]
			imgLogger.Info("Runner Pod already exists", "podName", existingPod.Name, "namespace", existingPod.Namespace)
			st.RunnerPodName = existingPod.Name
		} else {
			// Pod does not exist, create a new one
			if err := r.Create(ctx, pod); err != nil && !apierrors.IsAlreadyExists(err) {
				return ctrl.Result{}, err
			}
			imgLogger.Info("Created runner Pod", "podName", pod.Name, "namespace", pod.Namespace)
		}

		st.Generation = img.GetGeneration()
		st.SetCondition(cv1a1.CondReady, corev1.ConditionFalse, cv1a1.CondReasonRun, "Runner Pod created")
		if err := r.Status().Update(ctx, img); err != nil {
			return ctrl.Result{}, err
		}
		// start polling for completion
		imgLogger.Info("Runner Pod created, starting polling for completion", "podName", pod.Name)
		return ctrl.Result{RequeueAfter: h.RequeuePollThreshold}, nil
	}

	// 3) a Pod name is recorded: check its phase
	imgLogger.Info("Runner Pod exists, checking its status", "runnerPodName", st.RunnerPodName)
	var pod corev1.Pod
	if err := r.Get(ctx, client.ObjectKey{Namespace: img.Namespace, Name: st.RunnerPodName}, &pod); err != nil {
		if apierrors.IsNotFound(err) {
			imgLogger.Info("Runner Pod disappeared, clearing RunnerPodName", "runnerPodName", st.RunnerPodName)
			// Pod disappeared; clear and try again next loop
			st.RunnerPodName = ""
			if err2 := r.Status().Update(ctx, img); err2 != nil {
				return ctrl.Result{}, err2
			}
			return ctrl.Result{RequeueAfter: h.RequeuePollThreshold}, nil
		}
		return ctrl.Result{}, err
	}

	imgLogger.Info("Runner Pod status", "podName", pod.Name, "phase", pod.Status.Phase, "reason", pod.Status.Reason)
	switch pod.Status.Phase {
	case corev1.PodPending, corev1.PodRunning:
		// keep polling
		return ctrl.Result{RequeueAfter: h.RequeuePollThreshold}, nil

	case corev1.PodSucceeded, corev1.PodFailed, corev1.PodUnknown:
		// 4) collect result, update status, clear runner, possibly start another run
		st.LastRunPhase = string(pod.Status.Phase)
		st.LastRunReason = h.ContainerTerminatedReason(&pod)
		st.LastRunMessage = h.ContainerTerminatedMessage(&pod)
		st.LastUpdateTime = metav1.NewTime(now)
		st.SetCondition(cv1a1.CondReady, corev1.ConditionTrue, cv1a1.CondReasonDone, "Runner Pod finished")
		// clear the runner
		st.RunnerPodName = ""

		imgLogger.Info("Runner Pod finished", "podName", pod.Name, "terminationReason", st.LastRunReason,
			"terminationMessage", st.LastRunMessage, "phase", pod.Status.Phase)

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
			zones, err := r.decodeJSONToImageOffering(st.LastRunMessage)
			if err != nil {
				imgLogger.Error(err, "failed to decode image offerings from JSON")
				return ctrl.Result{}, err
			}
			img.Status.Zones = zones

			// Update or create the ConfigMap with the image offerings
			yamlData, err := r.generateYAML(img.Status.Zones)
			if err != nil {
				imgLogger.Error(err, "failed to generate input YAML for ConfigMap")
				return ctrl.Result{}, err
			}

			imgLogger.Info("Generated YAML for ConfigMap", "yamlData", yamlData)

			if err := h.ProviderProfileCMUpdate(ctx, r.Client, pp, yamlData, "images.yaml"); err != nil {
				imgLogger.Error(err, "failed to update/create ConfigMap for image offerings")
				return ctrl.Result{}, err
			}

			// Delete the Pod after processing
			imgLogger.Info("Deleting runner Pod after successful processing", "podName", pod.Name)
			if err := r.Delete(ctx, &pod); err != nil {
				imgLogger.Error(err, "failed to delete runner Pod after processing", "podName", pod.Name)
				// continue processing even if deletion fails
			}
		}

		// no more work
		if err := r.Status().Update(ctx, img); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil

	default:
		// unexpected; check again soon
		return ctrl.Result{RequeueAfter: h.RequeuePollThreshold}, nil
	}

}

// SetupWithManager sets up the controller with the Manager.
func (r *ImageReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cv1a1.Image{}).
		Named("core-image").
		Complete(r)
}

// buildRunnerPod creates a image-finder Pod that finds images based on the spec.
// Keep it idempotent; use GenerateName + ownerRef.
func (r *ImageReconciler) buildRunnerPod(img *cv1a1.Image, pp *cv1a1.ProviderProfile, jsonData string) (*corev1.Pod, error) {
	// fetch provider credentials from the secret
	cred, err := r.fetchProviderProfileCred(context.Background(), pp)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create image-finder Pod")
	}

	mapVars := lo.Assign(cred, map[string]string{
		"PROVIDER":   pp.Spec.Platform,
		"REGION":     pp.Spec.Region,
		"INPUT_JSON": jsonData,
	})
	envVars := make([]corev1.EnvVar, 0, len(mapVars))
	for k, v := range mapVars {
		envVars = append(envVars, corev1.EnvVar{
			Name:  strings.ToUpper(k),
			Value: v,
		})
	}

	var imgLogger = zap.New(h.CustomLogger()).WithName("[Images]")
	imgLogger.Info("Building image-finder Pod", "namespace", img.Namespace, "name", img.Name, "envVars", mapVars)

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    img.Namespace,
			GenerateName: img.Name + "-image-finder",
			Labels:       h.DefaultPodLabels(pp.Spec.Platform, pp.Spec.Region),
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{{
				Name:  "runner",
				Image: "etesami/image-finder:latest",
				Env:   envVars,
			}},
		},
	}, nil
}

func (r *ImageReconciler) fetchProviderProfileCred(ctx context.Context, pp *cv1a1.ProviderProfile) (map[string]string, error) {
	// Fetch the AWS credentials from the secret
	secrets := &corev1.SecretList{}
	ll := map[string]string{
		"skycluster.io/managed-by":        "skycluster",
		"skycluster.io/provider-platform": pp.Spec.Platform,
		"skycluster.io/secret-role":       "credentials",
	}
	if err := r.List(ctx, secrets, client.InNamespace(h.SKYCLUSTER_NAMESPACE), client.MatchingLabels(ll)); err != nil {
		return nil, fmt.Errorf("unable to list credentials secret: %w", err)
	}
	if len(secrets.Items) == 0 {
		return nil, fmt.Errorf("no credentials secret found for platform %s", pp.Spec.Platform)
	}
	if len(secrets.Items) > 1 {
		return nil, fmt.Errorf("multiple credentials secrets found for platform %s, expected only one", pp.Spec.Platform)
	}

	secret := secrets.Items[0]
	if secret.Data == nil {
		return nil, fmt.Errorf("no data found in credentials secret")
	}
	if len(secret.Data) == 0 {
		return nil, fmt.Errorf("credentials secret is empty")
	}

	// Convert the secret data to a map
	cred := make(map[string]string)

	switch pp.Spec.Platform {
	case "aws":
		// AWS credentials are expected to be in the secret
		if _, ok := secret.Data["aws_access_key_id"]; !ok {
			return nil, fmt.Errorf("AWS credentials secret does not contain 'aws_access_key_id'")
		}
		if _, ok := secret.Data["aws_secret_access_key"]; !ok {
			return nil, fmt.Errorf("AWS credentials secret does not contain 'aws_secret_access_key'")
		}
		for k, v := range secret.Data {
			cred[strings.ToUpper(k)] = string(v)
		}
	case "azure":
		// Azure credentials are expected to be in the secret
		if _, ok := secret.Data["configs"]; !ok {
			return nil, fmt.Errorf("Azure credentials secret does not contain 'configs'")
		}
		cred["AZ_CONFIG_JSON"] = string(secret.Data["configs"])
	case "gcp":
		// GCP credentials are expected to be in the secret
		if _, ok := secret.Data["configs"]; !ok {
			return nil, fmt.Errorf("GCP credentials secret does not contain 'configs'")
		}
		cred["SERVICE_ACCOUNT_JSON"] = string(secret.Data["configs"])
		cred["GOOGLE_CLOUD_PROJECT"] = "dummy" // GCP requires a project, but we don't use it in the image-finder Pod
	default:
		return nil, fmt.Errorf("unsupported platform %s for credentials secret", pp.Spec.Platform)
	}

	return cred, nil
}

func (r *ImageReconciler) generateJSON(zones []cv1a1.ImageOffering) (string, error) {
	type payload struct {
		Zones []cv1a1.ImageOffering `json:"zones"`
	}
	// Wrap zones in a payload struct
	wrapped := payload{Zones: zones}
	// Use the wrapped struct to ensure the correct JSON structure
	// with "zones" as the top-level key
	jsonDataByte, err := json.Marshal(wrapped)
	if err != nil {
		return "", fmt.Errorf("failed to marshal input JSON: %w", err)
	}
	// Convert to string and return
	return string(jsonDataByte), nil
}

func (r *ImageReconciler) generateYAML(zones []cv1a1.ImageOffering) (string, error) {
	yamlDataByte, err := yaml.Marshal(zones)
	if err != nil {
		return "", fmt.Errorf("failed to marshal input YAML: %w", err)
	}
	// Convert to string and return
	return string(yamlDataByte), nil
}

func (r *ImageReconciler) decodeJSONToImageOffering(jsonData string) ([]cv1a1.ImageOffering, error) {
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
