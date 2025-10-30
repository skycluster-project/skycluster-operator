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
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	"github.com/samber/lo"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	hv1a1 "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"
	pv1a1 "github.com/skycluster-project/skycluster-operator/api/policy/v1alpha1"
	helper "github.com/skycluster-project/skycluster-operator/internal/helper"
)

// SkyXRDReconciler reconciles a SkyXRD object
type SkyXRDReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Logger logr.Logger
}

type subnetMetadata struct {
	Type string `yaml:"type"`
	CIDR string `yaml:"cidr"`
}

type gatewayMetadata struct {
	Flavor     string `yaml:"flavor"`
	VolumeType string `yaml:"volumeType"`
	VolumeSize int    `yaml:"volumeSize"`
}

type providerMetadata struct {
	VPCCIDR string           `yaml:"vpcCidr"`
	Subnets []subnetMetadata `yaml:"subnets"`
	Gateway gatewayMetadata  `yaml:"gateway"`
	ServiceCidr string `yaml:"serviceCidr,omitempty"`
	NodeCidr    string `yaml:"nodeCidr,omitempty"`
	PodCidr     PodCidrSpec `yaml:"podCidr,omitempty"`
}

type PodCidrSpec struct {
	Cidr 	string `yaml:"cidr"`
	Public string `yaml:"public,omitempty"`
	Private string `yaml:"private,omitempty"`
}


// +kubebuilder:rbac:groups=core,resources=skyxrds,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=skyxrds/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=skyxrds/finalizers,verbs=update

func (r *SkyXRDReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Logger.Info("Reconciler started")

	// Fetch the object
	skyxrd := &cv1a1.SkyXRD{}
	if err := r.Get(ctx, req.NamespacedName, skyxrd); err != nil {
		r.Logger.Info("SkyXRD not found.")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	manifests, _,  err := r.createManifests(req.Name, req.Namespace, skyxrd)
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "error creating manifests.")
	}
	
	if skyxrd.Spec.Approve {
		for _, xrd := range manifests {
			var obj map[string]any
			if err := yaml.Unmarshal([]byte(xrd.Manifest), &obj); err != nil {
				r.Logger.Error(err, "error unmarshalling manifest yaml.", "manifest", xrd.Manifest)
				return ctrl.Result{}, err
			}

			svcKind := xrd.ComponentRef.Kind
			if svcKind != "XProvider" && svcKind != "XKube" {continue} // only create providers for now

			unstrObj := &unstructured.Unstructured{Object: obj}
			unstrObj.SetAPIVersion(xrd.ComponentRef.APIVersion)
			unstrObj.SetKind(xrd.ComponentRef.Kind)
			unstrObj.SetGenerateName(fmt.Sprintf("%s-", xrd.ComponentRef.Name))
			// if err := ctrl.SetControllerReference(skyxrd, unstrObj, r.Scheme); err != nil {
			// 	return ctrl.Result{}, errors.Wrap(err, "error setting controller reference.")
			// }
			if err := r.Create(ctx, unstrObj); err != nil && !apierrors.IsAlreadyExists(err) {
				return ctrl.Result{}, client.IgnoreAlreadyExists(err)
			}
			// r.Logger.Info("Created SkyXRD object!", "name", xrd.ComponentRef.Name)
		}
	}

	if !reflect.DeepEqual(skyxrd.Status.Manifests, manifests) {
    skyxrd.Status.Manifests = manifests
    if err := r.Status().Update(ctx, skyxrd); err != nil {
        return ctrl.Result{}, errors.Wrap(err, "error updating SkyXRD status")
    }
		// r.Logger.Info("Updated SkyXRD status manifests.", "count", len(manifests))
	}
	
	return ctrl.Result{}, nil
}

func (r *SkyXRDReconciler) updateStatusManifests(skyxrd *cv1a1.SkyXRD) error {
	updateErr := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		latest := &cv1a1.SkyXRD{}
		if err := r.Get(context.TODO(), client.ObjectKey{Namespace: skyxrd.Namespace, Name: skyxrd.Name}, latest); err != nil {
			return err
		}
		latest.Status.Manifests = skyxrd.Status.Manifests // copy prepared status
		return r.Status().Update(context.TODO(), latest)
	})
	return updateErr
}

