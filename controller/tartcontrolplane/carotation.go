package tartcontrolplane

import (
	"context"
	"errors"
	"maps"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	"github.com/walnuts1018/cluster-api-provider-tart/adapter/talos"
	"github.com/walnuts1018/cluster-api-provider-tart/adapter/talos/certbuilder"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/controller"
	clusterdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/cluster"
	domaincontrolplane "github.com/walnuts1018/cluster-api-provider-tart/domain/controlplane"
)

// controlPlaneCARotationStateはCA rotationの現在の観測結果を保持する。program counterではなく、reconcileのたびにTalosとbundle Secretの観測から再計算した値を運ぶだけの一時変数である。
type controlPlaneCARotationState struct {
	active       bool
	reason       string
	message      string
	requeueAfter time.Duration
}

// controlPlaneCARotationObservationは単一control-plane Machineについて観測したCA rotationの進行段階である。
type controlPlaneCARotationObservation struct {
	host        *infrav1alpha1.TartHost
	endpoint    string
	usedPending bool
	stage       domaincontrolplane.CATrustStage
}

// reconcileCARotationはTartCluster.spec.caRotationRequestedGenerationで要求されたCA rotationを、Talos公式の段階的CA更新手順(accepted CA追加→issuing CA切替→旧CA削除)に沿って進める。
// 進行段階はStatusのstep番号ではなく、毎回Pending/Active bundle Secretと各control-plane Machineの実際のTalos machine configurationから再計算するため、controller再起動後も安全に継続できる。
// observeControlPlaneCARotationは、削除中でない各control-plane MachineへTalos認証接続してCA trust stageを観測する。
// 途中で観測不能なMachineがあれば、rotationを一時停止するstateをhaltStateとして返す(errは返さずnilエラーで停止する既存の挙動を維持する)。
func (r *TartControlPlaneReconciler) observeControlPlaneCARotation(ctx context.Context, machines []clusterv1.Machine, activeBundle, pendingBundle *secrets.Bundle, activeCAs, pendingCAs domaincontrolplane.CertBundle) ([]controlPlaneCARotationObservation, *controlPlaneCARotationState) {
	observations := make([]controlPlaneCARotationObservation, 0, len(machines))
	for index := range machines {
		machine := &machines[index]
		if !machine.DeletionTimestamp.IsZero() {
			continue
		}
		hostObject, _, err := r.observeMachineIdentity(ctx, machine)
		if err != nil {
			return nil, &controlPlaneCARotationState{active: true, reason: reasonMachineUnavailable, message: "A control-plane Machine could not be observed; CA rotation is paused.", requeueAfter: 30 * time.Second}
		}
		endpoint := controller.HostTalosEndpoint(hostObject)
		talosClient, usedPending, err := r.dialForRotation(ctx, endpoint, activeBundle, pendingBundle)
		if err != nil {
			return nil, &controlPlaneCARotationState{active: true, reason: "MachineUnreachable", message: "A control-plane Machine Talos API could not be reached; CA rotation is paused.", requeueAfter: 30 * time.Second}
		}
		configuration, configErr := talosClient.ActiveMachineConfiguration(ctx)
		if closeErr := talosClient.Close(); closeErr != nil {
			ctrl.LoggerFrom(ctx).Error(closeErr, "close authenticated Talos client")
		}
		if configErr != nil {
			return nil, &controlPlaneCARotationState{active: true, reason: "MachineConfigurationUnavailable", message: "A control-plane Machine active configuration could not be observed; CA rotation is paused.", requeueAfter: 30 * time.Second}
		}
		stage, stageErr := certbuilder.ObserveCATrustStage(configuration, activeCAs, pendingCAs)
		if stageErr != nil || stage == domaincontrolplane.CATrustStageUnknown {
			return nil, &controlPlaneCARotationState{active: true, reason: reasonCAConfigurationUnrecognized, message: messageCAConfigurationUnrecognized}
		}
		observations = append(observations, controlPlaneCARotationObservation{host: hostObject, endpoint: endpoint, usedPending: usedPending, stage: stage})
	}
	return observations, nil
}

