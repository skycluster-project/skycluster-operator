package core

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GetNestedField returns the nested field of a map[string]interface{} object
// It returns the nested field if it exists, and an error if it doesn't
// The fields are the keys to access the nested field
func GetNestedField(obj map[string]any, fields ...string) (map[string]any, error) {
	if len(fields) == 0 {
		return nil, errors.New("no fields provided")
	}
	m := obj
	for _, field := range fields {
		if val, ok := m[field].(map[string]interface{}); ok {
			m = val
		} else {
			return nil, fmt.Errorf("field %s not found in the object or its type is not map[string]interface{}", field)
		}
	}
	return m, nil // the last field is not found in the object
}

// deplyHasLabels returns true if the deployment has the given labels
func deploymentHasLabels(deploy *appsv1.Deployment, labels map[string]string) bool {
	for k, v := range labels {
		if deploy.Spec.Template.ObjectMeta.Labels[k] != v {
			return false
		}
	}
	return true
}

// sortComputeResources sorts the compute resources by cpu and ram
// It returns -1 if i < j, 1 if i > j, and 0 if i == j
func sortComputeResources(i, j computeResource) int {
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

// getUniqueFlavors returns a list of unique flavors from all configmaps
func getUniqueFlavors(allConfigMap *corev1.ConfigMapList) []string {
	allFlavors := make([]string, 0)
	allFlavors_set := make(map[string]struct{}, 0)
	for _, cm := range allConfigMap.Items {
		for k := range cm.Data {
			if !strings.Contains(k, "skyvm_flavor") {
				continue
			}
			flavorName := strings.Split(k, "_")[2]
			if _, ok := allFlavors_set[flavorName]; ok {
				continue
			}
			allFlavors = append(allFlavors, flavorName)
			allFlavors_set[flavorName] = struct{}{}
		}
	}
	return allFlavors
}

// getCompatibleFlavors returns the flavors names that satisfy the minimum
// requirements for a compute resource. The flavors are in the format of "vCPU-RAM"
func getCompatibleFlavors(minCPU, minRAM float64, allFlavors []string) ([]string, error) {
	okFlavors := make([]string, 0)
	for _, skyFlavor := range allFlavors {
		cpu := strings.Split(skyFlavor, "-")[0]
		cpu = strings.Replace(cpu, "vCPU", "", -1)
		cpu_int, err1 := (strconv.Atoi(cpu))
		cpu_float := float64(cpu_int)
		ram := strings.Split(skyFlavor, "-")[1]
		ram = strings.Replace(ram, "GB", "", -1)
		ram_int, err2 := (strconv.Atoi(ram))
		ram_float := float64(ram_int)
		if err1 != nil || err2 != nil {
			if err1 != nil {
				return nil, errors.Wrap(err1, "Error converting flavor spec to int.")
			}
			if err2 != nil {
				return nil, errors.Wrap(err1, "Error converting flavor spec to int.")
			}
			// if there are error processing the flavors we ignore them and not add them to the list
			continue
		}
		if cpu_float >= minCPU && ram_float >= minRAM {
			okFlavors = append(okFlavors, fmt.Sprintf("%s|1", skyFlavor))
		}
	}
	return okFlavors, nil
}

// getContainerComputeResources returns the cpu and memory resources for a container
// If the limits are set, it returns the limits, if not, it returns the requests
func getContainerComputeResources(container corev1.Container) (float64, float64) {
	// Get the resources
	resources := container.Resources
	// Get the limits
	limits := resources.Limits
	// Check the limits
	cpuQtyLimit, cpuOkLimit := limits["cpu"]
	memQtyLimit, memOkLimit := limits["memory"]

	// Get the requests
	requests := resources.Requests
	// Check the requests
	cpuQtyReq, cpuOkReq := requests["cpu"]
	memQtyReq, memOkReq := requests["memory"]

	var cpu float64
	var mem float64
	if cpuOkLimit {
		cpu = cpuQtyLimit.AsApproximateFloat64()
	} else if cpuOkReq {
		cpu = cpuQtyReq.AsApproximateFloat64()
	}
	if memOkLimit {
		memBytes := memQtyLimit.Value()
		mem = float64(memBytes) / (1 << 30) // GiB
	} else if memOkReq {
		memBytes := memQtyReq.Value()
		mem = float64(memBytes) / (1 << 30) // GiB
	}
	return cpu, mem
}

// computeResourcesForFlavors returns a list of computeResource structs
// based on the flavor names in the input map
func computeResourcesForFlavors(configData map[string]string) ([]computeResource, error) {
	allFlavorsCpuRam := make([]computeResource, 0)
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
		allFlavorsCpuRam = append(allFlavorsCpuRam, computeResource{name: flavor, cpu: float64(cpu), ram: float64(ram)})
	}
	return allFlavorsCpuRam, nil
}

// findSuitableComputeResource returns the name of the compute resource that satisfies the
// minimum requirements for the given compute resource. If no compute resource satisfies
// the requirements, it returns an empty string
func findSuitableComputeResource(cmResource computeResource, allComputeResources []computeResource) (*computeResource, bool) {
	for _, cr := range allComputeResources {
		if cr.cpu >= cmResource.cpu && cr.ram >= cmResource.ram {
			return &computeResource{name: cr.name, cpu: cr.cpu, ram: cr.ram}, true
		}
	}
	return nil, false
}

// generateNewDeplyFromDeploy generates a new deployment from the given deployment
// with the same selector and template
func generateNewDeplyFromDeploy(deploy *appsv1.Deployment) appsv1.Deployment {
	newDeploy := appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: deploy.APIVersion,
			Kind:       deploy.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploy.Name,
			Namespace: deploy.Namespace,
			Labels:    deploy.Labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: deploy.Spec.Replicas,
			Selector: deploy.Spec.Selector,
			Template: deploy.Spec.Template,
		},
	}
	return newDeploy
}

// generateNewServiceFromService generates a new service from the given service
// with the same selector and ports
func generateNewServiceFromService(svc *corev1.Service) corev1.Service {
	newSvc := corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: svc.APIVersion,
			Kind:       svc.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      svc.Name,
			Namespace: svc.Namespace,
			Labels:    svc.Labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: svc.Spec.Selector,
			Ports:    svc.Spec.Ports,
		},
	}
	return newSvc
}

// attemptPlaceDeployment returns true if the deployment can be placed on any of given nodes
// and if it is possible to use any node, it updates the corresponding
// node with the new used cpu and memory
func attemptPlaceDeployment(dep computeResource, nodes []computeResource) (bool, []computeResource) {
	for i, node := range nodes {
		if (node.cpu-node.usedCPU) >= dep.cpu && (node.ram-node.usedRAM) >= dep.ram {
			nodes[i].usedCPU += dep.cpu
			nodes[i].usedRAM += dep.ram
			return true, nodes
		}
	}
	return false, nodes
}

// generateYAMLManifest generates a string YAML manifest from the given object
func generateYAMLManifest(obj any) (string, error) {
	var inInterface map[string]interface{}
	inrec, _ := json.Marshal(obj)
	json.Unmarshal(inrec, &inInterface)
	objYAML, err := yaml.Marshal(&inInterface)
	if err != nil {
		return "", errors.Wrap(err, "Error marshalling obj manifests.")
	}
	return string(objYAML), nil
}
