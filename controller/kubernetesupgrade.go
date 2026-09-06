package controller

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	controlplanev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/controlplane/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/talos"
)

// Kubernetes version upgradeはcluster-wide operationであり、TartControlPlaneだけが所有する。
// desired versionのsource of truthはCAPIから伝わるTartControlPlane.spec.versionであり、
// Talos側で独立したversion stateを持たない。実際のupgrade手順はTalos upstream実装(talos.KubernetesUpgradeRunner)へ委譲する。

const (
	// kubernetesUpgradeLeaseDurationは、leaseを保持しているcontroller replicaがrenewせずに排他を保てる時間である。
	// controllerがcrashした場合、この時間が経過すると他のreplicaがleaseを引き継いで同じupgradeを再実行できる。
	kubernetesUpgradeLeaseDuration = 5 * time.Minute
	// kubernetesUpgradeLeaseRenewIntervalは、upgrade実行中にleaseをrenewする間隔である。
	kubernetesUpgradeLeaseRenewInterval = time.Minute
	// kubernetesUpgradeTimeoutは1回のupgrade実行の上限である。leaseはこの間renewし続ける。
	kubernetesUpgradeTimeout = 30 * time.Minute
	// kubernetesUpgradeObserveTimeoutは、cluster側の現在versionを観測する際の上限である。
	kubernetesUpgradeObserveTimeout = 60 * time.Second
	// kubernetesUpgradeRequeueは、preflightや排他の待ちで再評価するまでの間隔である。
	kubernetesUpgradeRequeue = 60 * time.Second
)

// controlPlaneKubernetesUpgradeStateは、Conditionとstatusへ反映するcoarse-grainedなupgrade観測結果である。
type controlPlaneKubernetesUpgradeState struct {
	active          bool
	reason          string
	message         string
	targetVersion   string
	observedVersion string
	failureMessage  string
	requeueAfter    time.Duration
}

// kubernetesUpgradePreflightは、upgradeを開始してよいかを判定するための観測値である。
// 判定は外部依存のない純粋関数で行い、controller再起動後も同じ入力から同じ結論を再計算できるようにする。
type kubernetesUpgradePreflight struct {
	// desiredVersionはTartControlPlane.spec.versionである。
	desiredVersion string
	// observedVersionはclusterから観測した現在のKubernetes versionである。空の場合は未観測を意味する。
	observedVersion string
	// controlPlaneInitializedは初回bootstrapが完了していることを示す。
	controlPlaneInitialized bool
	// workloadAPIReadyはworkload Kubernetes APIが応答していることを示す。
	workloadAPIReady bool
	// talosReachableはcontrol-plane nodeのTalos APIへ接続できることを示す。
	talosReachable bool
	// etcdQuorumHealthyは全control-plane memberのetcdがhealthyでquorumを満たすことを示す。
	etcdQuorumHealthy bool
	// machinesReadyは全desired control-plane MachineがReadyであることを示す。
	machinesReady bool
	// otherOperationInProgressは、CA rotationやscale-downなど他のlifecycle operationが進行中であることを示す。
	otherOperationInProgress bool
}

// kubernetesUpgradeActionはpreflightの結論である。
type kubernetesUpgradeAction int

const (
	// kubernetesUpgradeActionNoneは、desired versionへ既に収束しておりupgradeが不要であることを示す。
	kubernetesUpgradeActionNone kubernetesUpgradeAction = iota
	// kubernetesUpgradeActionWaitは、まだupgradeを開始できないため待機することを示す。
	kubernetesUpgradeActionWait
	// kubernetesUpgradeActionFailは、desired versionそのものが不正であり安全停止することを示す。
	kubernetesUpgradeActionFail
	// kubernetesUpgradeActionUpgradeは、upgradeを実行してよいことを示す。
	kubernetesUpgradeActionUpgrade
)

// kubernetesUpgradeDecisionはpreflightの結論と、そのままCondition/Eventへ出せる理由を保持する。
type kubernetesUpgradeDecision struct {
	action  kubernetesUpgradeAction
	reason  string
	message string
}

