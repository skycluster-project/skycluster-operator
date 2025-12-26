package core

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	// "time"

	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/pkg/errors"
	"github.com/samber/lo"
	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	hv1a1 "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"
	pv1a1 "github.com/skycluster-project/skycluster-operator/api/policy/v1alpha1"
)

type computeProfileService struct {
	name            string
	cpu             float64
	ram             float64
	usedCPU         float64
	usedRAM         float64
	gpuEnabled      bool
	gpuModel        string
	gpuCount        string
	gpuMem          string
	gpuManufacturer string
	deployCost      float64
}

type FlavorPattern struct {
	Cpu       float64
	CpuAny    bool
	Ram       float64
	RamAny    bool
	GpuCount  int    // 0 = not specified
	GpuModel  string // empty = not specified
	GpuMem    float64
	GpuMemAny bool
}

type virtualSvcStruct struct {
	Name       string                `json:"name,omitempty"`
	Spec       *runtime.RawExtension `json:"spec,omitempty"`
	ApiVersion string                `json:"apiVersion,omitempty"`
	Kind       string                `json:"kind,omitempty"`
	Count      string                `json:"count,omitempty"`
	Price      float64               `json:"price,omitempty"`
}

type locStruct struct {
	Name        string `json:"name,omitempty"`
	PType       string `json:"pType,omitempty"`
	RegionAlias string `json:"regionAlias,omitempty"`
	Region      string `json:"region,omitempty"`
	Platform    string `json:"platform,omitempty"`
}

type optTaskStruct struct {
	Task               string               `json:"task"`
	ApiVersion         string               `json:"apiVersion"`
	Kind               string               `json:"kind"`
	PermittedLocations []locStruct          `json:"permittedLocations"`
	RequiredLocations  [][]locStruct        `json:"requiredLocations"`
	RequestedVServices [][]virtualSvcStruct `json:"requestedVServices"`
	MaxReplicas        string               `json:"maxReplicas"`
}

func (r *ILPTaskReconciler) findK8SVirtualServiceForProvider(
	ns string,
	dpPolicyItem pv1a1.DeploymentPolicyItem,
	providerProfille hv1a1.ProviderRefSpec) ([][]virtualSvcStruct, error) {

	supportedKinds := []string{"Deployment", "XNodeGroup"}
	if !slices.Contains(supportedKinds, dpPolicyItem.ComponentRef.Kind) {
		return nil, fmt.Errorf("unsupported component kind %s for Kubernetes execution environment", dpPolicyItem.ComponentRef.Kind)
	}

	providerSpec := hv1a1.ProviderRefSpec{
		Name:        providerProfille.Name,
		Platform:    providerProfille.Platform,
		RegionAlias: providerProfille.RegionAlias,
		Region:      providerProfille.Region,
		Zone:        providerProfille.Zone,
	}

	vsList := make([][]virtualSvcStruct, 0)
	for _, vsc := range dpPolicyItem.VirtualServiceConstraint {

		if len(vsc.AnyOf) == 0 {
			return nil, fmt.Errorf("no virtual service alternatives found for component %s, %v", dpPolicyItem.ComponentRef.Name, vsc)
		}

		altVSList := make([]virtualSvcStruct, 0)
		for _, alternativeVS := range vsc.AnyOf {

			supportedVSKinds := []string{"ComputeProfile"}
			if !slices.Contains(supportedVSKinds, alternativeVS.Kind) {
				return nil, fmt.Errorf("unsupported virtual service kind %s for Kubernetes execution environment", alternativeVS.Kind)
			}

			if alternativeVS.Kind == "ComputeProfile" {
				if alternativeVS.Spec == nil {
					continue
				}

				pat, err := ParseFlavorFromJSON(alternativeVS.Spec)
				if err != nil {
					return nil, err
				}

				profiles, err := r.getAllComputeProfiles([]hv1a1.ProviderRefSpec{providerSpec})
				if err != nil {
					return nil, err
				}

				for _, off := range profiles {
					if off.deployCost == 0 || !offeringMatches(pat, off) {
						continue
					}
					// Add this offering as a ComputeProfile alternative
					newVS := virtualSvcStruct{
						ApiVersion: "skycluster.io/v1alpha1",
						Kind:       "ComputeProfile",
						Name:       off.name, // name is mapped to nameLabel
						Price:      off.deployCost,
						Count:      strconv.Itoa(alternativeVS.Count),
					}
					altVSList = append(altVSList, newVS)
				}
			}

			switch dpPolicyItem.ComponentRef.Kind {
			// Validate against deployment requirements
			case "Deployment":
				deployable, err := r.findDeployableComputeProfiles(ns, dpPolicyItem.ComponentRef.Name, dpPolicyItem.LocationConstraint.Permitted)
				if err != nil {
					return nil, err
				}
				// filter the altVSList to only include the ComputeProfiles that are suitable for the deployment
				altVSListFiltered := make([]virtualSvcStruct, 0)
				for _, alt := range altVSList {
					if _, ok := deployable[alt.Name]; ok {
						altVSListFiltered = append(altVSListFiltered, alt)
					}
				}

				if len(altVSList) > 0 && len(altVSListFiltered) == 0 {
					return nil, fmt.Errorf("no suitable ComputeProfile alternatives found for component %s", dpPolicyItem.ComponentRef.Name)
				}
				// if there are suitable ComputeProfile alternatives found for deployment,
				// use them (it satisfies the user specified ComputeProfile alternatives as well)
				if len(altVSListFiltered) > 0 {
					altVSList = altVSListFiltered
				}

			case "XNodeGroup", "XKube":
				// If the component is an XNodeGroup or XKube, it represents a set of Compute profile
				// that must be used for node groups for the execution cluster itself.
				// if it is tied to a specific provider, it targets the provider's Kubernetes cluster
				// If no provider is specified, the optimizer will choose the best provider minimizing
				// the node group costs plus other deployment costs (e.g., control plane).

				// Since the ComputeProfile is already handled above, we do not need to do anything here.
				r.Logger.Info("XNodeGroup/XKube component detected, handled via ComputeProfile alternatives", "component", dpPolicyItem.ComponentRef.Name)
				// selectedVSList = append(selectedVSList, altVSList...)

			default:
				return nil, fmt.Errorf("unsupported virtual service kind %s for Kubernetes execution environment", alternativeVS.Kind)
			}

			// remove duplicates; for an alternative VS it does not make sense to have duplicates
			altVSList = lo.UniqBy(altVSList, func(v virtualSvcStruct) string {
				return v.Name
			})
			// the list can be huge, so sort and limit to top 10 cheapest
			sort.Slice(altVSList, func(i, j int) bool {
				return altVSList[i].Price < altVSList[j].Price
			})
			if len(altVSList) > 10 {
				altVSList = altVSList[:10]
			}

			vsList = append(vsList, altVSList)
		} // end of for each alternative virtual service
	} // end of for each virtual service constraint in the deployment policy item

	if len(vsList) == 0 {
		// if no virtual services found, there may be no defined virtual service constraints
		// in that case, we can try to find the cheapest ComputeProfile for the component
		deployable, err := r.findDeployableComputeProfiles(ns, dpPolicyItem.ComponentRef.Name, dpPolicyItem.LocationConstraint.Permitted)
		if err != nil {
			return nil, err
		}
		deployableList := lo.Values(deployable)
		deployableList = lo.UniqBy(deployableList, func(v computeProfileService) string {
			return v.name
		})
		sort.Slice(deployableList, func(i, j int) bool {
			return deployableList[i].deployCost < deployableList[j].deployCost
		})
		if len(deployableList) > 0 {
			vsList = append(vsList, []virtualSvcStruct{{
				ApiVersion: "skycluster.io/v1alpha1",
				Kind:       "ComputeProfile",
				Name:       deployableList[0].name,
				Price:      deployableList[0].deployCost,
				Count:      "1",
			}})
		}
		if len(vsList) == 0 {
			// at this state, we have no virtual services found.
			return nil, fmt.Errorf("no suitable ComputeProfile alternatives found for component %s", dpPolicyItem.ComponentRef.Name)
		}
	}
	return vsList, nil
}