func (r *SkyXRDReconciler) createManifests(appId string, ns string, skyxrd *cv1a1.SkyXRD) ([]hv1a1.SkyService, []hv1a1.SkyService, error) {
	deployMap := skyxrd.Spec.DeployMap
	manifests := make([]hv1a1.SkyService, 0)
	
	// ######### Providers
	// Each deployment comes with component info (e.g. kind and apiVersion and name)
	// as well as the provider info (e.g. name, region, zone, type) that it should be deployed on
	// We extract all provider's info and create corresponding SkyProvider objects for each provider
	// r.Logger.Info("Generating provider manifests.")
	providersManifests, err := r.generateProviderManifests(appId, ns, deployMap.Component)
	if err != nil {
		return nil, nil, errors.Wrap(err, "Error generating provider manifests.")
	}
	for _, obj := range providersManifests {
		manifests = append(manifests, obj)
	}
	// r.Logger.Info("Generated provider manifests.", "count", len(providersManifests))

	depPolicy, err := r.fetchDeploymentPolicy(ns, skyxrd.Spec.DeploymentPolicyRef)
	if err != nil { return nil, nil, errors.Wrap(err, "fetching deployment policy") }

	requiredVirtSvcs := make(map[string][]pv1a1.VirtualServiceSelector)
	for _, skySrvc := range deployMap.Component {
		pName := skySrvc.ProviderRef.Name
		if _, ok := requiredVirtSvcs[pName]; !ok {
			requiredVirtSvcs[pName] = make([]pv1a1.VirtualServiceSelector, 0)
		}

		svcs, err := r.findReqVirtualSvcs(ns, skySrvc, *depPolicy)
		if err != nil {return nil, nil, errors.Wrap(err, "fetching required virtual services")}

		requiredVirtSvcs[pName] = append(requiredVirtSvcs[pName], svcs...)
	}

	// we now know the required virtual services for each provider
	managedK8sSvcs := make(map[string][]pv1a1.VirtualServiceSelector)
	for pName, virtSvcs := range requiredVirtSvcs {
		for _, vs := range virtSvcs {
			if vs.Name == "ManagedKubernetes" { // TODO: we use Name to identify the kind, need to change later.
				// we only need to create one ManagedKubernetes virtual service per provider
				// we filter all ComputeProfile (flavors) and use them to create workers in the cluster
				if _, ok := managedK8sSvcs[pName]; !ok {
					managedK8sSvcs[pName] = lo.Filter(virtSvcs, func(v pv1a1.VirtualServiceSelector, _ int) bool {
						return v.Name == "ComputeProfile" || v.Kind == "ComputeProfile"
					})
				}
			} else if vs.Name == "ComputeProfile" || vs.Kind == "ComputeProfile" {
				// TODO: Handle ComputeProfile virtual services
				// r.Logger.Info("ComputeProfile virtual service.", "service", vs.Name, "provider", pName)
			} else {
				return nil, nil, errors.New("unsupported virtual service kind: " + vs.Name)
			}
		}
	}

	// Handle ManagedKubernetes virtual services
	// managedK8sSvcs contains ComputeProfile (flavors) for the cluster
	// r.Logger.Info("Generating SkyK8SCluster manifests.")
	managedK8sManifests, err := r.generateMgmdK8sManifests(appId, managedK8sSvcs)
	if err != nil {
		return nil, nil, errors.Wrap(err, "Error generating SkyK8SCluster.")
	}
	for _, obj := range managedK8sManifests {
		manifests = append(manifests, obj)
	}
	r.Logger.Info("Generated SkyK8SCluster manifests.", "count", len(managedK8sManifests))

	// Kubernetes Mesh
	// r.Logger.Info("Generating XKubeMesh manifests.")
	skyMesh, err := r.generateK8sMeshManifests(appId, lo.Values(managedK8sManifests))
	if err != nil {
		return nil, nil, errors.Wrap(err, "Error generating XKubeMesh.")
	}
	manifests = append(manifests, *skyMesh)

	return manifests, nil, nil
}

