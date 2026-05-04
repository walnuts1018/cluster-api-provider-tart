//go:build e2e
// +build e2e

package provisioning

import (
	"context"
	"fmt"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/cluster-api/test/framework/clusterctl"
	"sigs.k8s.io/cluster-api/util"
)

var _ = Describe("Provisioning E2E tests", func() {
	var (
		namespace   *corev1.Namespace
		ctx         context.Context
		cancel      context.CancelFunc
		watchCancel context.CancelFunc
		result      *clusterctl.ApplyClusterTemplateAndWaitResult
		clusterName string
	)

	BeforeEach(func() {
		ctx, cancel = context.WithCancel(context.TODO())
		result = &clusterctl.ApplyClusterTemplateAndWaitResult{}
		clusterName = fmt.Sprintf("tart-e2e-%s", util.RandomString(6))

		Expect(bootstrapClusterProxy).NotTo(BeNil(), "BootstrapClusterProxy can't be nil")
		namespace, watchCancel = framework.CreateNamespaceAndWatchEvents(ctx, framework.CreateNamespaceAndWatchEventsInput{
			Creator:   bootstrapClusterProxy.GetClient(),
			ClientSet: bootstrapClusterProxy.GetClientSet(),
			Name:      fmt.Sprintf("tart-e2e-%s", util.RandomString(6)),
			LogFolder: filepath.Join(artifactsFolder, "clusters", bootstrapClusterProxy.GetName()),
		})
	})

	AfterEach(func() {
		if result.Cluster != nil {
			framework.DumpSpecResourcesAndCleanup(ctx, clusterName, bootstrapClusterProxy, artifactsFolder, namespace.Name, namespace, watchCancel, result.Cluster, e2eConfig.GetIntervals, skipCleanup)
		}
		cancel()
	})

	It("Should provision a workload cluster", func() {
		clusterctl.ApplyClusterTemplateAndWait(ctx, clusterctl.ApplyClusterTemplateAndWaitInput{
			ClusterProxy: bootstrapClusterProxy,
			ConfigCluster: clusterctl.ConfigClusterInput{
				LogFolder:                filepath.Join(artifactsFolder, "clusters", bootstrapClusterProxy.GetName()),
				ClusterctlConfigPath:     clusterctlConfig,
				KubeconfigPath:           bootstrapClusterProxy.GetKubeconfigPath(),
				InfrastructureProvider:   clusterctl.DefaultInfrastructureProvider,
				Flavor:                   clusterctl.DefaultFlavor,
				Namespace:                namespace.Name,
				ClusterName:              clusterName,
				KubernetesVersion:        e2eConfig.Variables["KUBERNETES_VERSION"],
				ControlPlaneMachineCount: ptr.To[int64](1),
				WorkerMachineCount:       ptr.To[int64](1),
			},
			WaitForClusterIntervals:      e2eConfig.GetIntervals(bootstrapClusterProxy.GetName(), "wait-cluster"),
			WaitForControlPlaneIntervals: e2eConfig.GetIntervals(bootstrapClusterProxy.GetName(), "wait-control-plane"),
			WaitForMachineDeployments:    e2eConfig.GetIntervals(bootstrapClusterProxy.GetName(), "wait-worker-nodes"),
		}, result)
	})
})
