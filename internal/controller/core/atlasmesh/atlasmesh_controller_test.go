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
	"encoding/json"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	istioClient "istio.io/client-go/pkg/apis/networking/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	hv1a1 "github.com/skycluster-project/skycluster-operator/api/helper/v1alpha1"
	utils "github.com/skycluster-project/skycluster-operator/internal/controller/utils"
	pkglog "github.com/skycluster-project/skycluster-operator/pkg/v1alpha1/log"
)

var _ = Describe("AtlasMesh Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"
		const namespace = "my-app"
		const appId = "my-app"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: namespace,
		}
		var atlasmesh *cv1a1.AtlasMesh

		BeforeEach(func() {})

		AfterEach(func() {
			By("Cleanup the specific resource instance AtlasMesh")
			if err := k8sClient.Get(ctx, typeNamespacedName, atlasmesh); err == nil {
				Expect(k8sClient.Delete(ctx, atlasmesh)).To(Succeed())
			}

			By("Cleanup the deployments")
			depList := &appsv1.DeploymentList{}
			Expect(k8sClient.List(ctx, depList, client.InNamespace(namespace), client.MatchingLabels{
				"skycluster.io/app-id": appId,
			})).To(Succeed())
			for _, dep := range depList.Items {
				Expect(k8sClient.Delete(ctx, &dep)).To(Succeed())
			}

			By("Cleanup the services")
			svcList := &corev1.ServiceList{}
			Expect(k8sClient.List(ctx, svcList, client.InNamespace(namespace), client.MatchingLabels{
				"skycluster.io/app-id": appId,
			})).To(Succeed())
			for _, svc := range svcList.Items {
				Expect(k8sClient.Delete(ctx, &svc)).To(Succeed())
			}
		})

		It("should successfully fetch provider profiles and match them with XKube and XSetup objects and return the provider config name map", func() {
			atlasmesh = createSampleAtlasMeshResource(resourceName, namespace)
			Expect(k8sClient.Create(ctx, atlasmesh)).To(Succeed())

			By("Reconciling the created resource")
			atlasmesh = createSampleAtlasMeshResource(resourceName, namespace)
			reconciler := &AtlasMeshReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Logger: zap.New(pkglog.CustomLogger()).WithName("[AtlasMesh]"),
			}

			provCfgNameMap, err := reconciler.getProviderConfigNameMap()
			Expect(err).NotTo(HaveOccurred())
			Expect(lo.Keys(provCfgNameMap)).To(ConsistOf("local", "aws-us-east-1a", "gcp-us-east1-a"))
		})

		It("should successfully generate namespace manifests", func() {
			By("creating sample app manifests")
			createSampleAppManifest(appId, namespace)

			By("creating sample atlas mesh resource with deployment")
			atlasmesh = createSampleAtlasMeshResourceWithDeployment(resourceName, namespace)
			Expect(k8sClient.Create(ctx, atlasmesh)).To(Succeed())

			By("Reconciling the created resource")
			reconciler := &AtlasMeshReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Logger: zap.New(pkglog.CustomLogger()).WithName("[AtlasMesh]"),
			}

			provCfgNameMap, err := reconciler.getProviderConfigNameMap()
			Expect(err).NotTo(HaveOccurred())
			Expect(lo.Keys(provCfgNameMap)).To(ConsistOf("local", "aws-us-east-1a", "gcp-us-east1-a"))

			nsManifests, err := reconciler.generateNamespaceManifests(atlasmesh.Namespace, appId, atlasmesh.Spec.DeployMap.Component, provCfgNameMap)
			Expect(err).NotTo(HaveOccurred())
			Expect(nsManifests).NotTo(BeNil())
			Expect(len(nsManifests)).To(Equal(1))
			Expect(nsManifests[0].Manifest.Raw).NotTo(BeNil())

			generatedNsManifest := map[string]any{}
			err = json.Unmarshal(nsManifests[0].Manifest.Raw, &generatedNsManifest)
			Expect(err).NotTo(HaveOccurred())

			v, found, err := unstructured.NestedString(generatedNsManifest, "kind")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(v).To(Equal("Namespace"))

			v, found, err = unstructured.NestedString(generatedNsManifest, "metadata", "name")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(v).To(Equal(namespace))

			// cleanup
			cleanupSampleAppManifest(appId, namespace)
		})

		It("should successfully generate configmap manifests", func() {
			By("creating sample app manifests")
			createSampleAppManifest(appId, namespace)

			By("creating sample atlas mesh resource with deployment")
			atlasmesh = createSampleAtlasMeshResourceWithDeployment(resourceName, namespace)
			Expect(k8sClient.Create(ctx, atlasmesh)).To(Succeed())

			By("Reconciling the created resource")
			reconciler := &AtlasMeshReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Logger: zap.New(pkglog.CustomLogger()).WithName("[AtlasMesh]"),
			}

			provCfgNameMap, err := reconciler.getProviderConfigNameMap()
			Expect(err).NotTo(HaveOccurred())
			Expect(lo.Keys(provCfgNameMap)).To(ConsistOf("local", "aws-us-east-1a", "gcp-us-east1-a"))

			cdManifests, err := reconciler.generateConfigDataManifests(atlasmesh.Namespace, appId, atlasmesh.Spec.DeployMap.Component, provCfgNameMap)
			Expect(err).NotTo(HaveOccurred())
			Expect(cdManifests).NotTo(BeNil())
			Expect(len(cdManifests)).To(Equal(1))

			generatedCdManifest := map[string]any{}
			err = json.Unmarshal(cdManifests[0].Manifest.Raw, &generatedCdManifest)
			Expect(err).NotTo(HaveOccurred())

			v, found, err := unstructured.NestedString(generatedCdManifest, "kind")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(v).To(Equal("ConfigMap"))

			v1, found, err := unstructured.NestedMap(generatedCdManifest, "data")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(lo.Keys(v1)).To(ConsistOf("config"))

			// cleanup
			cleanupSampleAppManifest(appId, namespace)
		})

		It("should successfully generate deployment manifests", func() {
			By("creating sample app manifests")
			createSampleAppManifest(appId, namespace)

			By("creating sample atlas mesh resource with deployment")
			atlasmesh = createSampleAtlasMeshResourceWithDeployment(resourceName, namespace)
			Expect(k8sClient.Create(ctx, atlasmesh)).To(Succeed())

			By("Reconciling the created resource")
			reconciler := &AtlasMeshReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Logger: zap.New(pkglog.CustomLogger()).WithName("[AtlasMesh]"),
			}

			provCfgNameMap, err := reconciler.getProviderConfigNameMap()
			Expect(err).NotTo(HaveOccurred())
			Expect(lo.Keys(provCfgNameMap)).To(ConsistOf("local", "aws-us-east-1a", "gcp-us-east1-a"))

			depManifests, err := reconciler.generateDeployManifests(atlasmesh.Namespace, atlasmesh.Spec.DeployMap, provCfgNameMap)
			Expect(err).NotTo(HaveOccurred())
			Expect(depManifests).NotTo(BeNil())
			Expect(len(depManifests)).To(Equal(1))

			generatedCdManifest := map[string]any{}
			err = json.Unmarshal(depManifests[0].Manifest.Raw, &generatedCdManifest)
			Expect(err).NotTo(HaveOccurred())

			v, found, err := unstructured.NestedString(generatedCdManifest, "kind")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(v).To(Equal("Deployment"))

			v1, found, err := unstructured.NestedString(generatedCdManifest, "metadata", "name")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(v1).To(Equal("deployment1"))

			// cleanup
			cleanupSampleAppManifest(appId, namespace)
		})

		It("should successfully generate priority labels for multiple deployments", func() {
			By("creating sample app manifests")
			createSampleAppManifestMultipleDeployments(appId, namespace)

			// A ─┐
			//    ├─> X (primary)
			// B ─┘
			//    	└─> X (in GCP) (backup)

			By("creating sample atlas mesh resource with deployment")
			atlasmesh = createSampleAtlasMeshResourceWithMultipleDeployments(resourceName, namespace)
			Expect(k8sClient.Create(ctx, atlasmesh)).To(Succeed())

			By("Deriving priorities")
			prioLabels := derivePriorities(atlasmesh.Spec.DeployMap.Component, atlasmesh.Spec.DeployMap.Edges)
			Expect(prioLabels).NotTo(BeNil())

			depA := "dep-a-aws-us-east-1a"
			depB := "dep-b-aws-us-east-1a"
			depX := "dep-x-aws-us-east-1a"
			depXBackup := "dep-x-gcp-us-east1-a"

			By("must include keys for all sources and targets")
			Expect(lo.Keys(prioLabels)).To(ConsistOf([]string{depA, depB, depX, depXBackup}))

			By("primary target must contain labels for all sources and additional labels for backup targets")
			Expect(prioLabels[depX].allLabels).To(HaveKey("failover/" + depA + "-" + depX))
			Expect(prioLabels[depX].allLabels).To(HaveKey("failover/" + depB + "-" + depX))
			Expect(prioLabels[depX].allLabels).To(HaveKey("failover/" + depA + "-" + depXBackup))
			Expect(prioLabels[depX].allLabels).To(HaveKey("failover/" + depB + "-" + depXBackup))

			By("source labels must have same labels as their primary target as well as backup labels")
			Expect(prioLabels[depA].sourceLabels[depX]).To(ConsistOf(
				&orderedLabels{
					key:   "failover/" + depA + "-" + depX,
					value: utils.ShortenLabelKey(depA + "-" + depX),
				},
				&orderedLabels{
					key:   "failover/" + depA + "-" + depXBackup,
					value: utils.ShortenLabelKey(depA+"-"+depXBackup) + "-backup",
				},
			))

			Expect(prioLabels[depB].sourceLabels[depX]).To(ConsistOf(
				&orderedLabels{
					key:   "failover/" + depB + "-" + depX,
					value: utils.ShortenLabelKey(depB + "-" + depX),
				},
				&orderedLabels{
					key:   "failover/" + depB + "-" + depXBackup,
					value: utils.ShortenLabelKey(depB+"-"+depXBackup) + "-backup",
				},
			))

			By("backup target must contain labels indicating the backup targets for the sources")
			// This means it must not have backup labels for itself, instead
			// it must have labels for the primary source-target pair
			Expect(prioLabels[depXBackup].allLabels).To(HaveKey("failover/" + depA + "-" + depX))
			Expect(prioLabels[depXBackup].allLabels).To(HaveKey("failover/" + depB + "-" + depX))

			// cleanup
			cleanupSampleAppManifest(appId, namespace)
		})

		It("should successfully generate deployment manifests for multiple deployments", func() {
			By("creating sample app manifests")
			createSampleAppManifestMultipleDeployments(appId, namespace)

			// A ─┐
			//    ├─> X (primary)
			// B ─┘
			//    	└─> X (in GCP) (backup)

			By("creating sample atlas mesh resource with deployment")
			atlasmesh = createSampleAtlasMeshResourceWithMultipleDeployments(resourceName, namespace)
			Expect(k8sClient.Create(ctx, atlasmesh)).To(Succeed())

			reconciler := &AtlasMeshReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Logger: zap.New(pkglog.CustomLogger()).WithName("[AtlasMesh]"),
			}

			provCfgNameMap, err := reconciler.getProviderConfigNameMap()
			Expect(err).NotTo(HaveOccurred())
			Expect(lo.Keys(provCfgNameMap)).To(ConsistOf("local", "aws-us-east-1a", "gcp-us-east1-a"))

			depManifests, err := reconciler.generateDeployManifests(atlasmesh.Namespace, atlasmesh.Spec.DeployMap, provCfgNameMap)
			Expect(err).NotTo(HaveOccurred())
			Expect(depManifests).NotTo(BeNil())
			Expect(len(depManifests)).To(Equal(4))

			depA := "dep-a-aws-us-east-1a"
			depB := "dep-b-aws-us-east-1a"
			depX := "dep-x-aws-us-east-1a"
			depXBackup := "dep-x-gcp-us-east1-a"
			for _, skyDeployment := range depManifests {

				By("primary target must contain labels corresponding to the source-primary target pair as well as backup labels")
				if strings.Contains(skyDeployment.Name, "dep-x") && skyDeployment.ProviderRef.Name == "aws-us-east-1a" {
					dep := &appsv1.Deployment{}
					err = json.Unmarshal(skyDeployment.Manifest.Raw, dep)
					Expect(err).NotTo(HaveOccurred())

					Expect(dep.Spec.Template.ObjectMeta.Labels).To(HaveKeyWithValue(
						"failover/"+depA+"-"+depX,
						utils.ShortenLabelKey(depA+"-"+depX),
					))
					Expect(dep.Spec.Template.ObjectMeta.Labels).To(HaveKeyWithValue(
						"failover/"+depA+"-"+depXBackup,
						utils.ShortenLabelKey(depA+"-"+depXBackup)+"-backup",
					))
					Expect(dep.Spec.Template.ObjectMeta.Labels).To(HaveKeyWithValue(
						"failover/"+depB+"-"+depX,
						utils.ShortenLabelKey(depB+"-"+depX),
					))
					Expect(dep.Spec.Template.ObjectMeta.Labels).To(HaveKeyWithValue(
						"failover/"+depB+"-"+depXBackup,
						utils.ShortenLabelKey(depB+"-"+depXBackup)+"-backup",
					))
				}

				By("backup target must contain labels corresponding to the source-primary target pair, and not the backup target itself")
				if strings.Contains(skyDeployment.Name, "dep-x") && skyDeployment.ProviderRef.Name == "gcp-us-east1-a" {
					dep := &appsv1.Deployment{}
					err = json.Unmarshal(skyDeployment.Manifest.Raw, dep)
					Expect(err).NotTo(HaveOccurred())

					Expect(dep.Spec.Template.ObjectMeta.Labels).To(HaveKeyWithValue(
						"failover/"+depA+"-"+depX,
						utils.ShortenLabelKey(depA+"-"+depX),
					))
					Expect(dep.Spec.Template.ObjectMeta.Labels).To(HaveKeyWithValue(
						"failover/"+depB+"-"+depX,
						utils.ShortenLabelKey(depB+"-"+depX),
					))
				}
			}

			// cleanup
			cleanupSampleAppManifest(appId, namespace)
		})

		It("should successfully generate service manifests for multiple deployments", func() {
			By("creating sample app manifests")
			createSampleAppManifestMultipleDeployments(appId, namespace)

			// A ─┐
			//    ├─> X (primary)
			// B ─┘
			//    	└─> X (in GCP) (backup)

			By("creating sample atlas mesh resource with deployment")
			atlasmesh = createSampleAtlasMeshResourceWithMultipleDeployments(resourceName, namespace)
			Expect(k8sClient.Create(ctx, atlasmesh)).To(Succeed())

			reconciler := &AtlasMeshReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Logger: zap.New(pkglog.CustomLogger()).WithName("[AtlasMesh]"),
			}

			provCfgNameMap, err := reconciler.getProviderConfigNameMap()
			Expect(err).NotTo(HaveOccurred())
			Expect(lo.Keys(provCfgNameMap)).To(ConsistOf("local", "aws-us-east-1a", "gcp-us-east1-a"))

			svcManifests, err := reconciler.generateServiceManifests(atlasmesh.Namespace, appId, atlasmesh.Spec.DeployMap.Component, provCfgNameMap)
			Expect(err).NotTo(HaveOccurred())
			Expect(svcManifests).NotTo(BeNil())

			By("must generate 2 service manifests, dep-x are deployed in both AWS and GCP")
			Expect(len(svcManifests)).To(Equal(2))

			// cleanup
			cleanupSampleAppManifest(appId, namespace)
		})

		It("should successfully generate istio configuration manifests for multiple deployments", func() {
			By("creating sample app manifests")
			createSampleAppManifestMultipleDeployments(appId, namespace)

			// A ─┐
			//    ├─> X (primary)
			// B ─┘
			//    	└─> X (in GCP) (backup)

			By("creating sample atlas mesh resource with deployment")
			atlasmesh = createSampleAtlasMeshResourceWithMultipleDeployments(resourceName, namespace)
			Expect(k8sClient.Create(ctx, atlasmesh)).To(Succeed())

			reconciler := &AtlasMeshReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Logger: zap.New(pkglog.CustomLogger()).WithName("[AtlasMesh]"),
			}

			provCfgNameMap, err := reconciler.getProviderConfigNameMap()
			Expect(err).NotTo(HaveOccurred())
			Expect(lo.Keys(provCfgNameMap)).To(ConsistOf("local", "aws-us-east-1a", "gcp-us-east1-a"))

			istioManifests, err := reconciler.generateIstioConfig(atlasmesh.Namespace, appId, atlasmesh.Spec.DeployMap, provCfgNameMap)
			Expect(err).NotTo(HaveOccurred())
			Expect(istioManifests).NotTo(BeNil())

			istioVirtualServices := lo.Filter(istioManifests, func(skyService *hv1a1.SkyService, _ int) bool {
				var obj *istioClient.VirtualService
				if err = json.Unmarshal(skyService.Manifest.Raw, &obj); err != nil {
					return false
				}
				if obj.Kind != "VirtualService" {
					return false
				}
				return true
			})
			istioDstRules := lo.Filter(istioManifests, func(skyService *hv1a1.SkyService, _ int) bool {
				var obj *istioClient.DestinationRule
				if err = json.Unmarshal(skyService.Manifest.Raw, &obj); err != nil {
					return false
				}
				if obj.Kind != "DestinationRule" {
					return false
				}
				return true
			})
			// Istio's virtual services are created in each source provider for each (source-target) pair
			// we have dep-a -> dep-x and dep-b -> dep-x, both sources are deployed in AWS us-east-1a
			// so we expect 1 virtual services for AWS only
			Expect(len(istioVirtualServices)).To(Equal(1))
			var obj *istioClient.VirtualService
			Expect(json.Unmarshal(istioVirtualServices[0].Manifest.Raw, &obj)).NotTo(HaveOccurred())
			Expect(obj.Spec.Hosts).To(ConsistOf("dep-x"))
			Expect(obj.Spec.Http).To(HaveLen(2))             // 2 HTTPRoutes, one for each source deployment
			Expect(obj.Spec.Http[0].Name).To(Equal("dep-a")) // 1 match, for dep-a
			Expect(obj.Spec.Http[1].Name).To(Equal("dep-b")) // 1 match, for dep-a

			// Same for istio destination rules,
			Expect(len(istioDstRules)).To(Equal(1))
			var obj1 *istioClient.DestinationRule
			Expect(json.Unmarshal(istioDstRules[0].Manifest.Raw, &obj1)).NotTo(HaveOccurred())
			Expect(obj1.Spec.Subsets).To(HaveLen(2))             // 2 subsets, one for each source deployment
			Expect(obj1.Spec.Subsets[0].Name).To(Equal("dep-a")) // 1 match, for dep-a
			Expect(obj1.Spec.Subsets[1].Name).To(Equal("dep-b")) // 1 match, for dep-a

			depA := "dep-a-aws-us-east-1a"
			depX := "dep-x-aws-us-east-1a"
			depXBackup := "dep-x-gcp-us-east1-a"
			failoverLabels := obj1.Spec.Subsets[0].TrafficPolicy.LoadBalancer.LocalityLbSetting.FailoverPriority
			Expect(failoverLabels).NotTo(BeNil())
			Expect(failoverLabels).To(ConsistOf(
				"failover/"+depA+"-"+depX+"="+utils.ShortenLabelKey(depA+"-"+depX),
				"failover/"+depA+"-"+depXBackup+"="+utils.ShortenLabelKey(depA+"-"+depXBackup)+"-backup",
			))

			// cleanup
			cleanupSampleAppManifest(appId, namespace)
		})

	})
})

