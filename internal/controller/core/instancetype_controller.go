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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	errors "github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	lo "github.com/samber/lo"
	"gopkg.in/yaml.v3"

	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	h "github.com/skycluster-project/skycluster-operator/internal/controller/core/helper"
)

// InstanceTypeReconciler reconciles a InstanceType object
type InstanceTypeReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core.skycluster.io,resources=instancetypes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.skycluster.io,resources=instancetypes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.skycluster.io,resources=instancetypes/finalizers,verbs=update

// Reconcile behavior:
//
// 1) If spec unchanged AND last update < updateThreshold ago => return without requeue.
// 2) Otherwise ensure a runner Pod exists; poll (requeueAfter) until Pod ends.
// 3) When Pod finishes, write results to .status, set .status.lastUpdateTime, and stop requeuing and delete the pod.
// 4) If spec changes while a Pod is running, mark .status.needsRerun=true; finish current run,
//    then immediately launch a fresh Pod for the new spec.

func (r *InstanceTypeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := zap.New(h.CustomLogger()).WithName("[InstanceType]")
	logger.Info("Reconciler started.", "name", req.Name)
	// Fetch the InstanceType resource
	it := &cv1a1.InstanceType{}
	if err := r.Get(ctx, req.NamespacedName, it); err != nil {
		logger.Info("unable to fetch InstanceType")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	// quick exit for deletions
	if !it.DeletionTimestamp.IsZero() {
		logger.Info("InstanceType is being deleted, skipping reconciliation", "name", it.Name)
		return ctrl.Result{}, nil
	}

	// convenience handles
	st, now := &it.Status, time.Now()

	// spec changed: requires to track ObservedGeneration in status
	// True means the spec has changed since the last run.
	specChanged := it.Generation != it.Status.Generation

	// Find the pp profile by name and if it does not exist, return an error
	pp := &cv1a1.ProviderProfile{}
	if err := r.Get(ctx, client.ObjectKey{Name: it.Spec.ProviderRef, Namespace: it.Namespace}, pp); err != nil {
		err := fmt.Errorf("unable to fetch ProviderProfile for image: %s", it.Spec.ProviderRef)
		return ctrl.Result{}, err
	}

	// If the last update is within the threshold, skip reconciliation
	if !specChanged &&
		!st.LastUpdateTime.IsZero() &&
		now.Sub(st.LastUpdateTime.Time) < h.UpdateThreshold &&
		st.RunnerPodName == "" && !st.NeedsRerun {
		logger.Info("Skipping reconciliation", "name", it.Name,
			"lastUpdateTime", st.LastUpdateTime.Time,
			"runnerPodName", st.RunnerPodName,
			"needsRerun", st.NeedsRerun)
		return ctrl.Result{RequeueAfter: h.UpdateThreshold - now.Sub(st.LastUpdateTime.Time)}, nil
	}

	if specChanged && st.RunnerPodName != "" {
		// do NOT kill the in-flight Pod (keeps behavior simple and idempotent).
		// Mark that we need another run right after this one completes.
		logger.Info("Spec changed, marking for re-run")
		if !st.NeedsRerun { // if re-run is not needed
			st.NeedsRerun = true
			logger.Info("Object spec changed, marking for re-run and returning")
			st.SetCondition(cv1a1.CondReady, corev1.ConditionFalse,
				cv1a1.CondReasonStale, "Spec changed; will re-run after current Pod finishes")
			if err := r.Status().Update(ctx, pp); err != nil {
				return ctrl.Result{}, err
			}
		}
		logger.Info("Requeuing for re-run...")
		return ctrl.Result{RequeueAfter: h.RequeuePollThreshold}, nil
	}

	// 2) ensure a runner Pod exists
	// Otherwise ensure a runner Pod exists; poll (requeueAfter) until Pod ends.
	if st.RunnerPodName == "" {
		logger.Info("No RunnerPodName is set, checking if we need to create a new Pod")
		// create the Pod for the current spec
		jsonData, err := r.generateJSON(pp.Spec.Zones)
		if err != nil {
			logger.Error(err, "failed to generate input JSON for runner Pod")
			return ctrl.Result{}, err
		}
		logger.Info("Generated JSON for runner Pod", "jsonData", jsonData, "Families", strings.Join(it.Spec.ProviderFamilies, ","))
		pod, err := r.buildRunnerPod(it, pp, jsonData)
		if err != nil {
			logger.Error(err, "failed to build runner Pod for runner")
			return ctrl.Result{}, err
		}
		if err := ctrl.SetControllerReference(it, pod, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}

		// Check if the pod already exists
		existingPods := &corev1.PodList{}
		err = r.List(ctx, existingPods,
			client.InNamespace(it.Namespace), client.MatchingLabels(h.DefaultPodLabels(pp.Spec.Platform, pp.Spec.Region)))
		if err == nil && len(existingPods.Items) > 0 {
			// Pod already exists, no need to create a new one
			existingPod := &existingPods.Items[0]
			logger.Info("Runner Pod already exists", "podName", existingPod.Name, "namespace", existingPod.Namespace)
			st.RunnerPodName = existingPod.Name
		} else {
			// Pod does not exist, create a new one
			if err := r.Create(ctx, pod); err != nil && !apierrors.IsAlreadyExists(err) {
				return ctrl.Result{}, err
			}
			logger.Info("Created runner Pod", "podName", pod.Name, "namespace", pod.Namespace)
		}

		st.Generation = it.GetGeneration()
		st.SetCondition(cv1a1.CondReady, corev1.ConditionFalse, cv1a1.CondReasonRun, "Runner Pod created")
		if err := r.Status().Update(ctx, it); err != nil {
			return ctrl.Result{}, err
		}
		// start polling for completion
		logger.Info("Runner Pod created, starting polling for completion", "podName", pod.Name)
		return ctrl.Result{RequeueAfter: h.RequeuePollThreshold}, nil
	}

	// 3) a Pod name is recorded: check its phase
	logger.Info("Runner Pod exists, checking its status", "runnerPodName", st.RunnerPodName)
	var pod corev1.Pod
	if err := r.Get(ctx, client.ObjectKey{Namespace: it.Namespace, Name: st.RunnerPodName}, &pod); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Runner Pod disappeared, clearing RunnerPodName", "runnerPodName", st.RunnerPodName)
			// Pod disappeared; clear and try again next loop
			st.RunnerPodName = ""
			if err2 := r.Status().Update(ctx, it); err2 != nil {
				return ctrl.Result{}, err2
			}
			return ctrl.Result{RequeueAfter: h.RequeuePollThreshold}, nil
		}
		return ctrl.Result{}, err
	}

	// Pod exists and now check its phase
	logger.Info("Runner Pod status", "podName", pod.Name, "phase", pod.Status.Phase, "reason", pod.Status.Reason)
	switch pod.Status.Phase {
	case corev1.PodPending, corev1.PodRunning:
		// keep polling
		return ctrl.Result{RequeueAfter: h.RequeuePollThreshold}, nil

	case corev1.PodSucceeded, corev1.PodFailed, corev1.PodUnknown:
		// 4) collect result, update status, clear runner, possibly start another run
		st.LastRunPhase = string(pod.Status.Phase)
		st.LastRunReason = h.ContainerTerminatedReason(&pod)
		podLog, err := r.getPodLogs(ctx, pod.Name, pod.Namespace)
		if err != nil {
			logger.Error(err, "failed to get logs from runner Pod", "podName", pod.Name)
			st.LastRunMessage = fmt.Sprintf("Failed to get Pod logs: %v", err)
		} else {
			st.LastRunMessage = podLog
		}
		st.LastUpdateTime = metav1.NewTime(now)
		st.SetCondition(cv1a1.CondReady, corev1.ConditionTrue, cv1a1.CondReasonDone, "Runner Pod finished")
		// clear the runner
		st.RunnerPodName = ""

		logger.Info("Runner Pod finished", "podName", pod.Name, "terminationReason", st.LastRunReason,
			"terminationMessage", st.LastRunMessage, "phase", pod.Status.Phase)

		// If spec changed during the run, immediately start a fresh run by requeueing soon.
		if st.NeedsRerun {
			st.NeedsRerun = false
			if err := r.Status().Update(ctx, it); err != nil {
				return ctrl.Result{}, err
			}
			// requeue quickly to create the next Pod for the new spec
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}

		// If the pod termination reason is completed,
		// we set the Zones based on the POD message
		if pod.Status.Phase == corev1.PodSucceeded && st.LastRunReason == "Completed" {
			zones, err := r.decodePodLogJSON(st.LastRunMessage)
			if err != nil {
				logger.Error(err, "failed to decode image offerings from JSON")
				return ctrl.Result{}, err
			}
			it.Status.Zones = zones

			// Update or create the ConfigMap with the image offerings
			yamlData, err := r.generateYAML(zones)
			if err != nil {
				logger.Error(err, "failed to generate input YAML for ConfigMap")
				return ctrl.Result{}, err
			}

			logger.Info("Generated YAML for ConfigMap", "yamlData", yamlData)

			if err := h.ProviderProfileCMUpdate(ctx, r.Client, pp, yamlData, "flavors.yaml"); err != nil {
				logger.Error(err, "failed to update/create ConfigMap for image offerings")
				return ctrl.Result{}, err
			}

			// Delete the Pod after processing
			logger.Info("Deleting runner Pod after successful processing", "podName", pod.Name)
			if err := r.Delete(ctx, &pod); err != nil {
				logger.Error(err, "failed to delete runner Pod after processing", "podName", pod.Name)
				// continue processing even if deletion fails
			}
		}

		// no more work
		if err := r.Status().Update(ctx, it); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil

	default:
		// unexpected; check again soon
		return ctrl.Result{RequeueAfter: h.RequeuePollThreshold}, nil
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *InstanceTypeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cv1a1.InstanceType{}).
		Named("core-instancetype").
		Complete(r)
}