// Creates manifest for setup of providers needed for the deployment
func (r *SkyXRDReconciler) generateProviderManifests(appId string, ns string, cmpnts []hv1a1.SkyService) (map[string]hv1a1.SkyService, error) {
	// We need to group the components based on the providers
	// and then generate the manifests for each provider
	// We then return the manifests for each provider
	uniqueProviders := make(map[string]hv1a1.ProviderRefSpec, 0)
	for _, cmpnt := range cmpnts {
		// ProviderName uniquely identifies a provider, set by the optimizer
		providerName := cmpnt.ProviderRef.Name
		if _, ok := uniqueProviders[providerName]; ok { continue }
		uniqueProviders[providerName] = cmpnt.ProviderRef
	}
	// Now we have all unique providers
	// We can now generate the manifests for each provider
	// we use settings corresponding to each provider stored in a configmap (ip ranges, etc.)
	provMetadata, err := r.loadProviderMetadata()
	if err != nil {
		return nil, errors.Wrap(err, "Error loading provider metadata.")
	}

	providerProfiles, err := r.fetchProviderProfilesMap()
	if err != nil {
		return nil, errors.Wrap(err, "Error fetching provider profiles.")
	}
	
	manifests := map[string]hv1a1.SkyService{}
	for pName, p := range uniqueProviders {
		id := appId + "-" + pName
		idx, err := MapToIndex(id, len(provMetadata[p.Platform]))
		if err != nil { return nil, err }

		obj := &unstructured.Unstructured{}
		obj.SetAPIVersion("skycluster.io/v1alpha1")
		obj.SetKind("XProvider")
		name := helper.EnsureK8sName("xp-" + pName + "-" + appId)
		rand := RandSuffix(name)
		name = name[0:int(math.Min(float64(len(name)), 15))]
		name = name + "-" + rand
		obj.SetName(name)

		znPrimary, ok := lo.Find(providerProfiles[pName].Spec.Zones, func(z cv1a1.ZoneSpec) bool { return z.DefaultZone })
		if !ok {return nil, errors.New("No primary zone found")}
		znSecondary, ok := lo.Find(providerProfiles[pName].Spec.Zones, func(z cv1a1.ZoneSpec) bool { return z.Name != znPrimary.Name })
		if !ok {return nil, errors.New("No secondary zone found")}

		spec := map[string]any{
			"applicationId": appId,
			"vpcCidr":       provMetadata[p.Platform][idx].VPCCIDR,
			"gateway": func() map[string]any {
				fields := make(map[string]any)
				if p.Platform == "aws" {
					fields["volumeType"] = "gp2"
					fields["volumeSize"] = 30
				}
				fields["flavor"] = func () string {
					if p.Platform == "aws" || p.Platform == "openstack" {
						return "4vCPU-16GB"
					}
					return "2vCPU-8GB"
				}()
				return fields
			}(),
			"subnets": func() []map[string]any {
				subnets := make([]map[string]any, 0)
				for _, sn := range provMetadata[p.Platform][idx].Subnets {
					subnet := map[string]any{
						"cidr": sn.CIDR,
						"zone": p.Zone,
					}
					if p.Platform == "aws" {
						subnet["type"] = sn.Type
					}
					if p.Platform == "baremetal" && sn.Type == "public" {
						subnet["default"] = true
					} 
					subnets = append(subnets, subnet)
				}
				return subnets
			}(),
			"externalNetwork": func() map[string]string {
				if p.Platform != "openstack" {
					return nil
				}
				return map[string]string{
					"networkName": "ext-net",
					"networkId":   "0a23c4ae-98ba-4f3a-8c85-5a7dc614cc4e",
					"subnetName":  "ext-subnet",
					"subnetId":    "ae9a8eac-5f6f-4681-98f6-71acf18dcfbb",
				}
			}(),
			"providerRef": map[string]any{
				"platform": p.Platform,
				"region": p.Region,
				"zones":  map[string]string{
					"primary": znPrimary.Name,
					"secondary": znSecondary.Name,
				},
			},
		}
		obj.Object["spec"] = spec
		// extra labels only for "SAVI" (openstack)
		if p.Platform == "openstack" {
			objAnnt := make(map[string]string, 0)
			objAnnt["skycluster.io/external-resources"] = "[{\"apiVersion\":\"identity.openstack.crossplane.io/v1alpha1\",\"kind\":\"ProjectV3\",\"id\":\"1e1c724728544ec2a9058303ddc8f30b\"}]"
			obj.SetAnnotations(objAnnt)
		}
		
		yamlObj, err := generateYAMLManifest(obj)
		if err != nil {
			return nil, errors.Wrap(err, "Error generating YAML manifest.")
		}
		manifests[pName] = hv1a1.SkyService{
			ComponentRef: corev1.ObjectReference{
				APIVersion: obj.GetAPIVersion(),
				Kind:       obj.GetKind(),
				Namespace:  obj.GetNamespace(),
				Name:       obj.GetName(),
			},
			Manifest: yamlObj,
			ProviderRef: hv1a1.ProviderRefSpec{
				Name:   pName,
				Type: p.Type,
				Platform: p.Platform,
				Region: p.Region,
				RegionAlias: p.RegionAlias,
				Zone:   p.Zone,
			},
		}
	}
	return manifests, nil
}