func (r *TartControlPlaneReconciler) reconcileCARotation(ctx context.Context, cluster *infrav1alpha1.TartCluster, scaleDownPending bool) (controlPlaneCARotationState, error) {
	notRequested := controlPlaneCARotationState{reason: "NotRequested", message: "No CA rotation has been requested."}
	if cluster == nil || cluster.Spec.CARotationRequestedGeneration == nil {
		return notRequested, nil
	}
	if scaleDownPending {
		return controlPlaneCARotationState{active: true, reason: "ScaleDownInProgress", message: "CA rotation is paused while a control-plane scale-down is in progress.", requeueAfter: controlPlaneScaleDownRequeue}, nil
	}
	if cluster.Spec.ClusterID.IsZero() {
		// cluster identityが未確定な段階はgetTartClusterで既に停止しているため、ここでは静かに何もしない。
		return notRequested, nil
	}
	clusterID := cluster.Spec.ClusterID
	target, err := domaincontrolplane.NextGeneration(cluster.Status.ActiveSecretGeneration)
	if err != nil {
		return controlPlaneCARotationState{active: true, reason: "RotationGenerationInvalid", message: "The next CA rotation secret bundle generation is invalid."}, err
	}
	if *cluster.Spec.CARotationRequestedGeneration != target {
		if state, recovered, err := r.recoverCompletedCARotationRequest(ctx, cluster, clusterID); recovered {
			return state, err
		}
		// 要求されたgenerationが次世代と一致しない場合、「未要求」と同じ扱いにはせず、要求自体が
		// 無効であることを区別できるreasonで返す。clusterは正常稼働中のためactiveにはしない。
		if r.Recorder != nil {
			r.Recorder.Eventf(cluster, corev1.EventTypeWarning, "InvalidCARotationRequest", "The requested CA rotation generation %d does not match the next expected generation %d.", *cluster.Spec.CARotationRequestedGeneration, target)
		}
		return controlPlaneCARotationState{reason: "InvalidCARotationRequest", message: "The requested CA rotation generation does not match the next expected generation."}, nil
	}

	clusterMachines, err := r.clusterMachinesForCARotation(ctx, cluster)
	if err != nil {
		return controlPlaneCARotationState{active: true, reason: reasonMachineUnavailable, message: "The cluster Machines could not be listed for CA rotation; rotation is paused.", requeueAfter: 30 * time.Second}, err
	}
	if len(clusterMachines) == 0 {
		return controlPlaneCARotationState{active: true, reason: reasonMachineUnavailable, message: "No CAPI Machine is available for CA rotation yet; rotation is paused until the cluster Machine inventory can be observed.", requeueAfter: 30 * time.Second}, nil
	}
	// TartControlPlaneのensureMachinesはcontrol-plane Machineだけを返すため、CA trustは同じClusterのworker Machineも含めて観測・更新する。SpecのclusterNameを正本とし、復元後にlabelが欠けたMachineも対象から外さない。
	machines := clusterMachines

	activeBundle, activeCAs, err := r.observeRotationBundle(ctx, cluster, clusterID, cluster.Status.ActiveSecretGeneration, domaincontrolplane.BundleStateActive)
	if err != nil {
		return controlPlaneCARotationState{active: true, reason: "ActiveBundleUnavailable", message: "The active cluster secret bundle could not be observed for CA rotation.", requeueAfter: 30 * time.Second}, nil //nolint:nilerr
	}
	pendingBundle, pendingCAs, err := r.observeRotationBundle(ctx, cluster, clusterID, target, domaincontrolplane.BundleStatePending)
	if err != nil {
		return controlPlaneCARotationState{active: true, reason: "PendingBundleUnavailable", message: "The next-generation Pending cluster secret bundle is not available yet.", requeueAfter: 30 * time.Second}, nil //nolint:nilerr
	}

	observations, haltState := r.observeControlPlaneCARotation(ctx, machines, activeBundle, pendingBundle, activeCAs, pendingCAs)
	if haltState != nil {
		return *haltState, nil
	}
	if len(observations) == 0 {
		return controlPlaneCARotationState{active: true, reason: reasonMachineUnavailable, message: "No control-plane Machine is available yet for CA rotation.", requeueAfter: 30 * time.Second}, nil
	}

	minStage := minimumCARotationStage(observations)

	switch minStage {
	case domaincontrolplane.CATrustStageStable:
		for _, observation := range observations {
			if observation.stage != domaincontrolplane.CATrustStageStable {
				continue
			}
			if err := r.advanceCATrust(ctx, observation, activeCAs, pendingCAs, activeBundle, pendingBundle, false); err != nil {
				return controlPlaneCARotationState{active: true, reason: reasonCATrustUpdateFailed, message: "Adding the next-generation certificate authorities to a control-plane Machine failed.", requeueAfter: 15 * time.Second}, err
			}
		}
		return controlPlaneCARotationState{active: true, reason: "AddingAcceptedCA", message: "CA rotation is adding the next-generation certificate authorities as accepted CAs on every control-plane Machine.", requeueAfter: 15 * time.Second}, nil
	case domaincontrolplane.CATrustStageDualTrust:
		for _, observation := range observations {
			if observation.stage != domaincontrolplane.CATrustStageDualTrust {
				continue
			}
			if err := r.advanceCATrust(ctx, observation, activeCAs, pendingCAs, activeBundle, pendingBundle, true); err != nil {
				return controlPlaneCARotationState{active: true, reason: reasonCATrustUpdateFailed, message: "Switching the issuing certificate authority on a control-plane Machine failed.", requeueAfter: 15 * time.Second}, err
			}
		}
		return controlPlaneCARotationState{active: true, reason: "SwitchingIssuingCA", message: "CA rotation is switching the issuing certificate authority to the next generation on every control-plane Machine.", requeueAfter: 15 * time.Second}, nil
	case domaincontrolplane.CATrustStageCutover:
		for _, observation := range observations {
			if observation.stage != domaincontrolplane.CATrustStageCutover {
				continue
			}
			if err := r.finalizeCATrust(ctx, observation, activeBundle, pendingBundle, pendingCAs); err != nil {
				return controlPlaneCARotationState{active: true, reason: reasonCATrustUpdateFailed, message: "Removing the previous-generation certificate authority from a control-plane Machine failed.", requeueAfter: 15 * time.Second}, err
			}
		}
		return controlPlaneCARotationState{active: true, reason: "RemovingAcceptedCA", message: "CA rotation is removing the previous-generation certificate authority from every control-plane Machine.", requeueAfter: 15 * time.Second}, nil
	case domaincontrolplane.CATrustStageRotated:
		if err := r.promoteCARotation(ctx, cluster, clusterID, target); err != nil {
			return controlPlaneCARotationState{active: true, reason: "CARotationPromotionFailed", message: "CA rotation completed on every control-plane Machine, but promoting the active secret bundle generation failed.", requeueAfter: 15 * time.Second}, err
		}
		return controlPlaneCARotationState{reason: "Completed", message: "CA rotation completed; the active cluster secret bundle generation was promoted."}, nil
	case domaincontrolplane.CATrustStageUnknown:
		return controlPlaneCARotationState{active: true, reason: reasonCAConfigurationUnrecognized, message: messageCAConfigurationUnrecognized}, nil
	default:
		return controlPlaneCARotationState{active: true, reason: reasonCAConfigurationUnrecognized, message: messageCAConfigurationUnrecognized}, nil
	}
}

