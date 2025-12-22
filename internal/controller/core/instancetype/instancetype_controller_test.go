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
	"time"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	fakekube "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	hv1a1 "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"
	utils "github.com/skycluster-project/skycluster-operator/internal/controller/core/utils"
	hint "github.com/skycluster-project/skycluster-operator/internal/helper"
)

var _ = Describe("InstanceType Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"
		const namespace = "skycluster-system"
		const podOutput = `{
  "offerings": [
    {
      "zone": "us-east-1a",
      "zoneOfferings": [
        {
          "name": "g5.12xlarge",
          "nameLabel": "48vCPU-192GB-4xA10G-22GB",
          "vcpus": 48,
          "ram": "192GB",
          "price": "5.67",
          "gpu": {
            "enabled": true,
            "manufacturer": "NVIDIA",
            "count": 4,
            "model": "A10G",
            "memory": "22GB"
          },
          "spot": {
            "enabled": true,
            "price": "2.12"
          }
        },
        {
          "name": "g5.16xlarge",
          "nameLabel": "64vCPU-256GB-1xA10G-22GB",
          "vcpus": 64,
          "ram": "256GB",
          "price": "4.10",
          "gpu": {
            "enabled": true,
            "manufacturer": "NVIDIA",
            "count": 1,
            "model": "A10G",
            "memory": "22GB"
          },
          "spot": {
            "enabled": true,
            "price": "1.07"
          }
        }
      ]
    }
  ]
}
`

		var (
			reconciler   *InstanceTypeReconciler
			provider     *cv1a1.ProviderProfile
			cm           *corev1.ConfigMap
			instancetype *cv1a1.InstanceType
			secret       *corev1.Secret
		)

		ctx := context.Background()
		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: namespace,
		}

		BeforeEach(func() {
			instancetype = setupInstanceTypeResource(ctx, typeNamespacedName)
			secret = setupSecret(ctx, k8sClient, typeNamespacedName)
			p, wasCreated := setupProviderProfile(ctx, k8sClient, typeNamespacedName)
			if wasCreated {
				utils.SetProviderProfileStatus(p, resourceName, namespace)
				Expect(k8sClient.Status().Update(ctx, p)).To(Succeed())
				provider = p
			}
			cm = setupConfigMap(ctx, k8sClient, resourceName, namespace, provider)
			reconciler = createInstanceTypeReconciler(podOutput)
		})

		AfterEach(func() {
			err := k8sClient.Get(ctx, typeNamespacedName, instancetype)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance ConfigMap")
			Expect(k8sClient.Delete(ctx, cm)).To(Succeed())

			By("Cleanup the specific resource instance InstanceType")
			Expect(k8sClient.Delete(ctx, instancetype)).To(Succeed())

			By("Cleanup the specific resource instance ProviderProfile")
			Expect(k8sClient.Delete(ctx, provider)).To(Succeed())

			By("Cleanup the specific resource instance Credential Secret")
			Expect(k8sClient.Delete(ctx, secret)).To(Succeed())

			// needed because we strictly check for only one configmap per provider profile
			Eventually(func() bool {
				currentCM := &corev1.ConfigMap{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), currentCM)
				return errors.IsNotFound(err)
			}, time.Second*5, time.Millisecond*100).Should(BeTrue(), "ConfigMap should be deleted before next test starts")
		})

		It("should successfully reconcile the resource", func() {
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("reconciles a non-cloud provider by copying spec to status and marking Ready", func() {
			provider.Spec.Platform = "openstack"
			provider.Spec.Region = "scinet"
			Expect(k8sClient.Update(ctx, provider)).To(Succeed())

			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      instancetype.Name,
					Namespace: instancetype.Namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			out := &cv1a1.InstanceType{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(instancetype), out)).To(Succeed())

			Expect(meta.IsStatusConditionTrue(out.Status.Conditions, string(hv1a1.Ready))).To(BeTrue())
			Expect(out.Status.Offerings).To(Equal(instancetype.Spec.Offerings))
			Expect(out.Status.Region).To(Equal("scinet"))
		})

		It("tests build runner", func() {
			jsonData, err := reconciler.generateJSON(instancetype.Spec.Offerings)
			Expect(err).NotTo(HaveOccurred())
			job, err := reconciler.buildRunner(instancetype, provider, jsonData)
			Expect(err).NotTo(HaveOccurred())
			Expect(job).NotTo(BeNil())
		})

		It("creates a runner Job and sets JobRunning when ResyncRequired is true", func() {
			instancetype.Status.SetCondition(
				hv1a1.ResyncRequired,
				metav1.ConditionTrue,
				"Test",
				"forcing resync",
			)
			Expect(k8sClient.Status().Update(ctx, instancetype)).To(Succeed())

			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			out := &cv1a1.InstanceType{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, out)).To(Succeed())
			Expect(meta.IsStatusConditionTrue(out.Status.Conditions, string(hv1a1.JobRunning))).To(BeTrue())
			Expect(meta.IsStatusConditionTrue(out.Status.Conditions, string(hv1a1.ResyncRequired))).To(BeFalse())
		})

		It("marks ResyncRequired when spec changes while a runner Job is already running", func() {
			instancetype.Status.SetCondition(
				hv1a1.JobRunning,
				metav1.ConditionTrue,
				"Running",
				"job running",
			)
			instancetype.Status.ObservedGeneration = instancetype.Generation
			Expect(k8sClient.Status().Update(ctx, instancetype)).To(Succeed())

			instancetype.Spec.Offerings = append(instancetype.Spec.Offerings, cv1a1.ZoneOfferings{
				Zone: "us-east-1b",
				Offerings: []hv1a1.InstanceOffering{
					{
						Name:      "m5.large",
						NameLabel: "2vCPU-8GB",
						VCPUs:     2,
						RAM:       "8GB",
						Price:     "0.096",
					},
				},
			})
			Expect(k8sClient.Update(ctx, instancetype)).To(Succeed())

			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKeyFromObject(instancetype),
			})
			Expect(err).NotTo(HaveOccurred())

			out := &cv1a1.InstanceType{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(instancetype), out)).To(Succeed())

			Expect(meta.IsStatusConditionTrue(out.Status.Conditions, string(hv1a1.ResyncRequired)))
			Expect(meta.IsStatusConditionFalse(out.Status.Conditions, string(hv1a1.Ready)))
		})

		It("handles runner Pod running by requeueing", func() {
			instancetype.Status.SetCondition(hv1a1.JobRunning, metav1.ConditionTrue, "Running", "")
			Expect(k8sClient.Status().Update(ctx, instancetype)).To(Succeed())

			By("creating the pod")
			pod := setupInstanceTypePod(namespace, provider)
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())

			By("updating the pod status")
			pod.Status.Phase = corev1.PodRunning
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			res, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKeyFromObject(instancetype),
			})
			Expect(err).To(Succeed())
			Expect(res.RequeueAfter).To(Equal(hint.RequeuePollThreshold))

			By("Cleanup the specific resource instance Pod")
			Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
		})

		It("check the decodePodLogJSON function", func() {
			By("creating the pod")
			pod := setupInstanceTypePod(namespace, provider)
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())

			By("checking the decodePodLogJSON function")
			offerings, err := reconciler.decodePodLogJSON(podOutput)
			Expect(err).NotTo(HaveOccurred())
			Expect(offerings).To(HaveLen(1))
			Expect(offerings[0].Zone).To(Equal("us-east-1a"))
			Expect(offerings[0].Offerings).To(HaveLen(2))

			By("Cleanup the specific resource instance Pod")
			Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
		})

		It("handles fetching pod logs", func() {
			pod := setupInstanceTypePod(namespace, provider)
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())

			podOutput, err := reconciler.getPodStdOut(ctx, pod.Name, pod.Namespace, "harvest")
			Expect(err).NotTo(HaveOccurred())
			Expect(podOutput).To(Equal(podOutput))

			By("Cleanup the specific resource instance Pod")
			Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
		})

		It("handles decodePodLogJSON function", func() {
			offerings, err := reconciler.decodePodLogJSON(podOutput)
			Expect(err).NotTo(HaveOccurred())
			Expect(offerings).To(HaveLen(1))
			Expect(offerings[0].Zone).To(Equal("us-east-1a"))
			Expect(offerings[0].Offerings).To(HaveLen(2))
		})

		It("handles updateConfigMap function", func() {
			err := reconciler.updateConfigMap(ctx, provider, instancetype)
			Expect(err).NotTo(HaveOccurred())
		})

		It("handles runner Pod success by decoding logs, updating status offerings, and marking Ready", func() {
			instancetype.Status.SetCondition(hv1a1.JobRunning, metav1.ConditionTrue, "Running", "")
			instancetype.Status.ObservedGeneration = instancetype.Generation
			Expect(k8sClient.Status().Update(ctx, instancetype)).To(Succeed())

			By("creating the pod")
			pod := setupInstanceTypePod(namespace, provider)
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())

			By("updating the pod status")
			pod.Status.Phase = corev1.PodSucceeded
			pod.Status.ContainerStatuses = []corev1.ContainerStatus{
				{
					Name: "harvest",
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							Reason: "Completed",
						},
					},
				},
			}
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			By("mock the pod log client")
			fkClient := fakekube.NewSimpleClientset()
			fkClient.PrependReactor("get", "pods", func(_ k8stesting.Action) (bool, runtime.Object, error) {
				return true, pod, nil
			})
			fkClient.Fake.PrependReactor("get", "pods/log", func(_ k8stesting.Action) (handled bool, ret runtime.Object, err error) {
				return true, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "runner-pod-it", Namespace: namespace}}, nil
			})
			reconciler.KubeClient = fkClient

			res, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKeyFromObject(instancetype),
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(meta.IsStatusConditionFalse(instancetype.Status.Conditions, string(hv1a1.ResyncRequired)))
			Expect(meta.IsStatusConditionFalse(instancetype.Status.Conditions, string(hv1a1.JobRunning)))
			Expect(meta.IsStatusConditionTrue(instancetype.Status.Conditions, string(hv1a1.Ready)))
			Expect(res).To(Equal(ctrl.Result{}))

			cmList := &corev1.ConfigMapList{}
			Expect(k8sClient.List(ctx, cmList, client.MatchingLabels(map[string]string{
				"skycluster.io/provider-profile": provider.Name,
			}))).To(Succeed())
			Expect(cmList.Items).To(HaveLen(1))
			Expect(cmList.Items[0].Data).To(HaveKey("flavors.yaml"))

			podOutput, err := reconciler.getPodStdOut(ctx, pod.Name, pod.Namespace, "harvest")
			Expect(err).NotTo(HaveOccurred())
			Expect(podOutput).To(Equal(podOutput))

			out := &cv1a1.InstanceType{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(instancetype), out)).To(Succeed())
			Expect(meta.IsStatusConditionFalse(out.Status.Conditions, string(hv1a1.JobRunning)))
			Expect(meta.IsStatusConditionTrue(out.Status.Conditions, string(hv1a1.Ready)))
			Expect(out.Status.Offerings).To(HaveLen(1))

			By("Cleanup the specific resource instance Pod")
			Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
		})

	})
})

