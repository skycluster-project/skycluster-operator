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
	utils "github.com/skycluster-project/skycluster-operator/internal/controller/core/utils"
	hint "github.com/skycluster-project/skycluster-operator/internal/helper"
)

func setupImageResource(ctx context.Context, k8sClient client.Client, resourceName, namespace string) *cv1a1.Image {
	image := &cv1a1.Image{}
	err := k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, image)
	if err != nil && errors.IsNotFound(err) {
		image = utils.SetupImageResource(ctx, resourceName, namespace)
		Expect(k8sClient.Create(ctx, image)).To(Succeed())
		return image
	}
	return image
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
			image = setupImageResource(ctx, k8sClient, resourceName, namespace)
			secret = setupSecret(ctx, k8sClient, typeNamespacedName)
			p, wasCreated := setupProviderProfile(ctx, k8sClient, typeNamespacedName)
			if wasCreated {
				utils.SetProviderProfileStatus(p, resourceName, namespace)
				Expect(k8sClient.Status().Update(ctx, p)).To(Succeed())
				provider = p
			}
			cm = setupConfigMap(ctx, k8sClient, resourceName, namespace, provider)
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
			pod := utils.SetupImagePod("runner-pod", "image-finder", namespace, provider)
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
			pod := utils.SetupImagePod("runner-pod", "image-finder", namespace, provider)
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())

			By("checking the decodeJSONToImageOffering function")
			zones, err := reconciler.decodeJSONToImageOffering(podOutput)
			Expect(err).NotTo(HaveOccurred())
			Expect(zones).To(HaveLen(3))

			By("Cleanup the specific resource instance Pod")
			Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
		})

		It("handles fetching pod logs", func() {
			pod := utils.SetupImagePod("runner-pod", "image-finder", namespace, provider)
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
		// 	pod := SetupImagePod(namespace, provider)
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

func createImageReconciler(podOutput string) *ImageReconciler {
	return &ImageReconciler{
		Client:       k8sClient,
		Scheme:       k8sClient.Scheme(),
		Recorder:     record.NewFakeRecorder(100),
		Logger:       logr.Discard(),
		KubeClient:   fakekube.NewSimpleClientset(),
		PodLogClient: &utils.FakePodLogger{LogOutput: podOutput},
	}
}
