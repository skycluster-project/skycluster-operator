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
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	ec2 "github.com/aws/aws-sdk-go-v2/service/ec2"

	corev1alpha1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	"github.com/skycluster-project/skycluster-operator/internal/controller/core/helper"
)

// ImagesReconciler reconciles a Images object
type ImagesReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core.skycluster.io,resources=images,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.skycluster.io,resources=images/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.skycluster.io,resources=images/finalizers,verbs=update

func (r *ImagesReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := zap.New(helper.CustomLogger()).WithName("[Images]")
	logger.Info("Reconciler started.", "name", req.Name)

	const updateThreshold = time.Duration(corev1alpha1.DefaultRefreshHourImages) * time.Hour

	// Fetch the Images resource
	images := &corev1alpha1.Images{}
	if err := r.Get(ctx, req.NamespacedName, images); err != nil {
		logger.Info("unable to fetch Images")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Check if the last update is within the threshold
	lastUpdate, exists := images.Annotations["skycluster.io/last-update"]
	var lastUpdateTime time.Time
	if exists {
		lastUpdateTime, _ = time.Parse(time.RFC3339, lastUpdate)
	}

	// If the last update is within the threshold, skip reconciliation
	now := time.Now()
	if exists && now.Sub(lastUpdateTime) < updateThreshold {
		logger.Info("Skipping reconciliation, last update is within threshold", "lastUpdate", lastUpdateTime, "threshold", updateThreshold)
		return ctrl.Result{RequeueAfter: updateThreshold - now.Sub(lastUpdateTime)}, nil
	}

	// Update the last update annotation
	images.Annotations["skycluster.io/last-update"] = now.Format(time.RFC3339)
	if err := r.Update(ctx, images); err != nil {
		logger.Error(err, "failed to update last update annotation")
		return ctrl.Result{}, err
	}

	// Find the provider by name and if it does not exist, return an error
	provider := &corev1alpha1.Provider{}
	if err := r.Get(ctx, client.ObjectKey{Name: images.Spec.ProviderRef, Namespace: images.Namespace}, provider); err != nil {
		logger.Info("unable to fetch Provider for image", "name", images.Spec.ProviderRef)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Fetch the AWS credentials
	accessKeyID, secretAccessKey, err := helper.FetchAWSKeysFromSecret(ctx, r.Client)
	if err != nil {
		logger.Error(err, "unable to fetch AWS credentials from secret")
		return ctrl.Result{}, err
	}

	// construct the ec2 client with the fetched credentials and region
	awsConfig, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(provider.Spec.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, "")))
	if err != nil {
		logger.Error(err, "unable to load AWS config")
		return ctrl.Result{}, err
	}
	ec2client := ec2.NewFromConfig(awsConfig)

	// Iterate through the images and fetch the equivalent AWS images
	// Since the images are region specific, if we know the mapping we use again
	mappings := make(map[string]string)
	for i, zone := range images.Spec.Zones {
		if zone.NameLabel == "" {
			logger.Info("Skipping empty zone nameLabel")
			continue
		}
		if _, exists := mappings[zone.NameLabel]; exists {
			logger.Info("Skipping already processed zone", "nameLabel", zone.NameLabel)
			images.Spec.Zones[i].Name = mappings[zone.NameLabel]
			continue
		}
		awsImages, err := helper.FetchAwsImage(ec2client, zone.NameLabel)
		if err != nil {
			logger.Error(err, "unable to fetch AWS image", "nameLabel", zone.NameLabel)
			continue
		}
		if len(awsImages) == 0 {
			logger.Info("No AWS images found for", "nameLabel", zone.NameLabel)
			continue
		}
		mappings[zone.NameLabel] = aws.ToString(awsImages[0].ImageId)
		// Update the Images status with the fetched AWS images
		images.Spec.Zones[i].Name = aws.ToString(awsImages[0].ImageId)
	}

	// Update the Images status with the region and zones
	images.Status.Region = provider.Spec.Region
	images.Status.Zones = images.Spec.Zones
	logger.Info("Updating Images status", "name", images.Name, "zones", images.Status.Zones)

	if err := r.Status().Update(ctx, images); err != nil {
		logger.Error(err, "unable to update Images status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ImagesReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1alpha1.Images{}).
		Named("core-images").
		Complete(r)
}
