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

package policy

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	pv1a1 "github.com/skycluster-project/skycluster-operator/api/policy/v1alpha1"
	pkglog "github.com/skycluster-project/skycluster-operator/pkg/v1alpha1/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("DataflowPolicy Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "skycluster-system",
		}
		dataflowpolicy := &pv1a1.DataflowPolicy{}

		BeforeEach(func() {

			// By("ensuring the namespace exists")
			// ns := &corev1.Namespace{
			// 	ObjectMeta: metav1.ObjectMeta{
			// 		Name: "skycluster-system",
			// 	},
			// }
			// err := k8sClient.Get(ctx, types.NamespacedName{Name: "skycluster-system"}, ns)
			// if err != nil && errors.IsNotFound(err) {
			// 	Expect(k8sClient.Create(ctx, ns)).To(Succeed())
			// }

			By("creating the custom resource for the Kind DataflowPolicy")
			err := k8sClient.Get(ctx, typeNamespacedName, dataflowpolicy)
			if err != nil && errors.IsNotFound(err) {
				resource := &pv1a1.DataflowPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "skycluster-system",
					},
					Spec: pv1a1.DataflowPolicySpec{
						DataDependencies: []pv1a1.DataDapendency{},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
				dataflowpolicy = resource
			}
		})

		AfterEach(func() {
			resource := &pv1a1.DataflowPolicy{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance DataflowPolicy")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should successfully reconcile the resource and create the ILPTask", func() {
			By("Reconciling the created resource")
			reconciler := &DataflowPolicyReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Logger: zap.New(pkglog.CustomLogger()).WithName("[DataflowPolicy]"),
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking the ILPTask is created")
			ilptask := &cv1a1.ILPTask{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: dataflowpolicy.Name, Namespace: dataflowpolicy.Namespace}, ilptask)).To(Succeed())
			Expect(ilptask.Spec.DataflowPolicyRef.Name).To(Equal(dataflowpolicy.Name))
			Expect(ilptask.Spec.DataflowPolicyRef.DataflowResourceVersion).To(Equal(dataflowpolicy.GetResourceVersion()))
		})
	})
})
