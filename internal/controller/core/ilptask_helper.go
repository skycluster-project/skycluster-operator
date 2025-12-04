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
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/samber/lo"
	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	hv1a1 "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"
	pv1a1 "github.com/skycluster-project/skycluster-operator/api/policy/v1alpha1"
)

type computeProfileService struct {
	name    string
	cpu     float64
	ram     float64
	usedCPU float64
	usedRAM float64
	gpuEnabled bool
	gpuModel   string
	gpuCount	 string
	gpuMem     string
	gpuManufacturer string
	deployCost     float64
}

type flavorPattern struct {
	cpu       float64
	cpuAny    bool
	ram       float64
	ramAny    bool
	gpuCount  int    // 0 = not specified
	gpuModel   string // empty = not specified
	gpuMem    float64
	gpuMemAny bool
}

type virtualSvcStruct struct {
	Name 		 string `json:"name,omitempty"`
	Spec     string `json:"spec,omitempty"`
	ApiVersion  string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Count      string `json:"count,omitempty"`
	Price      float64 `json:"price,omitempty"`
}

type locStruct struct {
	Name 	 string `json:"name,omitempty"`
	PType 	string `json:"pType,omitempty"`
	RegionAlias string `json:"regionAlias,omitempty"`
	Region 	string `json:"region,omitempty"`
	Platform 	string `json:"platform,omitempty"`
}

type optTaskStruct struct {
	Task               string         `json:"task"`
	ApiVersion         string          `json:"apiVersion"`
	Kind               string          `json:"kind"`
	PermittedLocations []locStruct     `json:"permittedLocations"`
	RequiredLocations  [][]locStruct   `json:"requiredLocations"`
	RequestedVServices [][]virtualSvcStruct  `json:"requestedVServices"`
	MaxReplicas        string          `json:"maxReplicas"`
}

