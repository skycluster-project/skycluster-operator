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
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	hv1a1 "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"
	pv1a1 "github.com/skycluster-project/skycluster-operator/api/policy/v1alpha1"
	cv1a1ctrl "github.com/skycluster-project/skycluster-operator/internal/controller/core"
	cv1a1ppctrl "github.com/skycluster-project/skycluster-operator/internal/controller/core/providerprofile"
	coreutils "github.com/skycluster-project/skycluster-operator/internal/controller/core/utils"
	pv1a1ctrl "github.com/skycluster-project/skycluster-operator/internal/controller/policy"
	pkglog "github.com/skycluster-project/skycluster-operator/pkg/v1alpha1/log"
)

var _ = Describe("ILPTask Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"
		const namespace = "skycluster-system"

		ctx := context.Background()

		var ilptask *cv1a1.ILPTask
		var dfPolicy *pv1a1.DataflowPolicy
		var dpPolicy *pv1a1.DeploymentPolicy

		BeforeEach(func() {
		})

		AfterEach(func() {
			By("Cleanup the specific resource instance DataflowPolicy")
			Expect(k8sClient.Delete(ctx, dfPolicy)).To(Succeed())

			By("Cleanup the specific resource instance DeploymentPolicy")
			Expect(k8sClient.Delete(ctx, dpPolicy)).To(Succeed())

			By("Cleanup the specific resource instance ILPTask")
			Expect(k8sClient.Delete(ctx, ilptask)).To(Succeed())

			By("Cleanup the deployments")
			deployments := &appsv1.DeploymentList{}
			Expect(k8sClient.List(ctx, deployments, client.InNamespace(namespace), client.MatchingLabels{
				"skycluster.io/app-scope": "distributed",
			})).To(Succeed())
			for _, deployment := range deployments.Items {
				Expect(k8sClient.Delete(ctx, &deployment)).To(Succeed())
			}

			By("Ensure the ilptask object is deleted")
			Eventually(func() bool {

				ilptask := &cv1a1.ILPTask{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: namespace}, ilptask)
				return errors.IsNotFound(err)
			}, time.Second*5, time.Millisecond*100).Should(BeTrue(), "ILPTask should be deleted before next test starts")
		})

		It("should successfully generate the deployment plan for VirtualMachine execution environment", func() {
			By("generating deployment policy and dataflow policy")
			dpPolicy, dfPolicy = createPoliciesForVMExecEnv(resourceName, namespace, providerprofileAWS)
			Expect(k8sClient.Create(ctx, dpPolicy)).To(Succeed())
			Expect(k8sClient.Create(ctx, dfPolicy)).To(Succeed())
			ilptask = prepareILPTask(dpPolicy, dfPolicy)

			// run reconciler for ilptask and check the status
			ilptaskReconciler := getILPTaskReconciler()
			res, err := ilptaskReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ilptask.Name, Namespace: ilptask.Namespace},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(res.RequeueAfter).To(Equal(3 * time.Second))

			ilpOut := &cv1a1.ILPTask{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: ilptask.Name, Namespace: ilptask.Namespace}, ilpOut)
			Expect(err).NotTo(HaveOccurred())

			By("checking the ilptask pod is created")
			podList := &corev1.PodList{}
			Expect(k8sClient.List(ctx, podList, client.InNamespace(namespace), client.MatchingLabels{
				"skycluster.io/managed-by": "skycluster",
				"skycluster.io/component":  "optimization",
				"ilptask":                  resourceName,
			})).To(Succeed())
			Expect(len(podList.Items)).To(Equal(1))
			Expect(podList.Items[0].Name).To(Equal(ilpOut.Status.Optimization.PodRef.Name))

			By("checking the requeue after 3 seconds")
			Expect(res.RequeueAfter).To(Equal(3 * time.Second))
		})

		It("should correctly generate tasks.json for VirtualMachine execution environment", func() {
			By("generating deployment policy and dataflow policy")
			dpPolicy, dfPolicy = createPoliciesForVMExecEnv(resourceName, namespace, providerprofileAWS)
			Expect(k8sClient.Create(ctx, dpPolicy)).To(Succeed())
			Expect(k8sClient.Create(ctx, dfPolicy)).To(Succeed())

			ilptask = prepareILPTask(dpPolicy, dfPolicy)
			// run reconciler for ilptask and check the status
			ilptaskReconciler := getILPTaskReconciler()
			_, err := ilptaskReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: dpPolicy.Name, Namespace: namespace},
			})
			Expect(err).NotTo(HaveOccurred())

			By("fetching the candidate providers for aws us-east-1")
			candidates, err := ilptaskReconciler.getCandidateProviders([]cv1a1.ProviderProfileSpec{
				{
					Platform:    "aws",
					Region:      "us-east-1",
					RegionAlias: "us-east",
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(len(candidates)).To(Equal(1))

			By("fetching the compute profiles for the candidate provider")
			// two ComputeProfile are available for aws us-east-1
			cmProfiles, err := ilptaskReconciler.getComputeProfileForProvider(candidates[0])
			Expect(err).NotTo(HaveOccurred())
			Expect(len(cmProfiles)).To(Equal(4))

			By("fetching the compute profiles for the provider reference")
			cmProfiles, err = ilptaskReconciler.getAllComputeProfiles([]hv1a1.ProviderRefSpec{
				{
					Name:     providerprofileAWS.Name,
					Type:     "cloud",
					Region:   "us-east-1",
					Platform: "aws",
				},
			})
			Expect(err).NotTo(HaveOccurred())
			// four compute profiles are defined for aws us-east-1
			Expect(len(cmProfiles)).To(Equal(4))

			// iterate over the deployment policies and find the virtual services
			// only component with kind XInstance is supported
			// and the virtual service constraint must be only ComputeProfile
			// Except one ComputeProfile according to the deployment policy
			By("finding the virtual services for the deployment policy")
			optTasks, err := ilptaskReconciler.findVMVirtualServices(dpPolicy.Spec.DeploymentPolicies)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(optTasks)).To(Equal(1))
			Expect(optTasks[0].Kind).To(Equal("XInstance"))
			Expect(optTasks[0].RequestedVServices).To(Equal([][]hv1a1.VirtualServiceSelector{
				{
					{
						Name:       "64vCPU-256GB-1xA10G-22GB",
						ApiVersion: "skycluster.io/v1alpha1",
						Kind:       "ComputeProfile",
						Count:      "1",
						Price:      4.10,
					},
				},
			}))
			// Expect the permitted locations to be the same as the deployment policy
			Expect(optTasks[0].PermittedLocations).To(Equal([]locStruct{
				{
					Name:     providerprofileAWS.Name,
					PType:    "cloud",
					Region:   "us-east-1",
					Platform: "aws",
				},
			}))
			// and no required locations
			Expect(optTasks[0].RequiredLocations).To(Equal([][]locStruct{}))

			// finally generate the tasks.json and verify it
			By("generating tasks.json and verifying it")
			tasksJson, err := ilptaskReconciler.generateTasksJson(*dpPolicy)
			Expect(err).NotTo(HaveOccurred())
			Expect(tasksJson).NotTo(BeEmpty())
			Expect(tasksJson).To(MatchJSON(getOptTaskJsonVM(resourceName, providerprofileAWS.Name)))
		})

		It("should correctly generate tasks.json for Kubernetes execution environment", func() {
			By("generating deployment policy and dataflow policy")
			dpPolicy, dfPolicy = createPoliciesForK8sExecEnv(resourceName, namespace)
			Expect(k8sClient.Create(ctx, dpPolicy)).To(Succeed())
			Expect(k8sClient.Create(ctx, dfPolicy)).To(Succeed())

			ilptask = prepareILPTask(dpPolicy, dfPolicy)
			// run reconciler for ilptask and check the status
			ilptaskReconciler := getILPTaskReconciler()
			_, err := ilptaskReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: dpPolicy.Name, Namespace: namespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// iterate over the deployment policies and find the virtual services
			// only component with kind XInstance is supported
			// and the virtual service constraint must be only ComputeProfile
			// Except one ComputeProfile according to the deployment policy
			By("finding the virtual services for the deployment policy")
			optTasks, err := ilptaskReconciler.findK8SVirtualServices(namespace, dpPolicy.Spec.DeploymentPolicies)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(optTasks)).To(Equal(1))
			Expect(optTasks[0].Kind).To(Equal("XNodeGroup"))
			// Expect to include all ComputeProfiles for the XNodeGroup
			// 2 for aws and 2 for gcp
			Expect(optTasks[0].RequestedVServices).To(Equal([][]hv1a1.VirtualServiceSelector{
				{
					{
						Name:       "12vCPU-49GB-1xL4-24GB",
						ApiVersion: "skycluster.io/v1alpha1",
						Kind:       "ComputeProfile",
						Count:      "1",
						Price:      0.4893,
					},
					{
						Name:       "48vCPU-192GB-4xA10G-22GB",
						ApiVersion: "skycluster.io/v1alpha1",
						Kind:       "ComputeProfile",
						Count:      "1",
						Price:      5.67,
					},
				},
			}))
			// Expect the permitted locations to be the same as the deployment policy,
			// no permitted locations means all, which is handled by the optimizer
			Expect(optTasks[0].PermittedLocations).To(Equal([]locStruct{}))
			// and no required locations
			Expect(optTasks[0].RequiredLocations).To(Equal([][]locStruct{}))

			// finally generate the tasks.json and verify it
			By("generating tasks.json and verifying it")
			tasksJson, err := ilptaskReconciler.generateTasksJson(*dpPolicy)
			Expect(err).NotTo(HaveOccurred())
			Expect(tasksJson).NotTo(BeEmpty())
			Expect(tasksJson).To(MatchJSON(getOptTaskJsonK8s(resourceName)))
		})

		It("should correctly generate tasks.json for Kubernetes execution environment with multiple VS constraints", func() {
			By("generating deployment policy and dataflow policy")
			dpPolicy, dfPolicy = createPoliciesForK8sExecEnvMultipleVSConstraints(resourceName, namespace)
			Expect(k8sClient.Create(ctx, dpPolicy)).To(Succeed())
			Expect(k8sClient.Create(ctx, dfPolicy)).To(Succeed())

			ilptask = prepareILPTask(dpPolicy, dfPolicy)
			// run reconciler for ilptask and check the status
			ilptaskReconciler := getILPTaskReconciler()
			_, err := ilptaskReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: dpPolicy.Name, Namespace: namespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// iterate over the deployment policies and find the virtual services
			// only component with kind XInstance is supported
			// and the virtual service constraint must be only ComputeProfile
			// Except one ComputeProfile according to the deployment policy
			By("finding the virtual services for the deployment policy")
			optTasks, err := ilptaskReconciler.findK8SVirtualServices(namespace, dpPolicy.Spec.DeploymentPolicies)
			Expect(err).NotTo(HaveOccurred())
			// Expect(len(optTasks)).To(Equal(1))
			Expect(optTasks[0].Kind).To(Equal("XNodeGroup"))
			// Expect to include all ComputeProfiles for the XNodeGroup
			// 2 for aws and 2 for gcp
			// Expect(optTasks[0].RequestedVServices[0]).To(HaveLen(4))
			Expect(optTasks[0].RequestedVServices).To(Equal([][]hv1a1.VirtualServiceSelector{
				{
					{
						Name:       "48vCPU-192GB-4xA10G-22GB",
						ApiVersion: "skycluster.io/v1alpha1",
						Kind:       "ComputeProfile",
						Count:      "1",
						Price:      5.67,
					},
				},
				{
					{
						Name:       "12vCPU-49GB-1xL4-24GB",
						ApiVersion: "skycluster.io/v1alpha1",
						Kind:       "ComputeProfile",
						Count:      "1",
						Price:      0.4893,
					},
				},
			}))
			// Expect the permitted locations to be the same as the deployment policy,
			// no permitted locations means all, which is handled by the optimizer
			Expect(optTasks[0].PermittedLocations).To(Equal([]locStruct{}))
			// and no required locations
			Expect(optTasks[0].RequiredLocations).To(Equal([][]locStruct{}))

			// finally generate the tasks.json and verify it
			By("generating tasks.json and verifying it")
			tasksJson, err := ilptaskReconciler.generateTasksJson(*dpPolicy)
			Expect(err).NotTo(HaveOccurred())
			Expect(tasksJson).NotTo(BeEmpty())
		})

		It("should correctly generate tasks.json for Kubernetes execution environment with Deployment components", func() {
			By("generating deployment policy and dataflow policy")
			dpPolicy, dfPolicy = createPoliciesForK8sExecEnvWithDeployment(resourceName, namespace)
			Expect(k8sClient.Create(ctx, dpPolicy)).To(Succeed())
			Expect(k8sClient.Create(ctx, dfPolicy)).To(Succeed())

			By("creating a sample deployment")
			deployment := createSampleDeployment(resourceName, "sample-deployment", namespace)
			Expect(k8sClient.Create(ctx, deployment)).To(Succeed())

			ilptask = prepareILPTask(dpPolicy, dfPolicy)
			// run reconciler for ilptask and check the status
			ilptaskReconciler := getILPTaskReconciler()
			_, err := ilptaskReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: dpPolicy.Name, Namespace: namespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// iterate over the deployment policies and find the virtual services
			// only component with kind XInstance is supported
			// and the virtual service constraint must be only ComputeProfile
			// Except one ComputeProfile according to the deployment policy
			By("finding the virtual services for the deployment policy")
			optTasks, err := ilptaskReconciler.findK8SVirtualServices(namespace, dpPolicy.Spec.DeploymentPolicies)
			Expect(err).NotTo(HaveOccurred())

			// expect containing one compute profile with gpu model L4
			Expect(optTasks[0].RequestedVServices).To(Equal([][]hv1a1.VirtualServiceSelector{
				{
					{
						Name:       "12vCPU-49GB-1xL4-24GB",
						Spec:       nil,
						ApiVersion: "skycluster.io/v1alpha1",
						Kind:       "ComputeProfile",
						Count:      "1",
						Price:      0.4893,
					},
				},
			}))
			Expect(optTasks[0].Kind).To(Equal("Deployment"))
		})

		It("should correctly generate tasks.json for Kubernetes execution environment with Deployment components with multiple VS constraints, and with XNodeGroup", func() {
			By("generating deployment policy and dataflow policy")
			dpPolicy, dfPolicy = createPoliciesForK8sExecEnvMultipleVSConstraintsWithDeployment(resourceName, namespace)
			Expect(k8sClient.Create(ctx, dpPolicy)).To(Succeed())
			Expect(k8sClient.Create(ctx, dfPolicy)).To(Succeed())

			By("creating a sample deployment")
			deployment := createSampleDeployment(resourceName, "deployment1", namespace)
			Expect(k8sClient.Create(ctx, deployment)).To(Succeed())

			ilptask = prepareILPTask(dpPolicy, dfPolicy)
			// run reconciler for ilptask and check the status
			ilptaskReconciler := getILPTaskReconciler()
			_, err := ilptaskReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: dpPolicy.Name, Namespace: namespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// iterate over the deployment policies and find the virtual services
			// only component with kind XInstance is supported
			// and the virtual service constraint must be only ComputeProfile
			// Except one ComputeProfile according to the deployment policy
			By("finding the virtual services for the deployment policy")
			optTasks, err := ilptaskReconciler.findK8SVirtualServices(namespace, dpPolicy.Spec.DeploymentPolicies)
			Expect(err).NotTo(HaveOccurred())

			// expect containing one compute profile with gpu model L4
			Expect(optTasks).To(Equal([]optTaskStruct{
				{
					Task:               "test-resource",
					ApiVersion:         "skycluster.io/v1alpha1",
					Kind:               "XNodeGroup",
					PermittedLocations: []locStruct{},
					RequiredLocations:  [][]locStruct{},
					RequestedVServices: [][]hv1a1.VirtualServiceSelector{
						{
							{
								Name:       "48vCPU-192GB-4xA10G-22GB",
								ApiVersion: "skycluster.io/v1alpha1",
								Kind:       "ComputeProfile",
								Count:      "1",
								Price:      5.67,
							},
						},
						{
							{
								Name:       "12vCPU-49GB-1xL4-24GB",
								ApiVersion: "skycluster.io/v1alpha1",
								Kind:       "ComputeProfile",
								Count:      "1",
								Price:      0.4893,
							},
						},
					},
					MaxReplicas: "-1",
				},
				{
					Task:               "deployment1",
					ApiVersion:         "apps/v1",
					Kind:               "Deployment",
					PermittedLocations: []locStruct{},
					RequiredLocations:  [][]locStruct{},
					RequestedVServices: [][]hv1a1.VirtualServiceSelector{
						{
							{
								Name:       "12vCPU-49GB-1xL4-24GB",
								ApiVersion: "skycluster.io/v1alpha1",
								Kind:       "ComputeProfile",
								Count:      "1",
								Price:      0.4893,
							},
						},
					},
					MaxReplicas: "-1",
				},
			}))

		})

		// TODO: test with flavors with spot offering enabled and disabled

		It("checking the generated providers.json for Kubernetes execution environment", func() {
			By("generating deployment policy and dataflow policy")
			dpPolicy, dfPolicy = createPoliciesForK8sExecEnv(resourceName, namespace)
			Expect(k8sClient.Create(ctx, dpPolicy)).To(Succeed())
			Expect(k8sClient.Create(ctx, dfPolicy)).To(Succeed())

			ilptask = prepareILPTask(dpPolicy, dfPolicy)
			// run reconciler for ilptask and check the status
			ilptaskReconciler := getILPTaskReconciler()
			_, err := ilptaskReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: dpPolicy.Name, Namespace: namespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// finally generate the providers.json and verify it
			// we expect all registered providers to be included
			By("generating providers.json and verifying it")
			providersJson, err := ilptaskReconciler.generateProvidersJson()
			Expect(err).NotTo(HaveOccurred())
			Expect(providersJson).NotTo(BeEmpty())
			Expect(providersJson).To(MatchJSON(getProvidersJson(providerprofileAWS, providerprofileGCP)))

			By("generating providers-attr.json and verifying it")
			providersAttrJson, err := ilptaskReconciler.generateProvidersAttrJson(namespace)
			Expect(err).NotTo(HaveOccurred())
			Expect(providersAttrJson).NotTo(BeEmpty())
			Expect(providersAttrJson).To(MatchJSON(getProvidersAttrJson(namespace, providerprofileAWS, providerprofileGCP)))
		})

		It("should correctly fetch the result of optimization from the optimization pod", func() {
			By("generating deployment policy and dataflow policy")
			dpPolicy, dfPolicy = createPoliciesForK8sExecEnvWithMultipleDeployment(resourceName, namespace)
			Expect(k8sClient.Create(ctx, dpPolicy)).To(Succeed())
			Expect(k8sClient.Create(ctx, dfPolicy)).To(Succeed())

			By("creating 3 sample deployments")
			for i := range 3 {
				deploy := createSampleDeployment(resourceName, "deployment"+strconv.Itoa(i+1), namespace)
				Expect(k8sClient.Create(ctx, deploy)).To(Succeed())
			}

			By("preparing ilptask")
			ilptask = prepareILPTask(dpPolicy, dfPolicy)
			// set pod name in ilptask status
			ilptask.Status.Optimization.PodRef = corev1.LocalObjectReference{Name: "opt-pod"}
			Expect(k8sClient.Status().Update(ctx, ilptask)).To(Succeed())

			By("creating opt pod")
			optPod := createOptPod("opt-pod", namespace)
			Expect(k8sClient.Create(ctx, optPod)).To(Succeed())
			// set pod status to succeeded
			optPod.Status.Phase = corev1.PodSucceeded
			Expect(k8sClient.Status().Update(ctx, optPod)).To(Succeed())

			By("creating configmap with deploy plan (as optimization result)")
			configMap := configMapForOptimizationResult(
				"deploy-plan-config", namespace, providerprofileAWS, providerprofileGCP,
			)
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

			deployPlan := cv1a1.DeployMap{}
			err := json.Unmarshal([]byte(configMap.Data["deploy-plan.json"]), &deployPlan)
			Expect(err).NotTo(HaveOccurred())

			By("finding the required services for the deploy plan")
			ilptaskReconciler := getILPTaskReconciler()
			requiredServices, err := ilptaskReconciler.findServicesForDeployPlan(namespace, deployPlan, *dpPolicy)
			Expect(err).NotTo(HaveOccurred())
			Expect(requiredServices).To(Equal(map[string][][]hv1a1.VirtualServiceSelector{
				"deployment1": {
					{
						{
							Name:       "12vCPU-49GB-1xL4-24GB",
							ApiVersion: "skycluster.io/v1alpha1",
							Kind:       "ComputeProfile",
							Count:      "1",
							Price:      0.4893,
						},
					},
				},
				"deployment2": {
					{
						{
							Name:       "64vCPU-256GB-1xA10G-22GB",
							Spec:       nil,
							ApiVersion: "skycluster.io/v1alpha1",
							Kind:       "ComputeProfile",
							Count:      "1",
							Price:      4.1,
						},
						{
							Name:       "48vCPU-192GB-4xA10G-22GB",
							Spec:       nil,
							ApiVersion: "skycluster.io/v1alpha1",
							Kind:       "ComputeProfile",
							Count:      "1",
							Price:      5.67,
						},
					},
				},
				"deployment3": {
					{
						{
							Name:       "2vCPU-4GB",
							ApiVersion: "skycluster.io/v1alpha1",
							Kind:       "ComputeProfile",
							Count:      "1",
							Price:      0.05,
						},
						{
							Name:       "12vCPU-49GB-1xL4-24GB",
							ApiVersion: "skycluster.io/v1alpha1",
							Kind:       "ComputeProfile",
							Count:      "1",
							Price:      0.4893,
						},
						{
							Name:       "64vCPU-256GB-1xA10G-22GB",
							ApiVersion: "skycluster.io/v1alpha1",
							Kind:       "ComputeProfile",
							Count:      "1",
							Price:      4.1,
						},
						{
							Name:       "48vCPU-192GB-4xA10G-22GB",
							ApiVersion: "skycluster.io/v1alpha1",
							Kind:       "ComputeProfile",
							Count:      "1",
							Price:      5.67,
						},
					},
				},
			}))

			By("running reconciler for ilptask and checking the status")
			res, err := ilptaskReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: dpPolicy.Name, Namespace: namespace},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(res).To(Equal(ctrl.Result{}))

			// fetch ilptask and check the status
			ilptask := &cv1a1.ILPTask{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: dpPolicy.Name}, ilptask)).To(Succeed())
			Expect(ilptask.Status.Optimization.Status).To(Equal("Succeeded"))
			Expect(ilptask.Status.Optimization.DeployMap.Component[0].Manifest).ToNot(BeNil())
			Expect(ilptask.Status.Optimization.DeployMap.Component[1].Manifest).ToNot(BeNil())
			Expect(ilptask.Status.Optimization.DeployMap.Component[2].Manifest).ToNot(BeNil())

			By("checking the atlas and atlasmesh are created")
			atlasList := &cv1a1.AtlasList{}
			Expect(k8sClient.List(ctx, atlasList, client.InNamespace(namespace), client.MatchingLabels{
				"skycluster.io/app-id": resourceName,
			})).To(Succeed())
			Expect(len(atlasList.Items)).To(Equal(1))
			atlasMeshList := &cv1a1.AtlasMeshList{}
			Expect(k8sClient.List(ctx, atlasMeshList, client.InNamespace(namespace), client.MatchingLabels{
				"skycluster.io/app-id": resourceName,
			})).To(Succeed())
			Expect(len(atlasMeshList.Items)).To(Equal(1))

			// clean objects
			Expect(k8sClient.Delete(ctx, configMap)).To(Succeed())
			Expect(k8sClient.Delete(ctx, optPod)).To(Succeed())
		})

		It("should correctly handle pod not found", func() {
			By("generating deployment policy and dataflow policy")
			dpPolicy, dfPolicy = createPoliciesForK8sExecEnvWithMultipleDeployment(resourceName, namespace)
			Expect(k8sClient.Create(ctx, dpPolicy)).To(Succeed())
			Expect(k8sClient.Create(ctx, dfPolicy)).To(Succeed())

			By("creating 3 sample deployments")
			for i := range 3 {
				deploy := createSampleDeployment(resourceName, "deployment"+strconv.Itoa(i+1), namespace)
				Expect(k8sClient.Create(ctx, deploy)).To(Succeed())
			}

			By("preparing ilptask")
			ilptask = prepareILPTask(dpPolicy, dfPolicy)
			// set pod name in ilptask status
			ilptask.Status.Optimization.PodRef = corev1.LocalObjectReference{Name: "opt-pod"}
			Expect(k8sClient.Status().Update(ctx, ilptask)).To(Succeed())

			By("running reconciler for ilptask and checking the status")
			ilptaskReconciler := getILPTaskReconciler()
			_, err := ilptaskReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: dpPolicy.Name, Namespace: namespace},
			})
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("failed to get optimization pod"))

			// fetch ilptask and check the status
			ilptask := &cv1a1.ILPTask{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: dpPolicy.Name}, ilptask)).To(Succeed())
			Expect(ilptask.Status.Optimization.PodRef).To(Equal(corev1.LocalObjectReference{}))
		})

	})
})

