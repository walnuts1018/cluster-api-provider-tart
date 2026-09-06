package tartcontrolplane

import (
	"context"
	"errors"
	"testing"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/walnuts1018/cluster-api-provider-tart/adapter/talos"
	controlplanev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/controlplane/v1alpha1"
)

// recordingUpgradeRunnerは、cluster-wide Kubernetes upgradeが実際に要求された回数を記録するtest doubleである。
type recordingUpgradeRunner struct {
	upgrades int
	detects  int
	version  string
}

func (r *recordingUpgradeRunner) DetectVersion(context.Context, talos.KubernetesUpgradeRequest) (string, error) {
	r.detects++
	return r.version, nil
}

func (r *recordingUpgradeRunner) Upgrade(context.Context, talos.KubernetesUpgradeRequest) error {
	r.upgrades++
	return nil
}

func leaseKeyForTest(cp *controlplanev1alpha1.TartControlPlane) client.ObjectKey {
	return client.ObjectKey{Namespace: cp.Namespace, Name: kubernetesUpgradeLeaseName(cp.Name)}
}

func TestEvaluateKubernetesUpgrade(t *testing.T) {
	t.Parallel()

	healthy := kubernetesUpgradePreflight{
		desiredVersion:          "v1.34.1",
		observedVersion:         "1.33.4",
		controlPlaneInitialized: true,
		workloadAPIReady:        true,
		talosReachable:          true,
		etcdQuorumHealthy:       true,
		machinesReady:           true,
	}
	mutate := func(apply func(*kubernetesUpgradePreflight)) kubernetesUpgradePreflight {
		preflight := healthy
		apply(&preflight)
		return preflight
	}

	tests := []struct {
		name      string
		preflight kubernetesUpgradePreflight
		want      kubernetesUpgradeAction
	}{
		{name: "healthy cluster upgrades", preflight: healthy, want: kubernetesUpgradeActionUpgrade},
		{name: "empty desired version stops", preflight: mutate(func(p *kubernetesUpgradePreflight) { p.desiredVersion = "" }), want: kubernetesUpgradeActionFail},
		{name: "already converged does nothing", preflight: mutate(func(p *kubernetesUpgradePreflight) { p.observedVersion = "v1.34.1" }), want: kubernetesUpgradeActionNone},
		{name: "uninitialized control plane waits", preflight: mutate(func(p *kubernetesUpgradePreflight) { p.controlPlaneInitialized = false }), want: kubernetesUpgradeActionWait},
		{name: "unavailable workload API waits", preflight: mutate(func(p *kubernetesUpgradePreflight) { p.workloadAPIReady = false }), want: kubernetesUpgradeActionWait},
		{name: "unreachable Talos API waits", preflight: mutate(func(p *kubernetesUpgradePreflight) { p.talosReachable = false }), want: kubernetesUpgradeActionWait},
		{name: "unproven etcd quorum waits", preflight: mutate(func(p *kubernetesUpgradePreflight) { p.etcdQuorumHealthy = false }), want: kubernetesUpgradeActionWait},
		{name: "not ready machines wait", preflight: mutate(func(p *kubernetesUpgradePreflight) { p.machinesReady = false }), want: kubernetesUpgradeActionWait},
		{name: "another operation waits", preflight: mutate(func(p *kubernetesUpgradePreflight) { p.otherOperationInProgress = true }), want: kubernetesUpgradeActionWait},
		{name: "unobserved version waits", preflight: mutate(func(p *kubernetesUpgradePreflight) { p.observedVersion = "" }), want: kubernetesUpgradeActionWait},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			decision := evaluateKubernetesUpgrade(test.preflight)
			if decision.action != test.want {
				t.Fatalf("evaluateKubernetesUpgrade() action = %d, want %d (reason %q)", decision.action, test.want, decision.reason)
			}
		})
	}
}

func TestKubernetesUpgradeLeaseAcquirable(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	lease := func(holder string, renewedAgo time.Duration) *coordinationv1.Lease {
		renew := metav1.NewMicroTime(now.Add(-renewedAgo))
		seconds := int32(kubernetesUpgradeLeaseDuration / time.Second)
		return &coordinationv1.Lease{Spec: coordinationv1.LeaseSpec{
			HolderIdentity:       &holder,
			LeaseDurationSeconds: &seconds,
			RenewTime:            &renew,
		}}
	}

	tests := []struct {
		name  string
		lease *coordinationv1.Lease
		want  bool
	}{
		{name: "missing lease is acquirable", lease: nil, want: true},
		{name: "own lease is reacquirable", lease: lease("self", time.Second), want: true},
		{name: "fresh lease of another holder is not acquirable", lease: lease("other", time.Second), want: false},
		{name: "expired lease of another holder is acquirable", lease: lease("other", kubernetesUpgradeLeaseDuration+time.Second), want: true},
		{name: "released lease is acquirable", lease: lease("", time.Second), want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := kubernetesUpgradeLeaseAcquirable(test.lease, "self", now); got != test.want {
				t.Fatalf("kubernetesUpgradeLeaseAcquirable() = %t, want %t", got, test.want)
			}
		})
	}
}

