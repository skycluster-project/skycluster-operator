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
	"k8s.io/apimachinery/pkg/types"
	fakekube "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	hv1a1 "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"
	hint "github.com/skycluster-project/skycluster-operator/internal/helper"
	depv1a1 "github.com/skycluster-project/skycluster-operator/pkg/v1alpha1/dep"
)

var _ = Describe("Image Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"
		const namespace = "skycluster-system"
		const podOutput = `{
  "images": [
    {
      "nameLabel": "ubuntu-20.04",
      "name": "ami-0fb0b230890ccd1e6",
      "zone": "us-east-1a"
    },
    {
      "nameLabel": "ubuntu-22.04",
      "name": "ami-0e70225fadb23da91",
      "zone": "us-east-1a"
    },
    {
      "nameLabel": "ubuntu-24.04",
      "name": "ami-07033cb190109bd1d",
      "zone": "us-east-1a"
    }
  ]
}
`

		var (
			reconciler *ImageReconciler
			provider   *cv1a1.ProviderProfile
			cm         *corev1.ConfigMap
			image      *cv1a1.Image
			secret     *corev1.Secret
		)

		ctx := context.Background()
		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: namespace,
		}

		BeforeEach(func() {
			image = setupImageResource(ctx, resourceName, namespace, typeNamespacedName)
			secret = setupSecret(ctx, namespace, typeNamespacedName)
			p, wasCreated := setupProviderProfile(ctx, resourceName, namespace, typeNamespacedName)
			if wasCreated {
				updateProviderProfileStatus(ctx, p, resourceName, namespace)
				provider = p
			}
			cm = setupConfigMap(ctx, resourceName, namespace, provider)
			reconciler = createImageReconciler(podOutput)
		})

		AfterEach(func() {
			err := k8sClient.Get(ctx, typeNamespacedName, image)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance ConfigMap")
			Expect(k8sClient.Delete(ctx, cm)).To(Succeed())

			By("Cleanup the specific resource instance Image")
			Expect(k8sClient.Delete(ctx, image)).To(Succeed())

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
					Name:      image.Name,
					Namespace: image.Namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			out := &cv1a1.Image{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(image), out)).To(Succeed())

			Expect(meta.IsStatusConditionTrue(out.Status.Conditions, string(hv1a1.Ready))).To(BeTrue())
			Expect(out.Status.Images).To(Equal(image.Spec.Images))
			Expect(out.Status.Region).To(Equal("scinet"))
		})

		It("tests build runner", func() {
			jsonData, err := reconciler.generateJSON(provider.Spec.Zones, image.Spec.Images)
			Expect(err).NotTo(HaveOccurred())
			job, err := reconciler.buildRunner(image, provider, jsonData)
			Expect(err).NotTo(HaveOccurred())
			Expect(job).NotTo(BeNil())
		})

		It("creates a runner Job and sets JobRunning when ResyncRequired is true", func() {
			image.Status.SetCondition(
				hv1a1.ResyncRequired,
				metav1.ConditionTrue,
				"Test",
				"forcing resync",
			)
			Expect(k8sClient.Status().Update(ctx, image)).To(Succeed())

			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			out := &cv1a1.Image{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, out)).To(Succeed())
			Expect(meta.IsStatusConditionTrue(out.Status.Conditions, string(hv1a1.JobRunning))).To(BeTrue())
			Expect(meta.IsStatusConditionTrue(out.Status.Conditions, string(hv1a1.ResyncRequired))).To(BeFalse())
		})

		It("marks ResyncRequired when spec changes while a runner Job is already running", func() {
			image.Status.SetCondition(
				hv1a1.JobRunning,
				metav1.ConditionTrue,
				"Running",
				"job running",
			)
			image.Status.ObservedGeneration = image.Generation
			Expect(k8sClient.Status().Update(ctx, image)).To(Succeed())

			image.Spec.Images = append(image.Spec.Images, cv1a1.ImageOffering{
				NameLabel: "ubuntu-24.04",
				Pattern:   "*hvm-ssd*/ubuntu-noble-24.04-amd64-server*",
			})
			Expect(k8sClient.Update(ctx, image)).To(Succeed())

			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKeyFromObject(image),
			})
			Expect(err).NotTo(HaveOccurred())

			out := &cv1a1.Image{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(image), out)).To(Succeed())

			Expect(meta.IsStatusConditionTrue(out.Status.Conditions, string(hv1a1.ResyncRequired))).To(BeTrue())
			Expect(meta.IsStatusConditionFalse(out.Status.Conditions, string(hv1a1.Ready))).To(BeTrue())
		})

		It("handles runner Pod running by requeueing", func() {
			image.Status.SetCondition(hv1a1.JobRunning, metav1.ConditionTrue, "Running", "")
			Expect(k8sClient.Status().Update(ctx, image)).To(Succeed())

			By("creating the pod")
			pod := setupPod(namespace, provider)
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())

			By("updating the pod status")
			pod.Status.Phase = corev1.PodRunning
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			res, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKeyFromObject(image),
			})
			Expect(err).To(Succeed())
			Expect(res.RequeueAfter).To(Equal(hint.RequeuePollThreshold))

			By("Cleanup the specific resource instance Pod")
			Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
		})

		It("check the decodeJSONToImageOffering function", func() {
			By("creating the pod")
			pod := setupPod(namespace, provider)
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())

			By("checking the decodeJSONToImageOffering function")
			zones, err := reconciler.decodeJSONToImageOffering(podOutput)
			Expect(err).NotTo(HaveOccurred())
			Expect(zones).To(HaveLen(3))

			By("Cleanup the specific resource instance Pod")
			Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
		})

		It("handles fetching pod logs", func() {
			pod := setupPod(namespace, provider)
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())

			podOutput, err := reconciler.getPodStdOut(ctx, pod.Name, pod.Namespace, "harvest")
			Expect(err).NotTo(HaveOccurred())
			Expect(podOutput).To(Equal(podOutput))

			By("Cleanup the specific resource instance Pod")
			Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
		})

		// It("handles runner Pod success by decoding logs, updating status images, and marking Ready", func() {
		// 	image.Status.SetCondition(hv1a1.JobRunning, metav1.ConditionTrue, "Running", "")
		// 	image.Status.ObservedGeneration = image.Generation
		// 	Expect(k8sClient.Status().Update(ctx, image)).To(Succeed())

		// 	By("creating the pod")
		// 	pod := setupPod(namespace, provider)
		// 	Expect(k8sClient.Create(ctx, pod)).To(Succeed())

		// 	By("updating the pod status")
		// 	pod.Status.Phase = corev1.PodSucceeded
		// 	pod.Status.ContainerStatuses = []corev1.ContainerStatus{
		// 		{
		// 			Name: "harvest",
		// 			State: corev1.ContainerState{
		// 				Terminated: &corev1.ContainerStateTerminated{
		// 					Reason: "Completed",
		// 				},
		// 			},
		// 		},
		// 	}
		// 	Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

		// 	// mock the pod log client
		// 	fkClient := fakekube.NewSimpleClientset()
		// 	fkClient.PrependReactor("get", "pods", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		// 		return true, pod, nil
		// 	})
		// 	fkClient.Fake.PrependReactor("get", "pods/log", func(_ k8stesting.Action) (handled bool, ret runtime.Object, err error) {
		// 		return true, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "runner-pod", Namespace: namespace}}, nil
		// 	})
		// 	reconciler.KubeClient = fkClient

		// 	res, err := reconciler.Reconcile(ctx, ctrl.Request{
		// 		NamespacedName: client.ObjectKeyFromObject(image),
		// 	})
		// 	Expect(err).NotTo(HaveOccurred())
		// 	Expect(meta.IsStatusConditionFalse(image.Status.Conditions, string(hv1a1.ResyncRequired)))
		// 	Expect(meta.IsStatusConditionFalse(image.Status.Conditions, string(hv1a1.JobRunning)))
		// 	Expect(meta.IsStatusConditionTrue(image.Status.Conditions, string(hv1a1.Ready)))
		// 	Expect(res).To(Equal(ctrl.Result{}))

		// 	cmList := &corev1.ConfigMapList{}
		// 	Expect(k8sClient.List(ctx, cmList, client.MatchingLabels(map[string]string{
		// 		"skycluster.io/provider-profile": provider.Name,
		// 	}))).To(Succeed())
		// 	Expect(cmList.Items).To(HaveLen(1))
		// 	Expect(cmList.Items[0].Data).To(HaveKey("images.yaml"))

		// 	podOutput, err := reconciler.getPodStdOut(ctx, pod.Name, pod.Namespace, "harvest")
		// 	Expect(err).NotTo(HaveOccurred())
		// 	Expect(podOutput).To(Equal(podOutput))

		// 	out := &cv1a1.Image{}
		// 	Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(image), out)).To(Succeed())
		// 	Expect(meta.IsStatusConditionFalse(out.Status.Conditions, string(hv1a1.JobRunning)))
		// 	Expect(meta.IsStatusConditionTrue(out.Status.Conditions, string(hv1a1.Ready)))
		// 	Expect(out.Status.Images).To(HaveLen(3))

		// 	By("Cleanup the specific resource instance Pod")
		// 	Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
		// })

	})
})