func configMapForOptimizationResult(name, namespace string, pp1, pp2 *cv1a1.ProviderProfile) *corev1.ConfigMap {
	pp1Name := pp1.Spec.Platform + "-" + pp1.Spec.Region + "-" + pp1.Spec.Zones[0].Name
	pp2Name := pp2.Spec.Platform + "-" + pp2.Spec.Region + "-" + pp2.Spec.Zones[0].Name

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string]string{
			"deploy-plan.json": `{
				"cost": "0.9602",
				"deployCost": "0.9602",
				"transferCost": "0",
				"components": [
					{
						"name": "deployment1",
						"namespace": "default",
						"componentRef": {
							"apiVersion": "apps/v1",
							"kind": "Deployment",
							"name": "deployment1",
							"namespace": "default"
						},
						"providerRef": {
							"name": "` + pp1Name + `",
							"platform": "` + pp1.Spec.Platform + `",
							"type": "` + pp1.Spec.Zones[0].Type + `",
							"region": "` + pp1.Spec.Region + `"
						}
					},
					{
						"name": "deployment2",
						"namespace": "default",
						"componentRef": {
							"apiVersion": "apps/v1",
							"kind": "Deployment",
							"name": "deployment2",
							"namespace": "default"
						},
						"providerRef": {
							"name": "` + pp2Name + `",
							"platform": "` + pp2.Spec.Platform + `",
							"type": "` + pp2.Spec.Zones[0].Type + `",
							"region": "` + pp2.Spec.Region + `"
						}
					},
					{
						"name": "deployment3",
						"namespace": "default",
						"componentRef": {
							"apiVersion": "apps/v1",
							"kind": "Deployment",
							"name": "deployment3",
							"namespace": "default"
						},
						"providerRef": {
							"name": "` + pp1Name + `",
							"platform": "` + pp1.Spec.Platform + `",
							"type": "` + pp1.Spec.Zones[0].Type + `",
							"region": "` + pp1.Spec.Region + `"
						}
					}
				],
				"edges": [
					{
						"from": {
							"name": "deployment1",
							"namespace": "default"
						},
						"to": {
							"name": "deployment2",
							"namespace": "default"
						},
						"latency": "50ms"
					},
					{
						"from": {
							"name": "deployment2",
							"namespace": "default"
						},
						"to": {
							"name": "deployment3",
							"namespace": "default"
						},
						"latency": "50ms"
					}
				]
			}`,
		},
	}
}