func (r *TartControlPlaneReconciler) recoverCompletedCARotationRequest(ctx context.Context, cluster *infrav1alpha1.TartCluster, clusterID clusterdomain.ClusterID) (controlPlaneCARotationState, bool, error) {
	if *cluster.Spec.CARotationRequestedGeneration != cluster.Status.ActiveSecretGeneration {
		return controlPlaneCARotationState{}, false, nil
	}
	// promotion後にcontrollerが停止するとStatusだけ更新済みでSecret labelがPendingのまま残る場合があるため、現在のgenerationのSecretが存在する場合だけpromotionを復旧しSecretが無い通常のstale requestは拒否する。
	err := r.recoverCARotationPromotion(ctx, cluster, clusterID, *cluster.Spec.CARotationRequestedGeneration)
	if err == nil {
		return controlPlaneCARotationState{reason: "Completed", message: "CA rotation completed; the active cluster secret bundle generation was promoted."}, true, nil
	}
	if !apierrors.IsNotFound(err) {
		return controlPlaneCARotationState{active: true, reason: "CARotationPromotionFailed", message: "CA rotation promotion recovery failed.", requeueAfter: 15 * time.Second}, true, err
	}
	return controlPlaneCARotationState{}, false, nil
}

func minimumCARotationStage(observations []controlPlaneCARotationObservation) domaincontrolplane.CATrustStage {
	minimum := observations[0].stage
	for _, observation := range observations[1:] {
		if observation.stage < minimum {
			minimum = observation.stage
		}
	}
	return minimum
}

func (r *TartControlPlaneReconciler) clusterMachinesForCARotation(ctx context.Context, cluster *infrav1alpha1.TartCluster) ([]clusterv1.Machine, error) {
	var allMachines clusterv1.MachineList
	if err := r.List(ctx, &allMachines, client.InNamespace(cluster.Namespace)); err != nil {
		return nil, err
	}
	machines := make([]clusterv1.Machine, 0, len(allMachines.Items))
	for index := range allMachines.Items {
		if allMachines.Items[index].Spec.ClusterName == cluster.Name {
			machines = append(machines, allMachines.Items[index])
		}
	}
	return machines, nil
}

