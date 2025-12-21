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
	"strings"
	"time"

	"github.com/go-logr/logr"
	errors "github.com/pkg/errors"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lo "github.com/samber/lo"

	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	hv1a1 "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"
	utils "github.com/skycluster-project/skycluster-operator/internal/controller/core/utils"
	hint "github.com/skycluster-project/skycluster-operator/internal/helper"
	pkgenc "github.com/skycluster-project/skycluster-operator/pkg/v1alpha1/encoding"
	pkgpod "github.com/skycluster-project/skycluster-operator/pkg/v1alpha1/pod"
)

// InstanceTypeReconciler reconciles a InstanceType object
type InstanceTypeReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	Recorder     record.EventRecorder
	Logger       logr.Logger
	KubeClient   kubernetes.Interface // cached kube client for logs
	PodLogClient utils.PodLogger            // cached log client for getting logs efficiently
}

// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=pods/log,verbs=get;list
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.skycluster.io,resources=instancetypes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.skycluster.io,resources=instancetypes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.skycluster.io,resources=instancetypes/finalizers,verbs=update

/*

Reconcile behavior:

Conditions: [Ready, JobRunning, ResyncRequired]

- Being deleted: clean up and return
- No changes, [N ResyncRequired, N JobRunning], within threshold: requeue 12 hours

- Set static status: st.region
- If not a cloud provider, copy spec to status, [Ready Y] and trigger ProviderProfile controller

- Changes Detected && JobRunning: [conflict] set [N Ready, Y ResyncRequired], requeue
- ResyncRequired:
    create a new Job, requeue, [N Ready, Y JobRunning, N ResyncRequired]
- JobRunning:
    - not finished: requeue,
		- failed: return
		- finished:	[N JobRunning, N ResyncRequired]
			- Failed: return error
    	- Succeeded: update status and requeue: 12 hours [Y JobSucceeded]
*/

// Note: if the platform is not one of the cloud providers, we need to triger ProviderProfile controller

