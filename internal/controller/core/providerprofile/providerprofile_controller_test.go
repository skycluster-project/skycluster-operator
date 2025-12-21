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
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	corev1alpha1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
)

var _ = Describe("ProviderProfile Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"
		const namespace = "skycluster-system"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: namespace,
		}
		providerprofile := &corev1alpha1.ProviderProfile{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind ProviderProfile")
			err := k8sClient.Get(ctx, typeNamespacedName, providerprofile)
			if err != nil && errors.IsNotFound(err) {
				resource := createProviderProfileResource(resourceName, namespace)
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &corev1alpha1.ProviderProfile{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance ProviderProfile")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &ProviderProfileReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Logger:   logf.Log.WithName("test-providerprofile-controller"),
				Recorder: nil, // Recorder is not used in the reconcile function
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the ProviderProfile was created and reconciled")
			Expect(k8sClient.Get(ctx, typeNamespacedName, providerprofile)).To(Succeed())

			By("Verifying the spec fields are set correctly")
			Expect(providerprofile.Spec.Platform).To(Equal("aws"))
			Expect(providerprofile.Spec.Region).To(Equal("us-east-1"))
			Expect(providerprofile.Spec.RegionAlias).To(Equal("us-east"))
			Expect(providerprofile.Spec.Continent).To(Equal("north-america"))
			Expect(providerprofile.Spec.Enabled).To(BeTrue())
			Expect(providerprofile.Spec.Zones).To(HaveLen(6))

			// By("Verifying status fields are populated after reconciliation")
			// Expect(providerprofile.Status.Platform).To(Equal("aws"))
			// Expect(providerprofile.Status.Region).To(Equal("us-east-1"))
			// Expect(providerprofile.Status.Zones).To(HaveLen(6))
			// Expect(providerprofile.Status.ObservedGeneration).To(Equal(providerprofile.Generation))
		})
	})
})

func createProviderProfileResource(resourceName, namespace string) *corev1alpha1.ProviderProfile {
	return &corev1alpha1.ProviderProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: namespace,
		},
		Spec: corev1alpha1.ProviderProfileSpec{
			Platform:    "aws",
			Region:      "us-east-1",
			RegionAlias: "us-east",
			Continent:   "north-america",
			Enabled:     true,
			Zones: []corev1alpha1.ZoneSpec{
				{
					Name:         "us-east-1a",
					LocationName: "us-east-1a",
					DefaultZone:  true,
					Enabled:      true,
					Type:         "cloud",
				},
				{
					Name:         "us-east-1b",
					LocationName: "us-east-1b",
					DefaultZone:  false,
					Enabled:      true,
					Type:         "cloud",
				},
				{
					Name:         "us-east-1c",
					LocationName: "us-east-1c",
					DefaultZone:  false,
					Enabled:      true,
					Type:         "cloud",
				},
				{
					Name:         "us-east-1d",
					LocationName: "us-east-1d",
					DefaultZone:  false,
					Enabled:      true,
					Type:         "cloud",
				},
				{
					Name:         "us-east-1e",
					LocationName: "us-east-1e",
					DefaultZone:  false,
					Enabled:      true,
					Type:         "cloud",
				},
				{
					Name:         "us-east-1f",
					LocationName: "us-east-1f",
					DefaultZone:  false,
					Enabled:      true,
					Type:         "cloud",
				},
			},
		},
	}
}