// observeRotationBundleは指定したgenerationとstateのbundle Secretを取得し、契約を検証してrotation対象CAを取り出す。
func (r *TartControlPlaneReconciler) observeRotationBundle(ctx context.Context, cluster *infrav1alpha1.TartCluster, clusterID clusterdomain.ClusterID, generation int32, state string) (*secrets.Bundle, domaincontrolplane.CertBundle, error) {
	name, err := domaincontrolplane.BundleName(cluster.Name, clusterID, generation)
	if err != nil {
		return nil, domaincontrolplane.CertBundle{}, err
	}
	var secret corev1.Secret
	if err := r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: name}, &secret); err != nil {
		return nil, domaincontrolplane.CertBundle{}, err
	}
	contractState := state
	// label promotionとStatus更新の間でcontrollerが停止しても、同じimmutable SecretをPendingとして再生成せず既にActiveになったtargetを継続利用する。
	if state == domaincontrolplane.BundleStatePending && secret.Labels[domaincontrolplane.BundleStateLabel] == domaincontrolplane.BundleStateActive {
		contractState = domaincontrolplane.BundleStateActive
	}
	if err := domaincontrolplane.ValidateBundleSecretContract(&secret, cluster.Namespace, cluster.Name, clusterID, generation, contractState, cluster.UID); err != nil {
		return nil, domaincontrolplane.CertBundle{}, err
	}
	bundle, err := certbuilder.DecodeBundleData(secret.Data, clusterID)
	if err != nil {
		return nil, domaincontrolplane.CertBundle{}, err
	}
	cas, err := certbuilder.ExtractRotationCertificateAuthorities(bundle)
	if err != nil {
		return nil, domaincontrolplane.CertBundle{}, err
	}
	return bundle, cas, nil
}

// dialForRotationはpending bundleの信頼情報から先に接続を試み、失敗した場合はactive bundleで再試行する。どちらのgenerationがnodeの現在のissuing CAとして実際に検証できたかを呼び出し側へ返す。
func (r *TartControlPlaneReconciler) dialForRotation(ctx context.Context, endpoint string, active, pending *secrets.Bundle) (*talos.Client, bool, error) {
	// Dual trust中は旧CAでのclient certificateも通るためactiveを先に試すとpending generationへ切替済みのnodeを誤ってactiveとして記録する。pendingを先に試し実際に検証できた最も新しいgenerationを観測結果へ反映する。
	pendingClient, pendingErr := r.dialRotationBundle(ctx, endpoint, pending)
	if pendingErr == nil {
		return pendingClient, true, nil
	}
	activeClient, activeErr := r.dialRotationBundle(ctx, endpoint, active)
	if activeErr == nil {
		return activeClient, false, nil
	}
	return nil, false, errors.Join(pendingErr, activeErr)
}

func (r *TartControlPlaneReconciler) dialRotationBundle(ctx context.Context, endpoint string, bundle *secrets.Bundle) (*talos.Client, error) {
	connectionContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	talosClient, err := talos.DialAuthenticatedFromBundle(connectionContext, endpoint, bundle)
	if err != nil {
		return nil, err
	}
	// talosclient.Newはconnectionをlazyに確立するためDial成功だけではCA検証済みと判断できない。認証済みの実RPCを実行し失敗したclientを必ず閉じてfallbackする。
	if _, err := talosClient.Version(connectionContext); err != nil {
		return nil, errors.Join(err, talosClient.Close())
	}
	return talosClient, nil
}

func (r *TartControlPlaneReconciler) dialObservedRotationBundle(ctx context.Context, observation controlPlaneCARotationObservation, activeBundle, pendingBundle *secrets.Bundle) (*talos.Client, error) {
	bundle := activeBundle
	if observation.usedPending {
		bundle = pendingBundle
	}
	return r.dialRotationBundle(ctx, observation.endpoint, bundle)
}