func createSampleAppManifestMultipleDeployments(appId, namespace string) {
	cm := createSampleConfigMap(appId, namespace)
	Expect(k8sClient.Create(ctx, cm)).To(Succeed())

	// Deployment A
	deploymentA := createSampleDeployment(appId, "dep-a", namespace)
	Expect(k8sClient.Create(ctx, deploymentA)).To(Succeed())

	// Deployment B
	deploymentB := createSampleDeployment(appId, "dep-b", namespace)
	Expect(k8sClient.Create(ctx, deploymentB)).To(Succeed())

	// Deployment X (primary target)
	deploymentX := createSampleDeployment(appId, "dep-x", namespace)
	Expect(k8sClient.Create(ctx, deploymentX)).To(Succeed())

	svcX := createSampleService(appId, "dep-x", namespace)
	Expect(k8sClient.Create(ctx, svcX)).To(Succeed())
}

func createSampleService(appId, serviceName, namespace string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: namespace,
			Labels: map[string]string{
				"skycluster.io/app-id":    appId,
				"skycluster.io/app-scope": "distributed",
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": serviceName,
			},
			Ports: []corev1.ServicePort{
				{
					Port: 80,
				},
			},
		},
	}
}

func createSampleAppManifest(appId, namespace string) {
	cm := createSampleConfigMap(appId, namespace)
	Expect(k8sClient.Create(ctx, cm)).To(Succeed())

	deployment := createSampleDeployment(appId, "deployment1", namespace)
	Expect(k8sClient.Create(ctx, deployment)).To(Succeed())
}

