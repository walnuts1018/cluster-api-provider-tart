package controller

import (
	"context"
	"errors"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/controlplane"
	"github.com/walnuts1018/cluster-api-provider-tart/host"
	"github.com/walnuts1018/cluster-api-provider-tart/recovery"
)

const reprovisionRequeue = 30 * time.Second

// ensureTalosIdentityBindingは、これからTalos installationをapplyするHostに対して、そのinstallationのrecovery identityを先に確立する。
// recovery Secretはprovider管理namespace上のimmutable Secretであり、同じTalos clusterかつ同じCA generationへ属するHost間で共有する。
// 既存bindingが別のTalos clusterを指す場合は上書きしない。それは旧installationのresetを承認できる唯一の根拠であり、新しいconfigurationの到着で失ってはならない。
// 同一cluster内でCA rotationが完了して有効なCAが変わった場合だけ、現在のactive bundleが表すidentityへbindingを更新する。
func (r *TartMachineReconciler) ensureTalosIdentityBinding(ctx context.Context, machine *infrav1alpha1.TartMachine, selected *infrav1alpha1.TartHost) error {
	if selected == nil {
		return nil
	}
	if r.ManagementNamespace == "" {
		return errors.New("management namespace is not configured for Talos recovery identity")
	}
	current := selected.Status.CurrentTalosIdentityRef
	bundle, err := r.activeSecretsBundle(ctx, machine)
	if err != nil {
		if current != nil {
			// 現在のcluster identityを解決できなくても、既存bindingを失ってはならない。
			return nil
		}
		return err
	}
	material, err := recovery.MaterialFromBundle(bundle)
	if err != nil {
		return err
	}
	secret, err := recovery.BuildSecret(r.ManagementNamespace, material)
	if err != nil {
		return err
	}
	if !shouldRebindTalosIdentity(current, material.ClusterID, secret.Name) {
		return nil
	}
	if err := r.Create(ctx, secret); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	original := selected.DeepCopy()
	selected.Status.CurrentTalosIdentityRef = &infrav1alpha1.TalosIdentityReference{
		ClusterID:         material.ClusterID,
		RecoverySecretRef: infrav1alpha1.ManagementNamespaceSecretReference{Name: secret.Name},
		BoundAt:           metav1.Now(),
	}
	return r.Status().Patch(ctx, selected, client.MergeFrom(original))
}

// shouldRebindTalosIdentityは、現在のbindingをこのclusterのactive recovery identityへ更新してよいかを判定する。
// bindingがない場合は確立し、同じclusterでCA rotationにより有効なCAが変わった場合だけ更新する。
// 別clusterを指すbindingは、そのHostが保持する旧installationをresetできる唯一の根拠であるため決して上書きしない。
func shouldRebindTalosIdentity(current *infrav1alpha1.TalosIdentityReference, clusterID, secretName string) bool {
	if current == nil {
		return true
	}
	if current.ClusterID != clusterID {
		return false
	}
	return current.RecoverySecretRef.Name != secretName
}

