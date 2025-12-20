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
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lo "github.com/samber/lo"

	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	hv1a1 "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"
	hint "github.com/skycluster-project/skycluster-operator/internal/helper"
	pkgenc "github.com/skycluster-project/skycluster-operator/pkg/v1alpha1/encoding"
	pkgpod "github.com/skycluster-project/skycluster-operator/pkg/v1alpha1/pod"
)

// ImageReconciler reconciles a Image object
type ImageReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	Recorder     record.EventRecorder
	Logger       logr.Logger
	KubeClient   kubernetes.Interface // cached kube client for getting logs efficiently
	PodLogClient PodLogger            // cached log client for getting logs efficiently
}

// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=pods/log,verbs=get;list
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.skycluster.io,resources=images,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.skycluster.io,resources=images/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.skycluster.io,resources=images/finalizers,verbs=update

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
        - finished: [N JobRunning, N ResyncRequired]
            - Failed: return error
        - Succeeded: update status and requeue: 12 hours [Y JobSucceeded]
*/

func (r *ImageReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger.WithValues("image", req.NamespacedName.String())
	logger.Info("Reconcile started")

	// lazy-init kube client for logs (cached on reconciler)
	if r.KubeClient == nil {
		cfg := ctrl.GetConfigOrDie()
		kc, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			logger.Error(err, "failed to create kubernetes client for logs")
			return ctrl.Result{}, err
		}
		r.KubeClient = kc
	}

	// Fetch the Image resource
	img := &cv1a1.Image{}
	if err := r.Get(ctx, req.NamespacedName, img); err != nil {
		if client.IgnoreNotFound(err) == nil {
			logger.Info("Image not found, ignoring", "name", req.NamespacedName)
			return ctrl.Result{}, nil
		}
		logger.Error(err, "unable to fetch Image", "name", req.NamespacedName)
		return ctrl.Result{}, err
	}

	// Quick exit for deletions
	if !img.DeletionTimestamp.IsZero() {
		logger.Info("Image is being deleted, skipping reconciliation", "name", img.Name)
		return ctrl.Result{}, nil
	}

	st := &img.Status
	now := time.Now()
	specChanged := img.Generation != img.Status.ObservedGeneration

	// Fetch ProviderProfile referenced by the image
	pf := &cv1a1.ProviderProfile{}
	if err := r.Get(ctx, client.ObjectKey{Name: img.Spec.ProviderRef, Namespace: img.Namespace}, pf); err != nil {
		logger.Error(err, "unable to fetch ProviderProfile", "providerRef", img.Spec.ProviderRef, "image", img.Name)
		return ctrl.Result{}, fmt.Errorf("unable to fetch ProviderProfile for image %s: %w", img.Spec.ProviderRef, err)
	}

	platform := strings.ToLower(pf.Spec.Platform)
	isCloud := lo.Contains([]string{"aws", "azure", "gcp"}, platform)

	// If provider is not a cloud platform, annotate PF and copy spec to status then return
	if !isCloud {
		if pf.Annotations == nil {
			pf.Annotations = map[string]string{}
		}
		pf.Annotations["skycluster.io/image-ref"] = img.Name
		if err := r.Patch(ctx, pf, client.MergeFrom(pf.DeepCopy())); err != nil {
			logger.Error(err, "failed to patch ProviderProfile", "provider", pf.Name, "image", img.Name)
			r.Recorder.Event(img, corev1.EventTypeWarning, "ProviderProfileUpdateFailed", fmt.Sprintf("failed to patch ProviderProfile %s: %v", pf.Name, err))
			return ctrl.Result{RequeueAfter: 1 * time.Second}, err
		}
		st.SetCondition(hv1a1.Ready, metav1.ConditionTrue, "HybridEdgeCloud", "image does not require fetching data.")
		st.Images = img.Spec.Images
		st.Region = pf.Spec.Region
		st.ObservedGeneration = img.GetGeneration()
		if err := r.Status().Update(ctx, img); err != nil {
			logger.Error(err, "failed to update image status", "image", img.Name)
			return ctrl.Result{RequeueAfter: 2 * time.Second}, err
		}
		logger.Info("Non-cloud provider - nothing to fetch", "image", img.Name, "provider", pf.Name)
		return ctrl.Result{}, nil
	}

	// compute current flags
	jobRunning := meta.IsStatusConditionTrue(st.Conditions, string(hv1a1.JobRunning))
	reconcileRequired := meta.IsStatusConditionTrue(st.Conditions, string(hv1a1.ResyncRequired))
	withinThreshold := !st.LastUpdateTime.IsZero() && now.Sub(st.LastUpdateTime.Time) < hint.NormalUpdateThreshold

	// No changes and within threshold - fast requeue
	if !specChanged && !jobRunning && !reconcileRequired && withinThreshold {
		requeueAfter := hint.NormalUpdateThreshold - now.Sub(st.LastUpdateTime.Time)
		logger.V(1).Info("No changes detected; requeuing", "requeueAfter", requeueAfter)
		return ctrl.Result{RequeueAfter: requeueAfter}, nil
	}

	// update static region
	st.Region = pf.Spec.Region

	// If spec changed while job running, mark for resync and requeue
	if specChanged && jobRunning {
		logger.Info("Spec changed while job running - marking for re-run", "image", img.Name)
		st.SetCondition(hv1a1.Ready, metav1.ConditionFalse, "SpecChanged", "Spec changed while a Pod is running")
		if !reconcileRequired {
			st.SetCondition(hv1a1.ResyncRequired, metav1.ConditionTrue, "SpecChanged", "Spec changed while a Pod is running")
			_ = r.Status().Update(ctx, img) // best-effort
		}
		return ctrl.Result{RequeueAfter: hint.RequeuePollThreshold}, nil
	}

	// If we need to start a new run, create the job and return
	if reconcileRequired {
		logger.Info("Resync required - creating runner job", "image", img.Name, "providerProfile", pf.Name)

		jsonData, err := r.generateJSON(pf.Spec.Zones, img.Spec.Images)
		if err != nil {
			logger.Error(err, "failed to generate input JSON for image-finder Pod")
			r.Recorder.Event(img, corev1.EventTypeWarning, "GenerateJSONFailed", err.Error())
			return ctrl.Result{}, err
		}

		job, err := r.buildRunner(img, pf, jsonData)
		if err != nil {
			logger.Error(err, "failed to build runner job")
			r.Recorder.Event(img, corev1.EventTypeWarning, "BuildJobFailed", err.Error())
			return ctrl.Result{}, err
		}

		if _, err := r.ensureJobRunner(ctx, job, img, pf); err != nil {
			logger.Error(err, "failed to ensure runner job exists")
			return ctrl.Result{}, err
		}

		// mark that a job was created
		st.SetCondition(hv1a1.ResyncRequired, metav1.ConditionFalse, "JobCreated", "Runner Job created")
		st.SetCondition(hv1a1.JobRunning, metav1.ConditionTrue, "JobCreated", "Runner Job is running")
		st.ObservedGeneration = img.GetGeneration()
		if err := r.Status().Update(ctx, img); err != nil {
			logger.Error(err, "failed to update image status after creating job")
			return ctrl.Result{}, err
		}

		logger.Info("Runner job ensured, will poll for completion", "jobName", job.Name)
		return ctrl.Result{RequeueAfter: hint.RequeuePollThreshold}, nil
	}

	// Otherwise assume job is running or we need to check pod(s)
	pod, err := r.fetchPod(ctx, pf, img)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Runner pod not found; marking for resync", "image", img.Name)
			st.SetCondition(hv1a1.JobRunning, metav1.ConditionFalse, "PodNotFound", "Runner Pod not found, will try to create a new one")
			st.SetCondition(hv1a1.ResyncRequired, metav1.ConditionTrue, "PodNotFound", "Runner Pod not found, will try to create a new one")
			if err2 := r.Status().Update(ctx, img); err2 != nil {
				logger.Error(err2, "failed to update status after missing pod")
				return ctrl.Result{}, err2
			}
			return ctrl.Result{RequeueAfter: hint.RequeuePollThreshold}, nil
		}
		logger.Error(err, "failed to list pods for image", "image", img.Name)
		return ctrl.Result{}, err
	}

	// Announce status
	msg := fmt.Sprintf("Runner Pod status [%s]", pod.Status.Phase)
	logger.Info(msg, "podName", pod.Name, "phase", pod.Status.Phase)
	r.Recorder.Event(img, corev1.EventTypeNormal, "RunnerPodStatus", msg)

	switch pod.Status.Phase {
	case corev1.PodPending, corev1.PodRunning:
		// Still running, poll again later
		return ctrl.Result{RequeueAfter: hint.RequeuePollThreshold}, nil
	default:
		// Pod finished (Succeeded/Failed/Unknown)
		st.SetCondition(hv1a1.JobRunning, metav1.ConditionFalse, "PodFinished", "Runner Pod finished")

		// If a new run was requested meanwhile, clear resync and quickly requeue
		if meta.IsStatusConditionTrue(st.Conditions, string(hv1a1.ResyncRequired)) {
			st.SetCondition(hv1a1.ResyncRequired, metav1.ConditionFalse, "StartingNewRun", "Starting a new run as requested")
			if err := r.Status().Update(ctx, img); err != nil {
				logger.Error(err, "failed to update status when starting new run")
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}

		// Attempt to retrieve logs from the "harvest" container (the container that prints the output)
		podOutput, logErr := r.getPodStdOut(ctx, pod.Name, pod.Namespace, "harvest")
		if logErr != nil {
			// Do not fail reconciliation permanently due to transient log retrieval errors.
			// Mark for re-run and requeue to try again shortly.
			logger.Error(logErr, "failed to get logs from runner Pod", "pod", pod.Name)
			r.Recorder.Event(img, corev1.EventTypeWarning, "GetPodLogsFailed", fmt.Sprintf("failed to get logs from runner Pod %s: %v", pod.Name, logErr))

			// mark for retry so we can pick up the logs on the next reconcile loop
			st.SetCondition(hv1a1.ResyncRequired, metav1.ConditionTrue, "LogFetchFailed", "Unable to retrieve runner pod logs, will retry")
			if err := r.Status().Update(ctx, img); err != nil {
				logger.Error(err, "failed to update status after log fetch failure")
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: hint.RequeuePollThreshold}, nil
		}

		// Determine success based on pod phase and termination reason
		success := pod.Status.Phase == corev1.PodSucceeded && pkgpod.ContainerTerminatedReason(pod) == "Completed"
		if success {
			zones, err := r.decodeJSONToImageOffering(podOutput)
			if err != nil {
				logger.Error(err, "failed to decode pod output into image offerings", "pod", pod.Name)
				r.Recorder.Event(img, corev1.EventTypeWarning, "DecodePodLogFailed", fmt.Sprintf("failed to decode pod log: %v", err))
				// mark as failed and requeue
				st.SetCondition(hv1a1.Ready, metav1.ConditionFalse, "DecodeFailed", "Failed to decode image offerings from runner pod logs")
				if err := r.Status().Update(ctx, img); err != nil {
					logger.Error(err, "failed to update status after decode failure")
					return ctrl.Result{}, err
				}
				return ctrl.Result{RequeueAfter: hint.RequeuePollThreshold}, nil
			}

			// Persist results to status
			st.Images = zones

			// Update ConfigMap with latest images (best-effort to fail early)
			if err := r.updateConfigMap(ctx, pf, img); err != nil {
				logger.Error(err, "failed to update/create ConfigMap for image offerings", "image", img.Name)
				r.Recorder.Event(img, corev1.EventTypeWarning, "ConfigMapUpdateFailed", fmt.Sprintf("Failed to update/create ConfigMap for image offerings: %v", err))
				// do not abort reconcile permanently; mark NotReady and requeue
				st.SetCondition(hv1a1.Ready, metav1.ConditionFalse, "ConfigMapFailed", "Failed to update images ConfigMap")
				if err := r.Status().Update(ctx, img); err != nil {
					logger.Error(err, "failed to update status after configmap failure")
					return ctrl.Result{}, err
				}
				return ctrl.Result{RequeueAfter: hint.RequeuePollThreshold}, nil
			}
		}

		// finalize status: update timestamps and readiness depending on success
		st.LastUpdateTime = metav1.NewTime(now)
		st.Region = pf.Spec.Region
		st.ObservedGeneration = img.GetGeneration()
		if success {
			st.SetCondition(hv1a1.Ready, metav1.ConditionTrue, "PodFinished", "Runner Pod finished successfully")
			logger.Info("Runner Pod finished successfully", "pod", pod.Name)
		} else {
			st.SetCondition(hv1a1.Ready, metav1.ConditionFalse, "PodFailed", fmt.Sprintf("Runner Pod finished with phase %s", pod.Status.Phase))
			logger.Info("Runner Pod finished unsuccessfully", "pod", pod.Name, "phase", pod.Status.Phase)
		}

		if err := r.Status().Update(ctx, img); err != nil {
			logger.Error(err, "failed to update image status after pod finished")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *ImageReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Initialize kube client once when manager is available
	cfg := ctrl.GetConfigOrDie()
	if r.KubeClient == nil {
		kc, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			return err
		}
		r.KubeClient = kc
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&cv1a1.Image{}).
		Named("core-image").
		Complete(r)
}

// buildRunner creates a image-finder Pod that finds images based on the spec.
// Keep it idempotent; use GenerateName + ownerRef.
func (r *ImageReconciler) buildRunner(img *cv1a1.Image, pf *cv1a1.ProviderProfile, jsonData string) (*batchv1.Job, error) {
	// fetch provider credentials from the secret
	cred, err := r.fetchProviderProfileCred(context.Background(), pf)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create image-finder Pod")
	}

	outputPath := fmt.Sprintf("/data/%s-%s.yaml", img.Name, pf.Spec.Platform)
	mapVars := lo.Assign(cred, map[string]string{
		"PROVIDER":    pf.Spec.Platform,
		"REGION":      pf.Spec.Region,
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
	labels["skycluster.io/job-type"] = "image-finder"
	labels["skycluster.io/provider-profile"] = pf.Name

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    img.Namespace,
			GenerateName: hint.TruncatedName(img.Name, "-img-finder-"),
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
						Name:  "runner",
						Image: "etesami/image-finder:latest",
						// ImagePullPolicy: corev1.PullAlways,
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
			BackoffLimit:            lo.ToPtr[int32](0),   // no retries
			TTLSecondsAfterFinished: lo.ToPtr[int32](300), // keep the job for 5 minutes after completion
			ActiveDeadlineSeconds:   lo.ToPtr[int64](600), // Kill the job after 10 minutes
		},
	}

	// Set owner reference to the Image
	if err := ctrl.SetControllerReference(img, job, r.Scheme); err != nil {
		return nil, fmt.Errorf("failed to set owner reference for job: %w", err)
	}
	return job, nil
}

