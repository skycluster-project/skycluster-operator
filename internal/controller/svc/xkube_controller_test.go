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

package svc

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	hv1a1 "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"
	pv1a1 "github.com/skycluster-project/skycluster-operator/api/policy/v1alpha1"
	svcv1alpha1 "github.com/skycluster-project/skycluster-operator/api/svc/v1alpha1"
	pkglog "github.com/skycluster-project/skycluster-operator/pkg/v1alpha1/log"
)

var _ = Describe("XKube Controller", func() {
	Context("When reconciling a basic XKube resource", func() {
		const resourceName = "test-xkube"
		const namespace = "default"

		ctx := context.Background()

		BeforeEach(func() {})

		AfterEach(func() {
			By("Cleaning up test resources")
			typeNamespacedName := types.NamespacedName{Name: resourceName, Namespace: namespace}
			xkube := &svcv1alpha1.XKube{}
			if err := k8sClient.Get(ctx, typeNamespacedName, xkube); err == nil {
				Expect(k8sClient.Delete(ctx, xkube)).To(Succeed())
			}

			// Also clean up created policies
			dp := &pv1a1.DeploymentPolicy{}
			if err := k8sClient.Get(ctx, typeNamespacedName, dp); err == nil {
				Expect(k8sClient.Delete(ctx, dp)).To(Succeed())
			}
			df := &pv1a1.DataflowPolicy{}
			if err := k8sClient.Get(ctx, typeNamespacedName, df); err == nil {
				Expect(k8sClient.Delete(ctx, df)).To(Succeed())
			}
		})

		It("should successfully reconcile and create DeploymentPolicy", func() {
			By("Setting up the reconciler")
			reconciler := &XKubeReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Logger: zap.New(pkglog.CustomLogger()).WithName("[XKube]"),
			}

			By("Triggering reconciliation")

			xkube := createXKubeWithAlternativeNodeGroups(resourceName, namespace)
			Expect(k8sClient.Create(ctx, xkube)).To(Succeed())

			res, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: resourceName, Namespace: namespace},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(res).To(Equal(reconcile.Result{}))

			By("Verifying DeploymentPolicy was created")
			dp := &pv1a1.DeploymentPolicy{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: namespace}, dp)).To(Succeed())
			Expect(dp.Name).To(Equal(resourceName))
			Expect(dp.Namespace).To(Equal(namespace))

			By("Verifying DeploymentPolicy has correct labels")
			Expect(dp.Labels).To(HaveKeyWithValue("skycluster.io/app-id", xkube.Spec.ApplicationID))
			Expect(dp.Labels).To(HaveKeyWithValue("skycluster.io/app-scope", "distributed"))

			By("Verifying DeploymentPolicy has owner reference")
			Expect(dp.OwnerReferences).To(HaveLen(1))
			Expect(dp.OwnerReferences[0].Kind).To(Equal("XKube"))
			Expect(dp.OwnerReferences[0].Name).To(Equal(resourceName))

			By("Verifying DeploymentPolicy spec")
			Expect(dp.Spec.ExecutionEnvironment).To(Equal("Kubernetes"))
			Expect(dp.Spec.DeploymentPolicies).To(HaveLen(2))
			Expect(dp.Spec.DeploymentPolicies[0].ComponentRef.Kind).To(Equal("XNodeGroup"))
			Expect(dp.Spec.DeploymentPolicies[0].ComponentRef.Name).To(Equal(resourceName + "-0"))
			By("Verifying XNodeGroup virtual service constraints")
			Expect(dp.Spec.DeploymentPolicies[0].VirtualServiceConstraint).To(HaveLen(1))
			By("Verifying alternative ComputeProfile for the XNodeGroup")
			Expect(dp.Spec.DeploymentPolicies[0].VirtualServiceConstraint[0].AnyOf).To(HaveLen(2))

			Expect(dp.Spec.DeploymentPolicies[1].ComponentRef.Kind).To(Equal("XNodeGroup"))
			Expect(dp.Spec.DeploymentPolicies[1].ComponentRef.Name).To(Equal(resourceName + "-1"))
			By("Verifying XNodeGroup virtual service constraints")
			Expect(dp.Spec.DeploymentPolicies[1].VirtualServiceConstraint).To(HaveLen(1))
			By("Verifying alternative ComputeProfile for the XNodeGroup")
			Expect(dp.Spec.DeploymentPolicies[1].VirtualServiceConstraint[0].AnyOf).To(HaveLen(1))

			By("Verifying DataflowPolicy was created")
			df := &pv1a1.DataflowPolicy{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: namespace}, df)).To(Succeed())
			Expect(err).NotTo(HaveOccurred())
			Expect(df.Name).To(Equal(resourceName))
			Expect(df.Namespace).To(Equal(namespace))

			By("Verifying DataflowPolicy has correct labels")
			Expect(df.Labels).To(HaveKeyWithValue("skycluster.io/app-id", xkube.Spec.ApplicationID))
			Expect(df.Labels).To(HaveKeyWithValue("skycluster.io/app-scope", "distributed"))

			By("Verifying DataflowPolicy has owner reference")
			Expect(df.OwnerReferences).To(HaveLen(1))
			Expect(df.OwnerReferences[0].Kind).To(Equal("XKube"))
			Expect(df.OwnerReferences[0].Name).To(Equal(resourceName))

			By("Verifying DataflowPolicy has empty data dependencies")
			Expect(df.Spec.DataDependencies).To(BeEmpty())
		})

	})

})

func createXKubeWithAlternativeNodeGroups(resourceName, namespace string) *svcv1alpha1.XKube {
	return &svcv1alpha1.XKube{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: "default",
		},
		Spec: svcv1alpha1.XKubeSpec{
			ApplicationID: "test-app-123",
			// Expect two nodegroups, one with two alternative ComputeProfiles and
			// one with one alternative ComputeProfile
			NodeGroups: []svcv1alpha1.NodeGroup{
				{
					InstanceTypes: []hv1a1.ComputeFlavor{
						// Alternative ComputeProfile for the XNodeGroup
						{GPU: hv1a1.GPU{Model: "A100"}},
						{GPU: hv1a1.GPU{Model: "A10G"}},
					},
				},
				{
					InstanceTypes: []hv1a1.ComputeFlavor{
						{GPU: hv1a1.GPU{Model: "L4"}},
					},
					PublicAccess: true,
					AutoScaling: &svcv1alpha1.AutoScaling{
						MinSize: 1,
						MaxSize: 5,
					},
				},
			},
		},
	}
}