// findK8SVirtualServices processes the deployment policies and extracts the virtual services
// when execution environment is Kubernetes.
func (r *ILPTaskReconciler) findK8SVirtualServices(ns string, dpPolicies []pv1a1.DeploymentPolicyItem) ([]optTaskStruct, error) {

	var optTasks []optTaskStruct
	for _, cmpnt := range dpPolicies {
		// TODO: Check the supported Kinds for e2e application support
		supportedKinds := []string{"Deployment", "XNodeGroup"}
		if !slices.Contains(supportedKinds, cmpnt.ComponentRef.Kind) {
			return nil, fmt.Errorf("unsupported component kind %s for Kubernetes execution environment", cmpnt.ComponentRef.Kind)
		}

		vsList := make([][]virtualSvcStruct, 0)
		for _, vsc := range cmpnt.VirtualServiceConstraint {

			if len(vsc.AnyOf) == 0 {
				return nil, fmt.Errorf("no virtual service alternatives found for component %s, %v", cmpnt.ComponentRef.Name, vsc)
			}

			altVSList := make([]virtualSvcStruct, 0)
			// selectedVSList := make([]virtualSvcStruct, 0)
			for _, alternativeVS := range vsc.AnyOf {

				// add any requested virtual service as is
				// e.g., ComputeProfile kind with spec
				// we filter all compute profiles and use them as alternatives
				// meaning cheapest suitable will be chosen by optimizer
				supportedVSKinds := []string{"ComputeProfile"}
				if !slices.Contains(supportedVSKinds, alternativeVS.Kind) {
					return nil, fmt.Errorf("unsupported virtual service kind %s for Kubernetes execution environment", alternativeVS.Kind)
				}

				if alternativeVS.Kind == "ComputeProfile" {
					if alternativeVS.Spec == nil {
						continue
					}

					pat, err := ParseFlavorFromJSON(alternativeVS.Spec)
					if err != nil {
						return nil, err
					}

					profiles, err := r.getAllComputeProfiles(cmpnt.LocationConstraint.Permitted)
					if err != nil {
						return nil, err
					}

					for _, off := range profiles {
						if off.deployCost == 0 || !offeringMatches(pat, off) {
							continue
						}
						// Add this offering as a ComputeProfile alternative
						newVS := virtualSvcStruct{
							ApiVersion: "skycluster.io/v1alpha1",
							Kind:       "ComputeProfile",
							Name:       off.name, // name is mapped to nameLabel
							Price:      off.deployCost,
							Count:      strconv.Itoa(alternativeVS.Count),
						}
						altVSList = append(altVSList, newVS)
					}
				}

				switch cmpnt.ComponentRef.Kind {
				// Validate against deployment requirements
				case "Deployment":
					deployable, err := r.findDeployableComputeProfiles(ns, cmpnt.ComponentRef.Name, cmpnt.LocationConstraint.Permitted)
					if err != nil {
						return nil, err
					}
					// filter the altVSList to only include the ComputeProfiles that are suitable for the deployment
					altVSListFiltered := make([]virtualSvcStruct, 0)
					for _, alt := range altVSList {
						if _, ok := deployable[alt.Name]; ok {
							altVSListFiltered = append(altVSListFiltered, alt)
						}
					}

					if len(altVSList) > 0 && len(altVSListFiltered) == 0 {
						return nil, fmt.Errorf("no suitable ComputeProfile alternatives found for component %s", cmpnt.ComponentRef.Name)
					}
					// if there are suitable ComputeProfile alternatives found for deployment,
					// use them (it satisfies the user specified ComputeProfile alternatives as well)
					if len(altVSListFiltered) > 0 {
						altVSList = altVSListFiltered
					}

				case "XNodeGroup", "XKube":
					// If the component is an XNodeGroup or XKube, it represents a set of Compute profile
					// that must be used for node groups for the execution cluster itself.
					// if it is tied to a specific provider, it targets the provider's Kubernetes cluster
					// If no provider is specified, the optimizer will choose the best provider minimizing
					// the node group costs plus other deployment costs (e.g., control plane).

					// Since the ComputeProfile is already handled above, we do not need to do anything here.
					r.Logger.Info("XNodeGroup/XKube component detected, handled via ComputeProfile alternatives", "component", cmpnt.ComponentRef.Name)
					// selectedVSList = append(selectedVSList, altVSList...)

				default:
					return nil, fmt.Errorf("unsupported virtual service kind %s for Kubernetes execution environment", alternativeVS.Kind)
				}

				// remove duplicates; for an alternative VS it does not make sense to have duplicates
				altVSList = lo.UniqBy(altVSList, func(v virtualSvcStruct) string {
					return v.Name
				})
				// the list can be huge, so sort and limit to top 10 cheapest
				sort.Slice(altVSList, func(i, j int) bool {
					return altVSList[i].Price < altVSList[j].Price
				})
				if len(altVSList) > 10 {
					altVSList = altVSList[:10]
				}
			} // end of for each alternative virtual service

			vsList = append(vsList, altVSList)
		} // end of for each virtual service constraint in the deployment policy item

		if len(vsList) == 0 {
			// if no virtual services found, there may be no defined virtual service constraints
			// in that case, we can try to find the cheapest ComputeProfile for the component
			deployable, err := r.findDeployableComputeProfiles(ns, cmpnt.ComponentRef.Name, cmpnt.LocationConstraint.Permitted)
			if err != nil {
				return nil, err
			}
			deployableList := lo.Values(deployable)
			deployableList = lo.UniqBy(deployableList, func(v computeProfileService) string {
				return v.name
			})
			sort.Slice(deployableList, func(i, j int) bool {
				return deployableList[i].deployCost < deployableList[j].deployCost
			})
			if len(deployableList) > 0 {
				vsList = append(vsList, []virtualSvcStruct{{
					ApiVersion: "skycluster.io/v1alpha1",
					Kind:       "ComputeProfile",
					Name:       deployableList[0].name,
					Price:      deployableList[0].deployCost,
				}})
			}
			if len(vsList) == 0 {
				// at this state, we have no virtual services found.
				return nil, fmt.Errorf("no suitable ComputeProfile alternatives found for component %s", cmpnt.ComponentRef.Name)
			}
		}

		perLocList := make([]locStruct, 0)
		for _, perLoc := range cmpnt.LocationConstraint.Permitted {
			newLoc := locStruct{
				Name:        perLoc.Name,
				PType:       perLoc.Type,
				RegionAlias: perLoc.RegionAlias,
				Region:      perLoc.Region,
				Platform:    perLoc.Platform,
			}
			perLocList = append(perLocList, newLoc)
		}

		reqLocList := make([][]locStruct, 0)
		for _, reqLoc := range cmpnt.LocationConstraint.Required {
			altReqLocList := make([]locStruct, 0)
			for _, altReqLoc := range reqLoc.AnyOf {
				newLoc := locStruct{
					Name:        altReqLoc.Name,
					PType:       altReqLoc.Type,
					RegionAlias: altReqLoc.RegionAlias,
					Region:      altReqLoc.Region,
					Platform:    altReqLoc.Platform,
				}
				altReqLocList = append(altReqLocList, newLoc)
			}
			reqLocList = append(reqLocList, altReqLocList)
		}

		optTasks = append(optTasks, optTaskStruct{
			Task:               cmpnt.ComponentRef.Name,
			ApiVersion:         cmpnt.ComponentRef.APIVersion,
			Kind:               cmpnt.ComponentRef.Kind, // Deployment | XNodeGroup
			PermittedLocations: perLocList,
			RequiredLocations:  reqLocList,
			RequestedVServices: vsList,
			MaxReplicas: func() string {
				// TODO: receive this from deployment policy
				// if cmpnt.ComponentRef.Name == "central-storage" {return "1"}
				return "-1"
			}(),
		})
	}
	return optTasks, nil
}

