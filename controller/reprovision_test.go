package controller

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kubernetesadapter "github.com/walnuts1018/cluster-api-provider-tart/adapter/kubernetes"
	"github.com/walnuts1018/cluster-api-provider-tart/adapter/talos/configbuilder"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	domainbootstrap "github.com/walnuts1018/cluster-api-provider-tart/domain/bootstrap"
	clusterdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/cluster"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/host"
	"github.com/walnuts1018/cluster-api-provider-tart/domain/network"
	"github.com/walnuts1018/cluster-api-provider-tart/recovery"
	"github.com/walnuts1018/cluster-api-provider-tart/talos"
	usecasebootstrap "github.com/walnuts1018/cluster-api-provider-tart/usecase/bootstrap"
	hostusecase "github.com/walnuts1018/cluster-api-provider-tart/usecase/host"
)

const (
	testManagementNamespace = "tart-system"
	testHostEndpoint        = "198.51.100.10:50000"
	testHostMAC             = "00:00:5E:00:53:01"
	testForeignMAC          = "00:00:5E:00:53:02"
	testSystemUUID          = "1f3a9c22-7f1e-4b7a-9f0c-5d2e8a4b6c10"
)

// fakeTalosNodeはTalos APIをホストせずにReprovision flowを検証するためのTalosNode実装である。
type fakeTalosNode struct {
	inventory        talos.Inventory
	configuration    []byte
	inventoryErr     error
	configurationErr error
	resetErr         error
	resets           int
	closed           bool
}

func (n *fakeTalosNode) Inventory(context.Context) (talos.Inventory, error) {
	return n.inventory, n.inventoryErr
}

func (n *fakeTalosNode) ActiveMachineConfiguration(context.Context) ([]byte, error) {
	return n.configuration, n.configurationErr
}

func (n *fakeTalosNode) Reset(context.Context) error {
	if n.resetErr != nil {
		return n.resetErr
	}
	n.resets++
	return nil
}

func (n *fakeTalosNode) Close() error {
	n.closed = true
	return nil
}

type fakeTalosDialer struct {
	recovery       *fakeTalosNode
	recoveryErr    error
	maintenance    *fakeTalosNode
	maintenanceErr error
}

func (d *fakeTalosDialer) DialRecovery(context.Context, string, recovery.Material) (TalosNode, error) {
	if d.recoveryErr != nil {
		return nil, d.recoveryErr
	}
	return d.recovery, nil
}

func (d *fakeTalosDialer) DialMaintenance(context.Context, string) (TalosNode, error) {
	if d.maintenanceErr != nil {
		return nil, d.maintenanceErr
	}
	return d.maintenance, nil
}

// testBundleは指定したcluster identityを持つTalos secret bundleを生成する。
func testBundle(t *testing.T, clusterID string) *secrets.Bundle {
	t.Helper()
	bundle, err := secrets.NewBundle(secrets.NewFixedClock(time.Now()), talosconfig.TalosVersionCurrent)
	if err != nil {
		t.Fatalf("secrets.NewBundle() error = %v", err)
	}
	bundle.Cluster.ID = clusterID
	return bundle
}

// testConfigurationは指定したbundleから完全なworker machine configurationを生成する。稼働中nodeのactive configurationの代用である。
func testConfiguration(t *testing.T, bundle *secrets.Bundle) []byte {
	t.Helper()
	configuration, err := configbuilder.GenerateMachineConfiguration(usecasebootstrap.MachineConfigurationContext{
		ClusterName:          "reuse",
		ControlPlaneEndpoint: "https://api.test.walnuts.dev:6443",
		KubernetesVersion:    "v1.34.1",
		MachineRole:          domainbootstrap.MachineRoleWorker,
		SecretsBundle:        bundle,
		InstallDisk: &domainbootstrap.InstallDisk{
			DevicePath: "/dev/vda",
			SizeBytes:  64 * 1024 * 1024 * 1024,
			Serial:     "disk-a",
			Transport:  "virtio",
		},
	})
	if err != nil {
		t.Fatalf("GenerateMachineConfiguration() error = %v", err)
	}
	return configuration
}

func testInventory(t *testing.T, macAddress, systemUUID string) talos.Inventory {
	t.Helper()
	mac, err := network.ParseMACAddress(macAddress)
	if err != nil {
		t.Fatalf("ParseMACAddress() error = %v", err)
	}
	parsed, err := uuid.Parse(systemUUID)
	if err != nil {
		t.Fatalf("uuid.Parse() error = %v", err)
	}
	return talos.Inventory{MACAddresses: []network.MACAddress{mac}, SystemUUID: parsed}
}

