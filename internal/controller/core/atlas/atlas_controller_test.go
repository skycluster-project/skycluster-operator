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
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lo "github.com/samber/lo"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	hv1a1 "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"
	pv1a1 "github.com/skycluster-project/skycluster-operator/api/policy/v1alpha1"
	pkglog "github.com/skycluster-project/skycluster-operator/pkg/v1alpha1/log"
)

var _ = Describe("Atlas Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"
		const namespace = "skycluster-system"

		ctx := context.Background()

		var (
			atlas    *cv1a1.Atlas
			dfPolicy *pv1a1.DataflowPolicy
			dpPolicy *pv1a1.DeploymentPolicy
		)

		BeforeEach(func() {
		})

		AfterEach(func() {
			By("Cleanup the specific resource instance Atlas")
			Expect(k8sClient.Delete(ctx, atlas)).To(Succeed())

			By("Cleanup the deployments")
			deployments := &appsv1.DeploymentList{}
			Expect(k8sClient.List(ctx, deployments, client.InNamespace(namespace), client.MatchingLabels{
				"skycluster.io/app-scope": "distributed",
			})).To(Succeed())
			for _, deployment := range deployments.Items {
				Expect(k8sClient.Delete(ctx, &deployment)).To(Succeed())
			}

		})

		It("should successfully generate provider manifests for the resource", func() {
			By("creating sample atlas resource")
			atlas = createSampleAtlasResource(resourceName, namespace)
			Expect(k8sClient.Create(ctx, atlas)).To(Succeed())

			reconciler := &AtlasReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Logger: zap.New(pkglog.CustomLogger()).WithName("[Atlas]"),
			}
			deployMap := createSampleDeployMap(resourceName)

			By("checking the provider manifests")
			pManifests, provToMetadataIdx, err := reconciler.generateProviderManifests(resourceName, namespace, deployMap.Component)
			Expect(err).NotTo(HaveOccurred())
			Expect(lo.Keys(pManifests)).To(ConsistOf([]string{"gcp-us-east1-a", "aws-us-east-1a"}))

			By("checking the provider metadata index")
			Expect(lo.Keys(provToMetadataIdx)).To(ConsistOf([]string{"gcp", "aws"}))
			Expect(lo.Keys(provToMetadataIdx["gcp"])).To(Equal([]string{"gcp-us-east1-a"}))
			Expect(lo.Keys(provToMetadataIdx["aws"])).To(Equal([]string{"aws-us-east-1a"}))

			By("checking the provider metadata index, each provider starts with 0 in its category")
			Expect(provToMetadataIdx["gcp"]["gcp-us-east1-a"]).To(Equal(0))
			Expect(provToMetadataIdx["aws"]["aws-us-east-1a"]).To(Equal(0))
		})

		It("should successfully generate Kubernetes manifests for the resource", func() {
			By("creating sample atlas resource")
			dpPolicy, dfPolicy = createSamplePoliciesForK8sExecEnvXNodeGroup(resourceName, namespace)
			Expect(k8sClient.Create(ctx, dpPolicy)).To(Succeed())
			Expect(k8sClient.Create(ctx, dfPolicy)).To(Succeed())

			reconciler := &AtlasReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Logger: zap.New(pkglog.CustomLogger()).WithName("[Atlas]"),
			}

			atlas = createSampleAtlasResource(resourceName, namespace)
			Expect(k8sClient.Create(ctx, atlas)).To(Succeed())

			deployMap := createSampleDeployMap(resourceName)

			pManifests, provToMetadataIdx, err := reconciler.generateProviderManifests(resourceName, namespace, deployMap.Component)
			Expect(err).NotTo(HaveOccurred())
			Expect(lo.Keys(pManifests)).To(ConsistOf([]string{"gcp-us-east1-a", "aws-us-east-1a"}))
			Expect(lo.Keys(provToMetadataIdx)).To(ConsistOf([]string{"gcp", "aws"}))
			Expect(len(provToMetadataIdx["gcp"])).To(Equal(1))
			Expect(len(provToMetadataIdx["aws"])).To(Equal(1))

			k8sManifests, err := reconciler.generateK8SManifests(resourceName, provToMetadataIdx, deployMap, *dpPolicy)
			Expect(err).NotTo(HaveOccurred())
			Expect(lo.Keys(k8sManifests)).To(ConsistOf([]string{"gcp-us-east1-a", "aws-us-east-1a"}))
			Expect(k8sManifests["gcp-us-east1-a"].Manifest.Raw).To(Not(BeNil()))
			Expect(k8sManifests["aws-us-east-1a"].Manifest.Raw).To(Not(BeNil()))

			// ========= checking the XKube GCP manifest =========
			By("checking the XKube GCP manifest")
			var manifest map[string]interface{}
			err = json.Unmarshal([]byte(k8sManifests["gcp-us-east1-a"].Manifest.Raw), &manifest)
			Expect(err).NotTo(HaveOccurred())

			spec, found, err := unstructured.NestedMap(manifest, "spec")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(spec).To(HaveKeyWithValue("nodeCidr", Equal("10.16.128.0/17")))
			Expect(spec).To(HaveKeyWithValue("serviceCidr", Equal("")))
			Expect(spec).To(HaveKeyWithValue("podCidr", HaveKeyWithValue("cidr", Equal("172.16.0.0/16"))))

			By("checking the XKube GCP node groups")
			ngs, found, err := unstructured.NestedSlice(manifest, "spec", "nodeGroups")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(ngs).To(HaveLen(1))

			instanceTypes := []any{"2vCPU-4GB", "12vCPU-49GB-1xL4-24GB"}
			Expect(ngs[0]).To(HaveKeyWithValue("instanceTypes", ConsistOf(instanceTypes...)))
			Expect(ngs[0]).To(HaveKeyWithValue("nodeCount", Equal(1.0)))
			Expect(ngs[0]).To(HaveKeyWithValue("publicAccess", Equal(false)))
			Expect(ngs[0]).To(HaveKeyWithValue("autoScaling", HaveKeyWithValue("enabled", Equal(true))))

			// ========= checking the XKube AWS manifest =========
			By("checking the XKube AWS manifest")
			err = json.Unmarshal([]byte(k8sManifests["aws-us-east-1a"].Manifest.Raw), &manifest)
			Expect(err).NotTo(HaveOccurred())

			spec, found, err = unstructured.NestedMap(manifest, "spec")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(spec).To(HaveKeyWithValue("nodeCidr", BeEmpty()))
			Expect(spec).To(HaveKeyWithValue("serviceCidr", Equal("10.255.0.0/16")))
			Expect(spec).To(HaveKeyWithValue("podCidr", HaveKeyWithValue("cidr", Equal("10.33.128.0/17"))))
			Expect(spec).To(HaveKeyWithValue("podCidr", HaveKeyWithValue("private", Equal("10.33.192.0/18"))))
			Expect(spec).To(HaveKeyWithValue("podCidr", HaveKeyWithValue("public", Equal("10.33.128.0/18"))))

			By("checking the XKube AWS node groups")
			ngs, found, err = unstructured.NestedSlice(manifest, "spec", "nodeGroups")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(ngs).To(HaveLen(1))
			instanceTypes = []any{"64vCPU-256GB-1xA10G-22GB", "48vCPU-192GB-4xA10G-22GB", "12vCPU-49GB-1xL4-24GB"}
			Expect(ngs[0]).To(HaveKeyWithValue("instanceTypes", ConsistOf(instanceTypes...)))
			Expect(ngs[0]).To(HaveKeyWithValue("nodeCount", Equal(1.0)))
			Expect(ngs[0]).To(HaveKeyWithValue("publicAccess", Equal(false)))
			Expect(ngs[0]).To(HaveKeyWithValue("autoScaling", HaveKeyWithValue("enabled", Equal(true))))

			// cleanup
			By("Cleanup the specific resource instance DataflowPolicy")
			Expect(k8sClient.Delete(ctx, dfPolicy)).To(Succeed())

			By("Cleanup the specific resource instance DeploymentPolicy")
			Expect(k8sClient.Delete(ctx, dpPolicy)).To(Succeed())
		})
	})
})

