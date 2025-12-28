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

var _ = Describe("XInstance Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"
		const namespace = "default"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: namespace,
		}

		BeforeEach(func() {})

		AfterEach(func() {
			resource := &svcv1alpha1.XInstance{}
			if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should successfully reconcile the resource", func() {
			xinstance := createBasicXInstance(resourceName, namespace, false)
			Expect(k8sClient.Create(ctx, xinstance)).To(Succeed())

			By("Reconciling the created resource")
			reconciler := &XInstanceReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Logger: zap.New(pkglog.CustomLogger()).WithName("[XInstance]"),
			}

			res, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(res).To(Equal(reconcile.Result{}))

			By("Verifying DeploymentPolicy was created")
			dp := &pv1a1.DeploymentPolicy{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: namespace}, dp)).To(Succeed())
			Expect(dp.Name).To(Equal(resourceName))
			Expect(dp.Namespace).To(Equal(namespace))

			By("Verifying DeploymentPolicy has correct labels")
			Expect(dp.Labels).To(HaveKeyWithValue("skycluster.io/app-id", xinstance.Spec.ApplicationID))
			Expect(dp.Labels).To(HaveKeyWithValue("skycluster.io/app-scope", "distributed"))

			By("Verifying DeploymentPolicy has owner reference")
			Expect(dp.OwnerReferences).To(HaveLen(1))
			Expect(dp.OwnerReferences[0].Kind).To(Equal("XInstance"))
			Expect(dp.OwnerReferences[0].Name).To(Equal(resourceName))

			By("Verifying DeploymentPolicy spec")
			Expect(dp.Spec.ExecutionEnvironment).To(Equal("VirtualMachine"))
			Expect(dp.Spec.DeploymentPolicies).To(HaveLen(1))
			Expect(dp.Spec.DeploymentPolicies[0].ComponentRef.Kind).To(Equal("XInstance"))
			Expect(dp.Spec.DeploymentPolicies[0].ComponentRef.Name).To(Equal(resourceName))
			By("Verifying XInstance virtual service constraints")
			Expect(dp.Spec.DeploymentPolicies[0].VirtualServiceConstraint).To(HaveLen(1))
			By("Verifying alternative ComputeProfile for the XInstance")
			Expect(dp.Spec.DeploymentPolicies[0].VirtualServiceConstraint[0].AnyOf).To(HaveLen(1))

			By("Verifying DataflowPolicy was created")
			df := &pv1a1.DataflowPolicy{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: namespace}, df)).To(Succeed())
			Expect(df.Name).To(Equal(resourceName))
			Expect(df.Namespace).To(Equal(namespace))

			By("Verifying DataflowPolicy has correct labels")
			Expect(df.Labels).To(HaveKeyWithValue("skycluster.io/app-id", xinstance.Spec.ApplicationID))
			Expect(df.Labels).To(HaveKeyWithValue("skycluster.io/app-scope", "distributed"))

			By("Verifying DataflowPolicy has owner reference")
			Expect(df.OwnerReferences).To(HaveLen(1))
			Expect(df.OwnerReferences[0].Kind).To(Equal("XInstance"))
			Expect(df.OwnerReferences[0].Name).To(Equal(resourceName))

			By("Verifying DataflowPolicy has empty data dependencies")
			Expect(df.Spec.DataDependencies).To(BeEmpty())
		})
	})
})

func createBasicXInstance(resourceName, namespace string, preferSpot bool) *svcv1alpha1.XInstance {
	return &svcv1alpha1.XInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: namespace,
		},
		Spec: svcv1alpha1.XInstanceSpec{
			ApplicationID: resourceName,
			Flavor: hv1a1.ComputeFlavor{
				// VCPUs: "2",
				// RAM: "4GB",
				GPU: hv1a1.GPU{
					Model: "L4",
					// Unit: "1",
					// Memory: "32GB",
				},
			},
			Image:      "ubuntu-24.04",
			PreferSpot: preferSpot,
			PublicKey:  "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAACAQC8...user@example.com",
			PublicIP:   false,
			UserData:   "echo 'Hello, World!' > /tmp/hello.txt",
			SecurityGroups: &svcv1alpha1.SecurityGroups{
				TCPPorts: []svcv1alpha1.PortRange{
					{FromPort: 22, ToPort: 22, Protocol: "tcp"},
				},
			},
			RootVolumes: []svcv1alpha1.RootVolume{
				{Size: "20", Type: "gp2"},
			},
		},
	}
}