func cleanupSampleAppManifest(appId, namespace string) {
	cmList := &corev1.ConfigMapList{}
	Expect(k8sClient.List(ctx, cmList, client.InNamespace(namespace), client.MatchingLabels{
		"skycluster.io/app-id": appId,
	})).To(Succeed())
	for _, cm := range cmList.Items {
		Expect(k8sClient.Delete(ctx, &cm)).To(Succeed())
	}

	deploymentList := &appsv1.DeploymentList{}
	Expect(k8sClient.List(ctx, deploymentList, client.InNamespace(namespace), client.MatchingLabels{
		"skycluster.io/app-id": appId,
	})).To(Succeed())
	for _, deployment := range deploymentList.Items {
		Expect(k8sClient.Delete(ctx, &deployment)).To(Succeed())
	}
}

func createSampleConfigMap(appId, namespace string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "configmap1",
			Namespace: namespace,
			Labels: map[string]string{
				"skycluster.io/app-id":    appId,
				"skycluster.io/app-scope": "distributed",
			},
		},
		Data: map[string]string{
			"config": "some-config",
		},
	}
}

func createSampleNamespace(appId string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: appId,
			Labels: map[string]string{
				"skycluster.io/app-id":    appId,
				"skycluster.io/app-scope": "distributed",
			},
		},
	}
}