func (r *SkyXRDReconciler) generateMgmdK8sManifests(appId string, svcList map[string][]pv1a1.VirtualServiceSelector) (map[string]hv1a1.SkyService, error) {
	// For managed K8S deployments, we start the cluster with nodes needed for control plane
	// and let the cluster autoscaler handle the rest of the scaling for the workload.
	// The optimization has decided that for requested workload, this provider is the best fit.

	k8sMetadata, err := r.loadProviderMetadata()
	if err != nil {return nil, errors.Wrap(err, "Error loading provider metadata.")}

	provProfiles, err := r.fetchProviderProfilesMap()
	if err != nil {return nil, errors.Wrap(err, "Error fetching provider profiles.")}

	manifests := map[string]hv1a1.SkyService{}
	for pName, computeProfileList := range svcList {
		// computeProfileList is the list of ComputeProfile virtual services for this provider
		// r.Logger.Info("Compute profiles for provider", "profiles", len(computeProfileList), "provider", pName)
		uniqueProfiles := lo.UniqBy(computeProfileList, func(v pv1a1.VirtualServiceSelector) string {
			return v.Name
		})
		// r.Logger.Info("Unique compute profiles for provider", "profiles", len(uniqueProfiles), "provider", pName)
		pp := provProfiles[pName]
		id := appId + "-" + pName
		idx, err := MapToIndex(id, len(k8sMetadata[pp.Spec.Platform]))
		if err != nil { return nil, err }

		znPrimary, ok := lo.Find(pp.Spec.Zones, func(z cv1a1.ZoneSpec) bool { return z.DefaultZone })
		if !ok {return nil, errors.New("No primary zone found")}
		znSecondary, ok := lo.Find(pp.Spec.Zones, func(z cv1a1.ZoneSpec) bool { return z.Name != znPrimary.Name })
		if !ok {return nil, errors.New("No secondary zone found")}

		// we need to create a XKube object
		xrdObj := &unstructured.Unstructured{}
		xrdObj.SetAPIVersion("skycluster.io/v1alpha1")
		xrdObj.SetKind("XKube")
		name := helper.EnsureK8sName("xk-" + pName + "-" + appId)
		rand := RandSuffix(name)
		name = name[0:int(math.Min(float64(len(name)), 15))]
		name = name + "-" + rand
		xrdObj.SetName(name)

		spec := map[string]any{
			"applicationId": appId,
			"serviceCidr":  k8sMetadata[pp.Spec.Platform][idx].ServiceCidr,
			"nodeCidr": func () string {
				if pp.Spec.Platform == "gcp" {
					return k8sMetadata[pp.Spec.Platform][idx].NodeCidr
				}
				return ""
			}(),
			"podCidr": func () map[string]any {
				fields := make(map[string]any)
				if pp.Spec.Platform == "aws" {
					fields["public"] = k8sMetadata[pp.Spec.Platform][idx].PodCidr.Public
					fields["private"] = k8sMetadata[pp.Spec.Platform][idx].PodCidr.Private
				}
				fields["cidr"] = k8sMetadata[pp.Spec.Platform][idx].PodCidr.Cidr
				return fields
			}(),
			"nodeGroups": func () []map[string]any {
				fields := make([]map[string]any, 0)
				if pp.Spec.Platform == "aws" {
					// default node group
					f := make(map[string]any)
					f["instanceTypes"] = []string{
						"2vCPU-4GB",
						"4vCPU-8GB",
						"8vCPU-32GB",
					}
					f["nodeCount"] = 3
					f["publicAccess"] = true
					f["autoScaling"] = map[string]any{
						"enabled": false,
						"minSize": 1,
						"maxSize": 4,
					}
					fields = append(fields, f)

					// workers
					for _, vs := range uniqueProfiles {
						f := make(map[string]any)
						f["instanceTypes"] = []string{vs.Name}
						f["publicAccess"] = false
						fields = append(fields, f)
					}
				}
				if pp.Spec.Platform == "gcp" {
					// workers only
					// manually limit this to prevent charging too much
					f := make(map[string]any)
						f["nodeCount"] = 2
						f["instanceType"] = "2vCPU-4GB"
						f["publicAccess"] = false
						f["autoScaling"] = map[string]any{
							"enabled": true,
							"minSize": 1,
							"maxSize": 3,
					}
					fields = append(fields, f)
					// for _, vs := range uniqueProfiles {
					// 	f := make(map[string]any)
					// 	f["nodeCount"] = 2
					// 	f["instanceType"] = vs.Name
					// 	f["publicAccess"] = false
					// 	f["autoScaling"] = map[string]any{
					// 		"enabled": true,
					// 		"minSize": 1,
					// 		"maxSize": 3,
					// 	}
					// 	fields = append(fields, f)
					// }
				}
				return fields
			}(),
			"principal": func () map[string]any {
				fields := make(map[string]any)
				if pp.Spec.Platform == "aws" {
					fields["type"] = "servicePrincipal"
					fields["id"] = "arn:aws:iam::885707601199:root"
				}
				return fields
			}(),
			"providerRef": func () map[string]any {
				fields := map[string]any{
					"platform": pp.Spec.Platform,
					"region": pp.Spec.Region,
				}
				if pp.Spec.Platform == "aws" {
					fields["zones"] = map[string]string{
						"primary": znPrimary.Name,
						"secondary": znSecondary.Name,
					}
				}
				if pp.Spec.Platform == "gcp" {
					fields["zones"] = map[string]string{
						"primary": znPrimary.Name,
					}
				}
				return fields
			}(),
		}

		xrdObj.Object["spec"] = spec

		yamlObj, err := generateYAMLManifest(xrdObj)
		if err != nil {
			return nil, errors.Wrap(err, "Error generating YAML manifest.")
		}
		manifests[pName] = hv1a1.SkyService{
			ComponentRef: corev1.ObjectReference{
				APIVersion: xrdObj.GetAPIVersion(),
				Kind:       xrdObj.GetKind(),
				Namespace:  xrdObj.GetNamespace(),
				Name:       xrdObj.GetName(),
			},
			Manifest: yamlObj,
			ProviderRef: hv1a1.ProviderRefSpec{
				Name:   pName,
				Type: znPrimary.Type,
				Platform: pp.Spec.Platform,
				Region: pp.Spec.Region,
				RegionAlias: pp.Spec.RegionAlias,
				Zone:   znPrimary.Name,
			},
		}
	}
	
	return manifests, nil
}