func (r *InstanceTypeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger.WithValues("instancetype", req.NamespacedName.String())
	logger.Info("Reconcile started")

	// ensure kube client for logs is initialized (lazy)
	if r.KubeClient == nil {
		cfg := ctrl.GetConfigOrDie()
		kc, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			logger.Error(err, "failed to create kube client for logs")
			return ctrl.Result{}, err
		}
		r.KubeClient = kc
	}

	// Fetch InstanceType
	it := &cv1a1.InstanceType{}
	if err := r.Get(ctx, req.NamespacedName, it); err != nil {
		if client.IgnoreNotFound(err) == nil {
			logger.Info("InstanceType not found, ignoring", "name", req.NamespacedName)
			return ctrl.Result{}, nil
		}
		logger.Error(err, "unable to fetch InstanceType", "name", req.NamespacedName)
		return ctrl.Result{}, err
	}

	// quick exit for deletions
	if !it.DeletionTimestamp.IsZero() {
		logger.Info("InstanceType is being deleted, skipping reconciliation", "name", it.Name)
		return ctrl.Result{}, nil
	}

	st := &it.Status
	now := time.Now()
	specChanged := it.Generation != it.Status.ObservedGeneration

	// Referenced ProviderProfile
	pf := &cv1a1.ProviderProfile{}
	if err := r.Get(ctx, client.ObjectKey{Name: it.Spec.ProviderRef, Namespace: it.Namespace}, pf); err != nil {
		logger.Error(err, "failed to get ProviderProfile", "providerRef", it.Spec.ProviderRef)
		return ctrl.Result{}, errors.Wrapf(err, "failed to get ProviderProfile %s", it.Spec.ProviderRef)
	}

	platform := strings.ToLower(pf.Spec.Platform)
	isCloud := lo.Contains([]string{"aws", "azure", "gcp"}, platform)

	// If not a cloud provider, annotate/update PF, copy spec to status and return early.
	if !isCloud {
		if pf.Annotations == nil {
			pf.Annotations = map[string]string{}
		}
		pf.Annotations["skycluster.io/instancetype-ref"] = it.Name
		if err := r.Patch(ctx, pf, client.MergeFrom(pf.DeepCopy())); err != nil {
			logger.Error(err, "failed to patch ProviderProfile", "provider", pf.Name, "instancetype", it.Name)
			r.Recorder.Event(it, corev1.EventTypeWarning, "ProviderProfileUpdateFailed", fmt.Sprintf("failed to patch ProviderProfile %s: %v", pf.Name, err))
			return ctrl.Result{RequeueAfter: 1 * time.Second}, err
		}
		st.SetCondition(hv1a1.Ready, metav1.ConditionTrue, "HybridEdgeCloud", "InstanceType does not require fetching data.")
		st.Offerings = it.Spec.Offerings
		st.Region = pf.Spec.Region
		st.ObservedGeneration = it.GetGeneration()
		if err := r.Status().Update(ctx, it); err != nil {
			logger.Error(err, "failed to update InstanceType status", "instancetype", it.Name)
			return ctrl.Result{RequeueAfter: 2 * time.Second}, err
		}
		logger.Info("Non-cloud provider - nothing to fetch", "instancetype", it.Name, "provider", pf.Name)
		return ctrl.Result{}, nil
	}

	// compute flags
	jobRunning := meta.IsStatusConditionTrue(st.Conditions, string(hv1a1.JobRunning))
	reconcileRequired := meta.IsStatusConditionTrue(st.Conditions, string(hv1a1.ResyncRequired))
	withinThreshold := !st.LastUpdateTime.IsZero() && now.Sub(st.LastUpdateTime.Time) < hint.NormalUpdateThreshold

	// Fast path: nothing to do
	if !specChanged && !jobRunning && !reconcileRequired && withinThreshold {
		requeueAfter := hint.NormalUpdateThreshold - now.Sub(st.LastUpdateTime.Time)
		logger.V(1).Info("No changes detected; requeuing", "requeueAfter", requeueAfter)
		return ctrl.Result{RequeueAfter: requeueAfter}, nil
	}

	// static status
	st.Region = pf.Spec.Region

	// spec changed while job running -> mark for re-run
	if specChanged && jobRunning {
		logger.Info("Spec changed while a job is running; marking for re-run", "instancetype", it.Name)
		st.SetCondition(hv1a1.Ready, metav1.ConditionFalse, "SpecChanged", "Spec changed while a Pod is running")
		if !reconcileRequired {
			st.SetCondition(hv1a1.ResyncRequired, metav1.ConditionTrue, "SpecChanged", "Spec changed while a Pod is running")
			_ = r.Status().Update(ctx, it) // best effort
		}
		return ctrl.Result{RequeueAfter: hint.RequeuePollThreshold}, nil
	}

	// Start a new run if requested
	if reconcileRequired {
		logger.Info("Resync required - creating runner job", "instancetype", it.Name)

		jsonData, err := r.generateJSON(it.Spec.Offerings)
		if err != nil {
			logger.Error(err, "failed to generate JSON for runner job")
			r.Recorder.Event(it, corev1.EventTypeWarning, "GenerateJSONFailed", err.Error())
			return ctrl.Result{}, err
		}

		job, err := r.buildRunner(it, pf, jsonData)
		if err != nil {
			logger.Error(err, "failed to build runner job")
			r.Recorder.Event(it, corev1.EventTypeWarning, "BuildJobFailed", err.Error())
			return ctrl.Result{}, err
		}

		if _, err := r.ensureJobRunner(ctx, job, it, pf); err != nil {
			logger.Error(err, "failed to ensure runner job")
			return ctrl.Result{}, err
		}

		st.SetCondition(hv1a1.ResyncRequired, metav1.ConditionFalse, "JobCreated", "Runner Job created")
		st.SetCondition(hv1a1.JobRunning, metav1.ConditionTrue, "JobCreated", "Runner Job is running")
		st.ObservedGeneration = it.GetGeneration()
		if err := r.Status().Update(ctx, it); err != nil {
			logger.Error(err, "failed to update status after creating job")
			return ctrl.Result{}, err
		}

		logger.Info("Runner job ensured; will poll for completion", "jobName", job.Name)
		return ctrl.Result{RequeueAfter: hint.RequeuePollThreshold}, nil
	}

	// JobRunning path: fetch Pod
	pod, err := r.fetchPod(ctx, pf, it)
	if err != nil {
		if apierrors.IsNotFound(err) {
			st.SetCondition(hv1a1.JobRunning, metav1.ConditionFalse, "PodNotFound", "Runner Pod not found, will try to create a new one")
			st.SetCondition(hv1a1.ResyncRequired, metav1.ConditionTrue, "PodNotFound", "Runner Pod not found, will try to create a new one")
			logger.Info("Runner Pod not found; will try to create a new one", "instancetype", it.Name)
			if err2 := r.Status().Update(ctx, it); err2 != nil {
				logger.Error(err2, "failed to update status after missing pod")
				return ctrl.Result{}, err2
			}
			return ctrl.Result{RequeueAfter: hint.RequeuePollThreshold}, nil
		}
		logger.Error(err, "failed to fetch pod for InstanceType", "instancetype", it.Name)
		return ctrl.Result{}, err
	}

	// Log pod status
	msg := fmt.Sprintf("Runner Pod status [%s]", pod.Status.Phase)
	logger.Info(msg, "podName", pod.Name, "phase", pod.Status.Phase)
	r.Recorder.Event(it, corev1.EventTypeNormal, "RunnerPodStatus", msg)

	switch pod.Status.Phase {
	case corev1.PodPending, corev1.PodRunning:
		return ctrl.Result{RequeueAfter: hint.RequeuePollThreshold}, nil
	default:
		// Pod finished
		st.SetCondition(hv1a1.JobRunning, metav1.ConditionFalse, "PodFinished", "Runner Pod finished")

		// If a re-run was requested meanwhile, clear and requeue to start next run
		if meta.IsStatusConditionTrue(st.Conditions, string(hv1a1.ResyncRequired)) {
			st.SetCondition(hv1a1.ResyncRequired, metav1.ConditionFalse, "StartingNewRun", "Starting a new run as requested")
			if err := r.Status().Update(ctx, it); err != nil {
				logger.Error(err, "failed to update status when starting new run")
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}

		// get pod logs (try harvest container first)
		podOutput, logErr := r.getPodStdOut(ctx, pod.Name, pod.Namespace, "harvest")
		if logErr != nil {
			logger.Error(logErr, "failed to get logs from runner Pod", "pod", pod.Name)
			r.Recorder.Event(it, corev1.EventTypeWarning, "GetPodLogsFailed", fmt.Sprintf("failed to get logs from runner Pod %s: %v", pod.Name, logErr))

			// mark for retry and requeue
			st.SetCondition(hv1a1.ResyncRequired, metav1.ConditionTrue, "LogFetchFailed", "Unable to retrieve runner pod logs, will retry")
			if err := r.Status().Update(ctx, it); err != nil {
				logger.Error(err, "failed to update status after log fetch failure")
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: hint.RequeuePollThreshold}, nil
		}

		// success condition: succeeded + container reason Completed
		success := pod.Status.Phase == corev1.PodSucceeded && pkgpod.ContainerTerminatedReason(pod) == "Completed"
		if success {
			zones, err := r.decodePodLogJSON(podOutput)
			if err != nil {
				logger.Error(err, "failed to decode pod output into offerings", "pod", pod.Name)
				r.Recorder.Event(it, corev1.EventTypeWarning, "DecodePodLogFailed", fmt.Sprintf("failed to decode pod log: %v", err))

				st.SetCondition(hv1a1.Ready, metav1.ConditionFalse, "DecodeFailed", "Failed to decode offerings from runner pod logs")
				if err := r.Status().Update(ctx, it); err != nil {
					logger.Error(err, "failed to update status after decode failure")
					return ctrl.Result{}, err
				}
				return ctrl.Result{RequeueAfter: hint.RequeuePollThreshold}, nil
			}
			it.Status.Offerings = zones

			// Update ConfigMap with latest offerings
			if err := r.updateConfigMap(ctx, pf, it); err != nil {
				logger.Error(err, "failed to update/create ConfigMap for offerings", "instancetype", it.Name)
				r.Recorder.Event(it, corev1.EventTypeWarning, "ConfigMapUpdateFailed", fmt.Sprintf("Failed to update/create ConfigMap for offerings: %v", err))

				st.SetCondition(hv1a1.Ready, metav1.ConditionFalse, "ConfigMapFailed", "Failed to update offerings ConfigMap")
				if err := r.Status().Update(ctx, it); err != nil {
					logger.Error(err, "failed to update status after configmap failure")
					return ctrl.Result{}, err
				}
				return ctrl.Result{RequeueAfter: hint.RequeuePollThreshold}, nil
			}
		}

		// finalize status
		st.LastUpdateTime = metav1.NewTime(now)
		st.Region = pf.Spec.Region
		st.ObservedGeneration = it.GetGeneration()
		if success {
			st.SetCondition(hv1a1.Ready, metav1.ConditionTrue, "PodFinished", "Runner Pod finished successfully")
			logger.Info("Runner Pod finished successfully", "pod", pod.Name)
		} else {
			st.SetCondition(hv1a1.Ready, metav1.ConditionFalse, "PodFailed", fmt.Sprintf("Runner Pod finished with phase %s", pod.Status.Phase))
			logger.Info("Runner Pod finished unsuccessfully", "pod", pod.Name, "phase", pod.Status.Phase)
		}

		if err := r.Status().Update(ctx, it); err != nil {
			logger.Error(err, "failed to update InstanceType status after pod finished")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}
}

func (r *InstanceTypeReconciler) updateProviderProfile(ctx context.Context, pf *cv1a1.ProviderProfile, it *cv1a1.InstanceType) error {
	// refresh pf object
	if err := r.Get(ctx, client.ObjectKey{Name: it.Spec.ProviderRef, Namespace: it.Namespace}, pf); err != nil {
		return err
	}
	orig := pf.DeepCopy()
	if pf.Annotations == nil {
		pf.Annotations = map[string]string{}
	}
	pf.Annotations["instancetype-ref"] = it.Name
	return r.Patch(ctx, pf, client.MergeFrom(orig))
}

// SetupWithManager sets up the controller with the Manager.
func (r *InstanceTypeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// initialize kube client once when manager is available
	cfg := ctrl.GetConfigOrDie()
	if r.KubeClient == nil {
		kc, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			return err
		}
		r.KubeClient = kc
	}
	if r.PodLogClient == nil {
		r.PodLogClient = &utils.K8sPodLogger{KubeClient: r.KubeClient}
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&cv1a1.InstanceType{}).
		Named("core-instancetype").
		Complete(r)
}

