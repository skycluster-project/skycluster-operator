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

	"github.com/go-logr/logr"
	errors "github.com/pkg/errors"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lo "github.com/samber/lo"
	"gopkg.in/yaml.v3"

	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	hint "github.com/skycluster-project/skycluster-operator/internal/helper"
	pkgpod "github.com/skycluster-project/skycluster-operator/pkg/v1alpha1/pod"
)

// InstanceTypeReconciler reconciles a InstanceType object
type InstanceTypeReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	Logger   logr.Logger
}

// +kubebuilder:rbac:groups=core.skycluster.io,resources=instancetypes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.skycluster.io,resources=instancetypes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.skycluster.io,resources=instancetypes/finalizers,verbs=update

/*

Reconcile behavior:

- Being deleted: clean up and return
- No change, no running pod, within threshold: requeue 12 hours
- Spec changed && runner pod exists: set NeedsRerun, requeue
- No runner pod: create a new Pod, requeue
- Runner pod exists: poll until it finishes
- Pod finished: 
		- Failed: return error
    - Success: update status and requeue: 12 hours

*/


// Note: if the platform is not one of the cloud providers, we need to triger ProviderProfile controller

func (r *InstanceTypeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Logger.Info("Reconciler started.", "name", req.Name)

	// Fetch the InstanceType resource
	it := &cv1a1.InstanceType{}
	if err := r.Get(ctx, req.NamespacedName, it); err != nil {
		r.Logger.Info("unable to fetch InstanceType", "name", req.Name)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Initialize conditions/observedGeneration if needed
	if it.Status.Conditions == nil {
		it.Status.Conditions = []metav1.Condition{}
	}

	// quick exit for deletions
	if !it.DeletionTimestamp.IsZero() {
		r.Logger.Info("InstanceType is being deleted, skipping reconciliation", "name", req.Name, "instanceType", it.Name)
		return ctrl.Result{}, nil
	}

	// convenience handles
	st, now := &it.Status, time.Now()
	// spec changed: requires to track ObservedGeneration in status
	// True means the spec has changed since the last run.
	specChanged := it.Generation != it.Status.Generation

	// Find the pf profile by name and if it does not exist, return an error,
	// later, trigger the pf reconciler by updating pf's labels
	// with a reference to this InstanceType.
	pf := &cv1a1.ProviderProfile{}
	if err := r.Get(ctx, client.ObjectKey{Name: it.Spec.ProviderRef, Namespace: it.Namespace}, pf); err != nil {
		// st.SetCondition(
		// 	cv1a1.ConditionReady,
		// 	cv1a1.ConditionFalse,
		// 	cv1a1.ReasonProviderProfileNotFound,
		// 	fmt.Sprintf("providerRef %q not found: %v", it.Spec.ProviderRef, err),
		// )
		_ = r.Status().Update(ctx, it)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if pf.Annotations == nil {
		pf.Annotations = make(map[string]string)
	}

	// Patch ProviderProfile
	if err := r.updateProviderProfile(ctx, it); err != nil {
		r.Recorder.Event(it, corev1.EventTypeWarning, "ProviderProfileUpdateFailed",
			fmt.Sprintf("Failed to update ProviderProfile with InstanceType reference: %s", err.Error()))
		_ = r.Status().Update(ctx, it)
		return ctrl.Result{}, err
	}

	// If:
	//    no spec changes detected &&
	//    within the threshold, &&
	//    no runner pod &&
	//    no NeedRerun set ==> then nothing to do, requeue (12 hours)
	if !specChanged &&
		!st.LastUpdateTime.IsZero() && now.Sub(st.LastUpdateTime.Time) < hint.NormalUpdateThreshold &&
		st.RunnerJobName == "" && !st.NeedsRerun {
		r.Logger.Info("No changes detected, requeuing without action", "name", req.Name, "instanceType", it.Name, "lastUpdateTime", st.LastUpdateTime.Time)
		return ctrl.Result{RequeueAfter: hint.NormalUpdateThreshold - now.Sub(st.LastUpdateTime.Time)}, nil
	}

	// If:
	//   spec change detected &&
	//   a runner pod is in-flight ==> Requeue: set a NeedsRerun flag
	if specChanged && st.RunnerJobName != "" {
		r.Logger.Info("Spec changed while a Pod is running, setting NeedsRerun flag", "name", req.Name, "instanceType", it.Name)
		if !st.NeedsRerun { // if re-run is already set, do not set it again, just requeue
			st.NeedsRerun = true
			r.Logger.Info("Object spec changed, changing status to not ready", "name", req.Name, "instanceType", it.Name)
			// st.SetCondition(cv1a1.ConditionReady, cv1a1.ConditionFalse, cv1a1.ConditionStale,
			// 	"Spec changed; will re-run after current Pod finishes")
			_ = r.Status().Update(ctx, it) // best effort update
		}
		r.Logger.Info("Requeuing for re-run...")
		return ctrl.Result{RequeueAfter: hint.RequeuePollThreshold}, nil
	}

	// 2) ensure a runner Pod; poll (requeueAfter) until Pod ends.
	if st.RunnerJobName == "" {
		r.Logger.Info("No RunnerPodName is set, checking if we need to create a new Pod", "name", req.Name, "instanceType", it.Name)
		// create the Pod for the current spec
		jsonData, err := r.generateJSON(pf.Spec.Zones)
		if err != nil {return ctrl.Result{}, err}
		
		job, err := r.buildRunner(it, pf, jsonData)
		if err != nil {
			r.Logger.Error(err, "failed to build runner Job for runner")
			return ctrl.Result{}, err
		}

		// Check if a job already exists
		theJob, err := r.ensureJobRunner(ctx, job, it, pf)
		if err != nil {
			r.Logger.Error(err, "failed to ensure runner Job")
			return ctrl.Result{}, err
		}
		st.RunnerJobName = theJob.Name
		st.Generation = it.GetGeneration()
		// st.SetCondition(cv1a1.ConditionReady, cv1a1.ConditionFalse, cv1a1.ConditionRunning, "Runner Pod just created")
		if err := r.Status().Update(ctx, it); err != nil {
			return ctrl.Result{}, err
		}
		// start polling for completion
		r.Logger.Info("Runner Pod created, starting polling for completion", "podName", job.Name)
		r.Recorder.Event(it, corev1.EventTypeNormal, "RunnerPodCreated",
			fmt.Sprintf("Runner Pod %s created for InstanceType %s", job.Name, it.Name))
		return ctrl.Result{RequeueAfter: hint.RequeuePollThreshold}, nil
	}

	// 3) a job name is recorded: check its phase
	pod, err := r.fetchPod(ctx, st.RunnerJobName, it.Namespace)
	if err != nil {
		if apierrors.IsNotFound(err) {
			st.RunnerJobName = ""
			if err2 := r.Status().Update(ctx, it); err2 != nil {return ctrl.Result{}, err2}
			return ctrl.Result{RequeueAfter: hint.RequeuePollThreshold}, nil
		} else {return ctrl.Result{}, err}
	} 

	// Pod exists and now check its phase
	msg := fmt.Sprintf("Runner Pod %s is in phase %s for InstanceType %s", pod.Name, pod.Status.Phase, it.Name)
	r.Logger.Info(msg, "name", req.Name, "instanceType", it.Name, "podName", pod.Name)
	r.Recorder.Event(it, corev1.EventTypeNormal, "RunnerPodStatus", msg)
	
	switch pod.Status.Phase {
	case corev1.PodPending, corev1.PodRunning:
		// keep polling
		return ctrl.Result{RequeueAfter: hint.RequeuePollThreshold}, nil

	// 4) collect result, update status, clear runner, possibly start another run
	default:  // corev1.PodSucceeded, corev1.PodFailed, corev1.PodUnknown:

		// Pod is finished, but if NeedsRerun is set, we ifgnore it and return to start a new run
		if st.NeedsRerun {
			st.NeedsRerun = false
			if err := r.Status().Update(ctx, it); err != nil {return ctrl.Result{}, err }
			// requeue quickly to create the next Pod for the new spec
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}

		podOutput, err := r.getPodStdOut(ctx, pod.Name, pod.Namespace)
		if err != nil {
			msg := fmt.Sprintf("failed to get logs from runner Pod %s: %s", pod.Name, err.Error())
			r.Logger.Error(err, msg, "name", req.Name, "instanceType", it.Name)
			r.Recorder.Event(it, corev1.EventTypeWarning, "GetPodLogsFailed", msg)
			return ctrl.Result{}, err
		} 

		// clear the runner
		st.RunnerJobName = ""

		// If the pod termination reason is completed,
		// we set the Zones based on the POD message
		success := pod.Status.Phase == corev1.PodSucceeded && pkgpod.ContainerTerminatedReason(pod) == "Completed"
		if success {
			if zones, err := r.decodePodLogJSON(podOutput); err != nil {
				msg := fmt.Sprintf("Failed to decode image offerings from Pod logs: %s", err.Error())
				r.Logger.Error(err, msg, "name", req.Name, "instanceType", it.Name)
				r.Recorder.Event(it, corev1.EventTypeWarning, "DecodePodLogFailed", msg)
				return ctrl.Result{}, err
			} else { it.Status.Zones = zones }

			// Update or create the ConfigMap with the image offerings
			// yamlData, err := r.generateYAML(it.Status.Zones)
			// if err != nil {
			// 	msg := fmt.Sprintf("Failed to generate input YAML for ConfigMap: %s", err.Error())
			// 	r.Recorder.Event(it, corev1.EventTypeWarning, "GenerateYAMLFailed", msg)
			// 	r.Logger.Error(err, msg, "name", req.Name, "instanceType", it.Name)
			// 	return ctrl.Result{}, err
			// }

			// if err := h.ProviderProfileCMUpdate(ctx, r.Client, pf, yamlData, "flavors.yaml"); err != nil {
			// 	msg := fmt.Sprintf("Failed to update/create ConfigMap for image offerings: %s", err.Error())
			// 	r.Recorder.Event(it, corev1.EventTypeWarning, "ConfigMapUpdateFailed", msg)
			// 	r.Logger.Error(err, msg, "name", req.Name, "instanceType", it.Name)
			// 	return ctrl.Result{}, err
			// }

			// We let the job controller handle the deletion of the job Pod
		}

		// no more work
		st.LastUpdateTime = metav1.NewTime(now)
		st.Generation = it.GetGeneration()
		// st.SetCondition(cv1a1.ConditionReady,
		// 	lo.Ternary(success, cv1a1.ConditionTrue, cv1a1.ConditionFalse), 
		// 	"RunnerPodFinished", "Runner Pod finished")
		msg := fmt.Sprintf("Runner Pod finished with phase %s, termination reason: %s", pod.Status.Phase, pkgpod.ContainerTerminatedReason(pod))
		r.Logger.Info(msg, "name", req.Name, "instanceType", it.Name, "podName", pod.Name)
		
		if err := r.Status().Update(ctx, it); err != nil { return ctrl.Result{}, err }
		return ctrl.Result{}, nil
	}
}

func (r *InstanceTypeReconciler) updateProviderProfile(ctx context.Context, it *cv1a1.InstanceType) error {
	pf := &cv1a1.ProviderProfile{}
	if err := r.Get(ctx, client.ObjectKey{Name: it.Spec.ProviderRef, Namespace: it.Namespace}, pf); err != nil {
		return err
	}
	orig := pf.DeepCopy()

	if pf.Annotations == nil {
		pf.Annotations = map[string]string{}
	}
	pf.Annotations["skycluster.io/instancetype-name"] = it.Name

	return r.Patch(ctx, pf, client.MergeFrom(orig))
}

// SetupWithManager sets up the controller with the Manager.
func (r *InstanceTypeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cv1a1.InstanceType{}).
		Named("core-instancetype").
		Complete(r)
}

// buildRunner creates a runner that finds images based on the spec.
func (r *InstanceTypeReconciler) buildRunner(it *cv1a1.InstanceType, pf *cv1a1.ProviderProfile, jsonData string) (*batchv1.Job, error) {
	// fetch provider credentials from the secret
	cred, err := r.fetchProviderProfileCred(context.Background(), pf)
	if err != nil {
		r.Recorder.Event(it, corev1.EventTypeWarning, "CredentialsFetchFailed",
			fmt.Sprintf("Failed to fetch credentials for ProviderProfile %s: %s", pf.Name, err.Error()))
		return nil, errors.Wrap(err, "failed to fetch provider credentials")
	}

	outputPath := fmt.Sprintf("/data/%s-%s.yaml", it.Name, pf.Spec.Platform)
	mapVars := lo.Assign(cred, map[string]string{
		"PROVIDER":    pf.Spec.Platform,
		"REGION":      pf.Spec.Region,
		"FAMILY":      strings.Join(it.Spec.TypeFamilies, ","),
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

	labels := hint.DefaultPodLabels(pf.Spec.Platform, pf.Spec.Region)
	labels["skycluster.io/job-type"] = "instance-finder"
	labels["skycluster.io/provider-profile"] = pf.Name

	// Create a job Pod that runs the instance finder
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    it.Namespace,
			GenerateName: hint.TruncatedName(it.Name, "-it-finder-"),
			Labels:       labels,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
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
			},
			BackoffLimit: lo.ToPtr[int32](0), // no retries
			TTLSecondsAfterFinished: lo.ToPtr[int32](300), // keep the job for 5 minutes after completion
			ActiveDeadlineSeconds: lo.ToPtr[int64](600), // Kill the job after 10 minutes
		},
	}

	if err := ctrl.SetControllerReference(it, job, r.Scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference for Job: %w", err)
	}
	
	return job, nil
}