// reconcileReprovisionは、承認されたReprovision対象のHostがまだ旧Talos installationを保持している間の分岐を処理する。
// handledがfalseの場合は、このHostに破棄すべき旧installationが残っていないため、呼び出し側が通常のprovisioning経路を続行する。
// Statusをworkflowのstep番号として使わず、毎回recovery identityの有無と旧Talos APIの到達性を観測して継続位置を決める。
func (r *TartMachineReconciler) reconcileReprovision(ctx context.Context, machine *infrav1alpha1.TartMachine, selected *infrav1alpha1.TartHost, endpoint string) (ctrl.Result, bool, error) {
	if !host.ReprovisionApproved(selected.Spec) || selected.Status.CurrentTalosIdentityRef == nil {
		return ctrl.Result{}, false, nil
	}
	identity := selected.Status.CurrentTalosIdentityRef

	material, err := r.recoveryMaterial(ctx, identity)
	if err != nil {
		result, reportErr := r.reportTalosStatus(ctx, machine,
			metav1.ConditionFalse, infrav1alpha1.ReasonRecoveryIdentityUnavailable, "The recovery identity of the previous Talos installation could not be resolved.",
			metav1.ConditionFalse, infrav1alpha1.ReasonRecoveryIdentityUnavailable, "Reprovision is stopped because the previous Talos installation cannot be authenticated.",
			infrav1alpha1.ReasonRecoveryIdentityUnavailable, "Talos version cannot be verified before the previous installation is reset.",
			infrav1alpha1.ReasonRecoveryIdentityUnavailable, "The recovery Secret for the previous Talos installation is unavailable.",
			reprovisionRequeue)
		return result, true, reportErr
	}

	node, dialErr := r.talosDialer().DialRecovery(ctx, endpoint, material)
	if dialErr != nil {
		// 旧identityで認証できない場合、resetが既に完了しているか、Hostがまだ再起動中である。
		// maintenance modeで期待したHost identityを確認できたときだけbindingを解除し、確認できなければ安全に停止する。
		return r.confirmReprovisionCompleted(ctx, machine, selected, endpoint)
	}
	defer func() {
		if closeErr := node.Close(); closeErr != nil {
			ctrl.LoggerFrom(ctx).Error(closeErr, "close recovery Talos client")
		}
	}()

	observed, observeErr := observeResetTarget(ctx, node, endpoint)
	if observeErr != nil {
		result, reportErr := r.reportTalosStatus(ctx, machine,
			metav1.ConditionFalse, infrav1alpha1.ReasonReprovisioning, "The previous Talos installation could not be observed for identity verification.",
			metav1.ConditionFalse, infrav1alpha1.ReasonReprovisioning, "Reprovision is waiting for a verifiable previous Talos installation.",
			infrav1alpha1.ReasonReprovisioning, "Talos version cannot be verified before the previous installation is reset.",
			infrav1alpha1.ReasonReprovisioning, "The previous Talos installation identity is not observable yet.",
			reprovisionRequeue)
		return result, true, reportErr
	}

	expected := recovery.ExpectedIdentityForHost(selected, identity.ClusterID, endpoint)
	if verifyErr := recovery.VerifyResetTarget(expected, observed); verifyErr != nil {
		// データを不可逆に破棄する操作であるため、identityが完全に一致しない限りResetは実行せず、requeueもせずに安全停止する。
		result, reportErr := r.reportTalosStatus(ctx, machine,
			metav1.ConditionFalse, infrav1alpha1.ReasonIdentityConflict, "The Talos endpoint identity does not match the approved Reprovision target.",
			metav1.ConditionFalse, infrav1alpha1.ReasonIdentityConflict, "Reprovision is stopped because the reset target identity could not be confirmed.",
			infrav1alpha1.ReasonIdentityConflict, "Talos version cannot be trusted for a different Host or cluster.",
			infrav1alpha1.ReasonIdentityConflict, "The previous Talos installation belongs to a different Host or cluster identity.",
			0)
		return result, true, reportErr
	}

	if resetErr := node.Reset(ctx); resetErr != nil {
		result, reportErr := r.reportTalosStatus(ctx, machine,
			metav1.ConditionFalse, infrav1alpha1.ReasonReprovisioning, "The Talos reset request was rejected by the previous installation.",
			metav1.ConditionFalse, infrav1alpha1.ReasonReprovisioning, "Reprovision is waiting for the previous Talos installation to be reset.",
			infrav1alpha1.ReasonReprovisioning, "Talos version cannot be verified before the previous installation is reset.",
			infrav1alpha1.ReasonReprovisioning, "The previous Talos installation has not been reset yet.",
			reprovisionRequeue)
		return result, true, reportErr
	}

	result, reportErr := r.reportTalosStatus(ctx, machine,
		metav1.ConditionFalse, infrav1alpha1.ReasonReprovisioning, "Talos reset was requested on the verified previous installation.",
		metav1.ConditionFalse, infrav1alpha1.ReasonReprovisioning, "Reprovision is waiting for the Host to return to maintenance mode.",
		infrav1alpha1.ReasonReprovisioning, "Talos version cannot be verified before the Host returns to maintenance mode.",
		infrav1alpha1.ReasonReprovisioning, "The Host is being reset back to maintenance mode.",
		reprovisionRequeue)
	return result, true, reportErr
}