// advanceCATrustはstage Stableのnodeへ次generationのCAをaccepted CAとして追加し(cutover=false)、stage DualTrustのnodeへissuing CAを次generationへ切替える(cutover=true)。STAGED apply後にrebootまで完了させ、次回観測で実際のtrust stateを確認する。
func (r *TartControlPlaneReconciler) advanceCATrust(ctx context.Context, observation controlPlaneCARotationObservation, active, pending domaincontrolplane.CertBundle, activeBundle, pendingBundle *secrets.Bundle, cutover bool) error {
	talosClient, err := r.dialObservedRotationBundle(ctx, observation, activeBundle, pendingBundle)
	if err != nil {
		return err
	}
	configuration, err := talosClient.ActiveMachineConfiguration(ctx)
	if err != nil {
		if closeErr := talosClient.Close(); closeErr != nil {
			ctrl.LoggerFrom(ctx).Error(closeErr, "close authenticated Talos client")
		}
		return err
	}

	issuingMachine, issuingAPI, issuingAggregator := active.Machine, active.KubernetesAPI, active.KubernetesAggregator
	acceptedMachine, acceptedAPI, acceptedAggregator := pending.Machine, pending.KubernetesAPI, pending.KubernetesAggregator
	if cutover {
		issuingMachine, issuingAPI, issuingAggregator = pending.Machine, pending.KubernetesAPI, pending.KubernetesAggregator
		acceptedMachine, acceptedAPI, acceptedAggregator = active.Machine, active.KubernetesAPI, active.KubernetesAggregator
	}

	updated, err := talos.SetMachineCertificateAuthority(configuration, issuingMachine, acceptedMachine)
	if err == nil {
		updated, err = talos.SetKubernetesAPICertificateAuthority(updated, issuingAPI, acceptedAPI)
	}
	if err == nil {
		updated, err = talos.SetKubernetesAggregatorCertificateAuthority(updated, issuingAggregator, acceptedAggregator)
	}
	if err != nil {
		if closeErr := talosClient.Close(); closeErr != nil {
			ctrl.LoggerFrom(ctx).Error(closeErr, "close authenticated Talos client")
		}
		return err
	}
	return applyCARotationConfiguration(ctx, talosClient, updated)
}

// finalizeCATrustはstage Cutoverのnodeから旧generationのCAをaccepted CAから外し、issuing CAだけを信頼する最終状態にする。Talos公式のCA rotation手順の最終段階(旧CA削除)にあたる。
func (r *TartControlPlaneReconciler) finalizeCATrust(ctx context.Context, observation controlPlaneCARotationObservation, activeBundle, pendingBundle *secrets.Bundle, pending domaincontrolplane.CertBundle) error {
	talosClient, err := r.dialObservedRotationBundle(ctx, observation, activeBundle, pendingBundle)
	if err != nil {
		return err
	}
	configuration, err := talosClient.ActiveMachineConfiguration(ctx)
	if err != nil {
		if closeErr := talosClient.Close(); closeErr != nil {
			ctrl.LoggerFrom(ctx).Error(closeErr, "close authenticated Talos client")
		}
		return err
	}
	updated, err := talos.SetMachineCertificateAuthority(configuration, pending.Machine)
	if err == nil {
		updated, err = talos.SetKubernetesAPICertificateAuthority(updated, pending.KubernetesAPI)
	}
	if err == nil {
		updated, err = talos.SetKubernetesAggregatorCertificateAuthority(updated, pending.KubernetesAggregator)
	}
	if err != nil {
		if closeErr := talosClient.Close(); closeErr != nil {
			ctrl.LoggerFrom(ctx).Error(closeErr, "close authenticated Talos client")
		}
		return err
	}
	return applyCARotationConfiguration(ctx, talosClient, updated)
}

// applyCARotationConfigurationはSTAGED configurationを反映した後、同じgenerationのtrust設定をnodeが実際に読み込むためのrebootを必ず要求してからconnectionを閉じる。
func applyCARotationConfiguration(ctx context.Context, talosClient *talos.Client, configuration []byte) error {
	applyErr := talosClient.ApplyConfiguration(ctx, configuration)
	if applyErr != nil {
		return errors.Join(applyErr, talosClient.Close())
	}
	rebootErr := talosClient.Reboot(ctx)
	return errors.Join(rebootErr, talosClient.Close())
}