// evaluateKubernetesUpgradeは、観測値からupgradeを開始してよいかを決める。
// 不明な入力ではupgradeへ進まず、安全側(待機または停止)へ倒す。
func evaluateKubernetesUpgrade(preflight kubernetesUpgradePreflight) kubernetesUpgradeDecision {
	desired := talos.NormalizeKubernetesVersion(preflight.desiredVersion)
	if desired == "" {
		return kubernetesUpgradeDecision{action: kubernetesUpgradeActionFail, reason: "InvalidVersion", message: "The desired Kubernetes version is empty."}
	}
	if !preflight.controlPlaneInitialized {
		return kubernetesUpgradeDecision{action: kubernetesUpgradeActionWait, reason: "ControlPlaneNotInitialized", message: "The control plane is not initialized yet; the Kubernetes version upgrade is not started."}
	}
	observed := talos.NormalizeKubernetesVersion(preflight.observedVersion)
	if observed == desired {
		return kubernetesUpgradeDecision{action: kubernetesUpgradeActionNone, reason: "UpToDate", message: "The cluster is running the desired Kubernetes version."}
	}
	if !preflight.workloadAPIReady {
		return kubernetesUpgradeDecision{action: kubernetesUpgradeActionWait, reason: reasonWorkloadAPIUnavailable, message: "The workload Kubernetes API is not available; the Kubernetes version upgrade is not started."}
	}
	if !preflight.talosReachable {
		return kubernetesUpgradeDecision{action: kubernetesUpgradeActionWait, reason: "TalosAPIUnavailable", message: "The Talos API of the control-plane nodes is not reachable; the Kubernetes version upgrade is not started."}
	}
	if !preflight.etcdQuorumHealthy {
		return kubernetesUpgradeDecision{action: kubernetesUpgradeActionWait, reason: "EtcdQuorumUnproven", message: "The etcd quorum could not be proven healthy; the Kubernetes version upgrade is not started."}
	}
	if !preflight.machinesReady {
		return kubernetesUpgradeDecision{action: kubernetesUpgradeActionWait, reason: "MachinesNotReady", message: "Not all desired control-plane Machines are ready; the Kubernetes version upgrade is not started."}
	}
	if preflight.otherOperationInProgress {
		return kubernetesUpgradeDecision{action: kubernetesUpgradeActionWait, reason: "OperationInProgress", message: "Another control-plane lifecycle operation is in progress; the Kubernetes version upgrade is not started."}
	}
	if observed == "" {
		return kubernetesUpgradeDecision{action: kubernetesUpgradeActionWait, reason: "VersionUnobserved", message: "The current cluster Kubernetes version could not be observed; the Kubernetes version upgrade is not started."}
	}
	return kubernetesUpgradeDecision{action: kubernetesUpgradeActionUpgrade, reason: "Upgrading", message: "The cluster-wide Kubernetes version upgrade is in progress."}
}

// kubernetesUpgradeLeaseNameは、cluster単位で一意なlease名を返す。同一clusterのupgradeはこのleaseで直列化する。
func kubernetesUpgradeLeaseName(controlPlaneName string) string {
	return "tart-kubernetes-upgrade-" + controlPlaneName
}

// kubernetesUpgradeLeaseAcquirableは、指定identityがleaseを取得してよいかを判定する。
// holderが自分自身の場合(controller再起動後の再取得を含む)と、renewTimeからlease durationが経過して失効している場合だけ取得できる。
func kubernetesUpgradeLeaseAcquirable(lease *coordinationv1.Lease, identity string, now time.Time) bool {
	if identity == "" {
		return false
	}
	if lease == nil {
		return true
	}
	if lease.Spec.HolderIdentity == nil || *lease.Spec.HolderIdentity == "" || *lease.Spec.HolderIdentity == identity {
		return true
	}
	if lease.Spec.RenewTime == nil {
		return true
	}
	duration := kubernetesUpgradeLeaseDuration
	if lease.Spec.LeaseDurationSeconds != nil && *lease.Spec.LeaseDurationSeconds > 0 {
		duration = time.Duration(*lease.Spec.LeaseDurationSeconds) * time.Second
	}
	return !now.Before(lease.Spec.RenewTime.Add(duration))
}

// errKubernetesUpgradeLeaseHeldは、他のcontroller replicaが同じclusterのupgradeを実行中であることを示す。
var errKubernetesUpgradeLeaseHeld = errors.New("another controller holds the kubernetes upgrade lease")

// upgradeHolderIdentityは、このcontroller processを識別する文字列を返す。lease holderとして使う。
func (r *TartControlPlaneReconciler) upgradeHolderIdentity() string {
	if r.KubernetesUpgradeIdentity != "" {
		return r.KubernetesUpgradeIdentity
	}
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "tart-controller"
	}
	return fmt.Sprintf("%s-%d", hostname, os.Getpid())
}

func (r *TartControlPlaneReconciler) upgradeRunner() talos.KubernetesUpgradeRunner {
	if r.KubernetesUpgrade != nil {
		return r.KubernetesUpgrade
	}
	return talos.UpstreamKubernetesUpgradeRunner{}
}

