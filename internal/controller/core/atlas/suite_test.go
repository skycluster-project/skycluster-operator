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
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	cv1a1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
	cv1a1ctrl "github.com/skycluster-project/skycluster-operator/internal/controller/core"
	cv1a1ppctrl "github.com/skycluster-project/skycluster-operator/internal/controller/core/providerprofile"
	pkglog "github.com/skycluster-project/skycluster-operator/pkg/v1alpha1/log"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	ctx       context.Context
	cancel    context.CancelFunc
	testEnv   *envtest.Environment
	cfg       *rest.Config
	k8sClient client.Client

	providerprofileAWS *cv1a1.ProviderProfile
	providerprofileGCP *cv1a1.ProviderProfile
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	var err error
	err = cv1a1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// +kubebuilder:scaffold:scheme

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	// Retrieve the first found binary directory to allow running tests from IDEs
	if getFirstFoundEnvTestBinaryDir() != "" {
		testEnv.BinaryAssetsDirectory = getFirstFoundEnvTestBinaryDir()
	}

	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	By("creating skycluster-system namespace")
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "skycluster-system",
		},
	}
	err = k8sClient.Create(ctx, namespace)
	Expect(err).NotTo(HaveOccurred())

	By("creating the provider profile (aws)")
	providerprofileAWS = createProviderProfileAWS(types.NamespacedName{
		Name:      "aws-us-east-1a",
		Namespace: "skycluster-system",
	})
	err = k8sClient.Create(ctx, providerprofileAWS)
	Expect(err).NotTo(HaveOccurred())

	By("creating the provider profile (gcp)")
	providerprofileGCP = createProviderProfileGCP(types.NamespacedName{
		Name:      "gcp-us-east1-a",
		Namespace: "skycluster-system",
	})
	err = k8sClient.Create(ctx, providerprofileGCP)
	Expect(err).NotTo(HaveOccurred())

	By("reconciling the provider profile (aws)")
	ppReconciler := getProviderProfileReconciler()
	_, err = ppReconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: providerprofileAWS.Name, Namespace: providerprofileAWS.Namespace},
	})
	Expect(err).NotTo(HaveOccurred())

	By("reconciling the provider profile (gcp)")
	_, err = ppReconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: providerprofileGCP.Name, Namespace: providerprofileGCP.Namespace},
	})
	Expect(err).NotTo(HaveOccurred())

	By("fetching the latency objects")
	latList := &cv1a1.LatencyList{}
	err = k8sClient.List(ctx, latList, client.MatchingLabels(map[string]string{}))
	Expect(err).NotTo(HaveOccurred())
	Expect(latList.Items).To(HaveLen(1))

	By("reconciling the latency object")
	latReconciler := getLatencyReconciler()
	for _, lat := range latList.Items {
		_, err = latReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: lat.Name, Namespace: lat.Namespace},
		})
		Expect(err).NotTo(HaveOccurred())
	}

	// Fetch the config map
	cmList := &corev1.ConfigMapList{}
	err = k8sClient.List(ctx, cmList, client.MatchingLabels(map[string]string{
		"skycluster.io/config-type": "provider-profile",
	}))
	Expect(err).NotTo(HaveOccurred())
	Expect(cmList.Items).To(HaveLen(2))

	By("updating the config map(s)")
	for _, cm := range cmList.Items {
		zone := lo.Ternary(cm.Labels["skycluster.io/provider-platform"] == "aws", "us-east-1a", "us-east1-a")
		cm.Data = configMapData(zone)
		err = k8sClient.Update(ctx, &cm)
		Expect(err).NotTo(HaveOccurred())
	}

	By("creating the provider e2e config map")
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "provider-e2e-config",
			Namespace: "skycluster-system",
			Labels: map[string]string{
				"skycluster.io/config-type": "provider-metadata-e2e",
			},
		},
		Data: configMapDataProviderE2E(),
	}
	Expect(k8sClient.Create(ctx, cm)).NotTo(HaveOccurred())
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	cancel()
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

// getFirstFoundEnvTestBinaryDir locates the first binary in the specified path.
// ENVTEST-based tests depend on specific binaries, usually located in paths set by
// controller-runtime. When running tests directly (e.g., via an IDE) without using
// Makefile targets, the 'BinaryAssetsDirectory' must be explicitly configured.
//
// This function streamlines the process by finding the required binaries, similar to
// setting the 'KUBEBUILDER_ASSETS' environment variable. To ensure the binaries are
// properly set up, run 'make setup-envtest' beforehand.
func getFirstFoundEnvTestBinaryDir() string {
	basePath := filepath.Join("..", "..", "..", "..", "bin", "k8s")
	entries, err := os.ReadDir(basePath)
	if err != nil {
		logf.Log.Error(err, "Failed to read directory", "path", basePath)
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(basePath, entry.Name())
		}
	}
	return ""
}

func getLatencyReconciler() *cv1a1ctrl.LatencyReconciler {
	return &cv1a1ctrl.LatencyReconciler{
		Client: k8sClient,
		Scheme: k8sClient.Scheme(),
		Logger: zap.New(pkglog.CustomLogger()).WithName("[Latency]"),
	}
}

func getProviderProfileReconciler() *cv1a1ppctrl.ProviderProfileReconciler {
	return &cv1a1ppctrl.ProviderProfileReconciler{
		Client: k8sClient,
		Scheme: k8sClient.Scheme(),
		Logger: zap.New(pkglog.CustomLogger()).WithName("[ProviderProfile]"),
	}
}