// reprovisionFixtureはRetained済みでReprovisionを承認されたHostと、その旧installationのrecovery Secretを用意する。
type reprovisionFixture struct {
	reconciler *TartMachineReconciler
	machine    *infrav1alpha1.TartMachine
	host       *infrav1alpha1.TartHost
	clusterID  string
	dialer     *fakeTalosDialer
	node       *fakeTalosNode
}

func newReprovisionFixture(t *testing.T) *reprovisionFixture {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := infrav1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}

	clusterID := clusterdomain.NewClusterID().String()
	bundle := testBundle(t, clusterID)
	configuration := testConfiguration(t, bundle)
	material, err := recovery.MaterialFromBundle(bundle)
	if err != nil {
		t.Fatalf("MaterialFromBundle() error = %v", err)
	}
	recoverySecret, err := recovery.BuildSecret(testManagementNamespace, material)
	if err != nil {
		t.Fatalf("BuildSecret() error = %v", err)
	}

	macAddress, err := network.ParseMACAddress(testHostMAC)
	if err != nil {
		t.Fatalf("ParseMACAddress() error = %v", err)
	}
	previousMachineUID := types.UID("previous-machine")
	hostObject := &infrav1alpha1.TartHost{
		Name: "host-1",
		Spec: infrav1alpha1.TartHostSpec{
			HostID:          "9f1f6f4c-1b0a-4a26-91e0-3b2c1a5f9e77",
			MACAddress:      macAddress,
			TalosAPIAddress: network.Endpoint(testHostEndpoint),
			Power:           infrav1alpha1.PowerSpec{Backend: infrav1alpha1.PowerBackendManual},
			ConsumerRef: &corev1.ObjectReference{
				APIVersion: infrav1alpha1.GroupVersion.String(),
				Kind:       tartMachineKind,
				Namespace:  "default",
				Name:       "previous",
				UID:        previousMachineUID,
			},
		},
		Status: infrav1alpha1.TartHostStatus{
			Inventory: &infrav1alpha1.HostInventory{SystemUUID: testSystemUUID},
			CurrentTalosIdentityRef: &infrav1alpha1.TalosIdentityReference{
				ClusterID:         clusterID,
				RecoverySecretRef: infrav1alpha1.ManagementNamespaceSecretReference{Name: recoverySecret.Name},
				BoundAt:           metav1.Now(),
			},
		},
	}
	machineObject := &infrav1alpha1.TartMachine{
		Namespace: "default", Name: "next", UID: types.UID("next-machine"),
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(hostObject, machineObject, recoverySecret).
		WithStatusSubresource(hostObject, machineObject).
		Build()

	// Machine削除時のretentionを実際に通し、承認前のHostがRetainedになることを確認する。
	if err := kubernetesadapter.NewTartHostRepository(fakeClient).RetainHost(context.Background(), hostObject, *hostObject.Spec.ConsumerRef, infrav1alpha1.PreviousConsumerRef{
		Namespace: "default",
		Name:      "previous",
		UID:       previousMachineUID,
		ClusterID: clusterID,
	}); err != nil {
		t.Fatalf("RetainHost() error = %v", err)
	}
	if got := hostusecase.Classify(hostObject.Spec); got != hostdomain.Retained {
		t.Fatalf("hostusecase.Classify() after retention = %q, want Retained", got)
	}

	// ユーザーによる明示的なReprovision承認を与える。
	hostObject.Spec.ReusePolicy = infrav1alpha1.ReusePolicyAllowReuse
	hostObject.Spec.ReuseMode = infrav1alpha1.ReuseModeReprovision
	hostObject.Spec.ReuseApproval = &infrav1alpha1.ReuseApproval{PreviousConsumerUID: previousMachineUID}
	if err := fakeClient.Update(context.Background(), hostObject); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if got := hostusecase.Classify(hostObject.Spec); got != hostdomain.Reusable {
		t.Fatalf("hostusecase.Classify() after approval = %q, want Reusable", got)
	}

	node := &fakeTalosNode{
		configuration: configuration,
		inventory:     testInventory(t, testHostMAC, testSystemUUID),
	}
	dialer := &fakeTalosDialer{recovery: node}

	return &reprovisionFixture{
		reconciler: &TartMachineReconciler{Client: fakeClient, ManagementNamespace: testManagementNamespace, TalosDialer: dialer},
		machine:    machineObject,
		host:       hostObject,
		clusterID:  clusterID,
		dialer:     dialer,
		node:       node,
	}
}

func readyReason(machineObject *infrav1alpha1.TartMachine) string {
	condition := meta.FindStatusCondition(machineObject.Status.Conditions, infrav1alpha1.TartMachineReadyCondition)
	if condition == nil {
		return ""
	}
	return condition.Reason
}

