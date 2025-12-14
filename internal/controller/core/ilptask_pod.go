package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"github.com/samber/lo"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	hv1a1 "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"
	pv1a1 "github.com/skycluster-project/skycluster-operator/api/policy/v1alpha1"
	utils "github.com/skycluster-project/skycluster-operator/internal/controller"
)


func (r *ILPTaskReconciler) prepareAndBuildOptimizationPod(df pv1a1.DataflowPolicy, dp pv1a1.DeploymentPolicy, task *cv1a1.ILPTask) (string, error) {
	// Creating tasks.csv
	
	// write all JSON strings to a temporary directory for quick inspection
	tmpDir, err := os.MkdirTemp("/tmp", "ilp-debug-")
	if err != nil {
		return "", err
	}

	tasksJson, err := r.generateTasksJson(dp)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "tasks.json"), []byte(tasksJson), 0644); err != nil {
		return "", err
	}

	// Creating tasks-edges.csv
	tasksEdges, err := generateTasksEdgesJson(df)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "tasks-edges.json"), []byte(tasksEdges), 0644); err != nil {
		return "", err
	}

	providers, err := r.generateProvidersJson()
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "providers.json"), []byte(providers), 0644); err != nil {
		return "", err
	}

	providersAttr, err := r.generateProvidersAttrJson(hv1a1.SKYCLUSTER_NAMESPACE)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "providers-attr.json"), []byte(providersAttr), 0644); err != nil {
		return "", err
	}

	dataMap := map[string]string{
		"tasks.json":         tasksJson,
		"tasks-edges.json":   tasksEdges,
		"providers.json":     providers,
		"providers-attr.json": providersAttr,
	}

	// Results
	//    '/shared/optimization-stats.csv'
	//    '/shared/deploy-plan.json'
	// The result of the optimization is stored in the deploy-plan.json file and
	// a configmap is created to store the results with label
	// skycluster.io/config-type: optimization-status

	scripts, err := r.getOptimizationConfigMaps()
	if err != nil { return "", err }

	// Define Pod
	// If they differ the name of SkyCluster should be passed
	podName, err := r.buildOptimizationPod(task, scripts, dataMap)
	if err != nil {
		return "", err
	}

	return podName, nil
}

func (r *ILPTaskReconciler) generateTasksJson(dp pv1a1.DeploymentPolicy) (string, error) {
	execEnv := dp.Spec.ExecutionEnvironment

	if len(dp.Spec.DeploymentPolicies) == 0 {
		return "", fmt.Errorf("no deployment policies found in deployment policy")
	}
	
	var optTasks []optTaskStruct
	var err error
	switch execEnv {
		case "Kubernetes":
			optTasks, err = r.findK8SVirtualServices(dp.Namespace, dp.Spec.DeploymentPolicies)
			if err != nil {return "", err}
		case "VirtualMachine":
			optTasks, err = r.findVMVirtualServices(dp.Spec.DeploymentPolicies)
			if err != nil {return "", err}
		default:
			return "", fmt.Errorf("unsupported execution environment: %s", execEnv)
	}
	
	if len(optTasks) == 0 {
		return "", fmt.Errorf("no optimization tasks found in deployment policy")
	}
	optTasksJson, err := json.Marshal(optTasks)
	if err != nil {
		return "", fmt.Errorf("failed to marshal optimization tasks: %v", err)
	}
	return string(optTasksJson), nil
}

func (r *ILPTaskReconciler) generateProvidersJson() (string, error) {
	// fetch all providerprofiles objects
	// and generate the providers.json file
	var providers cv1a1.ProviderProfileList
	if err := r.List(context.Background(), &providers); err != nil {
		return "", err
	}

	type providerStruct struct {
		UpstreamName  string `json:"upstreamName,omitempty"`
		Name          string `json:"name,omitempty"`
		Platform      string `json:"platform,omitempty"`
		RegionAlias   string `json:"regionAlias,omitempty"`
		Zone          string `json:"zone,omitempty"`
		PType         string `json:"pType,omitempty"`
		Region        string `json:"region,omitempty"`
	}
	var providerList []providerStruct
	for _, p := range providers.Items {
		for _, zone := range p.Spec.Zones {
			if !zone.Enabled {continue}
			providerList = append(providerList, providerStruct{
				UpstreamName: p.Name,
				Name:         p.Spec.Platform + "-" + p.Spec.Region + "-" + zone.Name,
				Platform:     p.Spec.Platform,
				PType:        zone.Type,
				RegionAlias:  p.Spec.RegionAlias,
				Region:       p.Spec.Region,
				Zone:         zone.Name,
			})
		}
	}
	b, err := json.Marshal(providerList)
	if err != nil {
		return "", fmt.Errorf("failed to marshal providers: %v", err)
	}
	return string(b), nil
}

