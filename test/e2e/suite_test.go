//go:build e2e

// Package e2eは、GitHub Actions runner上のQEMU/libvirt bare-metal labを使い、Tartの
// WoL実装・PXEブート・Talos maintenance APIを実際に通した3本の必須E2E
// (FreshProvision/InPlaceUpgrade/ReconcileRecovery)を実行する。ginkgo/gomegaで記述し、
// shell sleepではなくAPI観測(Condition/Eventually)だけに依存する。
//
// 実行にはlinux + KVM + libvirtが必要であり、darwin開発環境では`go vet -tags e2e`による
// 静的検証のみが可能である(test/e2e/lab/lab_stub.goが肩代わりする)。
package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	bootstrapv1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/bootstrap/v1alpha1"
	controlplanev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/controlplane/v1alpha1"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/test/e2e/framework"
	"github.com/walnuts1018/cluster-api-provider-tart/test/e2e/lab"
	e2enetboot "github.com/walnuts1018/cluster-api-provider-tart/test/e2e/netboot"
	testutils "github.com/walnuts1018/cluster-api-provider-tart/test/utils"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Tart bare-metal E2E Suite")
}

const (
	kindClusterName = "tart-e2e"

	// labNetworkCIDRはRFC 5737 TEST-NET-2を使う(実機/外部networkと衝突しないprivate testing用block)。
	labNetworkCIDR = "198.51.100.0/24"
	labNetworkName = "tart-e2e-lab"
	labBridgeName  = "tartlab0"

	// controlPlaneVMMACAddressはIANA予約block(00-00-5E-00-53-00〜FF, RFC 7042)から
	// CLAUDE.mdの規約に従って割り当てる。
	controlPlaneVMMACAddress = "00:00:5E:00:53:01"
	controlPlaneVMName       = "tart-e2e-cp-0"
)

var (
	ctx = context.Background()

	scheme *runtime.Scheme

	kindCluster    *framework.KindCluster
	kubeconfigPath string
	k8sClient      client.Client

	testLab       lab.Lab
	labWorkDir    string
	wolGateway    *lab.Gateway
	netbootServer *e2enetboot.Server

	artifactRootDir string
)

