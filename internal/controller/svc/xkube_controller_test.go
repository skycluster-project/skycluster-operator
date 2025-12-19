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

	hv1a1 "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"
	pv1a1 "github.com/skycluster-project/skycluster-operator/api/policy/v1alpha1"
	svcv1alpha1 "github.com/skycluster-project/skycluster-operator/api/svc/v1alpha1"
)

var _ = Describe("XKube Controller", func() {
	// Test Context: Basic Reconciliation
	// This test demonstrates the fundamental pattern of testing a Kubernetes controller:
	// 1. Create a test resource
	// 2. Trigger reconciliation
	// 3. Verify the expected side effects (created resources, updated status, etc.)
	Context("When reconciling a basic XKube resource", func() {
		const resourceName = "test-xkube-basic"

		ctx := context.Background()
		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			By("Creating a minimal XKube resource for testing")
			// Best Practice: Use BeforeEach to set up test state
			// This ensures each test starts with a clean, known state
			xkube := &svcv1alpha1.XKube{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: svcv1alpha1.XKubeSpec{
					ApplicationID: "test-app-123",
					NodeGroups: []svcv1alpha1.NodeGroup{
						{
							InstanceTypes: []hv1a1.ComputeFlavor{
								{
									VCPUs: "4",
									RAM:   "16GB",
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, xkube)).To(Succeed())
		})

		AfterEach(func() {
			By("Cleaning up test resources")
			// Best Practice: Always clean up resources in AfterEach
			// This prevents test pollution and ensures tests are independent
			xkube := &svcv1alpha1.XKube{}
			err := k8sClient.Get(ctx, typeNamespacedName, xkube)
			if err == nil {
				Expect(k8sClient.Delete(ctx, xkube)).To(Succeed())
			}
			// Also clean up created policies
			dp := &pv1a1.DeploymentPolicy{}
			if err := k8sClient.Get(ctx, typeNamespacedName, dp); err == nil {
				k8sClient.Delete(ctx, dp)
			}
			df := &pv1a1.DataflowPolicy{}
			if err := k8sClient.Get(ctx, typeNamespacedName, df); err == nil {
				k8sClient.Delete(ctx, df)
			}
		})

		// It("should successfully reconcile and create DeploymentPolicy", func() {
		// 	By("Setting up the reconciler")
		// 	// Best Practice: Create the reconciler with the test client
		// 	// This allows us to verify state changes in the test environment
		// 	reconciler := &XKubeReconciler{
		// 		Client: k8sClient,
		// 		Scheme: k8sClient.Scheme(),
		// 	}

		// 	By("Triggering reconciliation")
		// 	// Best Practice: Call Reconcile directly to test controller logic
		// 	// In integration tests, you might use the manager, but unit tests call Reconcile directly
		// 	result, err := reconciler.Reconcile(ctx, reconcile.Request{
		// 		NamespacedName: typeNamespacedName,
		// 	})
		// 	Expect(err).NotTo(HaveOccurred())
		// 	Expect(result.Requeue).To(BeFalse())

		// 	By("Verifying DeploymentPolicy was created")
		// 	// Best Practice: Verify side effects explicitly
		// 	// Don't just check for no errors - verify the expected resources exist
		// 	dp := &pv1a1.DeploymentPolicy{}
		// 	err = k8sClient.Get(ctx, typeNamespacedName, dp)
		// 	Expect(err).NotTo(HaveOccurred())
		// 	Expect(dp.Name).To(Equal(resourceName))
		// 	Expect(dp.Namespace).To(Equal("default"))

		// 	By("Verifying DeploymentPolicy has correct labels")
		// 	// Best Practice: Test metadata like labels, annotations, owner references
		// 	// These are often critical for Kubernetes resource relationships
		// 	Expect(dp.Labels).To(HaveKeyWithValue("skycluster.io/app-id", "test-app-123"))
		// 	Expect(dp.Labels).To(HaveKeyWithValue("skycluster.io/app-scope", "distributed"))

		// 	By("Verifying DeploymentPolicy has owner reference")
		// 	// Best Practice: Verify owner references for cascade deletion
		// 	Expect(dp.OwnerReferences).To(HaveLen(1))
		// 	Expect(dp.OwnerReferences[0].Kind).To(Equal("XKube"))
		// 	Expect(dp.OwnerReferences[0].Name).To(Equal(resourceName))

		// 	By("Verifying DeploymentPolicy spec")
		// 	// Best Practice: Verify the spec matches expectations
		// 	// This ensures the controller logic correctly transforms input to output
		// 	Expect(dp.Spec.ExecutionEnvironment).To(Equal("Kubernetes"))
		// 	Expect(dp.Spec.DeploymentPolicies).To(HaveLen(1))
		// 	Expect(dp.Spec.DeploymentPolicies[0].ComponentRef.Kind).To(Equal("XNodeGroup"))
		// 	Expect(dp.Spec.DeploymentPolicies[0].ComponentRef.Name).To(Equal(resourceName + "-0"))
		// })

		// It("should successfully reconcile and create DataflowPolicy", func() {
		// 	By("Setting up the reconciler")
		// 	reconciler := &XKubeReconciler{
		// 		Client: k8sClient,
		// 		Scheme: k8sClient.Scheme(),
		// 	}

		// 	By("Triggering reconciliation")
		// 	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		// 		NamespacedName: typeNamespacedName,
		// 	})
		// 	Expect(err).NotTo(HaveOccurred())

		// 	By("Verifying DataflowPolicy was created")
		// 	df := &pv1a1.DataflowPolicy{}
		// 	err = k8sClient.Get(ctx, typeNamespacedName, df)
		// 	Expect(err).NotTo(HaveOccurred())
		// 	Expect(df.Name).To(Equal(resourceName))
		// 	Expect(df.Namespace).To(Equal("default"))

		// 	By("Verifying DataflowPolicy has correct labels")
		// 	Expect(df.Labels).To(HaveKeyWithValue("skycluster.io/app-id", "test-app-123"))
		// 	Expect(df.Labels).To(HaveKeyWithValue("skycluster.io/app-scope", "distributed"))

		// 	By("Verifying DataflowPolicy has owner reference")
		// 	Expect(df.OwnerReferences).To(HaveLen(1))
		// 	Expect(df.OwnerReferences[0].Kind).To(Equal("XKube"))

		// 	By("Verifying DataflowPolicy has empty data dependencies")
		// 	// Best Practice: Verify edge cases and default values
		// 	// Empty lists, nil values, and defaults are important to test
		// 	Expect(df.Spec.DataDependencies).To(BeEmpty())
		// })
	})

	// Test Context: Multiple Node Groups
	// This test demonstrates testing with more complex input data
	Context("When reconciling XKube with multiple node groups", func() {
		const resourceName = "test-xkube-multi-nodegroup"

		ctx := context.Background()
		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			By("Creating XKube with multiple node groups")
			xkube := &svcv1alpha1.XKube{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: svcv1alpha1.XKubeSpec{
					ApplicationID: "test-app-multi",
					NodeGroups: []svcv1alpha1.NodeGroup{
						{
							InstanceTypes: []hv1a1.ComputeFlavor{
								{VCPUs: "4", RAM: "16GB"},
								{VCPUs: "8", RAM: "32GB"},
							},
						},
						{
							InstanceTypes: []hv1a1.ComputeFlavor{
								{GPU: hv1a1.GPU{Model: "L4"}},
							},
							PublicAccess: true,
							AutoScaling: &svcv1alpha1.AutoScaling{
								MinSize: 2,
								MaxSize: 5,
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, xkube)).To(Succeed())
		})

		AfterEach(func() {
			xkube := &svcv1alpha1.XKube{}
			if err := k8sClient.Get(ctx, typeNamespacedName, xkube); err == nil {
				k8sClient.Delete(ctx, xkube)
			}
			dp := &pv1a1.DeploymentPolicy{}
			if err := k8sClient.Get(ctx, typeNamespacedName, dp); err == nil {
				k8sClient.Delete(ctx, dp)
			}
			df := &pv1a1.DataflowPolicy{}
			if err := k8sClient.Get(ctx, typeNamespacedName, df); err == nil {
				k8sClient.Delete(ctx, df)
			}
		})

		// It("should create DeploymentPolicy with multiple components", func() {
		// 	By("Triggering reconciliation")
		// 	reconciler := &XKubeReconciler{
		// 		Client: k8sClient,
		// 		Scheme: k8sClient.Scheme(),
		// 	}
		// 	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		// 		NamespacedName: typeNamespacedName,
		// 	})
		// 	Expect(err).NotTo(HaveOccurred())

		// 	By("Verifying DeploymentPolicy has correct number of components")
		// 	// Best Practice: Test that the controller correctly handles collections
		// 	// Verify the count and structure of items in lists/arrays
		// 	dp := &pv1a1.DeploymentPolicy{}
		// 	Expect(k8sClient.Get(ctx, typeNamespacedName, dp)).To(Succeed())
		// 	Expect(dp.Spec.DeploymentPolicies).To(HaveLen(2))

		// 	By("Verifying first node group component")
		// 	comp0 := dp.Spec.DeploymentPolicies[0]
		// 	Expect(comp0.ComponentRef.Name).To(Equal(resourceName + "-0"))
		// 	Expect(comp0.ComponentRef.Kind).To(Equal("XNodeGroup"))
		// 	// Verify AnyOf selectors for multiple instance types
		// 	Expect(comp0.VirtualServiceConstraint).To(HaveLen(1))
		// 	Expect(comp0.VirtualServiceConstraint[0].AnyOf).To(HaveLen(2))

		// 	By("Verifying second node group component")
		// 	comp1 := dp.Spec.DeploymentPolicies[1]
		// 	Expect(comp1.ComponentRef.Name).To(Equal(resourceName + "-1"))
		// 	Expect(comp1.VirtualServiceConstraint[0].AnyOf).To(HaveLen(1))
		// })
	})

	// Test Context: Provider Reference
	// This test demonstrates testing with optional fields and conditional logic
	Context("When reconciling XKube with provider reference", func() {
		const resourceName = "test-xkube-provider"

		ctx := context.Background()
		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			By("Creating XKube with provider reference")
			xkube := &svcv1alpha1.XKube{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: svcv1alpha1.XKubeSpec{
					ApplicationID: "test-app-provider",
					NodeGroups: []svcv1alpha1.NodeGroup{
						{
							InstanceTypes: []hv1a1.ComputeFlavor{
								{VCPUs: "4", RAM: "16GB"},
							},
						},
					},
					ProviderRef: hv1a1.ProviderRefSpec{
						Platform: "AWS",
						Region:   "us-east-1",
						Name:     "test-provider",
					},
				},
			}
			Expect(k8sClient.Create(ctx, xkube)).To(Succeed())
		})

		AfterEach(func() {
			xkube := &svcv1alpha1.XKube{}
			if err := k8sClient.Get(ctx, typeNamespacedName, xkube); err == nil {
				k8sClient.Delete(ctx, xkube)
			}
			dp := &pv1a1.DeploymentPolicy{}
			if err := k8sClient.Get(ctx, typeNamespacedName, dp); err == nil {
				k8sClient.Delete(ctx, dp)
			}
			df := &pv1a1.DataflowPolicy{}
			if err := k8sClient.Get(ctx, typeNamespacedName, df); err == nil {
				k8sClient.Delete(ctx, df)
			}
		})

		// It("should create DeploymentPolicy with location constraint", func() {
		// 	By("Triggering reconciliation")
		// 	reconciler := &XKubeReconciler{
		// 		Client: k8sClient,
		// 		Scheme: k8sClient.Scheme(),
		// 	}
		// 	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		// 		NamespacedName: typeNamespacedName,
		// 	})
		// 	Expect(err).NotTo(HaveOccurred())

		// 	By("Verifying location constraint is set")
		// 	// Best Practice: Test conditional logic paths
		// 	// When code has if/else branches, test both paths
		// 	dp := &pv1a1.DeploymentPolicy{}
		// 	Expect(k8sClient.Get(ctx, typeNamespacedName, dp)).To(Succeed())
		// 	comp := dp.Spec.DeploymentPolicies[0]
		// 	Expect(comp.LocationConstraint.Permitted).To(HaveLen(1))
		// 	Expect(comp.LocationConstraint.Permitted[0].Platform).To(Equal("AWS"))
		// 	Expect(comp.LocationConstraint.Permitted[0].Region).To(Equal("us-east-1"))
		// })
	})

	// Test Context: Resource Not Found
	// This test demonstrates testing error handling and edge cases
	Context("When reconciling a non-existent resource", func() {
		// It("should return without error when resource is not found", func() {
		// 	By("Attempting to reconcile a non-existent resource")
		// 	// Best Practice: Test error handling paths
		// 	// Controllers should handle missing resources gracefully
		// 	reconciler := &XKubeReconciler{
		// 		Client: k8sClient,
		// 		Scheme: k8sClient.Scheme(),
		// 	}

		// 	nonExistentName := types.NamespacedName{
		// 		Name:      "non-existent-resource",
		// 		Namespace: "default",
		// 	}

		// 	result, err := reconciler.Reconcile(context.Background(), reconcile.Request{
		// 		NamespacedName: nonExistentName,
		// 	})

		// 	By("Verifying no error is returned")
		// 	// Best Practice: Verify that expected errors are handled correctly
		// 	// client.IgnoreNotFound should prevent errors for missing resources
		// 	Expect(err).NotTo(HaveOccurred())
		// 	Expect(result.Requeue).To(BeFalse())
		// })
	})

	// Test Context: Deletion Handling
	// This test demonstrates testing deletion logic
	Context("When reconciling a resource being deleted", func() {
		const resourceName = "test-xkube-deleting"

		// ctx := context.Background()
		// typeNamespacedName := types.NamespacedName{
		// 	Name:      resourceName,
		// 	Namespace: "default",
		// }

		// It("should skip reconciliation when deletion timestamp is set", func() {
		// 	By("Creating XKube resource")
		// 	xkube := &svcv1alpha1.XKube{
		// 		ObjectMeta: metav1.ObjectMeta{
		// 			Name:      resourceName,
		// 			Namespace: "default",
		// 		},
		// 		Spec: svcv1alpha1.XKubeSpec{
		// 			ApplicationID: "test-app-deleting",
		// 			NodeGroups: []svcv1alpha1.NodeGroup{
		// 				{
		// 					InstanceTypes: []hv1a1.ComputeFlavor{
		// 						{VCPUs: "4", RAM: "16GB"},
		// 					},
		// 				},
		// 			},
		// 		},
		// 	}
		// 	Expect(k8sClient.Create(ctx, xkube)).To(Succeed())

		// 	By("Setting deletion timestamp")
		// 	// Best Practice: Test deletion logic explicitly
		// 	// Many controllers have special handling for resources being deleted
		// 	now := metav1.Now()
		// 	xkube.DeletionTimestamp = &now
		// 	Expect(k8sClient.Update(ctx, xkube)).To(Succeed())

		// 	By("Triggering reconciliation")
		// 	reconciler := &XKubeReconciler{
		// 		Client: k8sClient,
		// 		Scheme: k8sClient.Scheme(),
		// 	}
		// 	result, err := reconciler.Reconcile(ctx, reconcile.Request{
		// 		NamespacedName: typeNamespacedName,
		// 	})

		// 	By("Verifying reconciliation is skipped")
		// 	Expect(err).NotTo(HaveOccurred())
		// 	Expect(result.Requeue).To(BeFalse())

		// 	By("Verifying no policies were created")
		// 	// Best Practice: Verify that side effects don't occur when they shouldn't
		// 	dp := &pv1a1.DeploymentPolicy{}
		// 	err = k8sClient.Get(ctx, typeNamespacedName, dp)
		// 	Expect(errors.IsNotFound(err)).To(BeTrue())

		// 	By("Cleaning up")
		// 	Expect(k8sClient.Delete(ctx, xkube)).To(Succeed())
		// })
	})

	// Test Context: Update Scenarios
	// This test demonstrates testing idempotency and update logic
	Context("When reconciling an existing XKube with existing policies", func() {
		const resourceName = "test-xkube-update"

		ctx := context.Background()
		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			By("Creating XKube resource")
			xkube := &svcv1alpha1.XKube{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: svcv1alpha1.XKubeSpec{
					ApplicationID: "test-app-update",
					NodeGroups: []svcv1alpha1.NodeGroup{
						{
							InstanceTypes: []hv1a1.ComputeFlavor{
								{VCPUs: "4", RAM: "16GB"},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, xkube)).To(Succeed())
		})

		AfterEach(func() {
			xkube := &svcv1alpha1.XKube{}
			if err := k8sClient.Get(ctx, typeNamespacedName, xkube); err == nil {
				k8sClient.Delete(ctx, xkube)
			}
			dp := &pv1a1.DeploymentPolicy{}
			if err := k8sClient.Get(ctx, typeNamespacedName, dp); err == nil {
				k8sClient.Delete(ctx, dp)
			}
			df := &pv1a1.DataflowPolicy{}
			if err := k8sClient.Get(ctx, typeNamespacedName, df); err == nil {
				k8sClient.Delete(ctx, df)
			}
		})

		// It("should update existing policies when application ID changes", func() {
		// 	By("First reconciliation - creating policies")
		// 	reconciler := &XKubeReconciler{
		// 		Client: k8sClient,
		// 		Scheme: k8sClient.Scheme(),
		// 	}
		// 	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		// 		NamespacedName: typeNamespacedName,
		// 	})
		// 	Expect(err).NotTo(HaveOccurred())

		// 	By("Verifying initial policies are created")
		// 	dp := &pv1a1.DeploymentPolicy{}
		// 	Expect(k8sClient.Get(ctx, typeNamespacedName, dp)).To(Succeed())
		// 	Expect(dp.Labels["skycluster.io/app-id"]).To(Equal("test-app-update"))

		// 	By("Updating XKube application ID")
		// 	// Best Practice: Test update scenarios
		// 	// Controllers should handle updates gracefully and update dependent resources
		// 	xkube := &svcv1alpha1.XKube{}
		// 	Expect(k8sClient.Get(ctx, typeNamespacedName, xkube)).To(Succeed())
		// 	xkube.Spec.ApplicationID = "test-app-updated"
		// 	Expect(k8sClient.Update(ctx, xkube)).To(Succeed())

		// 	By("Second reconciliation - updating policies")
		// 	_, err = reconciler.Reconcile(ctx, reconcile.Request{
		// 		NamespacedName: typeNamespacedName,
		// 	})
		// 	Expect(err).NotTo(HaveOccurred())

		// 	By("Verifying policies are updated")
		// 	// Best Practice: Verify that updates are applied correctly
		// 	// Re-fetch the resource to get the latest state
		// 	Expect(k8sClient.Get(ctx, typeNamespacedName, dp)).To(Succeed())
		// 	Expect(dp.Labels["skycluster.io/app-id"]).To(Equal("test-app-updated"))
		// })

		// It("should add owner reference to existing policy if missing", func() {
		// 	By("Creating policy manually without owner reference")
		// 	// Best Practice: Test edge cases where resources exist in unexpected states
		// 	// This simulates scenarios like manual resource creation or migration
		// 	dp := &pv1a1.DeploymentPolicy{
		// 		ObjectMeta: metav1.ObjectMeta{
		// 			Name:      resourceName,
		// 			Namespace: "default",
		// 			Labels: map[string]string{
		// 				"skycluster.io/app-id": "test-app-update",
		// 			},
		// 		},
		// 		Spec: pv1a1.DeploymentPolicySpec{
		// 			ExecutionEnvironment: "Kubernetes",
		// 			DeploymentPolicies:   []pv1a1.DeploymentPolicyItem{},
		// 		},
		// 	}
		// 	Expect(k8sClient.Create(ctx, dp)).To(Succeed())

		// 	By("Triggering reconciliation")
		// 	reconciler := &XKubeReconciler{
		// 		Client: k8sClient,
		// 		Scheme: k8sClient.Scheme(),
		// 	}
		// 	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		// 		NamespacedName: typeNamespacedName,
		// 	})
		// 	Expect(err).NotTo(HaveOccurred())

		// 	By("Verifying owner reference is added")
		// 	// Best Practice: Test that controllers fix missing metadata
		// 	// Owner references, labels, and annotations should be maintained
		// 	Expect(k8sClient.Get(ctx, typeNamespacedName, dp)).To(Succeed())
		// 	Expect(dp.OwnerReferences).To(HaveLen(1))
		// 	Expect(dp.OwnerReferences[0].Kind).To(Equal("XKube"))
		// 	Expect(dp.OwnerReferences[0].Name).To(Equal(resourceName))
		// })
	})

	// Test Context: Idempotency
	// This test demonstrates that reconciliation is idempotent
	Context("When reconciling the same resource multiple times", func() {
		const resourceName = "test-xkube-idempotent"

		ctx := context.Background()
		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			xkube := &svcv1alpha1.XKube{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: svcv1alpha1.XKubeSpec{
					ApplicationID: "test-app-idempotent",
					NodeGroups: []svcv1alpha1.NodeGroup{
						{
							InstanceTypes: []hv1a1.ComputeFlavor{
								{VCPUs: "4", RAM: "16GB"},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, xkube)).To(Succeed())
		})

		AfterEach(func() {
			xkube := &svcv1alpha1.XKube{}
			if err := k8sClient.Get(ctx, typeNamespacedName, xkube); err == nil {
				k8sClient.Delete(ctx, xkube)
			}
			dp := &pv1a1.DeploymentPolicy{}
			if err := k8sClient.Get(ctx, typeNamespacedName, dp); err == nil {
				k8sClient.Delete(ctx, dp)
			}
			df := &pv1a1.DataflowPolicy{}
			if err := k8sClient.Get(ctx, typeNamespacedName, df); err == nil {
				k8sClient.Delete(ctx, df)
			}
		})

		// It("should be idempotent - multiple reconciliations produce same result", func() {
		// 	By("Setting up reconciler")
		// 	reconciler := &XKubeReconciler{
		// 		Client: k8sClient,
		// 		Scheme: k8sClient.Scheme(),
		// 	}

		// 	By("First reconciliation")
		// 	// Best Practice: Test idempotency
		// 	// Controllers should be safe to run multiple times with the same input
		// 	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		// 		NamespacedName: typeNamespacedName,
		// 	})
		// 	Expect(err).NotTo(HaveOccurred())

		// 	By("Capturing state after first reconciliation")
		// 	dp1 := &pv1a1.DeploymentPolicy{}
		// 	Expect(k8sClient.Get(ctx, typeNamespacedName, dp1)).To(Succeed())
		// 	df1 := &pv1a1.DataflowPolicy{}
		// 	Expect(k8sClient.Get(ctx, typeNamespacedName, df1)).To(Succeed())

		// 	By("Second reconciliation")
		// 	_, err = reconciler.Reconcile(ctx, reconcile.Request{
		// 		NamespacedName: typeNamespacedName,
		// 	})
		// 	Expect(err).NotTo(HaveOccurred())

		// 	By("Verifying state is unchanged")
		// 	// Best Practice: Verify that repeated reconciliations don't cause unnecessary changes
		// 	// This prevents resource churn and API server load
		// 	dp2 := &pv1a1.DeploymentPolicy{}
		// 	Expect(k8sClient.Get(ctx, typeNamespacedName, dp2)).To(Succeed())
		// 	df2 := &pv1a1.DataflowPolicy{}
		// 	Expect(k8sClient.Get(ctx, typeNamespacedName, df2)).To(Succeed())

		// 	// ResourceVersion should be the same if no changes were made
		// 	// (Note: In practice, ResourceVersion might change due to status updates,
		// 	// but the spec should remain the same)
		// 	Expect(dp1.Spec).To(Equal(dp2.Spec))
		// 	Expect(df1.Spec).To(Equal(df2.Spec))
		// })
	})

	// Test Context: Virtual Service Constraints
	// This test demonstrates testing complex nested structures
	Context("When reconciling XKube with GPU instance types", func() {
		const resourceName = "test-xkube-gpu"

		ctx := context.Background()
		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			xkube := &svcv1alpha1.XKube{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: svcv1alpha1.XKubeSpec{
					ApplicationID: "test-app-gpu",
					NodeGroups: []svcv1alpha1.NodeGroup{
						{
							InstanceTypes: []hv1a1.ComputeFlavor{
								{
									GPU: hv1a1.GPU{Model: "A100"},
								},
								{
									GPU: hv1a1.GPU{Model: "L4"},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, xkube)).To(Succeed())
		})

		AfterEach(func() {
			xkube := &svcv1alpha1.XKube{}
			if err := k8sClient.Get(ctx, typeNamespacedName, xkube); err == nil {
				k8sClient.Delete(ctx, xkube)
			}
			dp := &pv1a1.DeploymentPolicy{}
			if err := k8sClient.Get(ctx, typeNamespacedName, dp); err == nil {
				k8sClient.Delete(ctx, dp)
			}
			df := &pv1a1.DataflowPolicy{}
			if err := k8sClient.Get(ctx, typeNamespacedName, df); err == nil {
				k8sClient.Delete(ctx, df)
			}
		})

		// It("should create VirtualServiceConstraints with GPU flavors", func() {
		// 	By("Triggering reconciliation")
		// 	reconciler := &XKubeReconciler{
		// 		Client: k8sClient,
		// 		Scheme: k8sClient.Scheme(),
		// 	}
		// 	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		// 		NamespacedName: typeNamespacedName,
		// 	})
		// 	Expect(err).NotTo(HaveOccurred())

		// 	By("Verifying VirtualServiceConstraints are created correctly")
		// 	// Best Practice: Test complex nested data structures
		// 	// Verify that the controller correctly serializes and structures complex data
		// 	dp := &pv1a1.DeploymentPolicy{}
		// 	Expect(k8sClient.Get(ctx, typeNamespacedName, dp)).To(Succeed())

		// 	comp := dp.Spec.DeploymentPolicies[0]
		// 	Expect(comp.VirtualServiceConstraint).To(HaveLen(1))
		// 	Expect(comp.VirtualServiceConstraint[0].AnyOf).To(HaveLen(2))

		// 	By("Verifying VirtualService selectors have correct structure")
		// 	for _, selector := range comp.VirtualServiceConstraint[0].AnyOf {
		// 		Expect(selector.VirtualService.Kind).To(Equal("ComputeProfile"))
		// 		Expect(selector.Count).To(Equal(1))
		// 		Expect(selector.VirtualService.Spec).NotTo(BeNil())
		// 	}
		// })
	})

	// Test Context: Empty Provider Reference
	// This test demonstrates testing with empty/zero values
	Context("When reconciling XKube without provider reference", func() {
		const resourceName = "test-xkube-no-provider"

		ctx := context.Background()
		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			xkube := &svcv1alpha1.XKube{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: svcv1alpha1.XKubeSpec{
					ApplicationID: "test-app-no-provider",
					NodeGroups: []svcv1alpha1.NodeGroup{
						{
							InstanceTypes: []hv1a1.ComputeFlavor{
								{VCPUs: "4", RAM: "16GB"},
							},
						},
					},
					// ProviderRef is empty/zero value
				},
			}
			Expect(k8sClient.Create(ctx, xkube)).To(Succeed())
		})

		AfterEach(func() {
			xkube := &svcv1alpha1.XKube{}
			if err := k8sClient.Get(ctx, typeNamespacedName, xkube); err == nil {
				k8sClient.Delete(ctx, xkube)
			}
			dp := &pv1a1.DeploymentPolicy{}
			if err := k8sClient.Get(ctx, typeNamespacedName, dp); err == nil {
				k8sClient.Delete(ctx, dp)
			}
			df := &pv1a1.DataflowPolicy{}
			if err := k8sClient.Get(ctx, typeNamespacedName, df); err == nil {
				k8sClient.Delete(ctx, df)
			}
		})

		// It("should create DeploymentPolicy with empty location constraint", func() {
		// 	By("Triggering reconciliation")
		// 	reconciler := &XKubeReconciler{
		// 		Client: k8sClient,
		// 		Scheme: k8sClient.Scheme(),
		// 	}
		// 	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		// 		NamespacedName: typeNamespacedName,
		// 	})
		// 	Expect(err).NotTo(HaveOccurred())

		// 	By("Verifying location constraint is empty when provider is not set")
		// 	// Best Practice: Test with zero/empty values
		// 	// Ensure the controller handles optional fields correctly
		// 	dp := &pv1a1.DeploymentPolicy{}
		// 	Expect(k8sClient.Get(ctx, typeNamespacedName, dp)).To(Succeed())
		// 	comp := dp.Spec.DeploymentPolicies[0]
		// 	Expect(comp.LocationConstraint.Permitted).To(BeEmpty())
		// })
	})
})