// acquireKubernetesUpgradeLeaseは、resourceVersionによるatomic CASでleaseを取得する。
// 複数replicaが同時にreconcileしても、Create/Updateのconflictによって高々1つだけが成功する。
func (r *TartControlPlaneReconciler) acquireKubernetesUpgradeLease(ctx context.Context, cp *controlplanev1alpha1.TartControlPlane) (*coordinationv1.Lease, error) {
	identity := r.upgradeHolderIdentity()
	now := metav1.NewMicroTime(time.Now())
	seconds := int32(kubernetesUpgradeLeaseDuration / time.Second)

	key := client.ObjectKey{Namespace: cp.Namespace, Name: kubernetesUpgradeLeaseName(cp.Name)}
	lease := &coordinationv1.Lease{}
	err := r.Get(ctx, key, lease)
	if apierrors.IsNotFound(err) {
		created := &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:       key.Namespace,
				Name:            key.Name,
				Labels:          map[string]string{clusterv1.ClusterNameLabel: cp.Labels[clusterv1.ClusterNameLabel]},
				OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(cp, controlplanev1alpha1.GroupVersion.String(), "TartControlPlane")},
			},
			Spec: coordinationv1.LeaseSpec{
				HolderIdentity:       &identity,
				LeaseDurationSeconds: &seconds,
				AcquireTime:          &now,
				RenewTime:            &now,
			},
		}
		if createErr := r.Create(ctx, created); createErr != nil {
			if apierrors.IsAlreadyExists(createErr) || apierrors.IsConflict(createErr) {
				return nil, errKubernetesUpgradeLeaseHeld
			}
			return nil, createErr
		}
		return created, nil
	}
	if err != nil {
		return nil, err
	}
	if !kubernetesUpgradeLeaseAcquirable(lease, identity, now.Time) {
		return nil, errKubernetesUpgradeLeaseHeld
	}
	lease.Spec.HolderIdentity = &identity
	lease.Spec.LeaseDurationSeconds = &seconds
	lease.Spec.AcquireTime = &now
	lease.Spec.RenewTime = &now
	// Updateは取得済みresourceVersionを伴うため、同時に取得を試みた他replicaはconflictで失敗する。
	if updateErr := r.Update(ctx, lease); updateErr != nil {
		if apierrors.IsConflict(updateErr) {
			return nil, errKubernetesUpgradeLeaseHeld
		}
		return nil, updateErr
	}
	return lease, nil
}

// renewKubernetesUpgradeLeaseは、contextがcancelされるまでleaseのrenewTimeを更新し続ける。
// 自分がholderでなくなった場合はrenewを止め、他replicaの保持を上書きしない。
func (r *TartControlPlaneReconciler) renewKubernetesUpgradeLease(ctx context.Context, lease *coordinationv1.Lease) {
	if lease == nil {
		return
	}
	logger := logf.FromContext(ctx)
	identity := r.upgradeHolderIdentity()
	ticker := time.NewTicker(kubernetesUpgradeLeaseRenewInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		current := &coordinationv1.Lease{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: lease.Namespace, Name: lease.Name}, current); err != nil {
			logger.V(1).Info("failed to read the Kubernetes upgrade lease for renewal", "error", err.Error())
			continue
		}
		if current.Spec.HolderIdentity == nil || *current.Spec.HolderIdentity != identity {
			return
		}
		now := metav1.NewMicroTime(time.Now())
		current.Spec.RenewTime = &now
		if err := r.Update(ctx, current); err != nil {
			logger.V(1).Info("failed to renew the Kubernetes upgrade lease", "error", err.Error())
		}
	}
}

// releaseKubernetesUpgradeLeaseは、自分が保持しているleaseを解放する。解放できなくてもlease durationの経過で回収されるためlogだけ残す。
func (r *TartControlPlaneReconciler) releaseKubernetesUpgradeLease(ctx context.Context, lease *coordinationv1.Lease) {
	if lease == nil {
		return
	}
	logger := logf.FromContext(ctx)
	identity := r.upgradeHolderIdentity()
	current := &coordinationv1.Lease{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: lease.Namespace, Name: lease.Name}, current); err != nil {
		if !apierrors.IsNotFound(err) {
			logger.Error(err, "failed to read the Kubernetes upgrade lease for release")
		}
		return
	}
	if current.Spec.HolderIdentity == nil || *current.Spec.HolderIdentity != identity {
		return
	}
	empty := ""
	current.Spec.HolderIdentity = &empty
	if err := r.Update(ctx, current); err != nil && !apierrors.IsConflict(err) {
		logger.Error(err, "failed to release the Kubernetes upgrade lease")
	}
}

