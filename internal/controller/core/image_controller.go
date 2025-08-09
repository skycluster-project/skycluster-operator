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
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	ec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	lo "github.com/samber/lo"

	corev1alpha1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
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

func (r *ImageReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := zap.New(helper.CustomLogger()).WithName("[Images]")
	logger.Info("Reconciler started.", "name", req.Name)

	const updateThreshold = time.Duration(corev1alpha1.DefaultRefreshHourImages) * time.Hour

	// Fetch the Images resource
	images := &corev1alpha1.Image{}
	if err := r.Get(ctx, req.NamespacedName, images); err != nil {
		logger.Info("unable to fetch Image")
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
	provider := &corev1alpha1.ProviderProfile{}
	if err := r.Get(ctx, client.ObjectKey{Name: images.Spec.ProviderRef, Namespace: images.Namespace}, provider); err != nil {
		logger.Info("unable to fetch ProviderProfile for image", "name", images.Spec.ProviderRef)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Based on provider platform, region and zone, fetch the configmap
	// containing the data and update the InstanceType resource

	defaultZone, ok := lo.Find(provider.Spec.Zones, func(zone corev1alpha1.ZoneSpec) bool {
		return zone.DefaultZone
	})
	if !ok {
		logger.Info("no default zone found for provider", "name", provider.Name)
		return ctrl.Result{}, nil
	}
	cmName := fmt.Sprintf("%s.%s.%s", provider.Spec.Platform, provider.Spec.Region, defaultZone.Name)
	cm, cmFound := &corev1.ConfigMap{}, true
	if err := r.Get(ctx, client.ObjectKey{Name: cmName, Namespace: helper.SKYCLUSTER_NAMESPACE}, cm); err != nil {
		cmFound = false
		if client.IgnoreNotFound(err) != nil {
			logger.Info("error fetching ConfigMap", "name", cmName)
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
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

	p := provider.Spec.Platform
	rg := provider.Spec.Region
	z := defaultZone.Name
	// Update the configMap with the new instance types
	type imagesDataType struct {
		Zones []corev1alpha1.ImageOffering `json:"zones"`
	}
	yamlData, err := helper.EncodeObjectToYAML(imagesDataType{
		Zones: images.Spec.Zones,
	})
	if err != nil {
		logger.Error(err, "failed to encode instance types to YAML")
		return ctrl.Result{}, err
	}
	err = r.updateConfigMap(ctx, cm, yamlData, p, rg, z)
	if err != nil {
		logger.Error(err, "failed to update ConfigMap with images data")
		return ctrl.Result{}, err
	}
	// If the ConfigMap does not exist, create it
	err = r.updateOrCreate(ctx, cm, cmFound, cmName)
	if err != nil {
		logger.Error(err, "unable to update or create ConfigMap for images")
		return ctrl.Result{}, err
	}

	// Update the Images status with the region and zones
	images.Status.Region = provider.Spec.Region
	images.Status.Zones = images.Spec.Zones
	logger.Info("Updating Images status", "name", images.Name, "zones", images.Status.Zones)

	if err := r.Status().Update(ctx, images); err != nil {
		logger.Error(err, "unable to update Images status")
		return ctrl.Result{}, err
	}

	// Add provider lables to the Images resource
	if images.Labels == nil {
		images.Labels = make(map[string]string)
	}
	images.Labels["skycluster.io/provider-platform"] = provider.Spec.Platform
	images.Labels["skycluster.io/provider-region"] = provider.Spec.Region
	images.Labels["skycluster.io/provider-zone"] = defaultZone.Name

	// Update the Images resource with the new labels
	if err := r.Update(ctx, images); err != nil {
		logger.Error(err, "unable to update Images labels")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ImageReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1alpha1.Image{}).
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