func setupPod(namespace string, provider *cv1a1.ProviderProfile) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "runner-pod",
			Namespace: namespace,
			Labels: map[string]string{
				"skycluster.io/job-type":          "image-finder",
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
				{
					Name:  "runner",
					Image: "etesami/image-finder:latest",
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

func setupImageResource(ctx context.Context, resourceName, namespace string, typeNamespacedName types.NamespacedName) *cv1a1.Image {
	By("creating the custom resource for the Kind Image")
	image := &cv1a1.Image{}
	err := k8sClient.Get(ctx, typeNamespacedName, image)
	if err != nil && errors.IsNotFound(err) {
		image = &cv1a1.Image{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: namespace,
			},
			Spec: cv1a1.ImageSpec{
				ProviderRef: resourceName,
				Images: []cv1a1.ImageOffering{
					{
						NameLabel: "ubuntu-20.04",
						Pattern:   "*hvm-ssd*/ubuntu-focal-20.04-amd64-server*",
					},
					{
						NameLabel: "ubuntu-22.04",
						Pattern:   "*hvm-ssd*/ubuntu-jammy-22.04-amd64-server*",
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, image)).To(Succeed())
	}
	return image
}

func setupConfigMap(ctx context.Context, resourceName, namespace string, provider *cv1a1.ProviderProfile) *corev1.ConfigMap {
	cm := &corev1.ConfigMap{}
	err := k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, cm)
	if err != nil && errors.IsNotFound(err) {
		cm = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: namespace,
				Labels: map[string]string{
					"skycluster.io/managed-by":        "skycluster",
					"skycluster.io/provider-profile":  provider.Name,
					"skycluster.io/provider-platform": provider.Spec.Platform,
					"skycluster.io/provider-region":   provider.Spec.Region,
				},
			},
			Data: configMapData(),
		}
		Expect(k8sClient.Create(ctx, cm)).To(Succeed())
	}
	return cm
}