func (r *ILPTaskReconciler) findDeployableComputeProfiles(
	ns, componentName string,
	permittedLocations []hv1a1.ProviderRefSpec) (map[string]computeProfileService, error) {

	// calculate the minimum compute resource required for this component (i.e. deployment)
	minCR, err := r.calculateMinComputeResource(ns, componentName)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate min compute resource for component %s: %w", componentName, err)
	}
	if minCR == nil {
		return nil, fmt.Errorf("nil min compute resource for component %s", componentName)
	}

	// Try to fetch flavors for the provider identified by alternativeVS.Name (fallback to component name)
	profiles, err := r.getAllComputeProfiles(permittedLocations)
	if err != nil {
		return nil, err
	}

	// Build deployment-compatible set
	deployable := make(map[string]computeProfileService)
	for _, off := range profiles {
		if off.cpu >= minCR.cpu && off.ram >= minCR.ram {
			deployable[off.name] = off
		}
	}
	return deployable, nil
}

// findVMVirtualServices processes the deployment policies and extracts the virtual services
// when execution environment is Virtual Machine.
func (r *ILPTaskReconciler) findVMVirtualServices(dpPolicies []pv1a1.DeploymentPolicyItem) ([]optTaskStruct, error) {
	var optTasks []optTaskStruct
	for _, cmpnt := range dpPolicies {
		supportedKinds := []string{"XInstance"}
		if !slices.Contains(supportedKinds, cmpnt.ComponentRef.Kind) {
			return nil, fmt.Errorf("unsupported component kind %s for Virtual Machine execution environment", cmpnt.ComponentRef.Kind)
		}

		vsList := make([][]virtualSvcStruct, 0)
		for _, vsc := range cmpnt.VirtualServiceConstraint {

			if len(vsc.AnyOf) == 0 {
				return nil, fmt.Errorf("no virtual service alternatives found for component %s, %v", cmpnt.ComponentRef.Name, vsc)
			}

			additionalVSList := make([]virtualSvcStruct, 0)
			for _, alternativeVS := range vsc.AnyOf {
				switch alternativeVS.Kind {
				case "ComputeProfile":
					// It is simpler since we only need to append requested ComputeProfiles
					// fetch ComputeProfile spec
					vmSpec := alternativeVS.Spec
					pat, err := ParseFlavorFromJSON(vmSpec)
					if err != nil {
						return nil, err
					}

					profiles, err := r.getAllComputeProfiles(cmpnt.LocationConstraint.Permitted)
					if err != nil {
						return nil, err
					}

					for _, off := range profiles {
						if !offeringMatches(pat, off) {
							continue
						}

						// Add this offering as a ComputeProfile alternative
						additionalVSList = append(additionalVSList, virtualSvcStruct{
							ApiVersion: "skycluster.io/v1alpha1",
							Kind:       "ComputeProfile",
							Name:       off.name, // name is mapped to nameLabel
							Count:      "1",
							Price:      off.deployCost,
						})
					}

				default:
					return nil, fmt.Errorf("unsupported virtual service kind %s for Virtual Machine execution environment", alternativeVS.Kind)
				}

				// remove duplicates; for an alternative VS it does not make sense to have duplicates
				additionalVSList = lo.UniqBy(additionalVSList, func(v virtualSvcStruct) string {
					return v.Name
				})
				// the list can be huge, so sort and limit to top 10 cheapest
				sort.Slice(additionalVSList, func(i, j int) bool {
					return additionalVSList[i].Price < additionalVSList[j].Price
				})
				if len(additionalVSList) > 10 {
					additionalVSList = additionalVSList[:10]
				}
			}

			if len(additionalVSList) > 0 {
				vsList = append(vsList, additionalVSList)
			}
		}

		perLocList := make([]locStruct, 0)
		for _, perLoc := range cmpnt.LocationConstraint.Permitted {
			newLoc := locStruct{
				Name:        perLoc.Name,
				PType:       perLoc.Type,
				RegionAlias: perLoc.RegionAlias,
				Region:      perLoc.Region,
				Platform:    perLoc.Platform,
			}
			perLocList = append(perLocList, newLoc)
		}

		reqLocList := make([][]locStruct, 0)
		for _, reqLoc := range cmpnt.LocationConstraint.Required {
			altReqLocList := make([]locStruct, 0)
			for _, altReqLoc := range reqLoc.AnyOf {
				newLoc := locStruct{
					Name:        altReqLoc.Name,
					PType:       altReqLoc.Type,
					RegionAlias: altReqLoc.RegionAlias,
					Region:      altReqLoc.Region,
					Platform:    altReqLoc.Platform,
				}
				altReqLocList = append(altReqLocList, newLoc)
			}
			reqLocList = append(reqLocList, altReqLocList)
		}

		optTasks = append(optTasks, optTaskStruct{
			Task:               cmpnt.ComponentRef.Name,
			ApiVersion:         cmpnt.ComponentRef.APIVersion,
			Kind:               cmpnt.ComponentRef.Kind,
			PermittedLocations: perLocList,
			RequiredLocations:  reqLocList,
			RequestedVServices: vsList,
			MaxReplicas: func() string {
				// if cmpnt.ComponentRef.Name == "dashboard" {return "1"}
				return "-1"
			}(),
		})
	}
	return optTasks, nil
}