func createSampleDeployment(appId, deployName, namespace string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deployName,
			Namespace: namespace,
			Labels: map[string]string{
				"skycluster.io/app-id":    appId,
				"skycluster.io/app-scope": "distributed",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": deployName,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": deployName,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "sample-container",
							Image: "sample-image",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("500m"),
									corev1.ResourceMemory: resource.MustParse("256Mi"),
								},
							},
						},
					},
				},
			},
		},
	}
}

func createSampleAtlasMeshResource(resourceName, namespace string) *cv1a1.AtlasMesh {
	return &cv1a1.AtlasMesh{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: namespace,
			Labels: map[string]string{
				"skycluster.io/app-id": resourceName,
			},
		},
		Spec: cv1a1.AtlasMeshSpec{
			Approve: false,
			DataflowPolicyRef: cv1a1.DataflowPolicyRef{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: resourceName,
				},
				DataflowResourceVersion: "1.0.0",
			},
			DeploymentPolicyRef: cv1a1.DeploymentPolicyRef{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: resourceName,
				},
				DeploymentPlanResourceVersion: "1.0.0",
			},
			DeployMap: createXInstanceDeployMap(resourceName),
		},
	}
}

func createSampleAtlasMeshResourceWithDeployment(resourceName, namespace string) *cv1a1.AtlasMesh {
	return &cv1a1.AtlasMesh{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: namespace,
			Labels: map[string]string{
				"skycluster.io/app-id": resourceName,
			},
		},
		Spec: cv1a1.AtlasMeshSpec{
			Approve: false,
			DataflowPolicyRef: cv1a1.DataflowPolicyRef{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: resourceName,
				},
				DataflowResourceVersion: "1.0.0",
			},
			DeploymentPolicyRef: cv1a1.DeploymentPolicyRef{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: resourceName,
				},
				DeploymentPlanResourceVersion: "1.0.0",
			},
			DeployMap: createDeploymentDeployMap(namespace),
		},
	}
}

