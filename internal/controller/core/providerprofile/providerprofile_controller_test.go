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
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"

	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
)

var _ = Describe("ProviderProfile Controller", func() {
	Context("When reconciling a resource", func() {

		const namespace = "skycluster-system"
		var (
			provider   *cv1a1.ProviderProfile
			reconciler *ProviderProfileReconciler
		)
		ctx := context.Background()

		BeforeEach(func() {
			By("creating the custom resource for the Kind ProviderProfile")
			reconciler = &ProviderProfileReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Logger:   logf.Log.WithName("test-providerprofile-controller"),
				Recorder: record.NewFakeRecorder(100),
			}
		})

		AfterEach(func() {})

		It("test1: should successfully reconcile the resource, create CM and set status fields", func() {
			resourceName := "test-resource1"
			typeNamespacedName := types.NamespacedName{Name: resourceName, Namespace: namespace}

			By("Creating the ProviderProfile")
			provider = createProviderProfileAWS(ctx, k8sClient, typeNamespacedName)

			By("Reconciling the ProviderProfile")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: resourceName, Namespace: namespace},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the ConfigMap was created")
			cmList := &corev1.ConfigMapList{}
			err = k8sClient.List(ctx, cmList, client.MatchingLabels(map[string]string{
				"skycluster.io/provider-profile": provider.Name,
				"skycluster.io/config-type":      "provider-profile",
			}))
			Expect(err).NotTo(HaveOccurred())
			Expect(cmList.Items).To(HaveLen(1))

			By("Verifying the ProviderProfile was updated")
			providerOut := &cv1a1.ProviderProfile{}
			err = k8sClient.Get(ctx, typeNamespacedName, providerOut)
			Expect(err).NotTo(HaveOccurred())
			Expect(providerOut.Status.DependencyManager.HasDependency(cmList.Items[0].Name, "ConfigMap", namespace)).To(BeTrue())

			// ensure other dependencies are created
			By("Verifying the Image was created")
			imgList := &cv1a1.ImageList{}
			err = k8sClient.List(ctx, imgList, client.MatchingLabels(map[string]string{
				"skycluster.io/provider-profile": provider.Name,
			}))
			Expect(err).NotTo(HaveOccurred())
			Expect(imgList.Items).To(HaveLen(1))
			Expect(providerOut.Status.DependencyManager.HasDependency(imgList.Items[0].Name, "Image", namespace)).To(BeTrue())

			// ensure instance type dependency is created
			By("Verifying the InstanceType was created")
			itList := &cv1a1.InstanceTypeList{}
			err = k8sClient.List(ctx, itList, client.MatchingLabels(map[string]string{
				"skycluster.io/provider-profile": provider.Name,
			}))
			Expect(err).NotTo(HaveOccurred())
			Expect(itList.Items).To(HaveLen(1))
			Expect(err).NotTo(HaveOccurred())
			Expect(providerOut.Status.DependencyManager.HasDependency(itList.Items[0].Name, "InstanceType", namespace)).To(BeTrue())

			// ensure egress costs are set
			By("Verifying the EgressCosts were set")
			Expect(providerOut.Status.EgressCostSpecs).To(HaveLen(3))

			// no latency objects should be created
			By("Verifying the Latency objects were not created")
			latList := &cv1a1.LatencyList{}
			err = k8sClient.List(ctx, latList, client.MatchingLabels(map[string]string{
				"skycluster.io/provider-platform": providerOut.Spec.Platform,
				"skycluster.io/provider-region":   providerOut.Spec.Region,
			}))
			Expect(err).NotTo(HaveOccurred())
			Expect(latList.Items).To(HaveLen(0))

			By("Cleanup the specific resource instance ProviderProfile")
			reconciler.cleanup(ctx, resourceName, namespace)
		})

		It("test2: handles creating two provider profiles and creates latency objects", func() {
			resourceName := "test-resource-it2"
			typeNamespacedName := types.NamespacedName{
				Name:      resourceName,
				Namespace: namespace,
			}
			provider = createProviderProfileAWS(ctx, k8sClient, typeNamespacedName)
			providerGCP := createProviderProfileGCP(resourceName+"-gcp", namespace)

			By("reconciling the first provider profile")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("reconciling the second provider profile")
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      providerGCP.Name,
					Namespace: providerGCP.Namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			var provList cv1a1.ProviderProfileList
			Expect(reconciler.List(ctx, &provList, client.InNamespace(providerGCP.Namespace))).NotTo(HaveOccurred())
			Expect(provList.Items).To(HaveLen(2))

			err = reconciler.ensureLatencies(ctx, providerGCP)
			if err != nil {
				fmt.Println("error ensuring latencies", err)
			}
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the Latency objects were created")
			latList := &cv1a1.LatencyList{}
			err = k8sClient.List(ctx, latList, client.MatchingLabels(map[string]string{}))
			Expect(err).NotTo(HaveOccurred())
			Expect(latList.Items).To(HaveLen(1))

			reconciler.cleanup(ctx, resourceName, namespace)
			reconciler.cleanup(ctx, resourceName+"-gcp", namespace)
		})
	})
})

func (r *ProviderProfileReconciler) cleanup(ctx context.Context, resourceName, namespace string) {
	// typeNamespacedName := types.NamespacedName{Name: resourceName, Namespace: namespace}
	pp := &cv1a1.ProviderProfile{ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: namespace}}
	Expect(client.IgnoreNotFound(r.Delete(ctx, pp))).To(Succeed())
	// triger reconciler to clean up the resource
	_, err := r.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: resourceName, Namespace: namespace},
	})
	Expect(err).NotTo(HaveOccurred())

	// needed because we strictly check for only one configmap per provider profile
	Eventually(func() bool {
		currentPP := &cv1a1.ProviderProfile{}
		err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pp), currentPP)
		return errors.IsNotFound(err)
	}, time.Second*5, time.Millisecond*100).Should(BeTrue(), "ProviderProfile should be deleted before next test starts")

}

// returns the provider profile and a boolean indicating if it was created
func createProviderProfileAWS(ctx context.Context, k8sClient client.Client, typeNamespacedName types.NamespacedName) *cv1a1.ProviderProfile {
	provider := &cv1a1.ProviderProfile{}
	provider = &cv1a1.ProviderProfile{
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
	Expect(k8sClient.Create(ctx, provider)).To(Succeed())
	return provider
}

func createProviderProfileGCP(resourceName, namespace string) *cv1a1.ProviderProfile {
	provider := &cv1a1.ProviderProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: namespace,
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
	Expect(k8sClient.Create(ctx, provider)).To(Succeed())
	return provider
}