func (r *ILPTaskReconciler) findVMVirtualServicesForProvider(
	ns string,
	dpPolicyItem pv1a1.DeploymentPolicyItem,
	providerProfille hv1a1.ProviderRefSpec) ([][]virtualSvcStruct, error) {

	providerSpec := hv1a1.ProviderRefSpec{
		Name:        providerProfille.Name,
		Platform:    providerProfille.Platform,
		RegionAlias: providerProfille.RegionAlias,
		Region:      providerProfille.Region,
		Zone:        providerProfille.Zone,
	}

	vsList := make([][]virtualSvcStruct, 0)
	for _, vsc := range dpPolicyItem.VirtualServiceConstraint {

		if len(vsc.AnyOf) == 0 {
			return nil, fmt.Errorf("no virtual service alternatives found for component %s, %v", dpPolicyItem.ComponentRef.Name, vsc)
		}

		additionalVSList := make([]virtualSvcStruct, 0)
		for _, alternativeVS := range vsc.AnyOf {
			switch alternativeVS.Kind {
			case "ComputeProfile":
				// It is simpler since we only need to append requested ComputeProfiles
				// fetch ComputeProfile spec
				vmSpec := alternativeVS.Spec
				if vmSpec == nil {
					continue
				}
				pat, err := ParseFlavorFromJSON(vmSpec)
				if err != nil {
					return nil, err
				}

				profiles, err := r.getAllComputeProfiles([]hv1a1.ProviderRefSpec{providerSpec})
				if err != nil {
					return nil, err
				}

				for _, off := range profiles {
					if !offeringMatches(pat, off) {
						continue
					}
					r.Logger.Info("off", "off", off)

					// Add this offering as a ComputeProfile alternative
					additionalVSList = append(additionalVSList, virtualSvcStruct{
						ApiVersion: "skycluster.io/v1alpha1",
						Kind:       "ComputeProfile",
						Name:       off.name, // name is mapped to nameLabel
						Count:      "1",
						Price:      off.deployCost,
					})
				}

			default:
				return nil, fmt.Errorf("unsupported virtual service kind %s for Virtual Machine execution environment", alternativeVS.Kind)
			}

			// remove duplicates; for an alternative VS it does not make sense to have duplicates
			additionalVSList = lo.UniqBy(additionalVSList, func(v virtualSvcStruct) string {
				return v.Name
			})
			// the list can be huge, so sort and limit to top 10 cheapest
			sort.Slice(additionalVSList, func(i, j int) bool {
				return additionalVSList[i].Price < additionalVSList[j].Price
			})
			if len(additionalVSList) > 10 {
				additionalVSList = additionalVSList[:10]
			}
		}

		if len(additionalVSList) > 0 {
			vsList = append(vsList, additionalVSList)
		}
	}

	return vsList, nil
}