// confirmReprovisionCompletedは、旧identityで認証できなくなったHostがmaintenance modeへ戻ったことを確認し、確認できた場合だけ旧recovery identityとのbindingを解除する。
func (r *TartMachineReconciler) confirmReprovisionCompleted(ctx context.Context, machine *infrav1alpha1.TartMachine, selected *infrav1alpha1.TartHost, endpoint string) (ctrl.Result, bool, error) {
	maintenance, err := r.talosDialer().DialMaintenance(ctx, endpoint)
	if err != nil {
		result, reportErr := r.reportTalosStatus(ctx, machine,
			metav1.ConditionFalse, infrav1alpha1.ReasonReprovisioning, "Neither the previous Talos installation nor the maintenance API is reachable.",
			metav1.ConditionFalse, infrav1alpha1.ReasonReprovisioning, "Reprovision is waiting for the Host to return to maintenance mode.",
			infrav1alpha1.ReasonReprovisioning, "Talos version cannot be verified before the Host returns to maintenance mode.",
			infrav1alpha1.ReasonReprovisioning, "The Host has not returned to maintenance mode yet.",
			reprovisionRequeue)
		return result, true, reportErr
	}
	inventory, inventoryErr := maintenance.Inventory(ctx)
	if closeErr := maintenance.Close(); closeErr != nil {
		ctrl.LoggerFrom(ctx).Error(closeErr, "close maintenance Talos client")
	}
	if inventoryErr != nil || !inventory.HasMAC(selected.Spec.MACAddress) {
		reason := infrav1alpha1.ReasonReprovisioning
		message := "The maintenance identity of the reset Host could not be observed."
		if inventoryErr == nil {
			reason = infrav1alpha1.ReasonIdentityConflict
			message = "The maintenance endpoint reports a different Host identity than the Reprovision target."
		}
		result, reportErr := r.reportTalosStatus(ctx, machine,
			metav1.ConditionFalse, reason, message,
			metav1.ConditionFalse, reason, "Reprovision is stopped until the reset Host identity is confirmed.",
			reason, "Talos version cannot be verified before the Host identity is confirmed.",
			reason, message,
			reprovisionRequeue)
		return result, true, reportErr
	}

	original := selected.DeepCopy()
	selected.Status.CurrentTalosIdentityRef = nil
	if err := r.Status().Patch(ctx, selected, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, true, err
	}
	// bindingを解除できたHostは破棄すべき旧installationを持たないため、同じreconcileで通常のfresh provisioning経路へ進む。
	return ctrl.Result{}, false, nil
}

func (r *TartMachineReconciler) recoveryMaterial(ctx context.Context, identity *infrav1alpha1.TalosIdentityReference) (recovery.Material, error) {
	if r.ManagementNamespace == "" {
		return recovery.Material{}, errors.New("management namespace is not configured for Talos recovery identity")
	}
	if identity == nil || identity.RecoverySecretRef.Name == "" {
		return recovery.Material{}, recovery.ErrSecretInvalid
	}
	secret := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: r.ManagementNamespace, Name: identity.RecoverySecretRef.Name}, secret); err != nil {
		return recovery.Material{}, err
	}
	return recovery.DecodeSecret(secret, identity.ClusterID)
}

// activeSecretsBundleはこのMachineが所属するTartClusterの現在activeなcluster secret bundleを解決する。
// Bootstrap Providerがmachine configurationを生成するときと同じbundleを参照し、recovery identityの正本を一つに保つ。
func (r *TartMachineReconciler) activeSecretsBundle(ctx context.Context, machine *infrav1alpha1.TartMachine) (*secrets.Bundle, error) {
	capiMachine, err := findCAPIMachineForInfrastructure(ctx, r.Client, machine)
	if err != nil {
		return nil, err
	}
	cluster := &clusterv1.Cluster{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: capiMachine.Namespace, Name: capiMachine.Spec.ClusterName}, cluster); err != nil {
		return nil, err
	}
	clusterRef := cluster.Spec.InfrastructureRef
	if clusterRef.APIGroup != infrav1alpha1.GroupVersion.Group || clusterRef.Kind != tartClusterKind || clusterRef.Name == "" {
		return nil, errBootstrapDataUnavailable
	}
	providerCluster := &infrav1alpha1.TartCluster{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: clusterRef.Name}, providerCluster); err != nil {
		return nil, err
	}
	if providerCluster.Status.ActiveSecretGeneration < 1 {
		return nil, errBootstrapDataUnavailable
	}
	clusterID, err := parseClusterID(providerCluster.Spec.ClusterID)
	if err != nil {
		return nil, err
	}
	name, err := controlplane.BundleName(providerCluster.Name, clusterID, providerCluster.Status.ActiveSecretGeneration)
	if err != nil {
		return nil, err
	}
	secret := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: providerCluster.Namespace, Name: name}, secret); err != nil {
		return nil, err
	}
	if err := controlplane.ValidateBundleSecretContract(secret, providerCluster.Namespace, providerCluster.Name, clusterID, providerCluster.Status.ActiveSecretGeneration, controlplane.BundleStateActive, providerCluster.UID); err != nil {
		return nil, err
	}
	return controlplane.DecodeBundleData(secret.Data, clusterID)
}

func observeResetTarget(ctx context.Context, node TalosNode, endpoint string) (recovery.ObservedIdentity, error) {
	configuration, err := node.ActiveMachineConfiguration(ctx)
	if err != nil {
		return recovery.ObservedIdentity{}, err
	}
	observedClusterID, err := recovery.ObservedClusterID(configuration)
	if err != nil {
		return recovery.ObservedIdentity{}, err
	}
	inventory, err := node.Inventory(ctx)
	if err != nil {
		return recovery.ObservedIdentity{}, err
	}
	return recovery.ObservedIdentity{
		ClusterID:    observedClusterID,
		MACAddresses: inventory.MACAddresses,
		SystemUUID:   inventory.SystemUUID.String(),
		Endpoint:     endpoint,
	}, nil
}