func (r *ILPTaskReconciler) generateProvidersAttrJson(ns string) (string, error) {
	// fetch all providerprofiles objects
	// and generate the providers.json file
	var providers cv1a1.ProviderProfileList
	if err := r.List(context.Background(), &providers); err != nil {
		return "", err
	}

	var latencies cv1a1.LatencyList
	if err := r.List(context.Background(), &latencies); err != nil {
		return "", err
	}
	type providerStruct struct {
		Name        string `json:"name,omitempty"`
		Platform   string `json:"platform,omitempty"`
		RegionAlias string `json:"regionAlias,omitempty"`
		Zone       string `json:"zone,omitempty"`
		PType      string `json:"pType,omitempty"`
		Region     string `json:"region,omitempty"`
	}
	type providerAttrStruct struct {
		SrcName 	string  `json:"srcName,omitempty"`
		DstName 	string  `json:"dstName,omitempty"`
		Src 			providerStruct `json:"src,omitempty"`
		Dst 			providerStruct `json:"dst,omitempty"`
		Latency 	float64 `json:"latency"`
		EgressCostDataRate float64 `json:"egressCost_dataRate"`
	}

	var providerList []providerAttrStruct
	// need to consider all zones of all providers
	for _, p := range providers.Items {
		for _, p2 := range providers.Items {

			for _, pz1 := range p.Spec.Zones {
				if !pz1.Enabled {continue}
				for _, pz2 := range p2.Spec.Zones {
					if !pz2.Enabled {continue}

					p1Name := p.Spec.Platform + "-" + p.Spec.Region + "-" + pz1.Name
					p2Name := p2.Spec.Platform + "-" + p2.Spec.Region + "-" + pz2.Name

					samePlatform := p.Spec.Platform == p2.Spec.Platform
					sameRegion := p.Spec.Region == p2.Spec.Region

					// consider inter-zone latencies as negligible
					// consider inter-zone costs as 0.01 per GB and 

					if samePlatform && sameRegion { 
						// same regions: intra-zone latency is zero, cost are 0 if same zones
						egressCostStr := lo.Filter(p.Status.EgressCostSpecs, func(t cv1a1.EgressCostSpec, _ int) bool {
							return t.Type == "inter-zone"
						})[0].Tiers[0].PricePerGB
						egressCostFloat, err := strconv.ParseFloat(egressCostStr, 64)
						if err != nil {
							return "", fmt.Errorf("failed to parse inter-zone egress cost for providers %s and %s", p.Name, p2.Name)
						}
						providerList = append(providerList, providerAttrStruct{
							SrcName: 	p1Name,
							DstName: 	p2Name,
							Src: providerStruct{
								Platform:    p.Spec.Platform,
								Region:      p.Spec.Region,
								Zone:        pz1.Name,
							},
							Dst: providerStruct{
								Platform:    p2.Spec.Platform,
								Region:      p2.Spec.Region,
								Zone:        pz2.Name,
							},
							Latency: 	0.0,
							EgressCostDataRate: lo.Ternary(pz1.Name == pz2.Name, 0.0, egressCostFloat),
						})
						continue
					}
					
					// assuming we are within the first tier of egress cost specs
					// TODO: fix tier selection based on the maount of data transfer
					egressCostStr := "0.0"
					if samePlatform { // inter-region egress cost
						egressCostStr = lo.Filter(p.Status.EgressCostSpecs, func(t cv1a1.EgressCostSpec, _ int) bool {
							return t.Type == "inter-region"
						})[0].Tiers[0].PricePerGB
					} else {
						egressCostStr = lo.Filter(p.Status.EgressCostSpecs, func(t cv1a1.EgressCostSpec, _ int) bool {
							return t.Type == "internet"
						})[0].Tiers[0].PricePerGB
					}
					
					aName := p.Spec.Platform + "-" + p.Spec.Region
					bName := p2.Spec.Platform + "-" + p2.Spec.Region
					a, b := utils.CanonicalPair(aName, bName)
					
					var lt *cv1a1.Latency
					for _, lat := range latencies.Items {
						if lat.Labels["skycluster.io/provider-pair"] == utils.SanitizeName(a)+"-"+utils.SanitizeName(b) {
							lt = &lat
							break
						}
					}

					if lt == nil {
						return "", fmt.Errorf("no latency object found for provider pair %s - %s", a, b)
					}

					var latencyValue string
					if lt.Status.P95 != "" {
						latencyValue = lt.Status.P95
					} else if lt.Status.P99 != "" {
						latencyValue = lt.Status.P99
					} else if lt.Status.LastMeasuredMs != "" {
						latencyValue = lt.Status.LastMeasuredMs
					} else {
						return "", fmt.Errorf("no latency value found in latency object %s", lt.Name)
					}
					latencyValueFloat, err1 := strconv.ParseFloat(latencyValue, 64)
					egressCostFloat, err2 := strconv.ParseFloat(egressCostStr, 64)
					if err1 != nil || err2 != nil {
						return "", fmt.Errorf("failed to parse latency or egress cost to float for providers %s and %s", p.Name, p2.Name)
					}

					providerList = append(providerList, providerAttrStruct{
						SrcName: 	p1Name,
						DstName: 	p2Name,
						Src: providerStruct{
							Platform:    p.Spec.Platform,
							Region:      p.Spec.Region,
						},
						Dst: providerStruct{
							Platform:    p2.Spec.Platform,
							Region:      p2.Spec.Region,
						},	
						Latency: 	latencyValueFloat,
						EgressCostDataRate: egressCostFloat,
					})

				}
			}
		}
	}
	b, err := json.Marshal(providerList)
	if err != nil {
		return "", fmt.Errorf("failed to marshal providers: %v", err)
	}
	return string(b), nil
}