// findServicesForDeployPlan finds the services that must be deployed based on the deployPlan.
// The deployPlan contains the mapping between tasks and providers, and this function
// finds the virtual services (like ComputeProfile) that must be deployed for each component
// in the chosen providers, similar to findVMVirtualServices and findK8SVirtualServices.
func (r *ILPTaskReconciler) findServicesForDeployPlan(
	ns string,
	deployPlan cv1a1.DeployMap,
	dp pv1a1.DeploymentPolicy) (map[string][][]virtualSvcStruct, error) {
	// Map from component name to list of virtual services that must be deployed
	servicesMap := make(map[string][][]virtualSvcStruct)

	// For each component in the deployPlan
	for _, skySvc := range deployPlan.Component {
		cmpntRef := skySvc.ComponentRef

		// expect the provider ref to be set from the optimizer
		if skySvc.ProviderRef.Name == "" {
			return nil, fmt.Errorf("provider ref not set for component %s", cmpntRef.Name)
		}
		provRef := skySvc.ProviderRef

		// Find the matching deployment policy item
		var matchingDPItem *pv1a1.DeploymentPolicyItem
		for i := range dp.Spec.DeploymentPolicies {
			cmpnt := &dp.Spec.DeploymentPolicies[i]
			if cmpnt.ComponentRef.Name == cmpntRef.Name &&
				cmpnt.ComponentRef.Kind == cmpntRef.Kind &&
				cmpnt.ComponentRef.APIVersion == cmpntRef.APIVersion {
				matchingDPItem = cmpnt
				break
			}
		}

		if matchingDPItem == nil {
			return nil, fmt.Errorf("no matching deployment policy item found for component %s (kind: %s, apiVersion: %s)",
				cmpntRef.Name, cmpntRef.Kind, cmpntRef.APIVersion)
		}

		var svcs [][]virtualSvcStruct
		var err error
		switch dp.Spec.ExecutionEnvironment {
		case "Kubernetes":
			svcs, err = r.findK8SVirtualServiceForProvider(ns, *matchingDPItem, provRef)
			if err != nil {
				return nil, err
			}
		case "VirtualMachine":
			svcs, err = r.findVMVirtualServicesForProvider(ns, *matchingDPItem, provRef)
			if err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("unsupported execution environment %s for services for deploy plan", dp.Spec.ExecutionEnvironment)
		}
		servicesMap[cmpntRef.Name] = svcs
	} // end of for each component in the deployPlan

	return servicesMap, nil
}