func (r *ImageReconciler) fetchProviderProfileCred(ctx context.Context, pp *cv1a1.ProviderProfile) (map[string]string, error) {
	// Fetch the provider credentials from the secret
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
			return nil, fmt.Errorf("AWS credentials secret does not contain 'aws_access_key_id'")
		}
		if _, ok := secret.Data["aws_secret_access_key"]; !ok {
			return nil, fmt.Errorf("AWS credentials secret does not contain 'aws_secret_access_key'")
		}
		for k, v := range secret.Data {
			cred[strings.ToUpper(k)] = string(v)
		}
	case "azure":
		if _, ok := secret.Data["configs"]; !ok {
			return nil, fmt.Errorf("Azure credentials secret does not contain 'configs'")
		}
		cred["AZ_CONFIG_JSON"] = string(secret.Data["configs"])
	case "gcp":
		if _, ok := secret.Data["configs"]; !ok {
			return nil, fmt.Errorf("GCP credentials secret does not contain 'configs'")
		}
		cred["SERVICE_ACCOUNT_JSON"] = string(secret.Data["configs"])
		cred["GOOGLE_CLOUD_PROJECT"] = "dummy"
	default:
		return nil, fmt.Errorf("unsupported platform %s for credentials secret", pp.Spec.Platform)
	}

	return cred, nil
}

