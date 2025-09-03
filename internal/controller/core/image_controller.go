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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
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
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	Logger   logr.Logger
}

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
		- finished:	[N JobRunning, N ResyncRequired]
			- Failed: return error
    	- Succeeded: update status and requeue: 12 hours [Y JobSucceeded]
*/

// Note: if the platform is not one of the cloud providers, we need to triger ProviderProfile controller

func (r *ImageReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Logger.Info("Reconciler started.", "name", req.Name)

	// Fetch the Images resource
	img := &cv1a1.Image{}
	if err := r.Get(ctx, req.NamespacedName, img); err != nil {
		r.Logger.Info("unable to fetch Image")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	// quick exit for deletions
	if !img.DeletionTimestamp.IsZero() {
		r.Logger.Info("Image is being deleted, skipping reconciliation", "name", img.Name)
		return ctrl.Result{}, nil
	}

	// convenience handles
	st, now := &img.Status, time.Now()
	specChanged := img.Generation != img.Status.ObservedGeneration

	// Referenced ProviderProfile
	pf := &cv1a1.ProviderProfile{}
	if err := r.Get(ctx, client.ObjectKey{Name: img.Spec.ProviderRef, Namespace: img.Namespace}, pf); err != nil {
		err := fmt.Errorf("unable to fetch ProviderProfile for image: %s", img.Spec.ProviderRef)
		return ctrl.Result{}, err
	}

	if !lo.Contains([]string{"aws", "azure", "gcp"}, strings.ToLower(pf.Spec.Platform)) {
		if pf.Annotations == nil { pf.Annotations = make(map[string]string) }
		pf.Annotations["skycluster.io/image-ref"] = img.Name
		if err := r.Patch(ctx, pf, client.MergeFrom(pf.DeepCopy())); err != nil {
			// we need to ensure the PF is updated, so if there is an error here, we requeue
			msg := fmt.Sprintf("failed to patch ProviderProfile %s", pf.Name)
			r.Logger.Error(err, "failed to patch ProviderProfile", "name", req.Name, "image", img.Name)
			r.Recorder.Event(img, corev1.EventTypeWarning, "ProviderProfileUpdateFailed", msg)
			return ctrl.Result{RequeueAfter: 1 * time.Second}, err
		}
		st.SetCondition(hv1a1.Ready, metav1.ConditionTrue, "HybridEdgeCloud", "image does not require fetching data.")
		st.Images = img.Spec.Images // copy spec to status
	}

	jobRunning := meta.IsStatusConditionTrue(st.Conditions, string(hv1a1.JobRunning))
	reconcileRequired := meta.IsStatusConditionTrue(st.Conditions, string(hv1a1.ResyncRequired))
	// lastUpdateTime, err := GetTimeAnnotation(img, "skycluster.io/last-update-time")
	// withinThreshold := false
	// if err == nil && lastUpdateTime != nil {
	// 	withinThreshold = now.Sub(lastUpdateTime.Time) < hint.NormalUpdateThreshold
	// }
	withinThreshold := !st.LastUpdateTime.IsZero() && now.Sub(st.LastUpdateTime.Time) < hint.NormalUpdateThreshold

	if !specChanged && withinThreshold && !jobRunning && !reconcileRequired {
		r.Logger.Info("No changes detected, requeuing without action", "name", req.Name, "image", img.Name, "lastUpdateTime", st.LastUpdateTime.Time)
		return ctrl.Result{RequeueAfter: hint.NormalUpdateThreshold - now.Sub(st.LastUpdateTime.Time)}, nil
	}

	st.Region = pf.Spec.Region // static status

	if !lo.Contains([]string{"aws", "azure", "gcp"}, pf.Spec.Platform) {
		// we trigger ProviderProfile by adding an annotation
		err := r.updateProviderProfile(ctx, pf, img)
		if err != nil {
			r.Logger.Error(err, "failed to update ProviderProfile", "name", req.Name, "image", img.Name)
			return ctrl.Result{RequeueAfter: 2 * time.Second}, err
		}
		st.SetCondition(hv1a1.Ready, metav1.ConditionTrue, "HybridEdgeCloud", "image does not required fetching data.")
		err = r.Status().Update(ctx, img)
		if err != nil {
			r.Logger.Error(err, "failed to update image status", "name", req.Name, "image", img.Name)
			return ctrl.Result{RequeueAfter: 2 * time.Second}, err
		}
		r.Logger.Info("image does not require fetching data, requeuing without action", "name", req.Name, "image", img.Name)
		return ctrl.Result{}, nil
	}


	// change detected && in-flight job ==> Requeue: set a NeedsRerun flag
	if specChanged && jobRunning {
		r.Logger.Info("Spec changed while a Pod is running", "name", req.Name, "image", img.Name)
		st.SetCondition(hv1a1.Ready, metav1.ConditionFalse, "SpecChanged", "Spec changed while a Pod is running")
		if !reconcileRequired { // if re-run is already set, do not set it again, just requeue
			r.Logger.Info("Image spec changed, marking for re-run and returning")
			st.SetCondition(hv1a1.ResyncRequired, metav1.ConditionTrue, "SpecChanged", "Spec changed while a Pod is running")
			_ = r.Status().Update(ctx, img) // best effort update
		}
		return ctrl.Result{RequeueAfter: hint.RequeuePollThreshold}, nil
	}


	if reconcileRequired {
		
		// create the Pod for the current spec
		jsonData, err := r.generateJSON(pf.Spec.Zones, img.Spec.Images)
		if err != nil {
			r.Logger.Error(err, "failed to generate input JSON for image-finder Pod")
			r.Recorder.Event(img, corev1.EventTypeWarning, "GenerateJSONFailed", err.Error())
			return ctrl.Result{}, err
		}

		job, err := r.buildRunner(img, pf, jsonData)
		if err != nil {
			r.Logger.Error(err, "failed to build runner Pod for image-finder")
			r.Recorder.Event(img, corev1.EventTypeWarning, "BuildJobFailed", err.Error())
			return ctrl.Result{}, err
		}

		// Check if a job already exists
		_, err = r.ensureJobRunner(ctx, job, img, pf)
		if err != nil {
			r.Logger.Error(err, "failed to ensure runner Job")
			return ctrl.Result{}, err
		}
		r.Logger.Info("Runner job ensured", "name", req.Name, "image", img.Name, "jobName", job.Name)
		st.SetCondition(hv1a1.ResyncRequired, metav1.ConditionFalse, "JobCreated", "Runner Job created")
		st.ObservedGeneration = img.GetGeneration()

		if err := r.Status().Update(ctx, img); err != nil {
			return ctrl.Result{}, err
		}
		// start polling for completion
		r.Logger.Info("Runner Pod created, starting polling for completion", "podName", job.Name)
		return ctrl.Result{RequeueAfter: hint.RequeuePollThreshold}, nil
	}

	// JobRunning
	pod, err := r.fetchPod(ctx, pf, img)
	if err != nil {
		if apierrors.IsNotFound(err) {
			st.SetCondition(hv1a1.JobRunning, metav1.ConditionFalse, "PodNotFound", "Runner Pod not found, will try to create a new one")
			st.SetCondition(hv1a1.ResyncRequired, metav1.ConditionTrue, "PodNotFound", "Runner Pod not found, will try to create a new one")
			r.Logger.Info("Runner Pod not found, will try to create a new one", "name", req.Name, "image", img.Name)
			if err2 := r.Status().Update(ctx, img); err2 != nil {return ctrl.Result{}, err2}
			return ctrl.Result{RequeueAfter: hint.RequeuePollThreshold}, nil
			} else {
			r.Logger.Error(err, "failed to fetch Pod for Image", "name", req.Name, "image", img.Name)
			return ctrl.Result{}, err
		}
	} 

	// Pod exists and now check its phase
	msg := fmt.Sprintf("Runner Pod status [%s]", pod.Status.Phase)
	r.Logger.Info(msg, "name", req.Name, "image", img.Name, "podName", pod.Name)
	r.Recorder.Event(img, corev1.EventTypeNormal, "RunnerPodStatus", msg)

	switch pod.Status.Phase {
	case corev1.PodPending, corev1.PodRunning:
		// keep polling
		return ctrl.Result{RequeueAfter: hint.RequeuePollThreshold}, nil

	// 4) collect result, update status, clear runner, possibly start another run
	default: // corev1.PodSucceeded, corev1.PodFailed, corev1.PodUnknown
		
		st.SetCondition(hv1a1.JobRunning, metav1.ConditionFalse, "PodFinished", "Runner Pod finished")
		// Pod is finished, but if NeedsRerun is set, we ifgnore it and return to start a new run
		if meta.IsStatusConditionTrue(st.Conditions, string(hv1a1.ResyncRequired)) {
			st.SetCondition(hv1a1.ResyncRequired, metav1.ConditionFalse, "StartingNewRun", "Starting a new run as requested")
			if err := r.Status().Update(ctx, img); err != nil {return ctrl.Result{}, err }
			// requeue quickly to create the next Pod for the new spec
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}

		podOutput, err := r.getPodStdOut(ctx, pod.Name, pod.Namespace)
		if err != nil {
			msg := fmt.Sprintf("failed to get logs from runner Pod %s: %s", pod.Name, err.Error())
			r.Logger.Error(err, msg, "name", req.Name, "image", img.Name)
			r.Recorder.Event(img, corev1.EventTypeWarning, "GetPodLogsFailed", msg)
			return ctrl.Result{}, err
		} 

		// If the pod termination reason is completed,
		// we set the Zones based on the POD message
		success := pod.Status.Phase == corev1.PodSucceeded && pkgpod.ContainerTerminatedReason(pod) == "Completed"
		if success {
			if zones, err := r.decodeJSONToImageOffering(podOutput); err != nil {
				r.Logger.Error(err, msg, "name", req.Name, "image", img.Name)
				r.Recorder.Event(img, corev1.EventTypeWarning, "DecodePodLogFailed", msg)
				return ctrl.Result{}, err
			} else { img.Status.Images = zones }
			
	
			// The configMap is also updated by the ProviderProfile,
			// I update it here to ensure latest update is applied to CM if manual 
			// changes are made to the image
			if err := r.updateConfigMap(ctx, pf, img); err != nil {
				msg := fmt.Sprintf("Failed to update/create ConfigMap for image offerings: %s", err.Error())
				r.Recorder.Event(img, corev1.EventTypeWarning, "ConfigMapUpdateFailed", msg)
				r.Logger.Error(err, msg, "name", req.Name, "image", img.Name)
				return ctrl.Result{}, err
			}
			// We let the job controller handle the deletion of the job Pod
		}

		// no more work
		st.LastUpdateTime = metav1.NewTime(now)
		// SetTimeAnnotation(img, "skycluster.io/last-update-time", metav1.NewTime(now))
		st.Region = pf.Spec.Region
		st.ObservedGeneration = img.GetGeneration()
		st.SetCondition(hv1a1.Ready, metav1.ConditionTrue, "PodFinished", "Runner Pod finished successfully")
		r.Logger.Info("Runner Pod finished", "name", req.Name, "image", img.Name, "podName", pod.Name, "podPhase", pod.Status.Phase, "terminationReason", pkgpod.ContainerTerminatedReason(pod))

		// if err := r.Update(ctx, img); err != nil {return ctrl.Result{}, err}
		if err := r.Status().Update(ctx, img); err != nil { return ctrl.Result{}, err }
		return ctrl.Result{}, nil
	}
}

/*

HELPER FUNCTIONS

*/

// SetupWithManager sets up the controller with the Manager.
func (r *ImageReconciler) SetupWithManager(mgr ctrl.Manager) error {
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
		"PROVIDER":   pf.Spec.Platform,
		"REGION":     pf.Spec.Region,
		"INPUT_JSON": jsonData,
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
						Env:   envVars,
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

	// Set owner reference to the Image
	if err := ctrl.SetControllerReference(img, job, r.Scheme); err != nil {
		return nil, fmt.Errorf("failed to set owner reference for job: %w", err)
	}	
	return job, nil
}

func (r *ImageReconciler) fetchProviderProfileCred(ctx context.Context, pp *cv1a1.ProviderProfile) (map[string]string, error) {
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
		cred["SERVICE_ACCOUNT_JSON"] = string(secret.Data["configs"])
		cred["GOOGLE_CLOUD_PROJECT"] = "dummy" // GCP requires a project, but we don't use it in the image-finder Pod
	default:
		return nil, fmt.Errorf("unsupported platform %s for credentials secret", pp.Spec.Platform)
	}

	return cred, nil
}