// only one mesh per application
func (r *SkyXRDReconciler) generateK8sMeshManifests(appId string, xKubeList []hv1a1.SkyService) (*hv1a1.SkyService, error) {
	
	clusterNames := make([]string, 0)
	for _, xkube := range xKubeList {
		kName := xkube.ComponentRef.Name
		clusterNames = append(clusterNames, kName)
	}
	
	// we need to create a XKube object
	xrdObj := &unstructured.Unstructured{}
	xrdObj.SetAPIVersion("skycluster.io/v1alpha1")
	xrdObj.SetKind("XKubeMesh")
	name := helper.EnsureK8sName("xkmesh-" + appId)
	rand := RandSuffix(name)
	name = name[0:int(math.Min(float64(len(name)), 15))]
	name = name + "-" + rand
	xrdObj.SetName(name)

	spec := map[string]any{
		"clusterNames": clusterNames,
		"localCluster": func () map[string]any {
			return map[string]any{
				"podCidr": "10.0.0.0/19",
				"serviceCidr": "10.0.32.0/19",
			}
		}(),
	}

	xrdObj.Object["spec"] = spec

	yamlObj, err := generateYAMLManifest(xrdObj)
	if err != nil {
		return nil, errors.Wrap(err, "Error generating YAML manifest.")
	}
	return &hv1a1.SkyService{
		ComponentRef: corev1.ObjectReference{
			APIVersion: xrdObj.GetAPIVersion(),
			Kind:       xrdObj.GetKind(),
			Namespace:  xrdObj.GetNamespace(),
			Name:       xrdObj.GetName(),
		},
		Manifest: yamlObj,
	}, nil
	
}