// getAllComputeProfiles fetches all compute profiles (config maps) for the referenced providers
// if provRefs is empty, fetches all available providers.
func (r *ILPTaskReconciler) getAllComputeProfiles(provRefs []hv1a1.ProviderRefSpec) ([]computeProfileService, error) {
	provProfiles := make([]cv1a1.ProviderProfileSpec, 0)
	for _, provRef := range provRefs {
		provProfiles = append(provProfiles, cv1a1.ProviderProfileSpec{
			Platform:    provRef.Platform,
			Region:      provRef.Region,
			RegionAlias: provRef.RegionAlias,
		})
	}

	candidates, err := r.getCandidateProviders(provProfiles)
	if err != nil {
		return nil, err
	}

	allComputeProfiles := make([]computeProfileService, 0)
	for _, c := range candidates {
		profiles, err := r.getComputeProfileForProvider(c)
		if err != nil {
			return nil, err
		}
		allComputeProfiles = append(allComputeProfiles, profiles...)
	}

	return allComputeProfiles, nil
}

// getCandidateProviders returns the list of ProviderProfiles that match the given filter.
func (r *ILPTaskReconciler) getCandidateProviders(filter []cv1a1.ProviderProfileSpec) ([]cv1a1.ProviderProfile, error) {
	providers := cv1a1.ProviderProfileList{}
	err := r.List(context.Background(), &providers)
	if err != nil {
		return nil, err
	}
	candidateProviders := make([]cv1a1.ProviderProfile, 0)

	// If no filter is provided, return all providers
	// TODO: Check if any conflict arises from this logic.
	// Since in the optimization module, if the candidate providers list is empty,
	// all providers are considered.
	if len(filter) == 0 {
		return providers.Items, nil
	}

	for _, item := range providers.Items {
		for _, ref := range filter {
			match := true
			if ref.Platform != "" && item.Spec.Platform != ref.Platform {
				match = false
			}
			if ref.Region != "" && item.Spec.Region != ref.Region {
				match = false
			}
			if ref.RegionAlias != "" && item.Spec.RegionAlias != ref.RegionAlias {
				match = false
			}
			if match {
				candidateProviders = append(candidateProviders, item)
			}
		}
	}

	return candidateProviders, nil
}

// getComputeProfileForProvider fetches the compute profile (config map) for a given provider profile.
func (r *ILPTaskReconciler) getComputeProfileForProvider(p cv1a1.ProviderProfile) ([]computeProfileService, error) {

	// List all config maps by labels
	var configMaps corev1.ConfigMapList
	if err := r.List(context.Background(), &configMaps, client.MatchingLabels{
		"skycluster.io/config-type":       "provider-profile",
		"skycluster.io/managed-by":        "skycluster",
		"skycluster.io/provider-profile":  p.Name,
		"skycluster.io/provider-platform": p.Spec.Platform,
		"skycluster.io/provider-region":   p.Spec.Region,
	}); err != nil {
		return nil, err
	}

	if len(configMaps.Items) == 0 {
		return nil, fmt.Errorf("no config maps found for provider %s", p.Name)
	}
	if len(configMaps.Items) > 1 {
		return nil, fmt.Errorf("multiple config maps found for provider %s", p.Name)
	}

	var vServicesList []computeProfileService
	cmData, ok := configMaps.Items[0].Data["flavors.yaml"]
	if ok {
		var zoneOfferings []cv1a1.ZoneOfferings
		if err := yaml.Unmarshal([]byte(cmData), &zoneOfferings); err == nil {
			if len(zoneOfferings) == 0 {
				r.Logger.Info("No zone offerings found for provider", "provider", p.Name)
			}
			for _, zo := range zoneOfferings {
				if len(zo.Offerings) == 0 {
					r.Logger.Info("No offerings found for zone", "zone", zo.Zone)
					continue
				}
				for _, of := range zo.Offerings {
					priceFloat, err := strconv.ParseFloat(strings.Trim(of.Price, "$"), 64)
					if err != nil {
						r.Logger.Info("Warning: Failed to parse price for offering", "offering", of.NameLabel, "error", err)
						continue
					}
					fRam, err1 := parseMemory(of.RAM)
					fCPU := float64(of.VCPUs)
					if err1 != nil {
						r.Logger.Info("Warning: Failed to parse memory for offering", "offering", of.NameLabel, "error", err1)
						continue
					}

					vServicesList = append(vServicesList, computeProfileService{
						name:            of.NameLabel,
						cpu:             fCPU,
						ram:             fRam,
						gpuEnabled:      of.GPU.Enabled,
						gpuModel:        of.GPU.Model,
						gpuCount:        of.GPU.Unit,
						gpuMem:          of.GPU.Memory,
						gpuManufacturer: of.GPU.Manufacturer,
						deployCost:      priceFloat,
					})
				}
			}
		} else {
			return nil, fmt.Errorf("failed to unmarshal flavors.yaml for provider %s: %w", p.Name, err)
		}
	} else {
		return nil, fmt.Errorf("no flavors.yaml found for provider %s", p.Name)
	}

	return vServicesList, nil
}