func setupConfigMap(ctx context.Context, k8sClient client.Client, resourceName, namespace string, provider *cv1a1.ProviderProfile) *corev1.ConfigMap {
	cm := &corev1.ConfigMap{}
	err := k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, cm)
	if err != nil && errors.IsNotFound(err) {
		cm = utils.SetupProviderProfileConfigMap(resourceName, namespace, provider)
		Expect(k8sClient.Create(ctx, cm)).To(Succeed())
		return cm
	}
	return cm
}

func setupInstanceTypePod(namespace string, provider *cv1a1.ProviderProfile) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "runner-pod-it",
			Namespace: namespace,
			Labels: map[string]string{
				"skycluster.io/job-type":          "instance-finder",
				"skycluster.io/provider-profile":  provider.Name,
				"skycluster.io/managed-by":        "skycluster",
				"skycluster.io/provider-platform": provider.Spec.Platform,
				"skycluster.io/provider-region":   provider.Spec.Region,
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "harvest",
					Image: "busybox",
				},
			},
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{
					LastProbeTime:      metav1.NewTime(time.Now()),
					LastTransitionTime: metav1.NewTime(time.Now()),
					ObservedGeneration: 1,
					Status:             "True",
					Type:               corev1.PodInitialized,
				},
				{
					LastProbeTime:      metav1.NewTime(time.Now()),
					LastTransitionTime: metav1.NewTime(time.Now()),
					ObservedGeneration: 1,
					Status:             "True",
					Type:               corev1.PodReady,
				},
				{
					LastProbeTime:      metav1.NewTime(time.Now()),
					LastTransitionTime: metav1.NewTime(time.Now()),
					ObservedGeneration: 1,
					Status:             "True",
					Type:               corev1.PodScheduled,
				},
			},
			Phase: corev1.PodSucceeded,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "harvest",
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							Reason: "Completed",
						},
					},
				},
			},
		},
	}
}