// promoteCARotationはTartCluster.status.activeSecretGenerationを新generationへ進め、Pending bundle SecretのlabelをActiveへ書き換える(dataはimmutableなまま維持する)。この呼び出しの直前に全対象Machineが新generationのCAだけを信頼していることを確認済みである。
func (r *TartControlPlaneReconciler) promoteCARotation(ctx context.Context, cluster *infrav1alpha1.TartCluster, clusterID clusterdomain.ClusterID, target int32) error {
	name, err := domaincontrolplane.BundleName(cluster.Name, clusterID, target)
	if err != nil {
		return err
	}
	var secret corev1.Secret
	if err := r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: name}, &secret); err != nil {
		return err
	}
	state := secret.Labels[domaincontrolplane.BundleStateLabel]
	if err := domaincontrolplane.ValidateBundleSecretContract(&secret, cluster.Namespace, cluster.Name, clusterID, target, state, cluster.UID); err != nil {
		return err
	}
	if err := validateCARotationPromotionState(state); err != nil {
		return err
	}
	// Statusを先に進めることでStatus patch成功後にlabel patchが失敗しても次回reconcileでtarget Secretを復旧できる。Secret dataはimmutableのまま扱う。
	if cluster.Status.ActiveSecretGeneration == target {
		return r.recoverCARotationPromotion(ctx, cluster, clusterID, target)
	}
	previousGeneration := cluster.Status.ActiveSecretGeneration
	original := cluster.DeepCopy()
	cluster.Status.ActiveSecretGeneration = target
	if err := r.Status().Patch(ctx, cluster, client.MergeFrom(original)); err != nil {
		return err
	}

	if state == domaincontrolplane.BundleStateActive {
		return r.demoteCARotationSecret(ctx, cluster, clusterID, previousGeneration)
	}
	originalSecret := secret.DeepCopy()
	secret.Labels = maps.Clone(secret.Labels)
	secret.Labels[domaincontrolplane.BundleStateLabel] = domaincontrolplane.BundleStateActive
	if err := r.Patch(ctx, &secret, client.MergeFrom(originalSecret)); err != nil {
		return err
	}
	return r.demoteCARotationSecret(ctx, cluster, clusterID, previousGeneration)
}

// recoverCARotationPromotionはStatusだけが先に更新されたpromotionをSecret labelまで冪等に進める。Secretが無い場合は呼び出し側がstale requestとして扱えるようNotFoundを返す。
func (r *TartControlPlaneReconciler) recoverCARotationPromotion(ctx context.Context, cluster *infrav1alpha1.TartCluster, clusterID clusterdomain.ClusterID, generation int32) error {
	name, err := domaincontrolplane.BundleName(cluster.Name, clusterID, generation)
	if err != nil {
		return err
	}
	var secret corev1.Secret
	if err := r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: name}, &secret); err != nil {
		return err
	}
	state := secret.Labels[domaincontrolplane.BundleStateLabel]
	if err := domaincontrolplane.ValidateBundleSecretContract(&secret, cluster.Namespace, cluster.Name, clusterID, generation, state, cluster.UID); err != nil {
		return err
	}
	if err := validateCARotationPromotionState(state); err != nil {
		return err
	}
	if state == domaincontrolplane.BundleStateActive {
		return r.demoteCARotationSecret(ctx, cluster, clusterID, generation-1)
	}
	original := secret.DeepCopy()
	secret.Labels = maps.Clone(secret.Labels)
	secret.Labels[domaincontrolplane.BundleStateLabel] = domaincontrolplane.BundleStateActive
	if err := r.Patch(ctx, &secret, client.MergeFrom(original)); err != nil {
		return err
	}
	return r.demoteCARotationSecret(ctx, cluster, clusterID, generation-1)
}

func validateCARotationPromotionState(state string) error {
	if state != domaincontrolplane.BundleStatePending && state != domaincontrolplane.BundleStateActive {
		return errInvalidCARotationPromotionState
	}
	return nil
}

func (r *TartControlPlaneReconciler) demoteCARotationSecret(ctx context.Context, cluster *infrav1alpha1.TartCluster, clusterID clusterdomain.ClusterID, generation int32) error {
	if generation < 1 {
		return nil
	}
	name, err := domaincontrolplane.BundleName(cluster.Name, clusterID, generation)
	if err != nil {
		return err
	}
	var secret corev1.Secret
	if err := r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: name}, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if secret.Labels[domaincontrolplane.BundleStateLabel] != domaincontrolplane.BundleStateActive {
		return nil
	}
	if err := domaincontrolplane.ValidateBundleSecretContract(&secret, cluster.Namespace, cluster.Name, clusterID, generation, domaincontrolplane.BundleStateActive, cluster.UID); err != nil {
		return err
	}
	original := secret.DeepCopy()
	secret.Labels = maps.Clone(secret.Labels)
	secret.Labels[domaincontrolplane.BundleStateLabel] = domaincontrolplane.BundleStateRetired
	return r.Patch(ctx, &secret, client.MergeFrom(original))
}