// buildRunnerPod creates a image-finder Pod that finds images based on the spec.
// Keep it idempotent; use GenerateName + ownerRef.
func (r *InstanceTypeReconciler) buildRunnerPod(it *cv1a1.InstanceType, pp *cv1a1.ProviderProfile, jsonData string) (*corev1.Pod, error) {
	// fetch provider credentials from the secret
	cred, err := r.fetchProviderProfileCred(context.Background(), pp)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create runner Pod")
	}

	outputPath := fmt.Sprintf("/data/%s-%s.yaml", it.Name, pp.Spec.Platform)
	mapVars := lo.Assign(cred, map[string]string{
		"PROVIDER":    pp.Spec.Platform,
		"REGION":      pp.Spec.Region,
		"FAMILY":      strings.Join(it.Spec.ProviderFamilies, ","),
		"INPUT_JSON":  jsonData,
		"OUTPUT_PATH": outputPath,
	})
	envVars := make([]corev1.EnvVar, 0, len(mapVars))
	for k, v := range mapVars {
		envVars = append(envVars, corev1.EnvVar{
			Name:  strings.ToUpper(k),
			Value: v,
		})
	}

	var logger = zap.New(h.CustomLogger()).WithName("[Images]")
	logger.Info("Building runner Pod", "namespace", it.Namespace, "name", it.Name, "envVars", mapVars)

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    it.Namespace,
			GenerateName: it.Name + "-it-finder",
			Labels:       h.DefaultPodLabels(pp.Spec.Platform, pp.Spec.Region),
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			InitContainers: []corev1.Container{{
				Name:            "runner",
				Image:           "etesami/instance-finder:latest",
				ImagePullPolicy: "Always",
				Env:             envVars,
				VolumeMounts: []corev1.VolumeMount{
					{Name: "work", MountPath: "/data"},
				},
			}},
			Containers: []corev1.Container{{
				Name:    "harvest",
				Image:   "busybox",
				Command: []string{"/bin/sh", "-c"},
				Args:    []string{"cat " + outputPath},
				VolumeMounts: []corev1.VolumeMount{
					{Name: "work", MountPath: "/data"},
				},
			}},
			Volumes: []corev1.Volume{
				{Name: "work", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
			},
		},
	}, nil
}

