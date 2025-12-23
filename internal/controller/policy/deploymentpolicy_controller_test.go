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
	hv1a1 "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"
	policyv1alpha1 "github.com/skycluster-project/skycluster-operator/api/policy/v1alpha1"
	pkglog "github.com/skycluster-project/skycluster-operator/pkg/v1alpha1/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("DeploymentPolicy Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "skycluster-system",
		}
		deploymentpolicy := &policyv1alpha1.DeploymentPolicy{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind DeploymentPolicy")
			err := k8sClient.Get(ctx, typeNamespacedName, deploymentpolicy)
			if err != nil && errors.IsNotFound(err) {
				resource := &policyv1alpha1.DeploymentPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "skycluster-system",
					},
					Spec: policyv1alpha1.DeploymentPolicySpec{
						ExecutionEnvironment: "Kubernetes",
						DeploymentPolicies: []policyv1alpha1.DeploymentPolicyItem{
							{
								// arbitrary component ref
								ComponentRef: hv1a1.ComponentRef{Name: resourceName},
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
				deploymentpolicy = resource
			}
		})

		AfterEach(func() {
			resource := &policyv1alpha1.DeploymentPolicy{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance DeploymentPolicy")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should successfully reconcile the resource and create the ILPTask", func() {
			By("Reconciling the created resource")
			reconciler := &DeploymentPolicyReconciler{
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
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: deploymentpolicy.Name, Namespace: deploymentpolicy.Namespace}, ilptask)).To(Succeed())
			Expect(ilptask.Spec.DeploymentPolicyRef.Name).To(Equal(deploymentpolicy.Name))
			Expect(ilptask.Spec.DeploymentPolicyRef.DeploymentPlanResourceVersion).To(Equal(deploymentpolicy.GetResourceVersion()))
		})
	})
})
