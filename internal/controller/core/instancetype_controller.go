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
	"sort"
	"time"

	lo "github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	ec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	pricing "github.com/aws/aws-sdk-go-v2/service/pricing"

	corev1alpha1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	"github.com/skycluster-project/skycluster-operator/internal/controller/core/helper"
)

// InstanceTypeReconciler reconciles a InstanceType object
type InstanceTypeReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core.skycluster.io,resources=instancetypes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.skycluster.io,resources=instancetypes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.skycluster.io,resources=instancetypes/finalizers,verbs=update

func (r *InstanceTypeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := zap.New(helper.CustomLogger()).WithName("[InstanceType]")
	logger.Info("Reconciler started.", "name", req.Name)

	const updateThreshold = time.Duration(corev1alpha1.DefaultRefreshHourInstanceType) * time.Hour

	// Fetch the InstanceType resource
	it := &corev1alpha1.InstanceType{}
	if err := r.Get(ctx, req.NamespacedName, it); err != nil {
		logger.Info("unable to fetch InstanceType")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Check if the last update is within the threshold
	lastUpdate, exists := it.Annotations["skycluster.io/last-update"]
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
	it.Annotations["skycluster.io/last-update"] = now.Format(time.RFC3339)
	if err := r.Update(ctx, it); err != nil {
		logger.Error(err, "failed to update last update annotation")
		return ctrl.Result{}, err
	}

	// Reconciliation starts here
	// Find the provider by name and if it does not exist, return an error
	provider := &corev1alpha1.Provider{}
	if err := r.Get(ctx, client.ObjectKey{Name: it.Spec.ProviderRef, Namespace: it.Namespace}, provider); err != nil {
		logger.Info("unable to fetch Provider for instance type", "name", it.Spec.ProviderRef)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Based on provider platform, region and zone, fetch the configmap
	// containing the data and update the InstanceType resource
	// Find the primary zone
	defaultZone, ok := lo.Find(provider.Spec.Zones, func(zone corev1alpha1.ZoneSpec) bool {
		return zone.DefaultZone
	})
	if !ok {
		logger.Info("No default zone found for provider", "provider", provider.Name)
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

	// Construct the EC2 client with the fetched credentials and region
	awsConfig, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(provider.Spec.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, "")))
	if err != nil {
		logger.Error(err, "unable to load AWS config")
		return ctrl.Result{}, err
	}
	ec2Client := ec2.NewFromConfig(awsConfig)
	awsConfigPricing := awsConfig.Copy()
	awsConfigPricing.Region = "us-east-1" // Pricing API is only available in us-east-1
	pricingClient := pricing.NewFromConfig(awsConfigPricing)

	var zoneInstanceTypes []corev1alpha1.ZoneInstanceTypeSpec
	for _, zoneSpec := range provider.Spec.Zones {
		if !zoneSpec.Enabled {
			logger.Info("Skipping disabled zone", "zone", zoneSpec.Name)
			continue
		}

		// Fetch the instance types from AWS
		instanceTypes, err := helper.FetchAwsInstanceTypes(ec2Client, zoneSpec.Name)
		if err != nil {
			logger.Error(err, "failed to fetch AWS instance types for zone", "zone", zoneSpec.Name)
			continue
		}
		logger.Info("Fetched AWS instance types", "count", len(instanceTypes), "region", provider.Spec.Region, "zone", zoneSpec.Name)

		if len(instanceTypes) == 0 {
			logger.Info("No instance types found for zone", "zone", zoneSpec.Name)
			continue
		}

		// Iterate over the instance types and fetch their pricing and specs
		// flavors := []corev1alpha1.InstanceOffering{}
		flavors, err := helper.FetchAwsInstanceTypeDetials2(ec2Client, pricingClient, instanceTypes)
		if err != nil {
			logger.Info("Failed to fetch instance type details", "instanceType", it, "error", err)
			continue
		}

		if len(flavors) == 0 {
			logger.Info("No flavors found for zone", "zone", zoneSpec.Name)
			continue
		}
		logger.Info("Fetched flavors (detailed) for zone", "zone", zoneSpec.Name, "flavorsCount", len(flavors))

		// Fetch spot instance prices for the flavors
		instanceTypeNames := lo.Map(flavors, func(fl corev1alpha1.InstanceOffering, _ int) string { return fl.Name })
		spotPrices, err := helper.FetchAwsSpotInstanceTypes(ec2Client, instanceTypeNames, zoneSpec.Name)
		if err != nil {
			logger.Info("Failed to fetch AWS spot instance types", "error", err)
		} else {
			logger.Info("Fetched AWS spot instance types", "count", len(spotPrices), "region", provider.Spec.Region, "zone", zoneSpec.Name)
			for i, flavor := range flavors {
				if price, ok := spotPrices[flavor.Name]; ok {
					flavors[i].Spot.Price = price
					flavors[i].Spot.Enabled = true
					// logger.Info("Fetched spot price for flavor", "flavor", flavor.Name, "price", price)
				}
			}
		}

		// sort flavors by vCPUs and RAM ascending (smaller flavors first)
		sort.SliceStable(flavors, func(i, j int) bool {
			if flavors[i].VCPUs == flavors[j].VCPUs {
				return flavors[i].RAM < flavors[j].RAM
			}
			return flavors[i].VCPUs < flavors[j].VCPUs
		})
		// Remove duplicate flavors based on Name
		flavors = lo.UniqBy(flavors, func(fl corev1alpha1.InstanceOffering) string { return fl.Name })

		logger.Info("Adding instance types for zone", "zone", zoneSpec.Name, "flavorsCount", len(flavors))
		zoneInstanceTypes = append(zoneInstanceTypes, corev1alpha1.ZoneInstanceTypeSpec{
			ZoneName: zoneSpec.Name,
			Flavors:  flavors,
		})
	}

	// oldStatus := it.Status.DeepCopy()
	// if !equality.Semantic.DeepEqual(it.Status.ZoneInstanceTypes, zoneInstanceTypes) {
	// 	it.Status.ZoneInstanceTypes = zoneInstanceTypes
	// }
	// if it.Status.Region != provider.Spec.Region {
	// 	it.Status.Region = provider.Spec.Region
	// }
	// // Only update if there are changes to report
	// if changed := !equality.Semantic.DeepEqual(it.Status, oldStatus); changed {
	// 	logger.Info("Updating InstanceType status", "name", it.Name, "status", it.Status)
	// 	if err := r.Status().Update(ctx, it); err != nil {
	// 		logger.Error(err, "failed to update InstanceType status")
	// 		return ctrl.Result{}, err
	// 	}
	// }

	// it.Status.ZoneInstanceTypes = zoneInstanceTypes
	it.Status.Region = provider.Spec.Region

	p := provider.Spec.Platform
	rg := provider.Spec.Region
	z := defaultZone.Name
	// Update the configMap with the new instance types
	yamlData, err := helper.EncodeObjectToYAML(zoneInstanceTypes)
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

	// Add provider labels to the InstanceType resource
	if it.Labels == nil {
		it.Labels = make(map[string]string)
	}
	it.Labels["skycluster.io/provider-platform"] = p
	it.Labels["skycluster.io/provider-region"] = rg
	it.Labels["skycluster.io/provider-zone"] = defaultZone.Name

	// Update the InstanceType resource with the new labels
	if err := r.Update(ctx, it); err != nil {
		logger.Error(err, "unable to update InstanceType labels")
		return ctrl.Result{}, err
	}

	logger.Info("Reconciliation complete", "name", req.Name)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *InstanceTypeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1alpha1.InstanceType{}).
		Named("core-instancetype").
		Complete(r)
}

func (r *InstanceTypeReconciler) updateConfigMap(ctx context.Context, cm *corev1.ConfigMap, yamlData, p, rg, z string) error {
	if cm.Data == nil {
		cm.Data = make(map[string]string)
	}
	cm.Data["instance-types.yaml"] = yamlData
	if cm.Labels == nil {
		cm.Labels = make(map[string]string)
	}
	cm.Labels["skycluster.io/managed-by"] = "skycluster"
	cm.Labels["skycluster.io/provider-platform"] = p
	cm.Labels["skycluster.io/provider-region"] = rg
	cm.Labels["skycluster.io/provider-zone"] = z
	return nil
}

func (r *InstanceTypeReconciler) updateOrCreate(ctx context.Context, cm *corev1.ConfigMap, cmFound bool, cmName string) error {
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