func (r *InstanceTypeReconciler) getPodLogs(ctx context.Context, podName, podNS string) (string, error) {
	var pod corev1.Pod
	if err := r.Get(ctx, types.NamespacedName{Name: podName, Namespace: podNS}, &pod); err != nil {
		return "", fmt.Errorf("failed to get Pod %s: %w", podName, err)
	}

	cfg := ctrl.GetConfigOrDie()
	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return "", fmt.Errorf("unable to get runner pod's log, failed to create Kubernetes client: %w", err)
	}

	// Get the logs from the Pod
	req := kubeClient.CoreV1().Pods(podNS).GetLogs(podName, &corev1.PodLogOptions{})
	rr, err := req.Stream(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get logs for Pod %s: %w", podName, err)
	}
	defer rr.Close()

	var logs bytes.Buffer
	if _, err := io.Copy(&logs, rr); err != nil {
		return "", fmt.Errorf("failed to copy logs for Pod %s: %w", podName, err)
	}

	return logs.String(), nil
}

func (r *InstanceTypeReconciler) fetchProviderProfileCred(ctx context.Context, pp *cv1a1.ProviderProfile) (map[string]string, error) {
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

func (r *InstanceTypeReconciler) generateJSON(zones []cv1a1.ZoneSpec) (string, error) {
	type payload struct {
		Zones []cv1a1.ZoneSpec `json:"zones"`
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

func (r *InstanceTypeReconciler) generateYAML(zones []cv1a1.ZoneInstanceTypeSpec) (string, error) {
	yamlDataByte, err := yaml.Marshal(zones)
	if err != nil {
		return "", fmt.Errorf("failed to marshal input YAML: %w", err)
	}
	// Convert to string and return
	return string(yamlDataByte), nil
}

func (r *InstanceTypeReconciler) decodePodLogJSON(jsonData string) ([]cv1a1.ZoneInstanceTypeSpec, error) {
	var payload struct {
		Zones []cv1a1.ZoneInstanceTypeSpec `json:"zones"`
	}
	if err := json.Unmarshal([]byte(jsonData), &payload); err != nil {
		return nil, err
	}
	if payload.Zones == nil {
		return nil, fmt.Errorf("missing or invalid zones field")
	}
	return payload.Zones, nil
}

// buildPVCForPV returns a PVC that will bind to the given PV.
// You MUST create this PVC in the namespace where your Pod will run.
func (r *InstanceTypeReconciler) buildPVCForPV(pvcNS, pvcName string) (*corev1.PersistentVolumeClaim, error) {
	// Get the default SkyCluster PV
	var pv *corev1.PersistentVolume
	if err := r.Get(context.TODO(), types.NamespacedName{Name: h.SKYCLUSTER_PV_NAME}, pv); err != nil {
		return nil, fmt.Errorf("failed to get default PV: %w", err)
	}
	// Ensure the PV is not nil
	if pv == nil {
		return nil, fmt.Errorf("default PV %s not found", h.SKYCLUSTER_PV_NAME)
	}

	// Derive request size from PV capacity (must be <= PV capacity)
	reqQty := pv.Spec.Capacity[corev1.ResourceStorage]
	// Copy access modes (use a subset if you want stricter)
	am := make([]corev1.PersistentVolumeAccessMode, len(pv.Spec.AccessModes))
	copy(am, pv.Spec.AccessModes)

	// Carry over storageClassName rules:
	// - If PV has a StorageClassName, PVC must match it.
	// - If PV has no StorageClassName (static PV), set PVC.storageClassName=nil.
	var sc *string
	if pv.Spec.StorageClassName != "" {
		sc = &pv.Spec.StorageClassName
	} else {
		sc = nil
	}

	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: pvcNS,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      am,
			StorageClassName: sc,
			// Static binding: pin to the specific PV by name
			VolumeName: pv.Name,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(reqQty.String()),
				},
			},
		},
	}, nil
}