func setupSecret(ctx context.Context, namespace string, typeNamespacedName types.NamespacedName) *corev1.Secret {
	By("Creating credential secret")
	secret := &corev1.Secret{}
	err := k8sClient.Get(ctx, typeNamespacedName, secret)
	if err != nil && errors.IsNotFound(err) {
		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "secret-aws",
				Namespace: namespace,
				Labels: map[string]string{
					"skycluster.io/managed-by":        "skycluster",
					"skycluster.io/provider-platform": "aws",
					"skycluster.io/secret-role":       "credentials",
				},
			},
			Data: map[string][]byte{
				"aws_access_key_id":     []byte("xyzswerd"),
				"aws_secret_access_key": []byte("shdrugydh"),
			},
			Type: "Opaque",
		}
		Expect(k8sClient.Create(ctx, secret)).To(Succeed())
	}
	return secret
}

func setupProviderProfile(ctx context.Context, resourceName, namespace string, typeNamespacedName types.NamespacedName) (*cv1a1.ProviderProfile, bool) {
	provider := &cv1a1.ProviderProfile{}
	err := k8sClient.Get(ctx, typeNamespacedName, provider)
	if err != nil && errors.IsNotFound(err) {
		provider = &cv1a1.ProviderProfile{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: namespace,
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
	return nil, false
}

func updateProviderProfileStatus(ctx context.Context, provider *cv1a1.ProviderProfile, resourceName, namespace string) {
	By("Updating provider profile status field")
	provider.Status = cv1a1.ProviderProfileStatus{
		Platform: "aws",
		Region:   "us-east-1",
		Zones: []cv1a1.ZoneSpec{
			{Name: "us-east-1a", Enabled: true, DefaultZone: true, Type: "cloud"},
		},
		DependencyManager: depv1a1.DependencyManager{
			Dependencies: []depv1a1.Dependency{
				{
					NameRef:   "aws-us-east-1",
					Kind:      "ConfigMap",
					Namespace: namespace,
				},
				{
					NameRef:   resourceName,
					Kind:      "Image",
					Namespace: namespace,
				},
			},
		},
		EgressCostSpecs: []cv1a1.EgressCostSpec{
			{
				Type: "internet",
				Unit: "GB",
				Tiers: []cv1a1.EgressTier{
					{FromGB: 0, ToGB: 10000, PricePerGB: "0.09"},
				},
			},
			{
				Type: "inter-zone",
				Unit: "GB",
				Tiers: []cv1a1.EgressTier{
					{FromGB: 0, ToGB: 10000, PricePerGB: "0.01"},
				},
			},
			{
				Type: "inter-region",
				Unit: "GB",
				Tiers: []cv1a1.EgressTier{
					{FromGB: 0, ToGB: 10000, PricePerGB: "0.02"},
				},
			},
		},
		ObservedGeneration: 1,
	}
	Expect(k8sClient.Status().Update(ctx, provider)).To(Succeed())
}

func createImageReconciler(podOutput string) *ImageReconciler {
	return &ImageReconciler{
		Client:       k8sClient,
		Scheme:       k8sClient.Scheme(),
		Recorder:     record.NewFakeRecorder(100),
		Logger:       logr.Discard(),
		KubeClient:   fakekube.NewSimpleClientset(),
		PodLogClient: &FakePodLogger{LogOutput: podOutput},
	}
}

func configMapData() map[string]string {
	return map[string]string{
		"flavors.yaml": `- zone: us-east-1a
zoneOfferings:
- name: g5.12xlarge
nameLabel: 48vCPU-192GB-4xA10G-22GB
vcpus: 48
ram: 192GB
price: "5.67"
gpu:
enabled: true
manufacturer: NVIDIA
count: 4
model: A10G
memory: 22GB
spot:
price: "2.12"
enabled: true
- name: g5.16xlarge
nameLabel: 64vCPU-256GB-1xA10G-22GB
vcpus: 64
ram: 256GB
price: "4.10"
gpu:
enabled: true
manufacturer: NVIDIA
count: 1
model: A10G
memory: 22GB
spot:
price: "1.07"
enabled: true`,
		"images.yaml": `- nameLabel: ubuntu-20.04
name: ami-0fb0b230890ccd1e6
zone: us-east-1a
- nameLabel: ubuntu-22.04
name: ami-0e70225fadb23da91
zone: us-east-1a
- nameLabel: ubuntu-24.04
name: ami-07033cb190109bd1d
zone: us-east-1a
- nameLabel: ubuntu-20.04
name: ami-0fb0b230890ccd1e6
zone: us-east-1b
- nameLabel: ubuntu-22.04
name: ami-0e70225fadb23da91
zone: us-east-1b
- nameLabel: ubuntu-24.04
name: ami-07033cb190109bd1d
zone: us-east-1b`,
		"managed-k8s.yaml": `- name: EKS
nameLabel: ManagedKubernetes
overhead:
	cost: "0.096"
	count: 1
	instanceType: m5.xlarge
price: "0.10"`,
	}
}