func (r *ImageReconciler) generateJSON(zones []cv1a1.ZoneSpec, imageLabels []cv1a1.ImageOffering) (string, error) {
	type ImageOffering struct {
		Zone      string `json:"zone"`
		NameLabel string `json:"nameLabel"`
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
			})
		}
	}

	// Wrap zones in a payload struct
	wrapped := payload{Images: imgOfferings}
	// Use the wrapped struct to ensure the correct JSON structure
	// with "zones" as the top-level key
	jsonDataByte, err := json.Marshal(wrapped)
	if err != nil {
		return "", fmt.Errorf("failed to marshal input JSON: %w", err)
	}
	// Convert to string and return
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
		return nil, fmt.Errorf("missing or invalid zones field")
	}
	return payload.Zones, nil
}

func (r *ImageReconciler) updateProviderProfile(ctx context.Context, pf *cv1a1.ProviderProfile, img *cv1a1.Image) error {
	if err := r.Get(ctx, client.ObjectKey{Name: img.Spec.ProviderRef, Namespace: img.Namespace}, pf); err != nil {
		return err
	}
	orig := pf.DeepCopy()

	if pf.Annotations == nil { pf.Annotations = map[string]string{} }
	pf.Annotations["instancetype-ref"] = img.Name

	return r.Patch(ctx, pf, client.MergeFrom(orig))
}