// TestReconcileReprovisionResetsVerifiedHostは、承認とidentityが揃ったHostに対してResetを実行し、maintenance mode復帰の観測でrecovery identityとのbindingを解除することを確認する。
func TestReconcileReprovisionResetsVerifiedHost(t *testing.T) {
	t.Parallel()

	fixture := newReprovisionFixture(t)
	ctx := context.Background()

	_, handled, err := fixture.reconciler.reconcileReprovision(ctx, fixture.machine, fixture.host, testHostEndpoint)
	if err != nil {
		t.Fatalf("reconcileReprovision() error = %v", err)
	}
	if !handled {
		t.Fatal("reconcileReprovision() must handle a Host that still holds the previous Talos installation")
	}
	if fixture.node.resets != 1 {
		t.Fatalf("reconcileReprovision() issued %d resets, want 1", fixture.node.resets)
	}
	if !fixture.node.closed {
		t.Fatal("reconcileReprovision() must close the recovery Talos connection")
	}
	if reason := readyReason(fixture.machine); reason != infrav1alpha1.ReasonReprovisioning {
		t.Fatalf("Ready reason after reset = %q, want %q", reason, infrav1alpha1.ReasonReprovisioning)
	}
	if fixture.host.Status.CurrentTalosIdentityRef == nil {
		t.Fatal("the recovery identity binding must remain until maintenance mode is confirmed")
	}

	// Reset後はrecovery CAで認証できなくなり、maintenance modeで期待したHost identityを確認できる。
	fixture.dialer.recoveryErr = errors.New("connection refused")
	fixture.dialer.maintenance = &fakeTalosNode{inventory: testInventory(t, testHostMAC, testSystemUUID)}

	_, handled, err = fixture.reconciler.reconcileReprovision(ctx, fixture.machine, fixture.host, testHostEndpoint)
	if err != nil {
		t.Fatalf("reconcileReprovision() error = %v", err)
	}
	if handled {
		t.Fatal("reconcileReprovision() must hand a reset Host back to the normal provisioning path")
	}
	if fixture.host.Status.CurrentTalosIdentityRef != nil {
		t.Fatal("the recovery identity binding must be released once maintenance mode is confirmed")
	}
	persisted := &infrav1alpha1.TartHost{}
	if err := fixture.reconciler.Get(ctx, client.ObjectKey{Name: fixture.host.Name}, persisted); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if persisted.Status.CurrentTalosIdentityRef != nil {
		t.Fatal("the released recovery identity binding must be persisted")
	}
}

// TestReconcileReprovisionRefusesWrongTargetは、承認済みでも観測したidentityが一致しない場合にResetを実行しないことを確認する。
func TestReconcileReprovisionRefusesWrongTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		prepare func(t *testing.T, fixture *reprovisionFixture)
	}{
		{
			name: "a different Host answers on the endpoint",
			prepare: func(t *testing.T, fixture *reprovisionFixture) {
				t.Helper()
				fixture.node.inventory = testInventory(t, testForeignMAC, testSystemUUID)
			},
		},
		{
			name: "the same Host reports a different system UUID",
			prepare: func(t *testing.T, fixture *reprovisionFixture) {
				t.Helper()
				fixture.node.inventory = testInventory(t, testHostMAC, "0a5e2f4a-2c67-4d13-8f0e-7a1cbb5f7d92")
			},
		},
		{
			name: "the endpoint runs a different Talos cluster",
			prepare: func(t *testing.T, fixture *reprovisionFixture) {
				t.Helper()
				fixture.node.configuration = testConfiguration(t, testBundle(t, clusterdomain.NewClusterID().String()))
			},
		},
		{
			name: "the machine configuration cannot be observed",
			prepare: func(_ *testing.T, fixture *reprovisionFixture) {
				fixture.node.configurationErr = errors.New("permission denied")
			},
		},
		{
			name: "the maintenance endpoint reports a different Host after the recovery identity fails",
			prepare: func(t *testing.T, fixture *reprovisionFixture) {
				t.Helper()
				fixture.dialer.recoveryErr = errors.New("connection refused")
				fixture.dialer.maintenance = &fakeTalosNode{inventory: testInventory(t, testForeignMAC, testSystemUUID)}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			fixture := newReprovisionFixture(t)
			test.prepare(t, fixture)

			_, handled, err := fixture.reconciler.reconcileReprovision(context.Background(), fixture.machine, fixture.host, testHostEndpoint)
			if err != nil {
				t.Fatalf("reconcileReprovision() error = %v", err)
			}
			if !handled {
				t.Fatal("reconcileReprovision() must not fall through to provisioning while the previous installation is unresolved")
			}
			if fixture.node.resets != 0 {
				t.Fatalf("reconcileReprovision() issued %d resets, want 0", fixture.node.resets)
			}
			if fixture.host.Status.CurrentTalosIdentityRef == nil {
				t.Fatal("the recovery identity binding must be retained when the reset target cannot be verified")
			}
		})
	}
}