// calculateMinComputeResource returns the minimum compute resource required for a deployment
// based on all its containers' resources
func (r *ILPTaskReconciler) calculateMinComputeResource(ns string, deployName string) (*computeProfileService, error) {
	// We proceed with structured objects for simplicity instead of
	// unsctructured objects
	depObj := &appsv1.Deployment{}
	if err := r.Get(context.TODO(), client.ObjectKey{
		Namespace: ns, Name: deployName,
	}, depObj); err != nil {
		return nil, errors.Wrap(err, "error getting deployment: " + deployName)
	}
	// Each deployment has a single pod but may contain multiple containers
	// For each deployment (and subsequently each pod) we dervie the
	// total cpu and memory for all its containers
	allContainers := []corev1.Container{}
	allContainers = append(allContainers, depObj.Spec.Template.Spec.Containers...)
	allContainers = append(allContainers, depObj.Spec.Template.Spec.InitContainers...)
	totalCPU, totalMem := 0.0, 0.0
	for _, container := range allContainers {
		cpu, mem := getContainerComputeResources(container)
		totalCPU += cpu
		totalMem += mem
	}
	return &computeProfileService{name: deployName, cpu: totalCPU, ram: totalMem}, nil
}

// dataMap contains the application and provider data (i.e., tasks.json, providers.json, etc.)
func (r *ILPTaskReconciler) buildOptimizationPod(taskMeta *cv1a1.ILPTask, scripts map[string]string, dataMap map[string]string) (string, error) {

	if len(scripts) == 0 || len(dataMap) == 0 {
		return "", fmt.Errorf("optimization scripts or data configmap is empty")
	}

	scriptFiles := make([]string, 0)
	for file, content := range scripts {
		// Use a heredoc for safe multiline content
    heredoc := fmt.Sprintf("cat << 'EOF' > /scripts/%s\n%s\nEOF", file, content)
    scriptFiles = append(scriptFiles, heredoc)
	}
	initCommandForScripts := strings.Join(scriptFiles, "\n")
	
	// Split into separate init containers since some files may be large
	filesMap := make(map[string]string)
	for file, content := range dataMap {
		// Use a heredoc for safe multiline content
    heredoc := fmt.Sprintf("cat << 'EOF' > /shared/%s\n%s\nEOF", file, content)
    filesMap[file] = heredoc
	}
	
	initContainers := make([]corev1.Container, 0)
	initContainers = append(initContainers, corev1.Container{
		Name:  "prepare-vservices",
		Image: "etesami/optimizer-helper:v0.0.4",
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command: []string{"/bin/sh", "-c"},
		Args:    []string{"/vservices"},
		Env: []corev1.EnvVar{
			{
				Name:  "OUTPUT_PATH",
				Value: "/shared/virtual-services.json",
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "shared",
				MountPath: "/shared",
			},
		},
	})
	for file, content := range filesMap {
		initContainers = append(initContainers, corev1.Container{
			Name:  "prepare-" + strings.ReplaceAll(file, ".", "-"),
			Image: "busybox",
			Command: []string{"/bin/sh", "-c"},
			Args:    []string{content},
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      "shared",
					MountPath: "/shared",
				},
				{
					Name:      "scripts",
					MountPath: "/scripts",
				},
			},
		})
	}
	initContainers = append(initContainers, corev1.Container{
		Name:  "prepare-scripts",
		Image: "busybox",
		Command: []string{"/bin/sh","-c"},
		Args:    []string{initCommandForScripts},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "shared",
				MountPath: "/shared",
			},
			{
				Name:      "scripts",
				MountPath: "/scripts",
			},
		},
	},)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: taskMeta.Name + "-",
			Namespace:    hv1a1.SKYCLUSTER_NAMESPACE,
			Labels: map[string]string{
				"skycluster.io/managed-by": "skycluster",
				"skycluster.io/component":  "optimization",
				"ilptask": taskMeta.Name, 
			},
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: "skycluster-sva",
			RestartPolicy:      corev1.RestartPolicyNever,
			InitContainers: 	 initContainers,
			Containers: []corev1.Container{
				{
					Name:  "ubuntu-python",
					Image: "etesami/optimizer-engine:latest",
					ImagePullPolicy: corev1.PullAlways,
					Command: []string{
						"/bin/bash", "-c",
					},
					Args: []string{"chmod +x /scripts/start.sh && /scripts/start.sh"},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "shared",
							MountPath: "/shared",
						},
						{
							Name:      "scripts",
							MountPath: "/scripts",
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "scripts",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
				{
					Name: "shared",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: "skycluster-pvc",
						},
					},
				},
			},
		},
	}
	// The pod is in skycluster namespace
	// The ilptask is in the default namespace, and cross namespace reference is not allowed
	
	// 1) Check for any existing optimization pod for this ILPTask (someone else may have created it)
	podFound := false
	var podList corev1.PodList
	if err := r.List(context.TODO(), &podList,
		client.InNamespace(hv1a1.SKYCLUSTER_NAMESPACE),
		client.MatchingLabels{"skycluster.io/component": "optimization", "ilptask": taskMeta.Name},
	); err == nil {
		for _, p := range podList.Items {
			// prefer a pod that is not terminating
			if p.DeletionTimestamp == nil {
				r.Logger.Info("Found existing optimization pod for ILPTask; reusing", "pod", p.Name)
				podFound = true
				pod = &p
				break
			}
		}
	}
	
	if !podFound {
		if err := r.Create(context.TODO(), pod); err != nil { return "", err }
		r.Logger.Info("Created new optimization pod for ILPTask", "pod", pod.Name)
	}

	// 3) Attempt to "claim" the pod by patching the ILPTask status (so only one reconcile will succeed)
	// Fetch the ILPTask as it is on the server
	orig := &cv1a1.ILPTask{}
	if err := r.Get(context.TODO(), client.ObjectKey{
		Namespace: taskMeta.Namespace,
		Name:      taskMeta.Name,
	}, orig); err != nil { // Cannot fetch ILPTask to claim; best-effort cleanup created pod
		_ = r.Delete(context.TODO(), pod)
		return "", errors.Wrapf(err, "failed to get ILPTask %s/%s to claim pod; deleted created pod", taskMeta.Namespace, taskMeta.Name)
	}


	if orig.Status.Optimization.PodRef.Name != "" {
			// Another reconcile already claimed a pod; delete what we created and return the claimed pod
			r.Logger.Info("ILPTask already has a claimed optimization pod; cleaning up created pod", "claimedPod", orig.Status.Optimization.PodRef.Name, "createdPod", pod.Name)
			_ = r.Delete(context.TODO(), pod)
			return orig.Status.Optimization.PodRef.Name, nil
	}

	// Patch the ILPTask status to set the pod ref using MergeFrom(orig) to detect conflicts
	updated := orig.DeepCopy()
	updated.Status.Optimization.PodRef = corev1.LocalObjectReference{Name: pod.Name}

	if err := r.Status().Patch(context.TODO(), updated, client.MergeFrom(orig)); err != nil {
		// Conflict or other error while claiming; delete created pod and return error
		_ = r.Delete(context.TODO(), pod)
		return "", errors.Wrap(err, "conflict while claiming pod and update the status. Deleted created pod")
	}

	r.Logger.Info("Successfully claimed optimization pod for ILPTask", "pod", pod.Name)
	return pod.Name, nil
}