func createSampleAtlasMeshResourceWithMultipleDeployments(resourceName, namespace string) *cv1a1.AtlasMesh {
	return &cv1a1.AtlasMesh{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: namespace,
			Labels: map[string]string{
				"skycluster.io/app-id": resourceName,
			},
		},
		Spec: cv1a1.AtlasMeshSpec{
			Approve: false,
			DataflowPolicyRef: cv1a1.DataflowPolicyRef{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: resourceName,
				},
				DataflowResourceVersion: "1.0.0",
			},
			DeploymentPolicyRef: cv1a1.DeploymentPolicyRef{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: resourceName,
				},
				DeploymentPlanResourceVersion: "1.0.0",
			},
			DeployMap: createMultipleDeploymentDeployMap(namespace),
		},
	}
}

func createSampleAtlasMeshResourceWithXNodeGroup(resourceName, namespace string) *cv1a1.AtlasMesh {
	return &cv1a1.AtlasMesh{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: namespace,
			Labels: map[string]string{
				"skycluster.io/app-id": resourceName,
			},
		},
		Spec: cv1a1.AtlasMeshSpec{
			Approve: false,
			DataflowPolicyRef: cv1a1.DataflowPolicyRef{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: resourceName,
				},
				DataflowResourceVersion: "1.0.0",
			},
			DeploymentPolicyRef: cv1a1.DeploymentPolicyRef{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: resourceName,
				},
				DeploymentPlanResourceVersion: "1.0.0",
			},
			DeployMap: createXKubeDeployMap(resourceName),
		},
	}
}