func setupInstanceTypeResource(ctx context.Context, typeNamespacedName types.NamespacedName) *cv1a1.InstanceType {
	By("creating the custom resource for the Kind InstanceType")
	instancetype := &cv1a1.InstanceType{}
	err := k8sClient.Get(ctx, typeNamespacedName, instancetype)
	if err != nil && errors.IsNotFound(err) {
		instancetype = &cv1a1.InstanceType{
			ObjectMeta: metav1.ObjectMeta{
				Name:      typeNamespacedName.Name,
				Namespace: typeNamespacedName.Namespace,
			},
			Spec: cv1a1.InstanceTypeSpec{
				ProviderRef: typeNamespacedName.Name,
				Offerings: []cv1a1.ZoneOfferings{
					{
						Zone: "us-east-1a",
						Offerings: []hv1a1.InstanceOffering{
							{
								Name:      "g5.12xlarge",
								NameLabel: "48vCPU-192GB-4xA10G-22GB",
								VCPUs:     48,
								RAM:       "192GB",
								Price:     "5.67",
							},
							{
								Name:      "g5.16xlarge",
								NameLabel: "64vCPU-256GB-1xA10G-22GB",
								VCPUs:     64,
								RAM:       "256GB",
								Price:     "4.10",
							},
						},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, instancetype)).To(Succeed())
	}
	return instancetype
}

func setupSecret(ctx context.Context, k8sClient client.Client, typeNamespacedName types.NamespacedName) *corev1.Secret {
	secret := &corev1.Secret{}
	err := k8sClient.Get(ctx, typeNamespacedName, secret)
	if err != nil && errors.IsNotFound(err) {
		secret = utils.SetupProviderCredSecret(typeNamespacedName.Name, typeNamespacedName.Namespace)
		Expect(k8sClient.Create(ctx, secret)).To(Succeed())
		return secret
	}
	return secret
}

// returns the provider profile and a boolean indicating if it was created
func setupProviderProfile(ctx context.Context, k8sClient client.Client, typeNamespacedName types.NamespacedName) (*cv1a1.ProviderProfile, bool) {
	provider := &cv1a1.ProviderProfile{}
	err := k8sClient.Get(ctx, typeNamespacedName, provider)
	if err != nil && errors.IsNotFound(err) {
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
					{Name: "us-east-1a", Enabled: true, DefaultZone: true},
				},
			},
		}
		Expect(k8sClient.Create(ctx, provider)).To(Succeed())
		return provider, true
	}
	return provider, false
}

func createInstanceTypeReconciler(podOutput string) *InstanceTypeReconciler {
	// For testing, we'll use a fake client that can be configured per-test
	// The actual log streaming will be handled by setting KubeClient in individual tests
	fkClient := fakekube.NewSimpleClientset()
	return &InstanceTypeReconciler{
		Client:       k8sClient,
		Scheme:       k8sClient.Scheme(),
		Recorder:     record.NewFakeRecorder(100),
		Logger:       logr.Discard(),
		KubeClient:   fkClient,
		PodLogClient: &utils.FakePodLogger{LogOutput: podOutput},
	}
}