func (r *ImageReconciler) generateJSON(zones []cv1a1.ZoneSpec, imageLabels []cv1a1.ImageOffering) (string, error) {
	type ImageOffering struct {
		Zone      string `json:"zone"`
		NameLabel string `json:"nameLabel"`
		Pattern   string `json:"pattern,omitempty"`
	}
	type payload struct {
		Images []ImageOffering `json:"images"`
	}

	imgOfferings := []ImageOffering{}
	for _, zone := range zones {
		if !zone.Enabled {
			continue
		}
		for _, img := range imageLabels {
			imgOfferings = append(imgOfferings, ImageOffering{
				Zone:      zone.Name,
				NameLabel: img.NameLabel,
				Pattern:   img.Pattern,
			})
		}
	}

	wrapped := payload{Images: imgOfferings}
	jsonDataByte, err := json.Marshal(wrapped)
	if err != nil {
		return "", fmt.Errorf("failed to marshal input JSON: %w", err)
	}
	return string(jsonDataByte), nil
}

func (r *ImageReconciler) decodeJSONToImageOffering(jsonData string) ([]cv1a1.ImageOffering, error) {
	var payload struct {
		Zones []cv1a1.ImageOffering `json:"images"`
	}
	if err := json.Unmarshal([]byte(jsonData), &payload); err != nil {
		return nil, err
	}
	if payload.Zones == nil {
		return nil, fmt.Errorf("missing or invalid images field")
	}
	return payload.Zones, nil
}