func createOptPod(name, namespace string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "opt-container",
					Image: "opt-image",
				},
			},
		},
	}
}

func createSampleDeployment(appId, deployName, namespace string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deployName,
			Namespace: namespace,
			Labels: map[string]string{
				"skycluster.io/app-id":    appId,
				"skycluster.io/app-scope": "distributed",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "sample-deployment",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "sample-deployment",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "sample-container",
							Image: "sample-image",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("500m"),
									corev1.ResourceMemory: resource.MustParse("256Mi"),
								},
							},
						},
					},
				},
			},
		},
	}
}

func getProvidersAttrJson(ns string, ppAWS, ppGCP *cv1a1.ProviderProfile) string {
	// get updated provider profiles
	pp1 := &cv1a1.ProviderProfile{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: ppAWS.Name, Namespace: ns}, pp1)
	Expect(err).NotTo(HaveOccurred())
	pp2 := &cv1a1.ProviderProfile{}
	err = k8sClient.Get(ctx, types.NamespacedName{Name: ppGCP.Name, Namespace: ns}, pp2)
	Expect(err).NotTo(HaveOccurred())

	pp1Name := pp1.Spec.Platform + "-" + pp1.Spec.Region + "-" + pp1.Spec.Zones[0].Name
	pp2Name := pp2.Spec.Platform + "-" + pp2.Spec.Region + "-" + pp2.Spec.Zones[0].Name

	// need to get latency objects
	latencyReconciler := getLatencyReconciler()
	latencyList := &cv1a1.LatencyList{}
	err = latencyReconciler.List(ctx, latencyList, client.InNamespace(ns))
	Expect(err).NotTo(HaveOccurred())
	Expect(latencyList.Items).To(HaveLen(1))
	latency := latencyList.Items[0]
	// first 95 percentile latency is used if available
	latencyValue := latency.Status.P95

	// egress costs, first tier of internet egress cost is used
	egressCostDataRateAWS, err := pp1.Status.GetEgressCostDataRate(0, "internet")
	Expect(err).NotTo(HaveOccurred())
	egCostAws := strconv.FormatFloat(egressCostDataRateAWS, 'f', -1, 64)
	egressCostDataRateGCP, err := pp2.Status.GetEgressCostDataRate(0, "internet")
	Expect(err).NotTo(HaveOccurred())
	egCostGCP := strconv.FormatFloat(egressCostDataRateGCP, 'f', -1, 64)

	// Note zone only used for intra-zone egress cost
	providerAttr := `
		[{
			"srcName": "` + pp1Name + `",
			"dstName": "` + pp1Name + `",
			"src": {
				"platform": "aws",
				"zone": "us-east-1a",
				"region": "us-east-1"
			},
			"dst": {
				"platform": "aws",
				"zone": "us-east-1a",
				"region": "us-east-1"
			},
			"latency": 0,
			"egressCost_dataRate": 0
		},
		{
			"srcName": "` + pp1Name + `",
			"dstName": "` + pp2Name + `",
			"src": {
				"platform": "aws",
				"region": "us-east-1"
			},
			"dst": {
				"platform": "gcp",
				"region": "us-east1"
			},
			"latency": ` + latencyValue + `,
			"egressCost_dataRate": ` + egCostAws + `
		},
		{
			"srcName": "` + pp2Name + `",
			"dstName": "` + pp1Name + `",
			"src": {
				"platform": "gcp",
				"region": "us-east1"
			},
			"dst": {
				"platform": "aws",
				"region": "us-east-1"
			},
			"latency": ` + latencyValue + `,
			"egressCost_dataRate": ` + egCostGCP + `
		},
		{
			"srcName": "` + pp2Name + `",
			"dstName": "` + pp2Name + `",
			"src": {
				"platform": "gcp",
				"zone": "us-east1-a",
				"region": "us-east1"
			},
			"dst": {
				"platform": "gcp",
				"zone": "us-east1-a",
				"region": "us-east1"
			},
			"latency": 0,
			"egressCost_dataRate": 0
		}]`
	return providerAttr
}