func ParseFlavorFromJSON(flavorJSON *runtime.RawExtension) (FlavorPattern, error) {
	var numberRe = regexp.MustCompile(`[\d.]+`)
	var fs hv1a1.ComputeFlavor
	var p FlavorPattern
	if err := json.Unmarshal(flavorJSON.Raw, &fs); err != nil {
		return p, err
	}

	// VCPU
	if strings.TrimSpace(fs.VCPUs) == "" {
		p.CpuAny = true
	} else if m := numberRe.FindString(fs.VCPUs); m != "" {
		if v, err := strconv.ParseFloat(m, 64); err == nil {
			p.Cpu = v
		} else {
			p.CpuAny = true
		}
	} else {
		p.CpuAny = true
	}

	// RAM
	if strings.TrimSpace(fs.RAM) == "" {
		p.RamAny = true
	} else if m := numberRe.FindString(fs.RAM); m != "" {
		if v, err := strconv.ParseFloat(m, 64); err == nil {
			p.Ram = v
		} else {
			p.RamAny = true
		}
	} else {
		p.RamAny = true
	}

	// GPU model (type)
	if strings.TrimSpace(fs.GPU.Model) != "" {
		p.GpuModel = strings.TrimSpace(fs.GPU.Model)
	}

	// GPU count
	if strings.TrimSpace(fs.GPU.Unit) != "" {
		if m := numberRe.FindString(fs.GPU.Unit); m != "" {
			if v, err := strconv.Atoi(m); err == nil {
				p.GpuCount = v
			}
		}
		// if parsing fails, leave gpuCount==0 meaning "not specified"
	}

	// GPU memory
	if strings.TrimSpace(fs.GPU.Memory) == "" {
		p.GpuMemAny = true
	} else if m := numberRe.FindString(fs.GPU.Memory); m != "" {
		if v, err := strconv.ParseFloat(m, 64); err == nil {
			p.GpuMem = v
		} else {
			p.GpuMemAny = true
		}
	} else {
		p.GpuMemAny = true
	}

	return p, nil
}

func parseGPUCountString(s string) (int, bool) {
	var numberRe = regexp.MustCompile(`[\d.]+`)
	if strings.TrimSpace(s) == "" {
		return 0, false
	}
	if m := numberRe.FindString(s); m != "" {
		if v, err := strconv.Atoi(m); err == nil {
			return v, true
		}
	}
	return 0, false
}

// parseGPUMemoryString parses strings like "32GB", "32 GiB", "32768MB", "0.5TB".
// It returns the size in GB and a bool indicating whether parsing succeeded.
func parseGPUMemoryString(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}

	re := regexp.MustCompile(`(?i)^\s*([\d]+(?:\.[\d]+)?)\s*(tb|tib|gb|gib|mb|mib|kb|kib)?\s*$`)
	m := re.FindStringSubmatch(s)
	if len(m) == 0 {
		return 0, false
	}

	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false
	}

	unit := strings.ToLower(m[2])
	switch unit {
	case "tb", "tib":
		v = v * 1024 // convert TB -> GB (binary TiB->GiB semantics)
	case "gb", "gib", "":
		// already in GB
	case "mb", "mib":
		v = v / 1024 // MB -> GB
	case "kb", "kib":
		v = v / (1024 * 1024) // KB -> GB
	}

	return v, true
}

func offeringMatches(p FlavorPattern, off computeProfileService) bool {
	availCPU := off.cpu
	availRAM := off.ram

	// CPU
	if !p.CpuAny && p.Cpu > 0 {
		if availCPU < p.Cpu {
			return false
		}
		// only allow up to5% deviation
		if availCPU > p.Cpu*1.05 {
			return false
		}
	}
	// RAM
	if !p.RamAny && p.Ram > 0 {
		if availRAM < p.Ram {
			return false
		}
		// only allow up to 25% deviation
		if availRAM > p.Ram*1.25 {
			return false
		}
	}

	// prefer structured fields on offering
	offGPUCount, offGPUCountOk := parseGPUCountString(off.gpuCount)
	offGPUMemory, offGPUMemoryOk := parseGPUMemoryString(off.gpuMem)
	offGPUModel := strings.TrimSpace(off.gpuModel)

	// GPU model/type check (only if requested)
	if p.GpuModel != "" {
		// check off.gpuModel first
		if offGPUModel != "" {
			if !strings.EqualFold(offGPUModel, p.GpuModel) {
				return false
			}
		} else {
			// offering lacks GPU info -> cannot satisfy specific model
			return false
		}
	}

	// if gpu is not requested, but offering has gpu, return false
	if p.GpuModel == "" && p.GpuCount == 0 && p.GpuMem == 0 && off.gpuEnabled {
		return false
	}

	// GPU count (if requested)
	if p.GpuCount > 0 {
		if !off.gpuEnabled {
			return false
		}
		// prefer structured count
		if offGPUCountOk {
			if offGPUCount < p.GpuCount {
				return false
			}
		} // else: no count info -> assume may satisfy
	}

	// GPU memory:
	if !p.GpuMemAny && p.GpuMem > 0 {
		if !off.gpuEnabled {
			return false
		}
		// prefer structured memory
		if offGPUMemoryOk {
			if offGPUMemory < p.GpuMem {
				return false
			}
		} // else: no memory info -> assume may satisfy
	}

	// ensure GPU enabled if model or count requested
	if (p.GpuModel != "" || p.GpuCount > 0) && !off.gpuEnabled {
		return false
	}

	return true
}

func parseMemory(s string) (float64, error) {
	var numRe = regexp.MustCompile(`([+-]?\d*\.?\d+(?:[eE][+-]?\d+)?)`)
	m := numRe.FindStringSubmatch(s)
	if len(m) < 2 {
		return 0, fmt.Errorf("no number found in %q", s)
	}
	return strconv.ParseFloat(m[1], 64)
}