// dpRef is the deployment plan reference from SkyXRD spec
func (r *SkyXRDReconciler) fetchDeploymentPolicy(ns string, dpRef cv1a1.DeploymentPolicyRef) (*pv1a1.DeploymentPolicy, error) {
	dp := &pv1a1.DeploymentPolicy{}
	err := r.Get(context.TODO(), client.ObjectKey{Namespace: ns, Name: dpRef.Name}, dp)
	if err != nil {return nil, err}
	return dp, nil
}

// dpPolicy := r.fetchDeploymentPolicy(ns, dpRef)
// findReqVirtualSvcs(ns, skySrv, dpPolicy)
func (r *SkyXRDReconciler) findCheapestVirtualSvcs(ns string, providerRef hv1a1.ProviderRefSpec, virtualServices []pv1a1.VirtualServiceSelector) (*pv1a1.VirtualServiceSelector, error) {
	if len(virtualServices) == 0 {
		return nil, nil
	}

	virtSvcMap, err := r.fetchVirtualServiceCfgMap(ns, providerRef)
	if err != nil { return nil, errors.Wrap(err, "failed fetching virtual service config map") }

	// Find the set with the minimum cost
	var cheapestVirtSvc *pv1a1.VirtualServiceSelector
	minCost := math.MaxFloat64
	for _, vs := range virtualServices {
		
		cost, err := r.calculateVirtualServiceSetCost(virtSvcMap, vs, providerRef.Zone)
		if err != nil {return nil, err}

		if cost < minCost {
			minCost = cost
			cheapestVirtSvc = &vs
		}	
	}

	return cheapestVirtSvc, nil
}

func (r *SkyXRDReconciler) fetchVirtualServiceCfgMap(ns string, providerRef hv1a1.ProviderRefSpec) (map[string]string, error) {
	cm := &corev1.ConfigMapList{}
	if err := r.List(context.Background(), cm, client.MatchingLabels{
		"skycluster.io/config-type": "provider-profile",
		"skycluster.io/managed-by": "skycluster",
		"skycluster.io/provider-platform": providerRef.Platform,
		"skycluster.io/provider-profile": providerRef.Name,
		"skycluster.io/provider-region": providerRef.Region,
	}); err != nil { return nil, errors.Wrap(err, "listing provider profile ConfigMaps") }
	if len(cm.Items) == 0 { return nil, errors.New("no provider profile ConfigMaps found") }
	if len(cm.Items) > 1 { return nil, errors.New("multiple provider profile ConfigMaps found") }
	
	return cm.Items[0].Data, nil
}