func getProvidersJson(ppAWS, ppGCP *cv1a1.ProviderProfile) string {
	pps := []*cv1a1.ProviderProfile{ppAWS, ppGCP}
	var providers []string
	for _, pp := range pps {
		name := pp.Spec.Platform + "-" + pp.Spec.Region + "-" + pp.Spec.Zones[0].Name
		provider := fmt.Sprintf(`
			{
				"upstreamName": "%s",
				"name": "%s",
				"platform": "%s",
				"regionAlias": "%s",
				"zone": "%s",
				"pType": "cloud",
				"region": "%s"
			}
		`, pp.Name, name, pp.Spec.Platform, pp.Spec.RegionAlias, pp.Spec.Zones[0].Name, pp.Spec.Region)
		providers = append(providers, provider)
	}
	return fmt.Sprintf(`[%s]`, strings.Join(providers, ","))
}

func getOptTaskJsonK8s(resourceName string) string {
	return fmt.Sprintf(`[
		{
			"task": "%s",
			"apiVersion": "skycluster.io/v1alpha1",
			"kind": "XNodeGroup",
			"permittedLocations": [],
			"requiredLocations": [],
			"requestedVServices": [
				[
					{
						"name": "12vCPU-49GB-1xL4-24GB",
						"apiVersion": "skycluster.io/v1alpha1",
						"kind": "ComputeProfile",
						"count": "1",
						"price": 0.4893
					},
					{
						"name": "48vCPU-192GB-4xA10G-22GB",
						"apiVersion": "skycluster.io/v1alpha1",
						"kind": "ComputeProfile",
						"count": "1",
						"price": 5.67
					}
				]
			],
			"maxReplicas": "-1"
		}
	]`, resourceName)
}