func (r *InstanceTypeReconciler) buildRunner(it *cv1a1.InstanceType, pf *cv1a1.ProviderProfile, jsonData string) (*batchv1.Job, error) {
	cred, err := r.fetchProviderProfileCred(context.Background(), pf)
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch provider credentials")
	}

	outputPath := fmt.Sprintf("/data/%s-%s.yaml", it.Name, pf.Spec.Platform)
	offeringsJSON, err := pkgenc.EncodeObjectToYAML(it.Status.Offerings)
	if err != nil {
		return nil, errors.Wrap(err, "failed to encode offerings to YAML")
	}
	mapVars := lo.Assign(cred, map[string]string{
		"PROVIDER":    pf.Spec.Platform,
		"REGION":      pf.Spec.Region,
		"FAMILY":      offeringsJSON,
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

	labels := hint.DefaultLabels(pf.Spec.Platform, pf.Spec.Region, "")
	labels["skycluster.io/job-type"] = "instance-finder"
	labels["skycluster.io/provider-profile"] = pf.Name

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
						ImagePullPolicy: corev1.PullIfNotPresent,
						Env:             envVars,
						VolumeMounts: []corev1.VolumeMount{
							{Name: "work", MountPath: "/data"},
						},
					}},
					Containers: []corev1.Container{{
						Name:            "harvest",
						Image:           "busybox",
						ImagePullPolicy: corev1.PullIfNotPresent,
						Command:         []string{"/bin/sh", "-c"},
						Args:            []string{"cat " + outputPath},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "work", MountPath: "/data"},
						},
					}},
					Volumes: []corev1.Volume{
						{Name: "work", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
					},
				},
			},
			BackoffLimit:            lo.ToPtr[int32](0),
			TTLSecondsAfterFinished: lo.ToPtr[int32](300),
			ActiveDeadlineSeconds:   lo.ToPtr[int64](600),
		},
	}

	if err := ctrl.SetControllerReference(it, job, r.Scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference for Job: %w", err)
	}
	return job, nil
}