// sortComputeResources sorts the compute resources by cpu and ram
// It returns -1 if i < j, 1 if i > j, and 0 if i == j
func sortComputeResources(i, j computeProfileService) int {
	if i.cpu != j.cpu {
		if i.cpu < j.cpu {
			return -1
		} else {
			return 1
		}
	}
	if i.ram != j.ram {
		if i.ram < j.ram {
			return -1
		} else {
			return 1
		}
	}
	return 0
}

// computeResourcesForFlavors returns a list of computeResource structs
// based on the flavor names in the input map
func computeResourcesForFlavors(configData map[string]string) ([]computeProfileService, error) {
	allFlavorsCpuRam := make([]computeProfileService, 0)
	for k, _ := range configData {
		if !strings.Contains(k, "skyvm_flavor") {
			continue
		}
		// a flavor is in the form of "skyvm_flavor_2vCPU-4GB"
		// we need to extract the cpu and ram from the flavor
		flavor := strings.Split(k, "_")[2]
		cpuString := strings.Split(flavor, "-")[0]
		ramString := strings.Split(flavor, "-")[1]
		cpu, err1 := strconv.Atoi(strings.Replace(cpuString, "vCPU", "", -1))
		// The pod's ram resources are presented in GB, so
		// We can keep the current format as the RAM are in GB
		ram, err2 := strconv.Atoi(strings.Replace(ramString, "GB", "", -1))
		if err1 != nil || err2 != nil {
			return nil, errors.Wrap(err1, "Error converting flavor to int in assigning deployments to nodes.")
		}
		allFlavorsCpuRam = append(allFlavorsCpuRam, computeProfileService{name: flavor, cpu: float64(cpu), ram: float64(ram)})
	}
	return allFlavorsCpuRam, nil
}

// findSuitableComputeResource returns the name of the compute resource that satisfies the
// minimum requirements for the given compute resource. If no compute resource satisfies
// the requirements, it returns an empty string
func findSuitableComputeResource(cmResource computeProfileService, allComputeResources []computeProfileService) (*computeProfileService, bool) {
	for _, cr := range allComputeResources {
		if cr.cpu >= cmResource.cpu && cr.ram >= cmResource.ram {
			return &computeProfileService{name: cr.name, cpu: cr.cpu, ram: cr.ram}, true
		}
	}
	return nil, false
}

// attemptPlaceDeployment returns true if the deployment can be placed on any of given nodes
// and if it is possible to use any node, it updates the corresponding
// node with the new used cpu and memory
func attemptPlaceDeployment(dep computeProfileService, nodes []computeProfileService) (bool, []computeProfileService) {
	for i, node := range nodes {
		if (node.cpu-node.usedCPU) >= dep.cpu && (node.ram-node.usedRAM) >= dep.ram {
			nodes[i].usedCPU += dep.cpu
			nodes[i].usedRAM += dep.ram
			return true, nodes
		}
	}
	return false, nodes
}

func OfferingMatches2(p FlavorPattern, off hv1a1.InstanceOffering) (bool, string) {
	availCPU := off.VCPUs
	availRAM, err := strconv.Atoi(strings.ReplaceAll(off.RAM, "GB", ""))
	if err != nil {
		return false, "parsing offering RAM"
	}

	// CPU
	if !p.CpuAny && p.Cpu > 0 {
		if availCPU < int(p.Cpu) {
			return false, "offering CPU less than requested"
		}
	}
	// RAM
	if !p.RamAny && p.Ram > 0 {
		if availRAM < int(p.Ram) {
			return false, "offering RAM less than requested"
		}
	}

	// prefer structured fields on offering
	offGPUCount, offGPUCountOk := parseGPUCountString(off.GPU.Unit)
	offGPUMemory, offGPUMemoryOk := parseGPUMemoryString(off.GPU.Memory)
	offGPUModel := strings.TrimSpace(off.GPU.Model)

	// GPU model/type check (only if requested)
	if p.GpuModel != "" {
		// check off.gpuModel first
		if offGPUModel != "" {
			if !strings.EqualFold(offGPUModel, p.GpuModel) {
				return false, "offering GPU model does not match requested"
			}
		} else {
			// offering lacks GPU info -> cannot satisfy specific model
			return false, "offering lacks GPU model information"
		}
	}

	// GPU count (if requested)
	if p.GpuCount > 0 {
		if !off.GPU.Enabled {
			return false, "offering GPU not enabled"
		}
		// prefer structured count
		if offGPUCountOk {
			if offGPUCount < p.GpuCount {
				return false, "offering GPU count less than requested"
			}
		} // else: no count info -> assume may satisfy
	}

	// GPU memory:
	if !p.GpuMemAny && p.GpuMem > 0 {
		if !off.GPU.Enabled {
			return false, "offering GPU not enabled"
		}
		// prefer structured memory
		if offGPUMemoryOk {
			if offGPUMemory < p.GpuMem {
				return false, "offering GPU memory less than requested"
			}
		} // else: no memory info -> assume may satisfy
	}

	// ensure GPU enabled if model or count requested
	if (p.GpuModel != "" || p.GpuCount > 0) && !off.GPU.Enabled {
		return false, "offering GPU not enabled"
	}

	return true, ""
}