func createSampleAtlasResource(resourceName, namespace string) *cv1a1.Atlas {
	return &cv1a1.Atlas{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName + "-atlas",
			Namespace: namespace,
			Labels: map[string]string{
				"skycluster.io/app-id": resourceName,
			},
		},
		Spec: cv1a1.AtlasSpec{
			Approve: false,

			ExecutionEnvironment: "Kubernetes",
			DataflowPolicyRef: cv1a1.DataflowPolicyRef{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: resourceName,
				},
				DataflowResourceVersion: "1.0.0",
			},
			DeploymentPolicyRef: cv1a1.DeploymentPolicyRef{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: resourceName,
				},
				DeploymentPlanResourceVersion: "1.0.0",
			},
			DeployMap: createSampleDeployMap(resourceName),
		},
	}
}

func createSampleDeployMap(resourceName string) cv1a1.DeployMap {
	return cv1a1.DeployMap{
		Component: []hv1a1.SkyService{
			{
				ComponentRef: hv1a1.ComponentRef{
					APIVersion: "skycluster.io/v1alpha1",
					Kind:       "XNodeGroup",
					Name:       resourceName,
				},
				Manifest: &runtime.RawExtension{Raw: []byte(`
					{
						"services": [
							{
								"apiVersion": "skycluster.io/v1alpha1",
								"count": "1",
								"kind": "ComputeProfile",
								"name": "64vCPU-256GB-1xA10G-22GB",
								"price": 4.1
							},
							{
								"apiVersion": "skycluster.io/v1alpha1",
								"count": "1",
								"kind": "ComputeProfile",
								"name": "48vCPU-192GB-4xA10G-22GB",
								"price": 5.67
							},
							{
								"apiVersion": "skycluster.io/v1alpha1",
								"count": "1",
								"kind": "ComputeProfile",
								"name": "12vCPU-49GB-1xL4-24GB",
								"price": 0.4893
							}
						]
					}
				`)},
				ProviderRef: hv1a1.ProviderRefSpec{
					Name:        "aws-us-east-1a",
					Platform:    "aws",
					Region:      "us-east-1",
					RegionAlias: "us-east",
					Type:        "cloud",
					Zone:        "us-east-1a",
				},
			},
			{
				ComponentRef: hv1a1.ComponentRef{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       resourceName + strconv.Itoa(1),
					Namespace:  "default",
				},
				Manifest: &runtime.RawExtension{Raw: []byte(`
					{
						"services": [
							{
								"apiVersion": "skycluster.io/v1alpha1",
								"count": "1",
								"kind": "ComputeProfile",
								"name": "2vCPU-4GB",
								"price": 0.05
							},
							{
								"apiVersion": "skycluster.io/v1alpha1",
								"count": "1",
								"kind": "ComputeProfile",
								"name": "12vCPU-49GB-1xL4-24GB",
								"price": 0.4893
							}
						]
					}
				`)},
				ProviderRef: hv1a1.ProviderRefSpec{
					Name:        "gcp-us-east1-a",
					Platform:    "gcp",
					Region:      "us-east1",
					RegionAlias: "us-east",
					Type:        "cloud",
					Zone:        "us-east1-a",
				},
			},
		},
	}
}

func createSamplePoliciesForK8sExecEnvXNodeGroup(
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