// TestReconcileReprovisionRequiresRecoveryIdentityは、承認や旧identityが揃わない場合の分岐を確認する。
func TestReconcileReprovisionRequiresRecoveryIdentity(t *testing.T) {
	t.Parallel()

	t.Run("an unapproved Host is not reset", func(t *testing.T) {
		t.Parallel()
		fixture := newReprovisionFixture(t)
		fixture.host.Spec.ReuseApproval = nil

		_, handled, err := fixture.reconciler.reconcileReprovision(context.Background(), fixture.machine, fixture.host, testHostEndpoint)
		if err != nil {
			t.Fatalf("reconcileReprovision() error = %v", err)
		}
		if handled || fixture.node.resets != 0 {
			t.Fatalf("reconcileReprovision() handled = %v, resets = %d; want false, 0", handled, fixture.node.resets)
		}
	})

	t.Run("a missing recovery Secret stops before any destructive operation", func(t *testing.T) {
		t.Parallel()
		fixture := newReprovisionFixture(t)
		fixture.host.Status.CurrentTalosIdentityRef.RecoverySecretRef.Name = recovery.SecretNamePrefix + clusterdomain.NewClusterID().String() + "-0123456789abcdef"

		_, handled, err := fixture.reconciler.reconcileReprovision(context.Background(), fixture.machine, fixture.host, testHostEndpoint)
		if err != nil {
			t.Fatalf("reconcileReprovision() error = %v", err)
		}
		if !handled || fixture.node.resets != 0 {
			t.Fatalf("reconcileReprovision() handled = %v, resets = %d; want true, 0", handled, fixture.node.resets)
		}
		if reason := readyReason(fixture.machine); reason != infrav1alpha1.ReasonRecoveryIdentityUnavailable {
			t.Fatalf("Ready reason = %q, want %q", reason, infrav1alpha1.ReasonRecoveryIdentityUnavailable)
		}
	})
}

// TestShouldRebindTalosIdentityは、recovery identity bindingの更新判定が旧installationの復旧手段を失わないことを確認する。
func TestShouldRebindTalosIdentity(t *testing.T) {
	t.Parallel()

	const (
		clusterID = "6b1b8e56-0a2c-4a5b-9c1f-1f2b7f0a9c31"
		otherID   = "0a5e2f4a-2c67-4d13-8f0e-7a1cbb5f7d92"
		current   = "tart-talos-recovery-6b1b8e56-0a2c-4a5b-9c1f-1f2b7f0a9c31-0123456789abcdef"
		rotated   = "tart-talos-recovery-6b1b8e56-0a2c-4a5b-9c1f-1f2b7f0a9c31-fedcba9876543210"
	)
	tests := []struct {
		name      string
		reference *infrav1alpha1.TalosIdentityReference
		clusterID string
		secret    string
		want      bool
	}{
		{name: "an unbound Host is bound to the current identity", clusterID: clusterID, secret: current, want: true},
		{
			name:      "an identical binding is left untouched",
			reference: &infrav1alpha1.TalosIdentityReference{ClusterID: clusterID, RecoverySecretRef: infrav1alpha1.ManagementNamespaceSecretReference{Name: current}},
			clusterID: clusterID,
			secret:    current,
		},
		{
			name:      "a rotated certificate authority in the same cluster rebinds",
			reference: &infrav1alpha1.TalosIdentityReference{ClusterID: clusterID, RecoverySecretRef: infrav1alpha1.ManagementNamespaceSecretReference{Name: current}},
			clusterID: clusterID,
			secret:    rotated,
			want:      true,
		},
		{
			name:      "a binding to a previous cluster is never overwritten",
			reference: &infrav1alpha1.TalosIdentityReference{ClusterID: otherID, RecoverySecretRef: infrav1alpha1.ManagementNamespaceSecretReference{Name: "tart-talos-recovery-0a5e2f4a-2c67-4d13-8f0e-7a1cbb5f7d92-0123456789abcdef"}},
			clusterID: clusterID,
			secret:    current,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := shouldRebindTalosIdentity(test.reference, test.clusterID, test.secret); got != test.want {
				t.Fatalf("shouldRebindTalosIdentity() = %v, want %v", got, test.want)
			}
		})
	}
}