func (r *InstanceTypeReconciler) getPodStdOut(ctx context.Context, podName, podNS string) (string, error) {
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
	if err := r.List(ctx, secrets, client.InNamespace(hint.SKYCLUSTER_NAMESPACE), client.MatchingLabels(ll)); err != nil {
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
		cred["GCP_SA_JSON"] = string(secret.Data["configs"])
		cred["GOOGLE_CLOUD_PROJECT"] = "dummy" // GCP requires a project, but we don't use it in the runner Pod
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
	if err := r.Get(context.TODO(), types.NamespacedName{Name: hint.SKYCLUSTER_PV_NAME}, pv); err != nil {
		return nil, fmt.Errorf("failed to get default PV: %w", err)
	}
	// Ensure the PV is not nil
	if pv == nil {
		return nil, fmt.Errorf("default PV %s not found", hint.SKYCLUSTER_PV_NAME)
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

// create the job if does not exist
func (r *InstanceTypeReconciler) ensureJobRunner(ctx context.Context, job *batchv1.Job, it *cv1a1.InstanceType, pf *cv1a1.ProviderProfile) (*batchv1.Job, error) {
	existings := &batchv1.JobList{}
	labels := hint.DefaultPodLabels(pf.Spec.Platform, pf.Spec.Region)
	labels["skycluster.io/job-type"] = "instance-finder"
	labels["skycluster.io/provider-profile"] = pf.Name
	err := r.List(ctx, existings, client.InNamespace(it.Namespace), client.MatchingLabels(labels))

	if err == nil && len(existings.Items) > 0 {
		// already exists, no need to create a new one
		theJob := &existings.Items[0]
		r.Logger.Info("Runner Pod already exists", "instanceType", it.Name, "podName", theJob.Name, "namespace", theJob.Namespace)
		return theJob, nil
	} 

	// does not exist, create a new one
	if err := r.Create(ctx, job); err != nil && !apierrors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("failed to create runner Job: %w", err)
	}
	
	r.Logger.Info("Created runner Pod", "instanceType", it.Name, "podName", job.Name, "namespace", job.Namespace)
	return job, nil
}

func (r *InstanceTypeReconciler) fetchPod(ctx context.Context, podName, podNS string) (*corev1.Pod, error) {
	var pod corev1.Pod
	if err := r.Get(ctx, client.ObjectKey{Namespace: podNS, Name: podName}, &pod); err != nil {
		r.Logger.Info("Runner Pod not found, should clear RunnerPodName and try again", )
		return nil, err
	}
	return &pod, nil
}