var _ = BeforeSuite(func() {
	scheme = runtime.NewScheme()
	Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
	Expect(clusterv1.AddToScheme(scheme)).To(Succeed())
	Expect(infrav1alpha1.AddToScheme(scheme)).To(Succeed())
	Expect(bootstrapv1alpha1.AddToScheme(scheme)).To(Succeed())
	Expect(controlplanev1alpha1.AddToScheme(scheme)).To(Succeed())

	projectDir, err := testutils.GetProjectDir()
	Expect(err).NotTo(HaveOccurred())

	artifactRootDir = envOrDefault("TART_E2E_ARTIFACT_DIR", filepath.Join(projectDir, "_artifacts", "e2e"))
	Expect(os.MkdirAll(artifactRootDir, 0o755)).To(Succeed())

	labWorkDir = envOrDefault("TART_E2E_LAB_WORKDIR", filepath.Join(os.TempDir(), "tart-e2e-lab"))
	Expect(os.Setenv("TART_E2E_LAB_WORKDIR", labWorkDir)).To(Succeed())

	By("bare-metal labを構築する(libvirt network + VM定義。電源はshutoffのまま)")
	vmSpecs := []lab.VMSpec{
		{
			Name:          controlPlaneVMName,
			MACAddress:    controlPlaneVMMACAddress,
			VCPUs:         4,
			MemoryMiB:     8192,
			SystemDiskGiB: 40,
			SSDDiskGiB:    20,
			HDDDiskGiB:    20,
		},
	}
	testLab, err = lab.NewLibvirtLab("qemu:///system", lab.Config{
		NetworkName:   labNetworkName,
		NetworkBridge: labBridgeName,
		NetworkCIDR:   labNetworkCIDR,
		WorkDir:       labWorkDir,
		VMs:           vmSpecs,
	})
	Expect(err).NotTo(HaveOccurred(), "failed to connect to libvirt; this suite requires a linux runner with KVM/libvirt (see .github/actions/setup-lab)")
	Expect(testLab.EnsureNetwork(ctx)).To(Succeed())
	for _, spec := range vmSpecs {
		_, err := testLab.EnsureVM(ctx, spec)
		Expect(err).NotTo(HaveOccurred())
	}

	By("wol-libvirt-gatewayを起動する")
	gatewayBinary := envOrDefault("TART_E2E_WOL_GATEWAY_BINARY", "/usr/local/bin/wol-libvirt-gateway")
	wolGateway, err = lab.StartGateway(ctx, gatewayBinary, "qemu:///system")
	Expect(err).NotTo(HaveOccurred())
	// TODO: WoLブロードキャストパケットがGitHub Actions runner上のnetwork構成でこのgatewayまで
	// 実際に届くかはCI実行で検証が必要である(lab/wolgateway.goのTODO参照)。届かない場合は
	// runner側のbroadcast/bridge設定を調整し、TartHost.spec.power.wakeOnLAN.broadcastAddressを
	// gatewayの実際のlisten先に合わせて再設定する。

	By("kind clusterを作成し、CAPI core + Tart providerをinstallする")
	kindCluster, err = framework.CreateKindCluster(ctx, kindClusterName)
	Expect(err).NotTo(HaveOccurred())

	// KUBECONFIG環境変数はこの後のすべてのkubectl呼び出し(失敗時のDumpAllを含む)が参照するため、
	// CAPI core等のinstallより先にexportし、途中で失敗しても診断情報を採取できるようにする。
	kubeconfigPath = envOrDefault("KUBECONFIG", filepath.Join(os.TempDir(), "tart-e2e-kubeconfig"))
	Expect(exportKindKubeconfig(kindClusterName, kubeconfigPath)).To(Succeed())

	Expect(framework.InstallCAPICore(ctx)).To(Succeed())

	// CIがlocalでbuildしたcontroller-manager/netboot-server imageをkindへloadし、config/default
	// 配下のkustomize manifestが参照する仮image(':latest'固定、実在しないregistry)を実際に
	// このimageへ置き換える。TART_E2E_PROVIDER_IMAGE_TAGが空の場合は仮imageのままapplyされ、
	// 通常はImagePullBackOffになる(ローカル検証用の抜け道として残す)。
	providerImageTag := envOrDefault("TART_E2E_PROVIDER_IMAGE_TAG", "")
	if providerImageTag != "" {
		Expect(framework.LoadProviderImagesForTag(providerImageTag)).To(Succeed())
	}

	Expect(framework.InstallTartProviders(ctx, providerImageTag)).To(Succeed())

	restConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	Expect(err).NotTo(HaveOccurred())
	k8sClient, err = client.New(restConfig, client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred())

	By("netboot-serverをテストプロセス内で起動する(ProxyDHCP/TFTP/HTTP)")
	netbootServer, err = e2enetboot.Start(ctx, e2enetboot.Config{
		KubeconfigPath:         kubeconfigPath,
		TFTPRoot:               filepath.Join(labWorkDir, "tftp"),
		DHCPBindAddress:        envOrDefault("TART_E2E_NETBOOT_DHCP_BIND", "0.0.0.0"),
		TFTPBindAddress:        envOrDefault("TART_E2E_NETBOOT_TFTP_BIND", "0.0.0.0:69"),
		HTTPBindAddress:        envOrDefault("TART_E2E_NETBOOT_HTTP_BIND", ":8080"),
		AdvertiseAddress:       envOrDefault("TART_E2E_NETBOOT_ADVERTISE_ADDRESS", ""),
		AdvertiseHTTPBaseURL:   envOrDefault("TART_E2E_NETBOOT_ADVERTISE_HTTP_BASE_URL", ""),
		ImageFactoryPXEBaseURL: envOrDefault("TART_E2E_IMAGE_FACTORY_PXE_BASE_URL", ""),
		DiscoveryTalosVersion:  envOrDefault("TART_E2E_DISCOVERY_TALOS_VERSION", ""),
		DiscoverySchematicID:   envOrDefault("TART_E2E_DISCOVERY_SCHEMATIC_ID", ""),
	})
	Expect(err).NotTo(HaveOccurred())
	// TODO: netboot-serverのProxyDHCPが、lab networkのdnsmasq(通常DHCP)と同一segmentで
	// 共存できているか(競合するport 67 bindにならないか)はCI実行で確認が必要である。
})

var _ = AfterSuite(func() {
	// CurrentSpecReport().Failed()はBeforeSuiteの失敗を反映しないため(AfterSuite自体は
	// failしていないとginkgoが判断する)、失敗有無に関わらず常にdumpする。
	// 成功時のartifactは追加コストが小さく、失敗調査の取りこぼしを避ける方を優先する。
	framework.DumpAll(filepath.Join(artifactRootDir, "final-state"))

	if netbootServer != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := netbootServer.Stop(stopCtx); err != nil {
			testutils.WarnError(fmt.Errorf("stop netboot server: %w", err))
		}
	}
	if wolGateway != nil {
		if err := wolGateway.DumpLogs(filepath.Join(artifactRootDir, "wol-gateway")); err != nil {
			testutils.WarnError(fmt.Errorf("dump wol gateway logs: %w", err))
		}
		if err := wolGateway.Stop(); err != nil {
			testutils.WarnError(fmt.Errorf("stop wol gateway: %w", err))
		}
	}
	if testLab != nil {
		if err := testLab.DestroyAll(context.Background()); err != nil {
			testutils.WarnError(fmt.Errorf("destroy lab: %w", err))
		}
		if err := testLab.Close(); err != nil {
			testutils.WarnError(fmt.Errorf("close lab connection: %w", err))
		}
	}
	if kindCluster != nil {
		if err := kindCluster.Delete(context.Background()); err != nil {
			testutils.WarnError(fmt.Errorf("delete kind cluster: %w", err))
		}
	}
})

// exportKindKubeconfigは`kind export kubeconfig`を実行し、指定pathへkubeconfigを書き出す。
// テストプロセス内client(k8sClient)とnetboot resolverの両方が同じkubeconfigを参照するために使う。
func exportKindKubeconfig(clusterName, path string) error {
	cmd := exec.CommandContext(ctx, "kind", "export", "kubeconfig", "--name", clusterName, "--kubeconfig", path)
	_, err := testutils.Run(cmd)
	return err
}

func envOrDefault(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}
	return fallback
}