func createXInstanceDeployMap(resourceName string) cv1a1.DeployMap {
	return cv1a1.DeployMap{
		Component: []hv1a1.SkyService{
			{
				ComponentRef: hv1a1.ComponentRef{
					APIVersion: "skycluster.io/v1alpha1",
					Kind:       "XInstance",
					Name:       resourceName,
				},
				Manifest: &runtime.RawExtension{Raw: []byte(`
					{
						"services": [
							[
								{
									"apiVersion": "skycluster.io/v1alpha1",
									"count": "1",
									"kind": "ComputeProfile",
									"name": "12vCPU-49GB-1xL4-24GB",
									"price": 0.4893
								}
							]
						]
					}
				`)},
				ProviderRef: hv1a1.ProviderRefSpec{
					Name:        "aws-us-east-1a",
					Platform:    "aws",
					Region:      "us-east-1",
					RegionAlias: "us-east",
					Type:        "cloud",
					Zone:        "us-east-1a",
				},
			},
		},
	}
}

func createXKubeDeployMap(resourceName string) cv1a1.DeployMap {
	return cv1a1.DeployMap{
		Component: []hv1a1.SkyService{
			{
				ComponentRef: hv1a1.ComponentRef{
					APIVersion: "skycluster.io/v1alpha1",
					Kind:       "XNodeGroup",
					Name:       resourceName,
				},
				Manifest: &runtime.RawExtension{Raw: []byte(`
					{
						"services": [
							[
								{
									"apiVersion": "skycluster.io/v1alpha1",
									"count": "1",
									"kind": "ComputeProfile",
									"name": "12vCPU-49GB-1xL4-24GB",
									"price": 0.4893
								}
							]
						]
					}
				`)},
				ProviderRef: hv1a1.ProviderRefSpec{
					Name:        "aws-us-east-1a",
					Platform:    "aws",
					Region:      "us-east-1",
					RegionAlias: "us-east",
					Type:        "cloud",
					Zone:        "us-east-1a",
				},
			},
		},
	}
}