// kubernetesUpgradeObservationは、preflightに必要なcluster側の観測結果である。
type kubernetesUpgradeObservation struct {
	endpoint      string
	configuration []byte
}

// observeKubernetesUpgradeTargetは、upgradeの実行に使うcontrol-plane nodeのTalos接続情報とetcd healthを観測する。
func (r *TartControlPlaneReconciler) observeKubernetesUpgradeTarget(ctx context.Context, machines []clusterv1.Machine) (*kubernetesUpgradeObservation, bool) {
	observations, ok := r.observeEtcdMachineObservations(ctx, machines)
	if !ok || len(observations) == 0 {
		return nil, false
	}
	_, healthyMembers, consistent := summarizeEtcdObservations(observations, "")
	if !consistent || healthyMembers != len(observations) {
		return nil, false
	}
	first := observations[0]
	return &kubernetesUpgradeObservation{
		endpoint:      hostTalosEndpoint(first.host),
		configuration: first.config,
	}, true
}

// reconcileKubernetesUpgradeは、desired Kubernetes versionとcluster観測の差分からcluster-wide upgradeを収束させる。
// Statusにはtarget version、観測version、失敗理由だけを残し、upgrade手順のstepは保存しない。
func (r *TartControlPlaneReconciler) reconcileKubernetesUpgrade(ctx context.Context, cp *controlplanev1alpha1.TartControlPlane, cluster *clusterv1.Cluster, machines []clusterv1.Machine, bootstrapState controlPlaneBootstrapState, otherOperationInProgress bool, desiredReplicas int32) controlPlaneKubernetesUpgradeState {
	logger := logf.FromContext(ctx)
	desired := cp.Spec.Version
	preflight := kubernetesUpgradePreflight{
		desiredVersion:           desired,
		observedVersion:          cp.Status.KubernetesUpgrade.ObservedVersion,
		controlPlaneInitialized:  bootstrapState.initialized,
		workloadAPIReady:         bootstrapState.workloadReady,
		machinesReady:            desiredReplicas > 0 && countMachineCondition(machines, clusterv1.MachineReadyCondition) == desiredReplicas,
		otherOperationInProgress: otherOperationInProgress,
	}

	// 現在versionは常にclusterから観測し直す。これにより、controllerがupgrade途中で停止しても
	// 再起動後に同じdesired versionと最新の観測値から同じupgrade operationを再開できる。
	var observation *kubernetesUpgradeObservation
	if bootstrapState.initialized && bootstrapState.workloadReady {
		var ok bool
		observation, ok = r.observeKubernetesUpgradeTarget(ctx, machines)
		preflight.talosReachable = ok
		preflight.etcdQuorumHealthy = ok
		if ok {
			observeContext, cancel := context.WithTimeout(ctx, kubernetesUpgradeObserveTimeout)
			version, err := r.observeClusterKubernetesVersion(observeContext, cluster, observation)
			cancel()
			if err != nil {
				logger.V(1).Info("the cluster Kubernetes version could not be observed", "error", err.Error())
			} else {
				preflight.observedVersion = version
			}
		}
	}

	decision := evaluateKubernetesUpgrade(preflight)
	state := controlPlaneKubernetesUpgradeState{
		reason:          decision.reason,
		message:         decision.message,
		targetVersion:   cp.Status.KubernetesUpgrade.TargetVersion,
		observedVersion: preflight.observedVersion,
		failureMessage:  cp.Status.KubernetesUpgrade.FailureMessage,
	}
	switch decision.action {
	case kubernetesUpgradeActionNone:
		state.failureMessage = ""
		state.targetVersion = desired
		return state
	case kubernetesUpgradeActionFail:
		state.failureMessage = decision.message
		return state
	case kubernetesUpgradeActionWait:
		state.active = desired != "" && preflight.observedVersion != "" && talos.NormalizeKubernetesVersion(preflight.observedVersion) != talos.NormalizeKubernetesVersion(desired)
		state.requeueAfter = kubernetesUpgradeRequeue
		return state
	}

	lease, err := r.acquireKubernetesUpgradeLease(ctx, cp)
	if err != nil {
		state.active = true
		state.requeueAfter = kubernetesUpgradeRequeue
		if errors.Is(err, errKubernetesUpgradeLeaseHeld) {
			state.reason = "UpgradeInProgressElsewhere"
			state.message = "Another controller instance is running the cluster-wide Kubernetes version upgrade."
			return state
		}
		logger.Error(err, "failed to acquire the Kubernetes upgrade lease")
		state.reason = "UpgradeLeaseUnavailable"
		state.message = "The Kubernetes upgrade lease could not be acquired."
		return state
	}
	defer r.releaseKubernetesUpgradeLease(ctx, lease)
	// upgrade実行中はleaseをrenewし続け、他replicaによる同時実行を防ぐ。processが停止するとrenewも止まるため、
	// lease durationの経過後に他replicaが同じupgradeを引き継げる。
	renewContext, stopRenew := context.WithCancel(ctx)
	defer stopRenew()
	go r.renewKubernetesUpgradeLease(renewContext, lease)

	logger.Info("starting the cluster-wide Kubernetes version upgrade", "from", preflight.observedVersion, "to", desired)
	upgradeContext, cancel := context.WithTimeout(ctx, kubernetesUpgradeTimeout)
	upgradeErr := r.runKubernetesUpgrade(upgradeContext, cluster, observation, preflight.observedVersion, desired)
	cancel()

	state.active = true
	state.targetVersion = desired
	state.requeueAfter = kubernetesUpgradeRequeue
	if upgradeErr != nil {
		logger.Error(upgradeErr, "the cluster-wide Kubernetes version upgrade failed")
		state.reason = "UpgradeFailed"
		state.message = "The cluster-wide Kubernetes version upgrade did not complete; it will be retried from the observed cluster state."
		state.failureMessage = state.message
		return state
	}
	state.failureMessage = ""
	state.reason = "Upgrading"
	state.message = "The cluster-wide Kubernetes version upgrade completed; waiting for all components to report the desired version."
	return state
}