// getPodStdOut fetches logs for a specific container from the pod (containerName recommended).
// Falls back to unspecified container if the named container fails.
func (r *InstanceTypeReconciler) getPodStdOut(ctx context.Context, podName, podNS, containerName string) (string, error) {
	// Try with container name first
	fetch := func(cName string) (string, error) {
		stream, err := r.PodLogClient.StreamLogs(ctx, podNS, podName, &corev1.PodLogOptions{Container: cName})
		if err != nil {
			return "", err
		}
		defer stream.Close()
		buf := new(bytes.Buffer)
		buf.ReadFrom(stream)
		return buf.String(), nil
	}

	// Primary attempt
	logs, err := fetch(containerName)
	if err != nil {
		return "", fmt.Errorf("failed to get logs for pod %s: %w", podName, err)
	}

	return logs, nil
}

func (r *InstanceTypeReconciler) fetchProviderProfileCred(ctx context.Context, pp *cv1a1.ProviderProfile) (map[string]string, error) {
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
	if secret.Data == nil || len(secret.Data) == 0 {
		return nil, fmt.Errorf("credentials secret is empty")
	}

	cred := make(map[string]string)
	switch pp.Spec.Platform {
	case "aws":
		if _, ok := secret.Data["aws_access_key_id"]; !ok {
			return nil, fmt.Errorf("AWS credentials secret missing 'aws_access_key_id'")
		}
		if _, ok := secret.Data["aws_secret_access_key"]; !ok {
			return nil, fmt.Errorf("AWS credentials secret missing 'aws_secret_access_key'")
		}
		for k, v := range secret.Data {
			cred[strings.ToUpper(k)] = string(v)
		}
	case "azure":
		if _, ok := secret.Data["configs"]; !ok {
			return nil, fmt.Errorf("Azure credentials secret missing 'configs'")
		}
		cred["AZ_CONFIG_JSON"] = string(secret.Data["configs"])
	case "gcp":
		if _, ok := secret.Data["configs"]; !ok {
			return nil, fmt.Errorf("GCP credentials secret missing 'configs'")
		}
		cred["GCP_SA_JSON"] = string(secret.Data["configs"])
		cred["GOOGLE_CLOUD_PROJECT"] = "dummy"
	default:
		return nil, fmt.Errorf("unsupported platform %s for credentials secret", pp.Spec.Platform)
	}
	return cred, nil
}