// virtSvcMap := fetchVirtualServiceCfgMap(ns, providerRef)
// calculateVirtualServiceSetCost(virtSvcMap, virtualService, zone)
func (r *SkyXRDReconciler) calculateVirtualServiceSetCost(virtualSrvcMap map[string]string, vs pv1a1.VirtualServiceSelector, zone string) (float64, error) {
	
	// limited number of virtual services are supported: ManagedKubernetes, ComputeProfile
	switch vs.Name {
	case "ManagedKubernetes":
		// cmData["managed-k8s.yaml"]
		mngK8sList := []hv1a1.ManagedK8s{}
		if err := yaml.Unmarshal([]byte(virtualSrvcMap["managed-k8s.yaml"]), &mngK8sList); err != nil {
			return -1, errors.Wrap(err, "unmarshalling managed-k8s.yaml")
		}
		for _, mngK8s := range mngK8sList {
			if mngK8s.NameLabel == vs.Name {
				// found matching managed K8s, calculate cost
				overheadCost, err := strconv.ParseFloat(mngK8s.Overhead.Cost, 64)
				if err != nil {return -1, errors.Wrap(err, "parsing overhead cost")}

				price, err := strconv.ParseFloat(mngK8s.Price, 64)
				if err != nil {return -1, errors.Wrap(err, "parsing price")}

				totalCost := price + (overheadCost * float64(vs.Count))
				return totalCost, nil
			}
		}

	case "ComputeProfile":
		// cmData["flavors.yaml"]
		var zoneOfferings []cv1a1.ZoneOfferings
		if err := yaml.Unmarshal([]byte(virtualSrvcMap["flavors.yaml"]), &zoneOfferings); err == nil {
			
			for _, zo := range zoneOfferings {
				if zo.Zone != zone {continue}
				
				for _, of := range zo.Offerings {
					if of.NameLabel != vs.Name {continue}
					
					priceFloat, err := strconv.ParseFloat(of.Price, 64)
					if err != nil {continue}
					
					return priceFloat, nil
				}
			}
		}
	}
	
	return -1, errors.New("unable to calculate cost for virtual service set")
}

// dpRef is the deployment plan reference from SkyXRD spec
// skySrvc is the SkyService component for which we are finding required virtual services
func (r *SkyXRDReconciler) findReqVirtualSvcs(ns string, skySrvc hv1a1.SkyService, dpPolicy pv1a1.DeploymentPolicy) ([]pv1a1.VirtualServiceSelector, error) {
	// parse the deployment plan to find required virtual services for the component
	cmpntRef := skySrvc.ComponentRef
	provRef := skySrvc.ProviderRef
	for _, cmpnt := range dpPolicy.Spec.DeploymentPolicies {

		if cmpnt.ComponentRef.Name == cmpntRef.Name && 
				cmpnt.ComponentRef.Kind == cmpntRef.Kind && 
				cmpnt.ComponentRef.APIVersion == cmpntRef.APIVersion {
		
			// found matching component, extract virtual services
			// list of (alternative virtual services)
			reqVirtualSvcs := make([]pv1a1.VirtualServiceSelector, 0) 
			for _, vsSet := range cmpnt.VirtualServiceConstraint {
				cheapestVirtualSvcs, err := r.findCheapestVirtualSvcs(ns, provRef, vsSet.AnyOf)
				if err != nil {return nil, errors.Wrap(err, "finding cheapest virtual services")}
				reqVirtualSvcs = append(reqVirtualSvcs, *cheapestVirtualSvcs)

				// if this requested virtual service is a managed Kubernetes
				// we find all potential ComputeProfile alternatives
				// and sort them by cost, then use top N=5 cheapest ones
				// change N to allow more alternatives (Karpenter will pick)
				// Alternativelly, if user specifies specific ComputeProfile,
				// we only use that one.
				if cheapestVirtualSvcs.Name == "ManagedKubernetes" {
					virtSvcMap, err := r.fetchVirtualServiceCfgMap(ns, provRef)
					if err != nil { return nil, errors.Wrap(err, "failed fetching virtual service config map for compute profiles") }

					type computeProfileAlternative struct {
						Name  string
						EstimatedCost float64
					}

					// cmData["flavors.yaml"]
					var zoneOfferings []cv1a1.ZoneOfferings
					err1 := yaml.Unmarshal([]byte(virtSvcMap["flavors.yaml"]), &zoneOfferings); 
					computeProfiles := make([]computeProfileAlternative, 0)
					if err1 == nil {
						for _, zo := range zoneOfferings {
							if zo.Zone != provRef.Zone {continue}
							
							for _, of := range zo.Offerings {
								priceFloat, err := helper.ParseAmount(of.Price)
								if err != nil {continue}

								if strings.Contains(of.NameLabel, "-0GB") {continue}
								
								computeProfiles = append(computeProfiles, computeProfileAlternative{
									Name:  of.NameLabel,
									EstimatedCost: priceFloat,
								})
							}
						}
						// remove duplicates
						uniqueProfiles := lo.UniqBy(computeProfiles, func(cp computeProfileAlternative) string {
							return cp.Name
						})
						// sort compute profiles by estimated cost
						sort.Slice(uniqueProfiles, func(i, j int) bool {
							return uniqueProfiles[i].EstimatedCost < uniqueProfiles[j].EstimatedCost
						})
						// take top N=5 cheapest compute profiles
						N := 5
						if len(uniqueProfiles) < N {
							N = len(uniqueProfiles)
						}
						tmp := make([]pv1a1.VirtualServiceSelector, 0)
						for _, cp := range uniqueProfiles[:N] {
							tmp = append(tmp, pv1a1.VirtualServiceSelector{
								VirtualService: hv1a1.VirtualService{
									Name: cp.Name,
									Kind: "ComputeProfile",
								},
								Count: 1,
							})
						}

						reqVirtualSvcs = append(reqVirtualSvcs, tmp...)
					}
				}

			}
			// one component is matched, no need to continue
			return reqVirtualSvcs, nil
		}
	}
	return nil, errors.New("no matching component found in deployment policy")
}

