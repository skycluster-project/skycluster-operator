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
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	hv1a1 "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"
	pv1a1 "github.com/skycluster-project/skycluster-operator/api/policy/v1alpha1"
	cv1a1ppctrl "github.com/skycluster-project/skycluster-operator/internal/controller/core/providerprofile"
	coreutils "github.com/skycluster-project/skycluster-operator/internal/controller/core/utils"
	pv1a1ctrl "github.com/skycluster-project/skycluster-operator/internal/controller/policy"
	pkglog "github.com/skycluster-project/skycluster-operator/pkg/v1alpha1/log"
	// /home/ubuntu/skycluster-operator/internal/helper/manifest.go
)

var _ = Describe("ILPTask Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"
		const namespace = "skycluster-system"

		ctx := context.Background()
		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "skycluster-system",
		}
		var ilptask *cv1a1.ILPTask
		var dfPolicy *pv1a1.DataflowPolicy
		var dpPolicy *pv1a1.DeploymentPolicy
		var providerprofile *cv1a1.ProviderProfile
		var cm *corev1.ConfigMap

		BeforeEach(func() {
		})

		AfterEach(func() {
			By("Cleanup the specific resource instance DataflowPolicy")
			Expect(k8sClient.Delete(ctx, dfPolicy)).To(Succeed())

			By("Cleanup the specific resource instance DeploymentPolicy")
			Expect(k8sClient.Delete(ctx, dpPolicy)).To(Succeed())

			// By("Cleanup the specific resource instance ProviderProfile")
			// Expect(k8sClient.Delete(ctx, providerprofile)).To(Succeed())

			// By("Cleanup the specific resource instance ConfigMap")
			// Expect(k8sClient.Delete(ctx, cm)).To(Succeed())
		})

		It("should successfully generate the deployment plan for VirtualMachine execution environment", func() {

			if err := setupOptimizationScripts(ctx, "../../../../", k8sClient); err != nil {
				panic(err)
			}

			providerprofile = createProviderProfileAWS(typeNamespacedName)
			if err := k8sClient.Create(ctx, providerprofile); err != nil {
				panic(err)
			}

			// need to trigger the reconciler
			ppReconciler := getProviderProfileReconciler()
			if _, err := ppReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: providerprofile.Name, Namespace: namespace},
			}); err != nil {
				panic(err)
			}

			// fetch the config map
			ppOut := &cv1a1.ProviderProfile{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: providerprofile.Name, Namespace: namespace}, ppOut); err != nil {
				panic(err)
			}

			cmName := ppOut.Status.DependencyManager.GetDependency("ConfigMap", namespace)
			if cmName == nil || cmName.NameRef == "" {
				panic("config map not found")
			}

			// Fetch the config map
			cm = &corev1.ConfigMap{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: cmName.NameRef, Namespace: namespace}, cm); err != nil {
				panic(err)
			}
			// update the config map
			cm.Data = configMapData()
			if err := k8sClient.Update(ctx, cm); err != nil {
				panic(err)
			}

			By("generating deployment policy and dataflow policy")
			dpPolicy, dfPolicy = createPoliciesForVMExecEnv(resourceName, namespace, providerprofile)
			Expect(k8sClient.Create(ctx, dpPolicy)).To(Succeed())
			Expect(k8sClient.Create(ctx, dfPolicy)).To(Succeed())

			// run reconcilers for dataflowpolicy and deploymentpolicy
			// expect the ilptask object to be created
			dfReconciler := getDataflowPolicyReconciler()
			_, err := dfReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: dfPolicy.Name, Namespace: namespace},
			})
			Expect(err).NotTo(HaveOccurred())

			dpReconciler := getDeploymentPolicyReconciler()
			_, err = dpReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: dpPolicy.Name, Namespace: namespace},
			})
			Expect(err).NotTo(HaveOccurred())

			By("checking the ilptask object is created")
			ilptask = &cv1a1.ILPTask{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: dfPolicy.Name, Namespace: namespace}, ilptask)).To(Succeed())
			Expect(ilptask.Spec.DeploymentPolicyRef.Name).To(Equal(dpPolicy.Name))
			Expect(ilptask.Spec.DataflowPolicyRef.Name).To(Equal(dfPolicy.Name))

			// run reconciler for ilptask and check the status
			ilptaskReconciler := getILPTaskReconciler()
			res, err := ilptaskReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ilptask.Name, Namespace: namespace},
			})
			Expect(err).NotTo(HaveOccurred())

			ilpOut := &cv1a1.ILPTask{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: ilptask.Name, Namespace: namespace}, ilpOut)
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

			// cleanup
			// By("Cleanup the specific resource instance ILPTask")
			// Expect(k8sClient.Delete(ctx, ilptask)).To(Succeed())
		})

		// reconcile
		// If result is already set and referenced resources didn't change -> skip

		// Otherwise create or ensure optimization pod

	})
})

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
											`{"cpu": "4", "ram": "16GB", "gpu": {"model": "L4", "unit": "1", "memory": "16GB"}}`,
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
								Zone:     providerProfile.Spec.Zones[0].Name,
							},
						}, []hv1a1.ProviderRefSpec{}),
					},
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

func setupOptimizationScripts(ctx context.Context, projPath string, k8sClient client.Client) error {
	err := coreutils.ApplyYAML(
		ctx,
		k8sClient,
		k8sClient.Scheme(),
		filepath.Join(projPath, "test/manifests/cms"),
	)
	return err
}

func configMapData() map[string]string {
	return map[string]string{
		"flavors.yaml": `- zone: us-east-1a
zoneOfferings:
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
enabled: true`,
		"images.yaml": `- nameLabel: ubuntu-20.04
name: ami-0fb0b230890ccd1e6
zone: us-east-1a
- nameLabel: ubuntu-22.04
name: ami-0e70225fadb23da91
zone: us-east-1a
- nameLabel: ubuntu-24.04
name: ami-07033cb190109bd1d
zone: us-east-1a
- nameLabel: ubuntu-20.04
name: ami-0fb0b230890ccd1e6
zone: us-east-1b
- nameLabel: ubuntu-22.04
name: ami-0e70225fadb23da91
zone: us-east-1b
- nameLabel: ubuntu-24.04
name: ami-07033cb190109bd1d
zone: us-east-1b`,
		"managed-k8s.yaml": `- name: EKS
nameLabel: ManagedKubernetes
overhead:
	cost: "0.096"
	count: 1
	instanceType: m5.xlarge
price: "0.10"`,
	}
}
