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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	hv1a1 "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"
)

var _ = Describe("Atlas Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"
		const namespace = "skycluster-system"

		ctx := context.Background()

		var atlas *cv1a1.Atlas

		BeforeEach(func() {
		})

		AfterEach(func() {

			By("Cleanup the specific resource instance Atlas")
			Expect(k8sClient.Delete(ctx, atlas)).To(Succeed())
		})

		It("should successfully reconcile the resource", func() {
			By("creating sample atlas resource")
			atlas = createSampleAtlasResource(resourceName, namespace)
			Expect(k8sClient.Create(ctx, atlas)).To(Succeed())

			By("reconciling the resource")
			reconciler := &AtlasReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			deployMap := createSampleDeployMap(resourceName)

			By("checking the provider manifests")
			pManifests, provToMetadataIdx, err := reconciler.generateProviderManifests(resourceName, namespace, deployMap.Component)
			Expect(err).NotTo(HaveOccurred())
			Expect(pManifests).To(Equal(2))
			Expect(provToMetadataIdx).To(Equal(map[string]map[string]int{
				"gcp-us-east1": {
					"us-east1-a": 0,
				},
				"aws-us-east-2": {
					"us-east-2a": 0,
				},
			}))
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
					Name:       resourceName + "-0",
				},
				Manifest: &runtime.RawExtension{Raw: []byte(`
					{
						"services": [
							{
								"apiVersion": "skycluster.io/v1alpha1",
								"count": "1",
								"kind": "ComputeProfile",
								"name": "4vCPU-16GB-1xL4-24GB",
								"price": 0.1631
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
			{
				ComponentRef: hv1a1.ComponentRef{
					APIVersion: "skycluster.io/v1alpha1",
					Kind:       "XNodeGroup",
					Name:       resourceName + "-1",
				},
				Manifest: &runtime.RawExtension{Raw: []byte(`
					{
						"services": [
							{
								"apiVersion": "skycluster.io/v1alpha1",
								"count": "1",
								"kind": "ComputeProfile",
								"name": "4vCPU-16GB-1xA10G-22GB",
								"price": 1.01
							},
							{
								"apiVersion": "skycluster.io/v1alpha1",
								"count": "1",
								"kind": "ComputeProfile",
								"name": "8vCPU-32GB-1xA10G-22GB",
								"price": 1.21
							},
							{
								"apiVersion": "skycluster.io/v1alpha1",
								"count": "1",
								"kind": "ComputeProfile",
								"name": "16vCPU-64GB-1xA10G-22GB",
								"price": 1.62
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
		},
	}
}