func (r *ImageReconciler) getPodStdOut(ctx context.Context, podName, podNS string) (string, error) {
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

// If any of the input data is not nil, it will update the ConfigMap with the data.
func (r *ImageReconciler) updateConfigMap(ctx context.Context, pf *cv1a1.ProviderProfile, img *cv1a1.Image) error {
	// early return if both are nil
	if img == nil { return nil }
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

	imgYamlData, err2 := pkgenc.EncodeObjectToYAML(img.Status.Images)

	if err2 == nil {cm.Data["images.yaml"] = imgYamlData}

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
	err := r.List(ctx, existings, client.InNamespace(img.Namespace), client.MatchingLabels(labels))

	if err == nil && len(existings.Items) > 0 {
		// already exists, no need to create a new one
		theJob := &existings.Items[0]
		return theJob, nil
	} 

	// set the owner reference for the job
	if err := ctrl.SetControllerReference(img, job, r.Scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference for Job: %w", err)
	}

	// does not exist, create a new one
	if err := r.Create(ctx, job); err != nil && !apierrors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("failed to create runner Job: %w", err)
	}
	
	return job, nil
}

func (r *ImageReconciler) fetchPod(ctx context.Context, pf *cv1a1.ProviderProfile, img *cv1a1.Image) (*corev1.Pod, error) {
	var pod corev1.PodList
	ll := hint.DefaultLabels(pf.Spec.Platform, pf.Spec.Region, "")
	ll["skycluster.io/job-type"] = "image-finder"
	ll["skycluster.io/provider-profile"] = pf.Name

	if err := r.List(ctx, &pod, client.InNamespace(img.Namespace), client.MatchingLabels(ll)); err != nil {
		return nil, fmt.Errorf("failed to list Pods for Image %s: %w", img.Name, err)
	}
	if len(pod.Items) == 0 {
		return nil, apierrors.NewNotFound(corev1.Resource("pod"), fmt.Sprintf("no Pod found for Image %s", img.Name))
	}
	if len(pod.Items) > 1 {
		return nil, fmt.Errorf("multiple Pods found for Image %s, expected only one", img.Name)
	}
	// return the single Pod found
	return &pod.Items[0], nil
}