func getOptTaskJsonVM(resourceName, ppName string) string {
	return fmt.Sprintf(`[
		{
			"task": "%s",
			"apiVersion": "skycluster.io/v1alpha1",
			"kind": "XInstance",
			"permittedLocations": [{
				"name": "%s",
				"pType": "cloud",
				"region": "us-east-1",
				"platform": "aws"
			}],
			"requiredLocations": [],
			"requestedVServices": [
				[
					{
						"name": "64vCPU-256GB-1xA10G-22GB",
						"apiVersion": "skycluster.io/v1alpha1",
						"kind": "ComputeProfile",
						"count": "1",
						"price": 4.10
					}
				]
			],
			"maxReplicas": "-1"
		}
	]`, resourceName, ppName)
}

func getProviderProfileReconciler() *cv1a1ppctrl.ProviderProfileReconciler {
	return &cv1a1ppctrl.ProviderProfileReconciler{
		Client: k8sClient,
		Scheme: k8sClient.Scheme(),
		Logger: zap.New(pkglog.CustomLogger()).WithName("[ProviderProfile]"),
	}
}

func getDeploymentPolicyReconciler() *pv1a1ctrl.DeploymentPolicyReconciler {
	return &pv1a1ctrl.DeploymentPolicyReconciler{
		Client: k8sClient,
		Scheme: k8sClient.Scheme(),
		Logger: zap.New(pkglog.CustomLogger()).WithName("[DeploymentPolicy]"),
	}
}

func getDataflowPolicyReconciler() *pv1a1ctrl.DataflowPolicyReconciler {
	return &pv1a1ctrl.DataflowPolicyReconciler{
		Client: k8sClient,
		Scheme: k8sClient.Scheme(),
		Logger: zap.New(pkglog.CustomLogger()).WithName("[DataflowPolicy]"),
	}
}

func getILPTaskReconciler() *ILPTaskReconciler {
	return &ILPTaskReconciler{
		Client: k8sClient,
		Scheme: k8sClient.Scheme(),
		Logger: zap.New(pkglog.CustomLogger()).WithName("[ILPTask]"),
	}
}

func getLatencyReconciler() *cv1a1ctrl.LatencyReconciler {
	return &cv1a1ctrl.LatencyReconciler{
		Client: k8sClient,
		Scheme: k8sClient.Scheme(),
		Logger: zap.New(pkglog.CustomLogger()).WithName("[Latency]"),
	}
}