func (r *ImageReconciler) updateProviderProfile(ctx context.Context, pf *cv1a1.ProviderProfile, img *cv1a1.Image) error {
	// operate on provided object (already fetched by caller)
	orig := pf.DeepCopy()
	if pf.Annotations == nil {
		pf.Annotations = map[string]string{}
	}
	pf.Annotations["instancetype-ref"] = img.Name
	return r.Patch(ctx, pf, client.MergeFrom(orig))
}

// Define what we actually need from the K8s API
type PodLogProvider interface {
	GetLogs(name string, opts *corev1.PodLogOptions) *rest.Request
}

// PodLogger abstracts the K8s log streaming call
type PodLogger interface {
	StreamLogs(ctx context.Context, namespace, name string, opts *corev1.PodLogOptions) (io.ReadCloser, error)
}

// Real implementation for production
type K8sPodLogger struct {
	KubeClient kubernetes.Interface
}

func (k *K8sPodLogger) StreamLogs(ctx context.Context, namespace, name string, opts *corev1.PodLogOptions) (io.ReadCloser, error) {
	return k.KubeClient.CoreV1().Pods(namespace).GetLogs(name, opts).Stream(ctx)
}

// getPodStdOut fetches logs for a specific container from the pod (containerName recommended).
// It retries with no container specified if the first attempt fails.
// Pass the kubernetes.Interface directly to make it mockable
func (r *ImageReconciler) getPodStdOut(ctx context.Context, podName, podNS, containerName string) (string, error) {
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

// If any of the input data is not nil, it will update the ConfigMap with the data.
func (r *ImageReconciler) updateConfigMap(ctx context.Context, pf *cv1a1.ProviderProfile, img *cv1a1.Image) error {
	if img == nil {
		return nil
	}
	ll := hint.DefaultLabels(pf.Spec.Platform, pf.Spec.Region, "")
	ll["skycluster.io/provider-profile"] = pf.Name

	cmList := &corev1.ConfigMapList{}
	if err := r.List(ctx, cmList, client.MatchingLabels(ll), client.InNamespace(hint.SKYCLUSTER_NAMESPACE)); err != nil {
		return fmt.Errorf("unable to list ConfigMaps for images: %w", err)
	}
	if len(cmList.Items) != 1 {
		return fmt.Errorf("error listing ConfigMaps for images: expected 1, got %d", len(cmList.Items))
	}
	cm := cmList.Items[0]

	if cm.Data == nil {
		cm.Data = make(map[string]string)
	}

	imgYamlData, err := pkgenc.EncodeObjectToYAML(img.Status.Images)
	if err == nil {
		cm.Data["images.yaml"] = imgYamlData
	}

	if err := r.Update(ctx, &cm); err != nil {
		return fmt.Errorf("failed to update ConfigMap for images: %w", err)
	}
	return nil
}

// create the job if does not exist
func (r *ImageReconciler) ensureJobRunner(ctx context.Context, job *batchv1.Job, img *cv1a1.Image, pf *cv1a1.ProviderProfile) (*batchv1.Job, error) {
	existings := &batchv1.JobList{}
	labels := hint.DefaultLabels(pf.Spec.Platform, pf.Spec.Region, "")
	labels["skycluster.io/job-type"] = "image-finder"
	labels["skycluster.io/provider-profile"] = pf.Name
	if err := r.List(ctx, existings, client.InNamespace(img.Namespace), client.MatchingLabels(labels)); err != nil {
		return nil, fmt.Errorf("failed to list existing jobs: %w", err)
	}
	if len(existings.Items) > 0 {
		// already exists, return it
		return &existings.Items[0], nil
	}

	// set the owner reference for the job
	if err := ctrl.SetControllerReference(img, job, r.Scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference for Job: %w", err)
	}

	// create a new one
	if err := r.Create(ctx, job); err != nil && !apierrors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("failed to create runner Job: %w", err)
	}
	return job, nil
}

func (r *ImageReconciler) fetchPod(ctx context.Context, pf *cv1a1.ProviderProfile, img *cv1a1.Image) (*corev1.Pod, error) {
	var podList corev1.PodList
	ll := hint.DefaultLabels(pf.Spec.Platform, pf.Spec.Region, "")
	ll["skycluster.io/job-type"] = "image-finder"
	ll["skycluster.io/provider-profile"] = pf.Name

	if err := r.List(ctx, &podList, client.InNamespace(img.Namespace), client.MatchingLabels(ll)); err != nil {
		return nil, fmt.Errorf("failed to list Pods for Image %s: %w", img.Name, err)
	}
	if len(podList.Items) == 0 {
		return nil, apierrors.NewNotFound(corev1.Resource("pod"), fmt.Sprintf("no Pod found for Image %s", img.Name))
	}
	if len(podList.Items) > 1 {
		return nil, fmt.Errorf("multiple Pods found for Image %s, expected only one", img.Name)
	}
	return &podList.Items[0], nil
}