// findK8SVirtualServices processes the deployment policies and extracts the virtual services
// when execution environment is Kubernetes.
func (r *ILPTaskReconciler) findK8SVirtualServices(ns string, dpPolicies []pv1a1.DeploymentPolicyItem) ([]optTaskStruct, error) {
	
	var optTasks []optTaskStruct
	for _, cmpnt := range dpPolicies {
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
			additionalVSList := make([]virtualSvcStruct, 0)			
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
					pat, err := parseFlavorFromJSON(alternativeVS.Spec)
					if err != nil {return nil, err}

					profiles, err := r.getAllComputeProfiles(cmpnt.LocationConstraint.Permitted)
					if err != nil { return nil, err }

					for _, off := range profiles {
						if !offeringMatches(pat, off) {continue}
						// Add this offering as a ComputeProfile alternative
						newVS := virtualSvcStruct{
							ApiVersion:  "skycluster.io/v1alpha1",
							Kind:       "ComputeProfile",
							Name:       off.name, // name is mapped to nameLabel
							Price:      off.deployCost,
							Count:  strconv.Itoa(alternativeVS.Count),
						}
						altVSList = append(altVSList, newVS)
					}
				}

				switch cmpnt.ComponentRef.Kind {
				case "Deployment":
					// If the component is a Deployment, we make sure there are compute profile
					// alternatives for deployments.
					
					// Compute the minimum compute resource required for this component (i.e. deployment)
					minCR, err := r.calculateMinComputeResource(ns, cmpnt.ComponentRef.Name)
					if err != nil {
						return nil, fmt.Errorf("failed to calculate min compute resource for component %s: %w", cmpnt.ComponentRef.Name, err)
					}
					if minCR == nil {
						return nil, fmt.Errorf("nil min compute resource for component %s", cmpnt.ComponentRef.Name)
					}

					// Try to fetch flavors for the provider identified by alternativeVS.Name (fallback to component name)
					profiles, err := r.getAllComputeProfiles(cmpnt.LocationConstraint.Permitted)
					if err != nil { return nil, err }

					// collect all suitable offerings
					for _, off := range profiles {
						if off.cpu < minCR.cpu { continue }
						if off.ram < minCR.ram { continue } // GiB

						// Add this offering as a ComputeProfile alternative
						additionalVSList = append(additionalVSList, virtualSvcStruct{
								ApiVersion: alternativeVS.APIVersion,
								Kind:       "ComputeProfile",
								Name:       off.name, // name is mapped to nameLabel
								Count:      "1",
								Price:      off.deployCost,
						})
					}

				case "XNodeGroup":
					// If the component is an XNodeGroup, it represents a set of Compute profile
					// that must be used for node groups for the execution cluster itself.
					// if it is tied to a specific provider, it targets the provider's Kubernetes cluster
					// If no provider is specified, the optimizer will choose the best provider minimizing
					// the node group costs plus other deployment costs (e.g., control plane).
					
					// Since the ComputeProfile is already handled above, we do not need to do anything here.
					r.Logger.Info("XNodeGroup component detected, handled via ComputeProfile alternatives", "component", cmpnt.ComponentRef.Name)
				
				default:
					return nil, fmt.Errorf("unsupported virtual service kind %s for Kubernetes execution environment", alternativeVS.Kind)
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

			if len(altVSList) > 0 {
				vsList = append(vsList, altVSList)
			}
			if len(additionalVSList) > 0 {
				vsList = append(vsList, additionalVSList)
			}
		}

		perLocList := make([]locStruct, 0)
		for _, perLoc := range cmpnt.LocationConstraint.Permitted {
			newLoc := locStruct{
				Name:       perLoc.Name,
				PType:      perLoc.Type,
				RegionAlias: perLoc.RegionAlias,
				Region:     perLoc.Region,
				Platform:   perLoc.Platform,
			}
			perLocList = append(perLocList, newLoc)
		}

		reqLocList := make([][]locStruct, 0)
		for _, reqLoc := range cmpnt.LocationConstraint.Required {
			altReqLocList := make([]locStruct, 0)
			for _, altReqLoc := range reqLoc.AnyOf {
				newLoc := locStruct{
					Name:       altReqLoc.Name,
					PType:      altReqLoc.Type,
					RegionAlias: altReqLoc.RegionAlias,
					Region:     altReqLoc.Region,
					Platform:   altReqLoc.Platform,
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
			MaxReplicas:        func () string {
				// TODO: receive this from deployment policy
				// if cmpnt.ComponentRef.Name == "central-storage" {return "1"}
				return "-1"
			}(),
		})
	}
	return optTasks, nil
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
				supportedVSKinds := []string{"ComputeProfile"}
				if !slices.Contains(supportedVSKinds, alternativeVS.Kind) {
					return nil, fmt.Errorf("unsupported virtual service kind %s for Virtual Machine execution environment", alternativeVS.Kind)
				}

				switch alternativeVS.Kind {
				case "ComputeProfile":
					// It is simpler since we only need to append requested ComputeProfiles
					// fetch ComputeProfile spec
					vmSpec := alternativeVS.Spec
					pat, err := parseFlavorFromJSON(vmSpec)
					if err != nil {return nil, err}

					profiles, err := r.getAllComputeProfiles(cmpnt.LocationConstraint.Permitted)
					if err != nil { return nil, err }

					for _, off := range profiles {
						if !offeringMatches(pat, off) {continue}
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
				Name:       perLoc.Name,
				PType:      perLoc.Type,
				RegionAlias: perLoc.RegionAlias,
				Region:     perLoc.Region,
				Platform:   perLoc.Platform,
			}
			perLocList = append(perLocList, newLoc)
		}

		reqLocList := make([][]locStruct, 0)
		for _, reqLoc := range cmpnt.LocationConstraint.Required {
			altReqLocList := make([]locStruct, 0)
			for _, altReqLoc := range reqLoc.AnyOf {
				newLoc := locStruct{
					Name:       altReqLoc.Name,
					PType:      altReqLoc.Type,
					RegionAlias: altReqLoc.RegionAlias,
					Region:     altReqLoc.Region,
					Platform:   altReqLoc.Platform,
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
			MaxReplicas:        func () string {
				// if cmpnt.ComponentRef.Name == "dashboard" {return "1"}
				return "-1"
			}(),
		})
	}
	return optTasks, nil
}

// getAllComputeProfiles fetches all compute profiles (config maps) for the referenced providers
// if provRefs is empty, fetches all available providers.
func (r *ILPTaskReconciler) getAllComputeProfiles(provRefs []hv1a1.ProviderRefSpec) ([]computeProfileService, error) {
	provProfiles := make([]cv1a1.ProviderProfileSpec, 0)
	for _, provRef := range provRefs {
		provProfiles = append(provProfiles, cv1a1.ProviderProfileSpec{
			Platform: provRef.Platform,
			Region:   provRef.Region,
			RegionAlias: provRef.RegionAlias,
		})
	}

	candidates, err := r.getCandidateProviders(provProfiles)
	if err != nil { return nil, err }

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
	if len(filter) == 0 {return providers.Items, nil}

	for _, item := range providers.Items {
		for _, ref := range filter {
			match := true
			if ref.Platform != "" && item.Spec.Platform != ref.Platform { match = false }
			if ref.Region != "" && item.Spec.Region != ref.Region { match = false }
			if ref.RegionAlias != "" && item.Spec.RegionAlias != ref.RegionAlias { match = false }
			if match { candidateProviders = append(candidateProviders, item) }
		}
	}

	return candidateProviders, nil
}