func (r *ILPTaskReconciler) getOptimizationConfigMaps() (map[string]string, error) {
	scripts := make(map[string]string)
	var configMapList corev1.ConfigMapList
	if err := r.List(context.TODO(), &configMapList, client.MatchingLabels{
		"skycluster.io/managed-by":  "skycluster",
		hv1a1.SKYCLUSTER_CONFIGTYPE_LABEL: "optimization-scripts",
	}); err != nil { return nil, err }

	if len(configMapList.Items) == 0 {
		return nil, errors.New("no configmap found (optimization-starter)")
	}

	for _, cm := range configMapList.Items {
		for key, val := range cm.Data {
			scripts[key] = val
		}
	}
	
	return scripts, nil
}

// Returns:
// - podStatus: The current status of the pod.
// - optimizationStatus: The status of the optimization process.
// - deploymentPlan: The deployment plan details.
// - error: An error object if an error occurred, otherwise nil.
func (r *ILPTaskReconciler) getPodStatusAndResult(podName string) (deployPlan string, err error) {
	pod := &corev1.Pod{}
	if err := r.Get(context.TODO(), client.ObjectKey{
		Namespace: hv1a1.SKYCLUSTER_NAMESPACE,
		Name:      podName,
	}, pod); err != nil {
		return "", err
	}
	if pod.Status.Phase == corev1.PodSucceeded {
		// get the result from the configmap
		configMap := &corev1.ConfigMap{}
		if err := r.Get(context.TODO(), client.ObjectKey{
			Namespace: hv1a1.SKYCLUSTER_NAMESPACE,
			Name:      "deploy-plan-config",
		}, configMap); err != nil {
			return "", err
		}
		// The result of the optimization could be Optimal or Infeasible
		return configMap.Data["deploy-plan.json"], nil
	}
	// When the pod is not completed yet or not succeeded
	// there is no result to return except the pod status
	return "", nil
}