func prepareILPTask(dpPolicy *pv1a1.DeploymentPolicy, dfPolicy *pv1a1.DataflowPolicy) *cv1a1.ILPTask {
	// run reconcilers for dataflowpolicy and deploymentpolicy
	// expect the ilptask object to be created
	dfReconciler := getDataflowPolicyReconciler()
	_, err := dfReconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: dfPolicy.Name, Namespace: dfPolicy.Namespace},
	})
	Expect(err).NotTo(HaveOccurred())

	dpReconciler := getDeploymentPolicyReconciler()
	_, err = dpReconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: dpPolicy.Name, Namespace: dpPolicy.Namespace},
	})
	Expect(err).NotTo(HaveOccurred())

	By("checking the ilptask object is created")
	ilptask := &cv1a1.ILPTask{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: dfPolicy.Name, Namespace: dpPolicy.Namespace}, ilptask)).To(Succeed())
	Expect(ilptask.Spec.DeploymentPolicyRef.Name).To(Equal(dpPolicy.Name))
	Expect(ilptask.Spec.DataflowPolicyRef.Name).To(Equal(dfPolicy.Name))

	return ilptask
}

// creates a dataflow policy and a deployment policy for a VirtualMachine execution environment
func createPoliciesForVMExecEnv(
	resourceName,
	namespace string,
	providerProfile *cv1a1.ProviderProfile) (*pv1a1.DeploymentPolicy, *pv1a1.DataflowPolicy) {

	dfPolicy := &pv1a1.DataflowPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: namespace,
			Labels: map[string]string{
				"skycluster.io/app-id":    resourceName,
				"skycluster.io/app-scope": "distributed",
			},
		},
		Spec: pv1a1.DataflowPolicySpec{
			DataDependencies: []pv1a1.DataDapendency{},
		},
	}

	// create a deployment policy with a single component: XInstance
	// reflecting a single virtual machine
	dpPolicy := &pv1a1.DeploymentPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: namespace,
			Labels: map[string]string{
				"skycluster.io/app-id":    resourceName,
				"skycluster.io/app-scope": "distributed",
			},
		},
		Spec: pv1a1.DeploymentPolicySpec{
			ExecutionEnvironment: "VirtualMachine",
			DeploymentPolicies: []pv1a1.DeploymentPolicyItem{
				{
					ComponentRef: hv1a1.ComponentRef{
						APIVersion: "skycluster.io/v1alpha1",
						Kind:       "XInstance",
						Name:       resourceName,
						// Namespace:  "", // cluster-scoped
					},
					// a single VirtualServiceConstraint whose AnyOf contains ComputeProfile
					VirtualServiceConstraint: []pv1a1.VirtualServiceConstraint{
						{
							AnyOf: []pv1a1.VirtualServiceSelector{
								{
									// VirtualServiceSelector embeds hv1a1.VirtualService inline.
									VirtualService: hv1a1.VirtualService{
										Kind: "ComputeProfile",
										Spec: &runtime.RawExtension{Raw: []byte(
											`{"vcpus": "64", "ram": "256GB", "gpu": {"model": "A10G", "unit": "1"}}`,
										)},
									},
									Count: 1,
								},
							},
						},
					},
					// LocationConstraint: permissive (no specific provider filters) to allow optimizer to choose
					LocationConstraint: hv1a1.LocationConstraint{
						Permitted: lo.Ternary(providerProfile.Spec.Platform != "", []hv1a1.ProviderRefSpec{
							{
								Name:     providerProfile.Name,
								Type:     providerProfile.Spec.Zones[0].Type,
								Platform: providerProfile.Spec.Platform,
								Region:   providerProfile.Spec.Region,
							},
						}, []hv1a1.ProviderRefSpec{}),
					},
				},
			},
		},
	}
	return dpPolicy, dfPolicy
}

func createPoliciesForK8sExecEnvWithMultipleDeployment(
	resourceName,
	namespace string) (*pv1a1.DeploymentPolicy, *pv1a1.DataflowPolicy) {

	dfPolicy := &pv1a1.DataflowPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: namespace,
			Labels: map[string]string{
				"skycluster.io/app-id":    resourceName,
				"skycluster.io/app-scope": "distributed",
			},
		},
		Spec: pv1a1.DataflowPolicySpec{
			DataDependencies: []pv1a1.DataDapendency{
				{
					From: hv1a1.ComponentRef{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       "deployment" + strconv.Itoa(1),
						Namespace:  "default",
					},
					To: hv1a1.ComponentRef{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       "deployment" + strconv.Itoa(2),
						Namespace:  "default",
					},
					Latency:           "100ms",
					TotalDataTransfer: "100MB",
				},
				{
					From: hv1a1.ComponentRef{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       "deployment" + strconv.Itoa(2),
						Namespace:  "default",
					},
					To: hv1a1.ComponentRef{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       "deployment" + strconv.Itoa(3),
						Namespace:  "default",
					},
					Latency:           "100ms",
					TotalDataTransfer: "100MB",
				},
			},
		},
	}

	// create a deployment policy with a multiple components
	dpPolicy := &pv1a1.DeploymentPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: namespace,
			Labels: map[string]string{
				"skycluster.io/app-id":    resourceName,
				"skycluster.io/app-scope": "distributed",
			},
		},
		Spec: pv1a1.DeploymentPolicySpec{
			ExecutionEnvironment: "Kubernetes",
			DeploymentPolicies: []pv1a1.DeploymentPolicyItem{
				{
					ComponentRef: hv1a1.ComponentRef{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       "deployment" + strconv.Itoa(1),
						Namespace:  "default",
					},
					// The Deployment component comes with cpu and ram requirements
					// We can specify additional requirements by defining a ComputeProfile
					VirtualServiceConstraint: []pv1a1.VirtualServiceConstraint{
						{
							AnyOf: []pv1a1.VirtualServiceSelector{
								{
									// additional requirements: a node with L4 GPU
									VirtualService: hv1a1.VirtualService{
										Kind: "ComputeProfile",
										Spec: &runtime.RawExtension{Raw: []byte(
											`{"gpu": {"model": "L4"}}`,
										)},
									},
									// Count: 2, // no need to specify for Deployment
								},
							},
						},
					},
					// LocationConstraint: permissive (no specific provider filters) to allow optimizer to choose
					LocationConstraint: hv1a1.LocationConstraint{},
				},
				{
					ComponentRef: hv1a1.ComponentRef{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       "deployment" + strconv.Itoa(2),
						Namespace:  "default",
					},
					VirtualServiceConstraint: []pv1a1.VirtualServiceConstraint{
						{
							AnyOf: []pv1a1.VirtualServiceSelector{
								{
									VirtualService: hv1a1.VirtualService{
										Kind: "ComputeProfile",
										Spec: &runtime.RawExtension{Raw: []byte(
											`{"gpu": {"model": "A10G"}}`,
										)},
									},
									// Count: 1,
								},
							},
						},
					},
				},
				{
					ComponentRef: hv1a1.ComponentRef{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       "deployment" + strconv.Itoa(3),
						Namespace:  "default",
					},
				},
			},
		},
	}
	return dpPolicy, dfPolicy
}

