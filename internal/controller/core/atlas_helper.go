package core

import (
	"context"
	"math"
	"sort"
	"strconv"

	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	hv1a1 "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"
	pv1a1 "github.com/skycluster-project/skycluster-operator/api/policy/v1alpha1"
)

func findSecondaryZone(p cv1a1.ProviderProfile, primary string) string {
	for _, z := range p.Spec.Zones {
		if z.Name != primary { return z.Name }
	}
	return ""
}

func buildIndexMap(pvs map[string]hv1a1.ProviderRefSpec) map[string]map[string]int {
	typedProviders := make(map[string][]string)
	for pname, p := range pvs {
		if typedProviders[p.Platform] == nil {
			typedProviders[p.Platform] = make([]string, 0)
		}
		typedProviders[p.Platform] = append(typedProviders[p.Platform], pname)
	}
	for pltf, pnames := range typedProviders {
		sort.Strings(pnames)
		typedProviders[pltf] = pnames
	}
	provToIdx := make(map[string]map[string]int)
	for pltf, pnames := range typedProviders {
		m := make(map[string]int)
		for id, pname := range pnames {
			m[pname] = id
		}
		provToIdx[pltf] = m
	}
	return provToIdx
}

// Load static metadata about providers from configmaps and return as a map
// key: platform name, value: yaml string
func (r *AtlasReconciler) fetchProviderMetadataMap() (map[string]string, error) {
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
func (r *AtlasReconciler) fetchProviderProfilesMap() (map[string]cv1a1.ProviderProfile, error) {
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

// Load static metadata about providers from configmaps
// key: platform name, value: list of provider metadata
func (r *AtlasReconciler) loadProviderMetadata() (map[string][]providerMetadata, error) {
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

// dpRef is the deployment plan reference from Atlas spec
func (r *AtlasReconciler) fetchDeploymentPolicy(ns string, dpRef cv1a1.DeploymentPolicyRef) (*pv1a1.DeploymentPolicy, error) {
	dp := &pv1a1.DeploymentPolicy{}
	err := r.Get(context.TODO(), client.ObjectKey{Namespace: ns, Name: dpRef.Name}, dp)
	if err != nil {return nil, err}
	return dp, nil
}

// dpPolicy := r.fetchDeploymentPolicy(ns, dpRef)
// findReqVirtualSvcs(ns, skySrv, dpPolicy)
func (r *AtlasReconciler) findCheapestVirtualSvcs(ns string, providerRef hv1a1.ProviderRefSpec, virtualServices []pv1a1.VirtualServiceSelector) (*pv1a1.VirtualServiceSelector, error) {
	if len(virtualServices) == 0 {return nil, nil} // no virtual services required

	virtSvcMap, err := r.fetchVirtualServiceCfgMap(ns, providerRef)
	if err != nil { return nil, errors.Wrap(err, "failed fetching virtual service config map") }

	// Find the set with the minimum cost
	var cheapestVirtSvc *pv1a1.VirtualServiceSelector
	minCost := math.MaxFloat64
	for _, vs := range virtualServices {
		cost, err := r.calculateVirtualServiceSetCost(virtSvcMap, vs, providerRef.Zone)
		if err != nil {return nil, err}
		if cost < 0 {continue} // unable to calculate cost, skip, maybe not offered?

		if cost < minCost {
			minCost = cost
			cheapestVirtSvc = &vs
		}	
	}
	if cheapestVirtSvc == nil {
		return nil, errors.New("no valid virtual service found" + " for provider " + providerRef.Name)
	}

	return cheapestVirtSvc, nil
}

func (r *AtlasReconciler) fetchVirtualServiceCfgMap(ns string, providerRef hv1a1.ProviderRefSpec) (map[string]string, error) {
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
func (r *AtlasReconciler) calculateVirtualServiceSetCost(virtualSrvcMap map[string]string, vs pv1a1.VirtualServiceSelector, zone string) (float64, error) {
	
	// limited number of virtual services are supported: ManagedKubernetes, ComputeProfile
	// TODO: Use Kind instead
	// switch vs.Name {
	// case "ManagedKubernetes":
	// 	// cmData["managed-k8s.yaml"]
	// 	mngK8sList := []hv1a1.ManagedK8s{}
	// 	if err := yaml.Unmarshal([]byte(virtualSrvcMap["managed-k8s.yaml"]), &mngK8sList); err != nil {
	// 		return -1, errors.Wrap(err, "unmarshalling managed-k8s.yaml")
	// 	}
	// 	for _, mngK8s := range mngK8sList {
	// 		if mngK8s.NameLabel == vs.Name {
	// 			// found matching managed K8s, calculate cost
	// 			overheadCost, err := strconv.ParseFloat(mngK8s.Overhead.Cost, 64)
	// 			if err != nil {return -1, errors.Wrap(err, "parsing overhead cost")}

	// 			price, err := strconv.ParseFloat(mngK8s.Price, 64)
	// 			if err != nil {return -1, errors.Wrap(err, "parsing price")}

	// 			totalCost := price + (overheadCost * float64(vs.Count))
	// 			return totalCost, nil
	// 		}
	// 	}

	// default:
	// 	r.Logger.Info("Unsupported virtual service by Name", "virtualService", vs.Name)
	// }

	switch vs.Kind {
	
	case "XInstance", "XNodeGroup":
		// duplicate of default case, kept to use strcutured ComputeProfile definition
		// TODO: ideally this should be adopted for default case below
		pat, err := parseFlavorFromJSON(vs.Spec)
		if err != nil {return -1, errors.Wrap(err, "parsing flavor from JSON")}

		var zoneOfferings []cv1a1.ZoneOfferings
		if err := yaml.Unmarshal([]byte(virtualSrvcMap["flavors.yaml"]), &zoneOfferings); err == nil {
			for _, zo := range zoneOfferings {
				if zo.Zone != zone {continue}
				for _, of := range zo.Offerings {
					if !offeringMatches2(pat, of) {continue}
					priceFloat, err := strconv.ParseFloat(of.Price, 64)
					if err != nil {continue}
					
					return priceFloat, nil
				}
			}
		} 

		// try the baremetal flavors
		var bmOfferings map[string]cv1a1.DeviceZoneSpec
		if err := yaml.Unmarshal([]byte(virtualSrvcMap["worker"]), &bmOfferings); err == nil {
			for _, deviceSpec := range bmOfferings {
				if !offeringMatches2(pat, *deviceSpec.Configs) {continue}
				priceFloat, err := strconv.ParseFloat(deviceSpec.Configs.Price, 64)
				if err != nil {continue}
				
				return priceFloat, nil
			}
		}
	
	// Try to match by Name as wildcard with flavors.yaml
	default:
		err := errors.New("calculating cost of virtual service by unknown Kind/Name. Not expected.")
		r.Logger.Error(err, "Unsupported virtual service by Kind/Name", "kind", vs.Kind, "name", vs.Name)
	}

	return -1, errors.New("no matching virtual service offering found for " + vs.Name)
}

// dpPolicy is the deployment plan reference from Atlas spec
// skySrvc is the SkyService component for which we are finding required virtual services
func (r *AtlasReconciler) findReqComputeProfile(ns string, skySrvc hv1a1.SkyService, dpPolicy pv1a1.DeploymentPolicy) ([]pv1a1.VirtualServiceSelector, error) {
	// parse the deployment plan to find required virtual services for the component
	cmpntRef := skySrvc.ComponentRef
	provRef := skySrvc.ProviderRef
	for _, cmpnt := range dpPolicy.Spec.DeploymentPolicies {

		// XInstance, XNodeGroup, Deployment
		if cmpnt.ComponentRef.Name != cmpntRef.Name ||
				cmpnt.ComponentRef.Kind != cmpntRef.Kind || 
				cmpnt.ComponentRef.APIVersion != cmpntRef.APIVersion {
					r.Logger.Info("Component reference does not match, continuing to next",
						"expected", cmpntRef,
						"found", cmpnt.ComponentRef,
					)
					continue
		}
	
		// Currently, only "ComputeProfile" virtual services are expected and supported.
		// First, extract virtual services and list of (alternative virtual services)
		// and find the cheapest one per set.
		reqVirtualSvcs := make([]pv1a1.VirtualServiceSelector, 0) 
		for _, vsSet := range cmpnt.VirtualServiceConstraint { 
			// specified alternative virtual services, defined by user
			// must include the cheapest one per set
			cheapestVirtualSvcs, err := r.findCheapestVirtualSvcs(ns, provRef, vsSet.AnyOf)
			if err != nil {return nil, errors.Wrap(err, "finding cheapest virtual services")}
			if cheapestVirtualSvcs == nil {
				return nil, errors.New("no available virtual service found for component " + cmpntRef.Name )
			}
			reqVirtualSvcs = append(reqVirtualSvcs, *cheapestVirtualSvcs)
		}
		// one component is matched, no need to continue
		// I don't expect multiple matching components (same name/kind/apiVersion)
		return reqVirtualSvcs, nil
	}
	return nil, errors.New("no matching component found in deployment policy")
}


// func test(execEnv string) ([]pv1a1.VirtualServiceSelector, error) {

// 	reqVirtualSvcs := make([]pv1a1.VirtualServiceSelector, 0)
// 	// if this requested virtual service is a managed Kubernetes
// 	// we find all potential ComputeProfile alternatives
// 	// and sort them by cost, then use top N=5 cheapest ones
// 	// change N to allow more alternatives (Karpenter will pick)
// 	// Alternativelly, if user specifies specific ComputeProfile,
// 	// we only use that one.
// 	if execEnv == "Kubernetes" {
// 		virtSvcMap, err := r.fetchVirtualServiceCfgMap(ns, provRef)
// 		if err != nil { return nil, errors.Wrap(err, "failed fetching virtual service config map for compute profiles") }

// 		type computeProfileAlternative struct {
// 			Name  string
// 			EstimatedCost float64
// 		}

// 		// cmData["flavors.yaml"]
// 		var zoneOfferings []cv1a1.ZoneOfferings
// 		err1 := yaml.Unmarshal([]byte(virtSvcMap["flavors.yaml"]), &zoneOfferings); 
// 		computeProfiles := make([]computeProfileAlternative, 0)
// 		if err1 == nil {
// 			for _, zo := range zoneOfferings {
// 				if zo.Zone != provRef.Zone {continue}
				
// 				for _, of := range zo.Offerings {
// 					priceFloat, err := helper.ParseAmount(of.Price)
// 					if err != nil {continue}

// 					if strings.Contains(of.NameLabel, "-0GB") {continue}
					
// 					computeProfiles = append(computeProfiles, computeProfileAlternative{
// 						Name:  of.NameLabel,
// 						EstimatedCost: priceFloat,
// 					})
// 				}
// 			}
// 			// remove duplicates
// 			uniqueProfiles := lo.UniqBy(computeProfiles, func(cp computeProfileAlternative) string {
// 				return cp.Name
// 			})
// 			// sort compute profiles by estimated cost
// 			sort.Slice(uniqueProfiles, func(i, j int) bool {
// 				return uniqueProfiles[i].EstimatedCost < uniqueProfiles[j].EstimatedCost
// 			})
// 			// take top N=5 cheapest compute profiles
// 			N := 5
// 			if len(uniqueProfiles) < N {
// 				N = len(uniqueProfiles)
// 			}
// 			tmp := make([]pv1a1.VirtualServiceSelector, 0)
// 			for _, cp := range uniqueProfiles[:N] {
// 				tmp = append(tmp, pv1a1.VirtualServiceSelector{
// 					VirtualService: hv1a1.VirtualService{
// 						Name: cp.Name,
// 						Kind: "ComputeProfile",
// 					},
// 					Count: 1,
// 				})
// 			}

// 			reqVirtualSvcs = append(reqVirtualSvcs, tmp...)
// 		}
// 	}
// 	return reqVirtualSvcs, nil
// }