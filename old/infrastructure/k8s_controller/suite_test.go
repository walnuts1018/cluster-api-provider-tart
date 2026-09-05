package controller

import (
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	envtestutil "github.com/walnuts1018/cluster-api-provider-tart/utils/testutils/envtest"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	// +kubebuilder:scaffold:imports
)

// これらのテストはGinkgo（BDDスタイルのGoテストフレームワーク）を使う。

const projectRoot = "../.."

var (
	testEnv   *envtest.Environment
	cfg       *rest.Config
	k8sClient client.Client
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	var err error
	err = infrastructurev1beta1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = clusterv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// +kubebuilder:scaffold:scheme

	By("bootstrapping test environment")

	crdPaths := localCRDPaths()

	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     crdPaths,
		ErrorIfCRDPathMissing: true,
	}

	assets := envtestutil.BinaryAssetsDirectory(projectRoot)
	Expect(assets).NotTo(BeEmpty(), envtestutil.MissingAssetsMessage())
	testEnv.BinaryAssetsDirectory = assets

	// cfg はこのファイルのグローバル変数として扱う。
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	if testEnv == nil {
		return
	}
	Eventually(func() error {
		return testEnv.Stop()
	}, time.Minute, time.Second).Should(Succeed())
})

func localCRDPaths() []string {
	return []string{
		filepath.Join(projectRoot, "config", "crd", "bases"),
		envtestutil.ClusterAPICRDDirectory(projectRoot),
	}
}