func (r *ILPTaskReconciler) removeOptimizationPod(ctx context.Context, taskName string) error {
	// Delete the optimization pod best effort
	pod := &corev1.PodList{}
	if err := r.List(ctx, pod, client.MatchingLabels{
		"skycluster.io/component": "optimization",
		"ilptask": taskName,
	}); err != nil {
		return err
	}
	if len(pod.Items) == 0 {
		return nil
	}
	for _, p := range pod.Items {
		if err := r.Delete(ctx, &p); err != nil {
			return err
		}
	}
	return nil
}

func generateTasksEdgesJson(df pv1a1.DataflowPolicy) (string, error) {
	type taskEdgeStruct struct {
		SrcTask          string `json:"srcTask,omitempty"`
		DstTask          string `json:"dstTask,omitempty"`
		Latency         string `json:"latency,omitempty"`
		DataRate       string `json:"dataRate,omitempty"`
	}
	var taskEdges []taskEdgeStruct
	for _, df := range df.Spec.DataDependencies {
		taskEdges = append(taskEdges, taskEdgeStruct{
			SrcTask:  df.From.Name,
			DstTask:  df.To.Name,
			Latency:  df.Latency,
			DataRate: df.TotalDataTransfer,
		})
	}
	
	if len(taskEdges) == 0 {return "", nil}
	
	b, err := json.Marshal(taskEdges)
	if err != nil {
		return "", fmt.Errorf("failed to marshal optimization task edges: %v", err)
	}
	return string(b), nil
}