func createPoliciesForK8sExecEnvWithDeployment(
	resourceName,
	namespace string) (*pv1a1.DeploymentPolicy, *pv1a1.DataflowPolicy) {

	dfPolicy := &pv1a1.DataflowPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: namespace,
			Labels: map[string]string{
				"skycluster.io/app-id":    resourceName,
				"skycluster.io/app-scope": "distributed",
			},
		},
		Spec: pv1a1.DataflowPolicySpec{
			DataDependencies: []pv1a1.DataDapendency{},
		},
	}

	// create a deployment policy with a single component: XInstance
	// reflecting a single virtual machine
	dpPolicy := &pv1a1.DeploymentPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: namespace,
			Labels: map[string]string{
				"skycluster.io/app-id":    resourceName,
				"skycluster.io/app-scope": "distributed",
			},
		},
		// For Kubernetes execution environment, the deployment policy can contain multiple components
		// each component is either a XNodeGroup or a Deployment
		// XNodeGroup represents a cluster specification for a multi-cluster Kubernetes deployment
		// Deployment represents a single Kubernetes deployment which resides in a single cluster
		// We test with both Deployment and XNodeGroup components here
		// For Deployment component, a corresponding Deployment object must exist in the cluster
		Spec: pv1a1.DeploymentPolicySpec{
			ExecutionEnvironment: "Kubernetes",
			DeploymentPolicies: []pv1a1.DeploymentPolicyItem{
				{
					ComponentRef: hv1a1.ComponentRef{
						APIVersion: "skycluster.io/v1alpha1",
						Kind:       "Deployment",
						Name:       "sample-deployment",
						Namespace:  "default",
					},
					// The Deployment component comes with cpu and ram requirements
					// We can specify additional requirements by defining a ComputeProfile
					VirtualServiceConstraint: []pv1a1.VirtualServiceConstraint{
						{
							AnyOf: []pv1a1.VirtualServiceSelector{
								{
									// additional requirements: a node with L4 GPU
									VirtualService: hv1a1.VirtualService{
										Kind: "ComputeProfile",
										Spec: &runtime.RawExtension{Raw: []byte(
											`{"gpu": {"model": "L4"}}`,
										)},
									},
									// Count: 2, // no need to specify for Deployment
								},
							},
						},
					},
					// LocationConstraint: permissive (no specific provider filters) to allow optimizer to choose
					LocationConstraint: hv1a1.LocationConstraint{},
				},
			},
		},
	}
	return dpPolicy, dfPolicy
}

func createPoliciesForK8sExecEnvMultipleVSConstraintsWithDeployment(
	resourceName,
	namespace string) (*pv1a1.DeploymentPolicy, *pv1a1.DataflowPolicy) {

	dfPolicy := &pv1a1.DataflowPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: namespace,
			Labels: map[string]string{
				"skycluster.io/app-id":    resourceName,
				"skycluster.io/app-scope": "distributed",
			},
		},
		Spec: pv1a1.DataflowPolicySpec{
			DataDependencies: []pv1a1.DataDapendency{},
		},
	}

	// create a deployment policy with a single component: XInstance
	// reflecting a single virtual machine
	dpPolicy := &pv1a1.DeploymentPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: namespace,
			Labels: map[string]string{
				"skycluster.io/app-id":    resourceName,
				"skycluster.io/app-scope": "distributed",
			},
		},
		// For Kubernetes execution environment, the deployment policy can contain multiple components
		// each component is either a XNodeGroup or a Deployment
		// XNodeGroup represents a cluster specification for a multi-cluster Kubernetes deployment
		// Deployment represents a single Kubernetes deployment which resides in a single cluster
		Spec: pv1a1.DeploymentPolicySpec{
			ExecutionEnvironment: "Kubernetes",
			DeploymentPolicies: []pv1a1.DeploymentPolicyItem{
				{
					ComponentRef: hv1a1.ComponentRef{
						APIVersion: "skycluster.io/v1alpha1",
						Kind:       "XNodeGroup",
						Name:       resourceName,
						// Namespace:  "", // cluster-scoped
					},
					// a single VirtualServiceConstraint whose AnyOf contains ComputeProfile
					VirtualServiceConstraint: []pv1a1.VirtualServiceConstraint{
						{
							AnyOf: []pv1a1.VirtualServiceSelector{
								{
									// first alternative ComputeProfile for the XNodeGroup
									VirtualService: hv1a1.VirtualService{
										Kind: "ComputeProfile",
										Spec: &runtime.RawExtension{Raw: []byte(
											`{"vcpus": "48", "gpu": {"model": "A10G"}}`,
										)},
									},
									// Count: 2, // no need to specify for XNodeGroup
								},
							},
						},
						{
							AnyOf: []pv1a1.VirtualServiceSelector{
								{
									VirtualService: hv1a1.VirtualService{
										Kind: "ComputeProfile",
										Spec: &runtime.RawExtension{Raw: []byte(
											`{"gpu": {"model": "L4"}}`,
										)},
									},
								},
							},
						},
					},
					// LocationConstraint: permissive (no specific provider filters) to allow optimizer to choose
					LocationConstraint: hv1a1.LocationConstraint{},
				},
				{
					ComponentRef: hv1a1.ComponentRef{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       "deployment" + strconv.Itoa(1),
						Namespace:  "default",
					},
					VirtualServiceConstraint: []pv1a1.VirtualServiceConstraint{
						{
							AnyOf: []pv1a1.VirtualServiceSelector{
								{
									VirtualService: hv1a1.VirtualService{
										Kind: "ComputeProfile",
										Spec: &runtime.RawExtension{Raw: []byte(
											`{"gpu": {"model": "L4"}}`,
										)},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	return dpPolicy, dfPolicy
}

func createPoliciesForK8sExecEnvMultipleVSConstraints(
	resourceName,
	namespace string) (*pv1a1.DeploymentPolicy, *pv1a1.DataflowPolicy) {

	dfPolicy := &pv1a1.DataflowPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: namespace,
			Labels: map[string]string{
				"skycluster.io/app-id":    resourceName,
				"skycluster.io/app-scope": "distributed",
			},
		},
		Spec: pv1a1.DataflowPolicySpec{
			DataDependencies: []pv1a1.DataDapendency{},
		},
	}

	// create a deployment policy with a single component: XInstance
	// reflecting a single virtual machine
	dpPolicy := &pv1a1.DeploymentPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: namespace,
			Labels: map[string]string{
				"skycluster.io/app-id":    resourceName,
				"skycluster.io/app-scope": "distributed",
			},
		},
		// For Kubernetes execution environment, the deployment policy can contain multiple components
		// each component is either a XNodeGroup or a Deployment
		// XNodeGroup represents a cluster specification for a multi-cluster Kubernetes deployment
		// Deployment represents a single Kubernetes deployment which resides in a single cluster
		Spec: pv1a1.DeploymentPolicySpec{
			ExecutionEnvironment: "Kubernetes",
			DeploymentPolicies: []pv1a1.DeploymentPolicyItem{
				{
					ComponentRef: hv1a1.ComponentRef{
						APIVersion: "skycluster.io/v1alpha1",
						Kind:       "XNodeGroup",
						Name:       resourceName,
						// Namespace:  "", // cluster-scoped
					},
					// a single VirtualServiceConstraint whose AnyOf contains ComputeProfile
					VirtualServiceConstraint: []pv1a1.VirtualServiceConstraint{
						{
							AnyOf: []pv1a1.VirtualServiceSelector{
								{
									// first alternative ComputeProfile for the XNodeGroup
									VirtualService: hv1a1.VirtualService{
										Kind: "ComputeProfile",
										Spec: &runtime.RawExtension{Raw: []byte(
											`{"vcpus": "48", "gpu": {"model": "A10G"}}`,
										)},
									},
									// Count: 2, // no need to specify for XNodeGroup
								},
							},
						},
						{
							AnyOf: []pv1a1.VirtualServiceSelector{
								{
									VirtualService: hv1a1.VirtualService{
										Kind: "ComputeProfile",
										Spec: &runtime.RawExtension{Raw: []byte(
											`{"gpu": {"model": "L4"}}`,
										)},
									},
								},
							},
						},
					},
					// LocationConstraint: permissive (no specific provider filters) to allow optimizer to choose
					LocationConstraint: hv1a1.LocationConstraint{},
				},
			},
		},
	}
	return dpPolicy, dfPolicy
}

func createPoliciesForK8sExecEnv(
	resourceName,
	namespace string) (*pv1a1.DeploymentPolicy, *pv1a1.DataflowPolicy) {

	dfPolicy := &pv1a1.DataflowPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: namespace,
			Labels: map[string]string{
				"skycluster.io/app-id":    resourceName,
				"skycluster.io/app-scope": "distributed",
			},
		},
		Spec: pv1a1.DataflowPolicySpec{
			DataDependencies: []pv1a1.DataDapendency{},
		},
	}

	// create a deployment policy with a single component: XInstance
	// reflecting a single virtual machine
	dpPolicy := &pv1a1.DeploymentPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: namespace,
			Labels: map[string]string{
				"skycluster.io/app-id":    resourceName,
				"skycluster.io/app-scope": "distributed",
			},
		},
		// For Kubernetes execution environment, the deployment policy can contain multiple components
		// each component is either a XNodeGroup or a Deployment
		// XNodeGroup represents a cluster specification for a multi-cluster Kubernetes deployment
		// Deployment represents a single Kubernetes deployment which resides in a single cluster
		Spec: pv1a1.DeploymentPolicySpec{
			ExecutionEnvironment: "Kubernetes",
			DeploymentPolicies: []pv1a1.DeploymentPolicyItem{
				{
					ComponentRef: hv1a1.ComponentRef{
						APIVersion: "skycluster.io/v1alpha1",
						Kind:       "XNodeGroup",
						Name:       resourceName,
						// Namespace:  "", // cluster-scoped
					},
					// a single VirtualServiceConstraint whose AnyOf contains ComputeProfile
					VirtualServiceConstraint: []pv1a1.VirtualServiceConstraint{
						{
							AnyOf: []pv1a1.VirtualServiceSelector{
								{
									// first alternative ComputeProfile for the XNodeGroup
									VirtualService: hv1a1.VirtualService{
										Kind: "ComputeProfile",
										Spec: &runtime.RawExtension{Raw: []byte(
											`{"vcpus": "48", "gpu": {"model": "A10G"}}`,
										)},
									},
									// Count: 2, // no need to specify for XNodeGroup
								},
								{
									// a second alternative ComputeProfile for the XNodeGroup
									VirtualService: hv1a1.VirtualService{
										Kind: "ComputeProfile",
										Spec: &runtime.RawExtension{Raw: []byte(
											`{"gpu": {"model": "L4"}}`,
										)},
									},
								},
							},
						},
					},
					// LocationConstraint: permissive (no specific provider filters) to allow optimizer to choose
					LocationConstraint: hv1a1.LocationConstraint{},
				},
			},
		},
	}
	return dpPolicy, dfPolicy
}