// getComputeProfileForProvider fetches the compute profile (config map) for a given provider profile.
func (r *ILPTaskReconciler) getComputeProfileForProvider(p cv1a1.ProviderProfile) ([]computeProfileService, error) {
	
	// List all config maps by labels
	var configMaps corev1.ConfigMapList
	if err := r.List(context.Background(), &configMaps, client.MatchingLabels{
		"skycluster.io/config-type": "provider-profile",
		"skycluster.io/managed-by": "skycluster",
		"skycluster.io/provider-profile": p.Name,
		"skycluster.io/provider-platform": p.Spec.Platform,
		"skycluster.io/provider-region": p.Spec.Region,
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
			for _, zo := range zoneOfferings {
				for _, of := range zo.Offerings{
					priceFloat, err := strconv.ParseFloat(of.Price, 64)
					if err != nil {continue}
					fRam, err1 := parseMemory(of.RAM)
					fCPU := float64(of.VCPUs)
					if err1 != nil {continue }

					vServicesList = append(vServicesList, computeProfileService{
						name:   of.NameLabel,
						cpu: 	fCPU,
						ram: fRam,
						gpuEnabled:    of.GPU.Enabled,
						gpuModel: of.GPU.Model,
						gpuCount: of.GPU.Unit,
						gpuMem: of.GPU.Memory,
						gpuManufacturer: of.GPU.Manufacturer,
						deployCost:     priceFloat,
					})
				}
			}
		}
	}
	
	return vServicesList, nil
}

func parseFlavorFromJSON(flavorJSON string) (flavorPattern, error) {
	var numberRe = regexp.MustCompile(`[\d.]+`)
	var fs hv1a1.FlavorSpec
	var p flavorPattern
	if err := json.Unmarshal([]byte(flavorJSON), &fs); err != nil {
		return p, err
	}

	// VCPU
	if strings.TrimSpace(fs.VCPU) == "" {
		p.cpuAny = true
	} else if m := numberRe.FindString(fs.VCPU); m != "" {
		if v, err := strconv.ParseFloat(m, 64); err == nil {
			p.cpu = v
		} else {
			p.cpuAny = true
		}
	} else {
		p.cpuAny = true
	}

	// RAM
	if strings.TrimSpace(fs.RAM) == "" {
		p.ramAny = true
	} else if m := numberRe.FindString(fs.RAM); m != "" {
		if v, err := strconv.ParseFloat(m, 64); err == nil {
			p.ram = v
		} else {
			p.ramAny = true
		}
	} else {
		p.ramAny = true
	}

	// GPU model (type)
	if strings.TrimSpace(fs.GPU.Model) != "" {
		p.gpuModel = strings.TrimSpace(fs.GPU.Model)
	}

	// GPU count
	if strings.TrimSpace(fs.GPU.Unit) != "" {
		if m := numberRe.FindString(fs.GPU.Unit); m != "" {
			if v, err := strconv.Atoi(m); err == nil {
				p.gpuCount = v
			}
		}
		// if parsing fails, leave gpuCount==0 meaning "not specified"
	}

	// GPU memory
	if strings.TrimSpace(fs.GPU.Memory) == "" {
		p.gpuMemAny = true
	} else if m := numberRe.FindString(fs.GPU.Memory); m != "" {
		if v, err := strconv.ParseFloat(m, 64); err == nil {
			p.gpuMem = v
		} else {
			p.gpuMemAny = true
		}
	} else {
		p.gpuMemAny = true
	}

	return p, nil
}

func parseGPUCountString(s string) (int, bool) {
	var numberRe = regexp.MustCompile(`[\d.]+`)
	if strings.TrimSpace(s) == "" {return 0, false}
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

func offeringMatches(p flavorPattern, off computeProfileService) bool {
	availCPU := off.cpu
	availRAM := off.ram

	// CPU
	if !p.cpuAny && p.cpu > 0 {
		if availCPU < p.cpu {
			return false
		}
	}
	// RAM
	if !p.ramAny && p.ram > 0 {
		if availRAM < p.ram {
			return false
		}
	}

	// prefer structured fields on offering
	offGPUCount, offGPUCountOk := parseGPUCountString(off.gpuCount)
	offGPUMemory, offGPUMemoryOk := parseGPUMemoryString(off.gpuMem)
	offGPUModel := strings.TrimSpace(off.gpuModel)

	// GPU model/type check (only if requested)
	if p.gpuModel != "" {
		// check off.gpuModel first
		if offGPUModel != "" {
			if !strings.EqualFold(offGPUModel, p.gpuModel) {return false}
		} else {
			// offering lacks GPU info -> cannot satisfy specific model
			return false
		}
	}

	// GPU count (if requested)
	if p.gpuCount > 0 {
		if !off.gpuEnabled {return false}
		// prefer structured count
		if offGPUCountOk {
			if offGPUCount < p.gpuCount {return false}
		} // else: no count info -> assume may satisfy
	}

	// GPU memory: 
	if !p.gpuMemAny && p.gpuMem > 0 {
		if !off.gpuEnabled {return false}
		// prefer structured memory
		if offGPUMemoryOk {
			if offGPUMemory < p.gpuMem {return false}
		} // else: no memory info -> assume may satisfy
	}

	// ensure GPU enabled if model or count requested
	if (p.gpuModel != "" || p.gpuCount > 0) && !off.gpuEnabled {
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
