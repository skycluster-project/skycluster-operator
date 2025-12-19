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
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	corev1alpha1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
)

var _ = Describe("Latency Controller", func() {
	Context("When reconciling a Latency resource", func() {
		const resourceName = "latency-aws-us-east-1-aws-us-east-2"
		const namespace = "skycluster-system"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: namespace,
		}

		BeforeEach(func() {
			By("ensuring the namespace exists")
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespace,
				},
			}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: namespace}, ns)
			if err != nil && errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, ns)).To(Succeed())
			}
		})

		AfterEach(func() {
			By("cleaning up the Latency resource")
			resource := &corev1alpha1.Latency{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should successfully reconcile when status is empty", func() {
			By("creating a Latency resource with empty status")
			latency := &corev1alpha1.Latency{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: namespace,
					Labels: map[string]string{
						"skycluster.io/provider-pair":     "aws-us-east-1-aws-us-east-2",
						"skycluster.io/provider-platform": "aws",
						"skycluster.io/provider-region":   "us-east-1",
					},
				},
				Spec: corev1alpha1.LatencySpec{
					ProviderRefA:   corev1.LocalObjectReference{Name: "aws-us-east-1"},
					ProviderRefB:   corev1.LocalObjectReference{Name: "aws-us-east-2"},
					FixedLatencyMs: "16.92",
				},
				Status: corev1alpha1.LatencyStatus{},
			}
			Expect(k8sClient.Create(ctx, latency)).To(Succeed())

			By("reconciling the resource")
			controllerReconciler := &LatencyReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Logger: logf.Log.WithName("test-latency-controller"),
			}

			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())

			By("verifying the status was updated from spec")
			updatedLatency := &corev1alpha1.Latency{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updatedLatency)).To(Succeed())
			Expect(updatedLatency.Status.LastMeasuredMs).To(Equal("16.92")) // copied from spec
			Expect(updatedLatency.Status.P95).To(Not(BeEmpty()))
			Expect(updatedLatency.Status.P99).To(Not(BeEmpty()))
		})

		It("should work with different provider references", func() {
			By("creating a Latency resource with different providers")
			latency := &corev1alpha1.Latency{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: namespace,
					Labels: map[string]string{
						"skycluster.io/provider-pair":     "gcp-us-west1-aws-us-east-1",
						"skycluster.io/provider-platform": "gcp",
						"skycluster.io/provider-region":   "us-west1",
					},
				},
				Spec: corev1alpha1.LatencySpec{
					ProviderRefA:   corev1.LocalObjectReference{Name: "gcp-us-west1"},
					ProviderRefB:   corev1.LocalObjectReference{Name: "aws-us-east-1"},
					FixedLatencyMs: "16.92",
				},
				Status: corev1alpha1.LatencyStatus{},
			}
			Expect(k8sClient.Create(ctx, latency)).To(Succeed())

			By("reconciling the resource")
			controllerReconciler := &LatencyReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Logger: logf.Log.WithName("test-latency-controller"),
			}

			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())

			By("verifying the status was updated correctly")
			updatedLatency := &corev1alpha1.Latency{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updatedLatency)).To(Succeed())
			Expect(updatedLatency.Status.LastMeasuredMs).To(Equal("16.92")) // copied from spec
			Expect(updatedLatency.Status.P95).ToNot(BeEmpty())
			Expect(updatedLatency.Status.P99).ToNot(BeEmpty())
		})
	})
})