// returns the provider profile and a boolean indicating if it was created
func createProviderProfileAWS(typeNamespacedName types.NamespacedName) *cv1a1.ProviderProfile {
	return &cv1a1.ProviderProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      typeNamespacedName.Name,
			Namespace: typeNamespacedName.Namespace,
		},
		Spec: cv1a1.ProviderProfileSpec{
			Platform:    "aws",
			RegionAlias: "us-east",
			Region:      "us-east-1",
			Zones: []cv1a1.ZoneSpec{
				{Name: "us-east-1a", Enabled: true, DefaultZone: true, Type: "cloud"},
			},
		},
	}
}

func createProviderProfileGCP(typeNamespacedName types.NamespacedName) *cv1a1.ProviderProfile {
	return &cv1a1.ProviderProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      typeNamespacedName.Name,
			Namespace: typeNamespacedName.Namespace,
		},
		Spec: cv1a1.ProviderProfileSpec{
			Platform:    "gcp",
			RegionAlias: "us-east",
			Region:      "us-east1",
			Zones: []cv1a1.ZoneSpec{
				{Name: "us-east1-a", Enabled: true, DefaultZone: true, Type: "cloud"},
			},
		},
	}
}

func setupOptimizationScripts(ctx context.Context, projPath string, k8sClient client.Client) error {
	err := coreutils.ApplyYAML(
		ctx,
		k8sClient,
		k8sClient.Scheme(),
		filepath.Join(projPath, "test/manifests/cms"),
	)
	return err
}

func configMapData(zone string) map[string]string {
	return map[string]string{
		"flavors.yaml": `- zone: ` + zone + `
  zoneOfferings:
  - name: m3.xlarge
    nameLabel: 2vCPU-4GB
    vcpus: 2
    ram: 4GB
    price: "0.05"
    gpu:
      enabled: false
    spot:
      price: "0.02"
      enabled: true
  - name: g5.12xlarge
    nameLabel: 48vCPU-192GB-4xA10G-22GB
    vcpus: 48
    ram: 192GB
    price: "5.67"
    gpu:
      enabled: true
      manufacturer: NVIDIA
      count: 4
      model: A10G
      memory: 22GB
    spot:
      price: "2.12"
      enabled: true
  - name: g5.16xlarge
    nameLabel: 64vCPU-256GB-1xA10G-22GB
    vcpus: 64
    ram: 256GB
    price: "4.10"
    gpu:
      enabled: true
      manufacturer: NVIDIA
      count: 1
      model: A10G
      memory: 22GB
    spot:
      price: "1.07"
      enabled: true
  - name: g2-standard-12   
    nameLabel: 12vCPU-49GB-1xL4-24GB
    vcpus: 12
    ram: 49GB
    price: "$0.4893"
    gpu:
      enabled: true
      manufacturer: NVIDIA
      count: 1
      model: L4
      memory: 24GB
    spot:
      price: "$0.1365"
      enabled: true`,
		"images.yaml": `- nameLabel: ubuntu-20.04
name: ami-0fb0b230890ccd1e6
zone: ` + zone + `
- nameLabel: ubuntu-22.04
name: ami-0e70225fadb23da91
zone: ` + zone + `
- nameLabel: ubuntu-24.04
name: ami-07033cb190109bd1d
zone: ` + zone,
		"managed-k8s.yaml": `- name: EKS
nameLabel: ManagedKubernetes
overhead:
  cost: "0.096"
  count: 1
  instanceType: m5.xlarge
price: "0.10"`,
	}
}