func (r *InstanceTypeReconciler) generateJSON(offerings []cv1a1.ZoneOfferings) (string, error) {
	type payload struct {
		Offerings []cv1a1.ZoneOfferings `json:"offerings"`
	}
	wrapped := payload{Offerings: offerings}
	b, err := json.Marshal(wrapped)
	if err != nil {
		return "", fmt.Errorf("failed to marshal input JSON: %w", err)
	}
	return string(b), nil
}

func (r *InstanceTypeReconciler) decodePodLogJSON(jsonData string) ([]cv1a1.ZoneOfferings, error) {
	var payload struct {
		Offerings []cv1a1.ZoneOfferings `json:"offerings"`
	}
	if err := json.Unmarshal([]byte(jsonData), &payload); err != nil {
		return nil, err
	}
	if payload.Offerings == nil {
		return nil, fmt.Errorf("missing or invalid offerings field")
	}
	return payload.Offerings, nil
}

// buildPVCForPV returns a PVC that will bind to the given PV.
// You MUST create this PVC in the namespace where your Pod will run.
func (r *InstanceTypeReconciler) buildPVCForPV(pvcNS, pvcName string) (*corev1.PersistentVolumeClaim, error) {
	// Get the default SkyCluster PV
	var pv corev1.PersistentVolume
	if err := r.Get(context.TODO(), types.NamespacedName{Name: hint.SKYCLUSTER_PV_NAME}, &pv); err != nil {
		return nil, fmt.Errorf("failed to get default PV: %w", err)
	}

	reqQty := pv.Spec.Capacity[corev1.ResourceStorage]
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

func (r *InstanceTypeReconciler) ensureJobRunner(ctx context.Context, job *batchv1.Job, it *cv1a1.InstanceType, pf *cv1a1.ProviderProfile) (*batchv1.Job, error) {
	existings := &batchv1.JobList{}
	labels := hint.DefaultLabels(pf.Spec.Platform, pf.Spec.Region, "")
	labels["skycluster.io/job-type"] = "instance-finder"
	labels["skycluster.io/provider-profile"] = pf.Name
	if err := r.List(ctx, existings, client.InNamespace(it.Namespace), client.MatchingLabels(labels)); err != nil {
		return nil, fmt.Errorf("failed to list existing jobs: %w", err)
	}
	if len(existings.Items) > 0 {
		return &existings.Items[0], nil
	}

	if err := ctrl.SetControllerReference(it, job, r.Scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference for Job: %w", err)
	}

	if err := r.Create(ctx, job); err != nil && !apierrors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("failed to create runner Job: %w", err)
	}
	return job, nil
}