// controlPlaneEndpointAddressは、workload Kubernetes APIのendpointをhost:port形式で返す。未設定の場合は空を返す。
func controlPlaneEndpointAddress(cluster *clusterv1.Cluster) string {
	if cluster == nil || !cluster.Spec.ControlPlaneEndpoint.IsValid() {
		return ""
	}
	return cluster.Spec.ControlPlaneEndpoint.String()
}

// dialKubernetesUpgradeClientは、観測済みcontrol-plane nodeへauthenticated Talos clientを接続する。
func (r *TartControlPlaneReconciler) dialKubernetesUpgradeClient(ctx context.Context, observation *kubernetesUpgradeObservation) (*talos.Client, error) {
	if observation == nil || observation.endpoint == "" {
		return nil, errors.New("no reachable control-plane Talos endpoint is available for the Kubernetes upgrade")
	}
	connectionContext, cancel := context.WithTimeout(ctx, kubernetesUpgradeObserveTimeout)
	defer cancel()
	return talos.DialAuthenticatedFromConfiguration(connectionContext, observation.endpoint, observation.configuration)
}

// observeClusterKubernetesVersionは、clusterで動作しているKubernetes componentの最も低いversionを観測する。
func (r *TartControlPlaneReconciler) observeClusterKubernetesVersion(ctx context.Context, cluster *clusterv1.Cluster, observation *kubernetesUpgradeObservation) (string, error) {
	talosClient, err := r.dialKubernetesUpgradeClient(ctx, observation)
	if err != nil {
		return "", err
	}
	version, detectErr := r.upgradeRunner().DetectVersion(ctx, talos.KubernetesUpgradeRequest{
		Client:               talosClient,
		ToVersion:            "detect",
		ControlPlaneEndpoint: controlPlaneEndpointAddress(cluster),
	})
	if closeErr := talosClient.Close(); closeErr != nil && detectErr == nil {
		return "", closeErr
	}
	if detectErr != nil {
		return "", detectErr
	}
	return version, nil
}

// runKubernetesUpgradeは、Talos upstream実装によるcluster-wide Kubernetes upgradeを一度だけ実行する。
func (r *TartControlPlaneReconciler) runKubernetesUpgrade(ctx context.Context, cluster *clusterv1.Cluster, observation *kubernetesUpgradeObservation, from, to string) error {
	talosClient, err := r.dialKubernetesUpgradeClient(ctx, observation)
	if err != nil {
		return err
	}
	upgradeErr := r.upgradeRunner().Upgrade(ctx, talos.KubernetesUpgradeRequest{
		Client:               talosClient,
		FromVersion:          from,
		ToVersion:            to,
		ControlPlaneEndpoint: controlPlaneEndpointAddress(cluster),
	})
	if closeErr := talosClient.Close(); closeErr != nil && upgradeErr == nil {
		return closeErr
	}
	return upgradeErr
}