// TestAcquireKubernetesUpgradeLeaseExcludesConcurrentControllersは、複数のcontroller replicaが
// 同一clusterのKubernetes upgradeを同時に開始できないことと、解放後に引き継げることを確認する。
func TestAcquireKubernetesUpgradeLeaseExcludesConcurrentControllers(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := coordinationv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	if err := controlplanev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	cp := &controlplanev1alpha1.TartControlPlane{}
	cp.Namespace = "cluster-a"
	cp.Name = "control-plane"
	cp.UID = "control-plane-uid"

	first := &TartControlPlaneReconciler{Client: fakeClient, KubernetesUpgradeIdentity: "controller-a"}
	second := &TartControlPlaneReconciler{Client: fakeClient, KubernetesUpgradeIdentity: "controller-b"}

	lease, err := first.acquireKubernetesUpgradeLease(t.Context(), cp)
	if err != nil {
		t.Fatalf("first acquire error = %v, want nil", err)
	}
	if _, err := second.acquireKubernetesUpgradeLease(t.Context(), cp); !errors.Is(err, errKubernetesUpgradeLeaseHeld) {
		t.Fatalf("second acquire error = %v, want errKubernetesUpgradeLeaseHeld", err)
	}
	// controller再起動後に同じidentityが再取得できることは、crash recoveryで同じupgradeを再開する前提である。
	if _, err := first.acquireKubernetesUpgradeLease(t.Context(), cp); err != nil {
		t.Fatalf("reacquire by the holder error = %v, want nil", err)
	}
	first.releaseKubernetesUpgradeLease(t.Context(), lease)
	if _, err := second.acquireKubernetesUpgradeLease(t.Context(), cp); err != nil {
		t.Fatalf("acquire after release error = %v, want nil", err)
	}
}

// TestReconcileKubernetesUpgradeStopsOnPreflightは、preflightを満たさない場合にupgradeを開始せず、
// leaseも取得しないことを確認する。
func TestReconcileKubernetesUpgradeStopsOnPreflight(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := coordinationv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	if err := controlplanev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	runner := &recordingUpgradeRunner{}
	reconciler := &TartControlPlaneReconciler{Client: fakeClient, KubernetesUpgrade: runner, KubernetesUpgradeIdentity: "controller-a"}

	cp := &controlplanev1alpha1.TartControlPlane{}
	cp.Namespace = "cluster-a"
	cp.Name = "control-plane"
	cp.Spec.Version = "v1.34.1"
	cluster := &clusterv1.Cluster{}

	state := reconciler.reconcileKubernetesUpgrade(t.Context(), cp, cluster, nil, controlPlaneBootstrapState{}, false, 3)
	if runner.upgrades != 0 {
		t.Fatalf("upgrades = %d, want 0 while the preflight is not satisfied", runner.upgrades)
	}
	if state.requeueAfter == 0 {
		t.Fatal("requeueAfter = 0, want a retry while the preflight is not satisfied")
	}
	lease := &coordinationv1.Lease{}
	if err := fakeClient.Get(t.Context(), leaseKeyForTest(cp), lease); err == nil {
		t.Fatal("the Kubernetes upgrade lease was created although the preflight was not satisfied")
	}
}

// TestReconcileKubernetesUpgradeSkipsConvergedClusterは、controller再起動後にdesired versionへ既に
// 収束しているclusterに対して、upgradeを再実行しないことを確認する。
func TestReconcileKubernetesUpgradeSkipsConvergedCluster(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := coordinationv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	if err := controlplanev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	runner := &recordingUpgradeRunner{}
	reconciler := &TartControlPlaneReconciler{Client: fakeClient, KubernetesUpgrade: runner, KubernetesUpgradeIdentity: "controller-a"}

	cp := &controlplanev1alpha1.TartControlPlane{}
	cp.Namespace = "cluster-a"
	cp.Name = "control-plane"
	cp.Spec.Version = "v1.34.1"
	// 再起動直後は、前回のreconcileが残したStatusの観測値からdesired versionとの一致を判断する。
	cp.Status.KubernetesUpgrade.ObservedVersion = "1.34.1"
	cluster := &clusterv1.Cluster{}

	state := reconciler.reconcileKubernetesUpgrade(t.Context(), cp, cluster, nil, controlPlaneBootstrapState{initialized: true}, false, 3)
	if runner.upgrades != 0 {
		t.Fatalf("upgrades = %d, want 0 for a converged cluster", runner.upgrades)
	}
	if state.active {
		t.Fatal("state.active = true, want false for a converged cluster")
	}
	if state.targetVersion != cp.Spec.Version {
		t.Fatalf("state.targetVersion = %q, want %q", state.targetVersion, cp.Spec.Version)
	}
}