func (r *InstanceTypeReconciler) fetchPod(ctx context.Context, pf *cv1a1.ProviderProfile, it *cv1a1.InstanceType) (*corev1.Pod, error) {
	var podList corev1.PodList
	ll := hint.DefaultLabels(pf.Spec.Platform, pf.Spec.Region, "")
	ll["skycluster.io/job-type"] = "instance-finder"
	ll["skycluster.io/provider-profile"] = pf.Name

	if err := r.List(ctx, &podList, client.InNamespace(it.Namespace), client.MatchingLabels(ll)); err != nil {
		return nil, fmt.Errorf("failed to list Pods for InstanceType %s: %w", it.Name, err)
	}
	if len(podList.Items) == 0 {
		return nil, apierrors.NewNotFound(corev1.Resource("pod"), fmt.Sprintf("no Pod found for InstanceType %s", it.Name))
	}
	if len(podList.Items) > 1 {
		return nil, fmt.Errorf("multiple Pods found for InstanceType %s, expected only one", it.Name)
	}
	return &podList.Items[0], nil
}

func (r *InstanceTypeReconciler) updateConfigMap(ctx context.Context, pf *cv1a1.ProviderProfile, it *cv1a1.InstanceType) error {
	if it == nil {
		return nil
	}
	ll := hint.DefaultLabels(pf.Spec.Platform, pf.Spec.Region, "")
	ll["skycluster.io/provider-profile"] = pf.Name

	cmList := &corev1.ConfigMapList{}
	if err := r.List(ctx, cmList, client.MatchingLabels(ll), client.InNamespace(hint.SKYCLUSTER_NAMESPACE)); err != nil {
		return fmt.Errorf("unable to list ConfigMaps for offerings: %w", err)
	}
	if len(cmList.Items) != 1 {
		return fmt.Errorf("error listing ConfigMaps for offerings: expected 1, got %d", len(cmList.Items))
	}
	cm := cmList.Items[0]

	if cm.Data == nil {
		cm.Data = make(map[string]string)
	}

	itYamlData, err := pkgenc.EncodeObjectToYAML(it.Status.Offerings)
	if err == nil {
		cm.Data["flavors.yaml"] = itYamlData
	}

	if err := r.Update(ctx, &cm); err != nil {
		return fmt.Errorf("failed to update ConfigMap for offerings: %w", err)
	}
	return nil
}