func createDeploymentDeployMap(ns string) cv1a1.DeployMap {
	return cv1a1.DeployMap{
		Component: []hv1a1.SkyService{
			{
				ComponentRef: hv1a1.ComponentRef{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "deployment1",
					Namespace:  ns,
				},
				Manifest: &runtime.RawExtension{Raw: []byte(`
					{
						"services": [
							[
								{
									"apiVersion": "skycluster.io/v1alpha1",
									"count": "1",
									"kind": "ComputeProfile",
									"name": "12vCPU-49GB-1xL4-24GB",
									"price": 0.4893
								}
							]
						]
					}
				`)},
				ProviderRef: hv1a1.ProviderRefSpec{
					Name:        "aws-us-east-1a",
					Platform:    "aws",
					Region:      "us-east-1",
					RegionAlias: "us-east",
					Type:        "cloud",
					Zone:        "us-east-1a",
				},
			},
		},
		Edges: []cv1a1.DeployMapEdge{},
	}
}

func createMultipleDeploymentDeployMap(ns string) cv1a1.DeployMap {
	// A ─┐
	//    ├─> X (in AWS) (primary)
	// B ─┘
	//    	└─> X (in GCP) (backup)
	return cv1a1.DeployMap{
		Component: []hv1a1.SkyService{
			{
				ComponentRef: hv1a1.ComponentRef{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "dep-a",
					Namespace:  ns,
				},
				Manifest: &runtime.RawExtension{Raw: []byte(`
					{
						"services": [
							[
								{
									"apiVersion": "skycluster.io/v1alpha1",
									"count": "1",
									"kind": "ComputeProfile",
									"name": "12vCPU-49GB-1xL4-24GB",
									"price": 0.4893
								}
							]
						]
					}
				`)},
				ProviderRef: hv1a1.ProviderRefSpec{
					Name:        "aws-us-east-1a",
					Platform:    "aws",
					Region:      "us-east-1",
					RegionAlias: "us-east",
					Type:        "cloud",
					Zone:        "us-east-1a",
				},
			},
			{
				ComponentRef: hv1a1.ComponentRef{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "dep-b",
					Namespace:  ns,
				},
				Manifest: &runtime.RawExtension{Raw: []byte(`
					{
						"services": [
							[
								{
									"apiVersion": "skycluster.io/v1alpha1",
									"count": "1",
									"kind": "ComputeProfile",
									"name": "12vCPU-49GB-1xL4-24GB",
									"price": 0.4893
								}
							]
						]
					}
				`)},
				ProviderRef: hv1a1.ProviderRefSpec{
					Name:        "aws-us-east-1a",
					Platform:    "aws",
					Region:      "us-east-1",
					RegionAlias: "us-east",
					Type:        "cloud",
					Zone:        "us-east-1a",
				},
			},
			{
				ComponentRef: hv1a1.ComponentRef{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "dep-x",
					Namespace:  ns,
				},
				Manifest: &runtime.RawExtension{Raw: []byte(`
					{
						"services": [
							[
								{
									"apiVersion": "skycluster.io/v1alpha1",
									"count": "1",
									"kind": "ComputeProfile",
									"name": "12vCPU-49GB-1xL4-24GB",
									"price": 0.4893
								}
							]
						]
					}
				`)},
				ProviderRef: hv1a1.ProviderRefSpec{
					Name:        "aws-us-east-1a",
					Platform:    "aws",
					Region:      "us-east-1",
					RegionAlias: "us-east",
					Type:        "cloud",
					Zone:        "us-east-1a",
				},
			},
			{
				ComponentRef: hv1a1.ComponentRef{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "dep-x",
					Namespace:  ns,
				},
				Manifest: &runtime.RawExtension{Raw: []byte(`
					{
						"services": [
							[
								{
									"apiVersion": "skycluster.io/v1alpha1",
									"count": "1",
									"kind": "ComputeProfile",
									"name": "12vCPU-49GB-1xL4-24GB",
									"price": 0.4893
								}
							]
						]
					}
				`)},
				ProviderRef: hv1a1.ProviderRefSpec{
					Name:        "gcp-us-east1-a",
					Platform:    "gcp",
					Region:      "us-east1",
					RegionAlias: "us-east",
					Type:        "cloud",
					Zone:        "us-east1-a",
				},
			},
		},
		Edges: []cv1a1.DeployMapEdge{
			{
				From: hv1a1.SkyService{
					ComponentRef: hv1a1.ComponentRef{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       "dep-a",
						Namespace:  ns,
					},
					ProviderRef: hv1a1.ProviderRefSpec{
						Name:        "aws-us-east-1a",
						Platform:    "aws",
						Region:      "us-east-1",
						RegionAlias: "us-east",
						Type:        "cloud",
						Zone:        "us-east-1a",
					},
				},
				To: hv1a1.SkyService{
					ComponentRef: hv1a1.ComponentRef{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       "dep-x",
						Namespace:  ns,
					},
					ProviderRef: hv1a1.ProviderRefSpec{
						Name:        "aws-us-east-1a",
						Platform:    "aws",
						Region:      "us-east-1",
						RegionAlias: "us-east",
						Type:        "cloud",
						Zone:        "us-east-1a",
					},
				},
				Latency: "100ms",
			},
			{
				From: hv1a1.SkyService{
					ComponentRef: hv1a1.ComponentRef{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       "dep-b",
						Namespace:  ns,
					},
					ProviderRef: hv1a1.ProviderRefSpec{
						Name:        "aws-us-east-1a",
						Platform:    "aws",
						Region:      "us-east-1",
						RegionAlias: "us-east",
						Type:        "cloud",
						Zone:        "us-east-1a",
					},
				},
				To: hv1a1.SkyService{
					ComponentRef: hv1a1.ComponentRef{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       "dep-x",
						Namespace:  ns,
					},
					ProviderRef: hv1a1.ProviderRefSpec{
						Name:        "aws-us-east-1a",
						Platform:    "aws",
						Region:      "us-east-1",
						RegionAlias: "us-east",
						Type:        "cloud",
						Zone:        "us-east-1a",
					},
				},
				Latency: "100ms",
			},
		},
	}
}