// Load static metadata about providers from configmaps
// key: platform name, value: list of provider metadata
func (r *SkyXRDReconciler) loadProviderMetadata() (map[string][]providerMetadata, error) {
	providerData, err := r.fetchProviderMetadataMap()
	if err != nil {
		return nil, errors.Wrap(err, "fetching provider metadata map")
	}

	providerMetadataByKey := make(map[string][]providerMetadata)
	for key, yamlStr := range providerData {
		var metas []providerMetadata
		if err := yaml.Unmarshal([]byte(yamlStr), &metas); err != nil {
			return nil, errors.Wrapf(err, "unmarshalling provider metadata for key %s", key)
		}
		providerMetadataByKey[key] = metas
	}

	return providerMetadataByKey, nil
}

// Load static metadata about providers from configmaps and return as a map
// key: platform name, value: yaml string
func (r *SkyXRDReconciler) fetchProviderMetadataMap() (map[string]string, error) {
	cmList := &corev1.ConfigMapList{}
	if err := r.List(context.Background(), cmList, client.MatchingLabels{
		"skycluster.io/config-type": "provider-metadata-e2e",
	}); err != nil {
		return nil, errors.Wrap(err, "listing provider metadata ConfigMaps")
	}

	if len(cmList.Items) == 0 {
		return nil, errors.New("no provider metadata ConfigMaps found")
	}
	if len(cmList.Items) > 1 {
		return nil, errors.New("more than one provider metadata ConfigMap found")
	}

	providerData := make(map[string]string, len(cmList.Items[0].Data))
	for k, v := range cmList.Items[0].Data {
		providerData[k] = v
	}
	return providerData, nil
}

// Fetch all provider profiles and return as a map
func (r *SkyXRDReconciler) fetchProviderProfilesMap() (map[string]cv1a1.ProviderProfile, error) {
	ppList := &cv1a1.ProviderProfileList{}
	if err := r.List(context.Background(), ppList); err != nil {
		return nil, errors.Wrap(err, "listing provider profiles")
	}

	if len(ppList.Items) == 0 {
		return nil, errors.New("no provider profiles found")
	}

	providerProfilesMap := make(map[string]cv1a1.ProviderProfile)
	for _, pp := range ppList.Items {
		if _, ok := providerProfilesMap[pp.Spec.Platform]; !ok {
			providerProfilesMap[pp.Name] = pp
		}
	}
	return providerProfilesMap, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SkyXRDReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cv1a1.SkyXRD{}).
		Named("core-skyxrd").
		WithOptions(controller.Options{
			RateLimiter: newCustomRateLimiter(),
		}).
		Complete(r)
}

func findSecondaryZone(p cv1a1.ProviderProfile, primary string) string {
	for _, z := range p.Spec.Zones {
		if z.Name != primary { return z.Name }
	}
	return ""
}

