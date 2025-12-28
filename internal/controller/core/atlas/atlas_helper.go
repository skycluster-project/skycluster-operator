package core

import (
	"context"
	"encoding/json"
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
	ilp "github.com/skycluster-project/skycluster-operator/internal/controller/core/ilptask"
)

func findSecondaryZone(p cv1a1.ProviderProfile, primary string) string {
	for _, z := range p.Spec.Zones {
		if z.Name != primary {
			return z.Name
		}
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
	if err != nil {
		return nil, err
	}
	return dp, nil
}

// findCheapestVirtualSvcs finds the cheapest virtual service from a list of alternatives
// dpPolicy := r.fetchDeploymentPolicy(ns, dpRef)
// findReqVirtualSvcs(ns, skySrv, dpPolicy)
func (r *AtlasReconciler) findCheapestVirtualSvcs(ns string, providerRef hv1a1.ProviderRefSpec, virtualServices []pv1a1.VirtualServiceSelector) (*pv1a1.VirtualServiceSelector, error) {
	if len(virtualServices) == 0 {
		return nil, nil
	} // no virtual services required

	virtSvcMap, err := r.fetchVirtualServiceCfgMap(ns, providerRef)
	if err != nil {
		return nil, errors.Wrap(err, "failed fetching virtual service config map")
	}

	// Find the set with the minimum cost
	var cheapestVirtSvc *pv1a1.VirtualServiceSelector
	minCost := math.MaxFloat64
	for _, vs := range virtualServices {
		cost, err := r.calculateVirtualServiceSetCost(virtSvcMap, vs, providerRef.Zone)
		if err != nil {
			return nil, err
		}
		if cost < 0 {
			continue
		} // unable to calculate cost, skip, maybe not offered?

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
		"skycluster.io/config-type":       "provider-profile",
		"skycluster.io/managed-by":        "skycluster",
		"skycluster.io/provider-platform": providerRef.Platform,
		"skycluster.io/provider-profile":  providerRef.Name,
		"skycluster.io/provider-region":   providerRef.Region,
	}); err != nil {
		return nil, errors.Wrap(err, "listing provider profile ConfigMaps")
	}
	if len(cm.Items) == 0 {
		return nil, errors.New("no provider profile ConfigMaps found")
	}
	if len(cm.Items) > 1 {
		return nil, errors.New("multiple provider profile ConfigMaps found")
	}

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

	r.Logger.Info("Calculating cost for virtual service", "kind", vs.Kind, "name", vs.Name, "count", vs.Count, "zone", zone)
	switch vs.Kind {

	// case "XInstance", "XNodeGroup":
	case "ComputeProfile":
		// TODO: ideally this should be adopted for default case below
		pat, err := ilp.ParseFlavorFromJSON(vs.Spec)
		if err != nil {
			return -1, errors.Wrap(err, "parsing flavor from JSON")
		}

		r.Logger.Info("pat spec", "cpu", pat.Cpu, "memoryGB", pat.GpuModel)

		var zoneOfferings []cv1a1.ZoneOfferings
		if err := yaml.Unmarshal([]byte(virtualSrvcMap["flavors.yaml"]), &zoneOfferings); err == nil {
			for _, zo := range zoneOfferings {
				if zo.Zone != zone {
					continue
				}
				for _, of := range zo.Offerings {
					if ok, _ := ilp.OfferingMatches2(pat, of); !ok {
						continue
					}
					priceFloat, err := strconv.ParseFloat(of.Price, 64)
					if err != nil {
						continue
					}

					return priceFloat, nil
				}
			}
		}

		// try the baremetal flavors
		var bmOfferings map[string]cv1a1.DeviceZoneSpec
		if err := yaml.Unmarshal([]byte(virtualSrvcMap["worker"]), &bmOfferings); err == nil {
			for _, deviceSpec := range bmOfferings {
				if ok, err := ilp.OfferingMatches2(pat, *deviceSpec.Configs); !ok {
					r.Logger.Info("baremetal offering does not match", "offering", deviceSpec.Configs.NameLabel, "reason", err)
					continue
				}
				priceFloat, err := strconv.ParseFloat(deviceSpec.Configs.Price, 64)
				if err != nil {
					continue
				}

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

func fixFlavorVCPUsFormat(flavor []byte) ([]byte, error) {
	// Step 1: unmarshal into a generic map
	var tmp map[string]interface{}
	json.Unmarshal(flavor, &tmp)

	// Step 2: convert vcpus string to int if necessary
	if v, ok := tmp["vcpus"].(string); ok {
		if n, err := strconv.Atoi(v); err == nil {
			tmp["vcpus"] = n
		}
	}

	// Step 3: marshal back to JSON
	fixedData, _ := json.Marshal(tmp)
	return fixedData, nil
}
