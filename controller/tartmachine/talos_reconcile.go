package tartmachine

import (
	"context"
	"errors"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/walnuts1018/cluster-api-provider-tart/adapter/talos"
	"github.com/walnuts1018/cluster-api-provider-tart/adapter/talos/configbuilder"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/controller"
	"github.com/walnuts1018/cluster-api-provider-tart/controller/runtimeextension"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/host"
	hostusecase "github.com/walnuts1018/cluster-api-provider-tart/usecase/host"
	machineusecase "github.com/walnuts1018/cluster-api-provider-tart/usecase/machine"
)

func (r *TartMachineReconciler) reconcileTalos(ctx context.Context, machine *infrav1alpha1.TartMachine, selected *infrav1alpha1.TartHost, configuration []byte) (ctrl.Result, error) {
	endpoint := controller.HostTalosEndpoint(selected)
	if endpoint == "" {
		return r.reportTalosStatus(ctx, machine,
			metav1.ConditionFalse, "EndpointUnavailable", "The Host has no reachable Talos maintenance endpoint.",
			metav1.ConditionFalse, "EndpointUnavailable", "Talos installation has not started because the Host endpoint is not observed.",
			"EndpointUnavailable", "Talos version cannot be verified before the Host is reachable.",
			"EndpointUnavailable", "The Host Talos endpoint is not available yet.",
			talosRequeue)
	}
	// 承認済みReprovisionでHostが旧Talos installationを保持している間は、まずそのinstallationを検証してresetする。
	// 旧installationは新しいconfigurationのCAでは認証できないため、この分岐を経ずにfresh provisioningへ進むことはない。
	if result, handled, err := r.reconcileReprovision(ctx, machine, selected, endpoint); handled {
		return result, err
	}
	// installationのrecovery identityは、configurationをHostへ渡すより前に確立する。
	// Machine削除の瞬間にSecretを退避する設計にせず、Hostがそのinstallationを保持する間ずっと参照できるようにする。
	if err := r.ensureTalosIdentityBinding(ctx, machine, selected); err != nil {
		return r.reportTalosStatus(ctx, machine,
			metav1.ConditionFalse, infrav1alpha1.ReasonRecoveryIdentityUnavailable, "The Talos recovery identity for this installation could not be established.",
			metav1.ConditionFalse, infrav1alpha1.ReasonRecoveryIdentityUnavailable, "Talos installation is stopped until its recovery identity is persisted.",
			infrav1alpha1.ReasonRecoveryIdentityUnavailable, "Talos version cannot be verified before the recovery identity is persisted.",
			infrav1alpha1.ReasonRecoveryIdentityUnavailable, "The Talos recovery Secret could not be established for this Host.",
			talosRequeue)
	}
	if result, handled, err := r.reconcileAuthenticatedTalos(ctx, machine, endpoint, configuration); handled {
		return result, err
	}
	return r.reconcileMaintenanceTalos(ctx, machine, selected, endpoint, configuration)
}

func (r *TartMachineReconciler) reconcileAuthenticatedTalos(ctx context.Context, machine *infrav1alpha1.TartMachine, endpoint string, configuration []byte) (ctrl.Result, bool, error) {
	connectionContext, cancel := context.WithTimeout(ctx, talosReconcileTimeout)
	authenticated, authErr := talos.DialAuthenticatedFromConfiguration(connectionContext, endpoint, configuration)
	cancel()
	if authErr != nil {
		if errors.Is(authErr, talos.ErrTalosConfigurationInvalid) || errors.Is(authErr, talos.ErrEndpointEmpty) {
			result, err := r.reportTalosStatus(ctx, machine,
				metav1.ConditionFalse, "TalosCredentialInvalid", "The Talos machine configuration cannot be used to establish an authenticated connection.",
				metav1.ConditionFalse, "TalosCredentialInvalid", "Talos installation is stopped until the machine configuration is valid.",
				"TalosCredentialInvalid", "The desired Talos credentials cannot be verified.",
				"TalosCredentialInvalid", "The Talos machine configuration is invalid for authenticated access.",
				talosRequeue)
			return result, true, err
		}
		ctrl.LoggerFrom(ctx).Info("authenticated Talos dial failed; falling back to maintenance mode observation", "error", authErr.Error(), "endpoint", endpoint)
		return ctrl.Result{}, false, nil
	}

	versionContext, versionCancel := context.WithTimeout(ctx, talosReconcileTimeout)
	version, versionErr := authenticated.Version(versionContext)
	versionCancel()
	if versionErr != nil {
		if closeErr := authenticated.Close(); closeErr != nil {
			ctrl.LoggerFrom(ctx).Error(closeErr, "close authenticated Talos client")
		}
		// grpc.Dialはlazy connectionのため、CAの不一致(nodeがまだmaintenance modeで
		// この設定を適用されていない場合など)はDialAuthenticatedFromConfiguration自体ではなく、
		// 最初のRPCであるVersion()の呼び出し時にcodes.Unavailableとして顕在化する。この場合は
		// authErrと同様にmaintenance mode観測へfallbackしなければ、node未設定のまま
		// 恒久的にTalosUnreachableへ張り付いてしまう。
		if status.Code(versionErr) == codes.Unavailable {
			ctrl.LoggerFrom(ctx).Info("authenticated Talos Version() call unavailable; falling back to maintenance mode observation", "error", versionErr.Error(), "endpoint", endpoint)
			return ctrl.Result{}, false, nil
		}
		ctrl.LoggerFrom(ctx).Info("authenticated Talos Version() call failed", "error", versionErr.Error(), "endpoint", endpoint)
		result, err := r.reportTalosStatus(ctx, machine,
			metav1.ConditionFalse, "TalosUnreachable", "The authenticated Talos API could not be queried.",
			metav1.ConditionFalse, "TalosUnreachable", "Talos provisioning has not been confirmed.",
			"TalosUnreachable", "The desired Talos version cannot be verified.",
			"TalosUnreachable", "The authenticated Talos API is not reachable.",
			talosRequeue)
		return result, true, err
	}

	schematicContext, schematicCancel := context.WithTimeout(ctx, talosReconcileTimeout)
	observedSchematicID, schematicErr := authenticated.SchematicID(schematicContext)
	schematicCancel()
	if schematicErr != nil {
		if closeErr := authenticated.Close(); closeErr != nil {
			ctrl.LoggerFrom(ctx).Error(closeErr, "close authenticated Talos client")
		}
		result, err := r.reportTalosStatusWithVersion(ctx, machine, version.Tag, "",
			metav1.ConditionTrue, "TalosReachable", "The authenticated Talos API is reachable.",
			metav1.ConditionTrue, "Provisioned", "Talos installation has completed and the node is running.",
			metav1.ConditionFalse, "SchematicUnavailable", "The Talos schematic identity could not be observed.",
			metav1.ConditionFalse, "SchematicUnavailable", "The desired Talos image cannot be verified without its schematic identity.",
			talosRequeue)
		return result, true, err
	}

	previousUpToDate := meta.FindStatusCondition(machine.Status.Conditions, infrav1alpha1.TartMachineTalosUpToDateCondition)
	imageUpgradePending := wasImageUpgradePending(previousUpToDate)
	if machine.Spec.Image.Version != "" && version.Tag == machine.Spec.Image.Version && observedSchematicID == machine.Spec.Image.SchematicID {
		return r.reconcileConvergedTalos(ctx, machine, authenticated, configuration, version.Tag, observedSchematicID, imageUpgradePending)
	}

	mismatchReason := "VersionMismatch"
	mismatchMessage := "The observed Talos version does not match the desired version."
	readyMessage := "The Host is running Talos, but not the desired version or schematic."
	if version.Tag == machine.Spec.Image.Version && observedSchematicID != machine.Spec.Image.SchematicID {
		mismatchReason = "SchematicMismatch"
		mismatchMessage = "The observed Talos schematic does not match the desired schematic."
		readyMessage = "The Host is running the desired Talos version, but not the desired schematic."
	}
	if previousUpToDate != nil && previousUpToDate.Status == metav1.ConditionFalse && previousUpToDate.Reason == infrav1alpha1.ReasonRolledBack && previousUpToDate.ObservedGeneration == machine.Generation {
		if closeErr := authenticated.Close(); closeErr != nil {
			ctrl.LoggerFrom(ctx).Error(closeErr, "close authenticated Talos client")
		}
		result, err := r.reportTalosStatusWithVersion(ctx, machine, version.Tag, observedSchematicID,
			metav1.ConditionTrue, "TalosReachable", "The authenticated Talos API is reachable.",
			metav1.ConditionTrue, "Provisioned", "Talos installation has completed and the node is running.",
			metav1.ConditionFalse, infrav1alpha1.ReasonRolledBack, "The previously observed desired Talos image rolled back; a new Machine generation is required before another upgrade is attempted.",
			metav1.ConditionFalse, infrav1alpha1.ReasonRolledBack, "Automatic image upgrades remain stopped after an observed Talos image rollback.",
			0)
		return result, true, err
	}
	wasUpToDate := previousUpToDate != nil && previousUpToDate.Status == metav1.ConditionTrue
	if wasUpToDate && machine.Status.TalosVersion == machine.Spec.Image.Version && machine.Status.TalosSchematicID == machine.Spec.Image.SchematicID {
		// 一度desired imageへ到達した後にrollbackした場合は、自動復旧を試みずfail-closedで停止する。
		// この分岐だけはapplyTalosUpgradeを呼ばない。
		mismatchMessage = "The previously observed Talos image is no longer running; automatic rollback recovery is stopped."
		readyMessage = "The Host no longer reports the previously observed desired Talos image."
		if closeErr := authenticated.Close(); closeErr != nil {
			ctrl.LoggerFrom(ctx).Error(closeErr, "close authenticated Talos client")
		}
		result, err := r.reportTalosStatusWithVersion(ctx, machine, version.Tag, observedSchematicID,
			metav1.ConditionTrue, "TalosReachable", "The authenticated Talos API is reachable.",
			metav1.ConditionTrue, "Provisioned", "Talos installation has completed and the node is running.",
			metav1.ConditionFalse, infrav1alpha1.ReasonRolledBack, mismatchMessage,
			metav1.ConditionFalse, infrav1alpha1.ReasonRolledBack, readyMessage,
			0)
		return result, true, err
	}

	// version/schematicがdesiredと一致しない場合、in-place updateを試みる。CAPI coreのRuntimeSDK
	// ExtensionConfig経由のUpdateMachine hookはKubeadmControlPlaneの内部実装専用であり、独自の
	// TartControlPlaneを持つこのproviderでは決して呼び出されない。そのためcontroller/runtimeextension
	// が実装済みの安全なstaged apply+reboot engine(etcd quorum gate、drain policy含む)を
	// このreconcile loopから直接呼び出す。
	outcome := r.applyTalosUpgrade(ctx, machine, version.Tag, authenticated)
	if closeErr := authenticated.Close(); closeErr != nil {
		ctrl.LoggerFrom(ctx).Error(closeErr, "close authenticated Talos client")
	}
	if outcome.FailureMessage != "" {
		result, err := r.reportTalosStatusWithVersion(ctx, machine, version.Tag, observedSchematicID,
			metav1.ConditionTrue, "TalosReachable", "The authenticated Talos API is reachable.",
			metav1.ConditionTrue, "Provisioned", "Talos installation has completed and the node is running.",
			metav1.ConditionFalse, mismatchReason, outcome.FailureMessage,
			metav1.ConditionFalse, mismatchReason, readyMessage,
			0)
		return result, true, err
	}
	progressMessage := outcome.RetryMessage
	if progressMessage == "" {
		progressMessage = mismatchMessage
	}
	result, err := r.reportTalosStatusWithVersion(ctx, machine, version.Tag, observedSchematicID,
		metav1.ConditionTrue, "TalosReachable", "The authenticated Talos API is reachable.",
		metav1.ConditionTrue, "Provisioned", "Talos installation has completed and the node is running.",
		metav1.ConditionFalse, mismatchReason, progressMessage,
		metav1.ConditionFalse, mismatchReason, readyMessage,
		talosRequeue)
	return result, true, err
}

func (r *TartMachineReconciler) reconcileConvergedTalos(ctx context.Context, machine *infrav1alpha1.TartMachine, authenticated *talos.Client, configuration []byte, version, schematicID string, imageUpgradePending bool) (ctrl.Result, bool, error) {
	if result, handled, err := r.finalizeObservedImageUpgrade(ctx, machine, authenticated, version, schematicID, imageUpgradePending); handled {
		return result, true, err
	}
	configurationOutcome := r.reconcileMachineConfiguration(ctx, machine, authenticated, configuration)
	if closeErr := authenticated.Close(); closeErr != nil {
		ctrl.LoggerFrom(ctx).Error(closeErr, "close authenticated Talos client")
	}
	if configurationOutcome.FailureMessage != "" {
		result, err := r.reportTalosStatusWithVersion(ctx, machine, version, schematicID,
			metav1.ConditionTrue, "TalosReachable", "The authenticated Talos API is reachable.",
			metav1.ConditionTrue, "Provisioned", "Talos installation has completed and the node is running.",
			metav1.ConditionTrue, "UpToDate", "The observed Talos version and schematic match the desired image.",
			metav1.ConditionFalse, "ConfigurationUpdateFailed", configurationOutcome.FailureMessage,
			0)
		return result, true, err
	}
	if configurationOutcome.RetryMessage != "" {
		result, err := r.reportTalosStatusWithVersion(ctx, machine, version, schematicID,
			metav1.ConditionTrue, "TalosReachable", "The authenticated Talos API is reachable.",
			metav1.ConditionTrue, "Provisioned", "Talos installation has completed and the node is running.",
			metav1.ConditionTrue, "UpToDate", "The observed Talos version and schematic match the desired image.",
			metav1.ConditionFalse, "ConfigurationUpdatePending", configurationOutcome.RetryMessage,
			talosRequeue)
		return result, true, err
	}
	result, err := r.reportTalosStatusWithVersion(ctx, machine, version, schematicID,
		metav1.ConditionTrue, "TalosReachable", "The authenticated Talos API is reachable.",
		metav1.ConditionTrue, "Provisioned", "Talos installation has completed and the node is running.",
		metav1.ConditionTrue, "UpToDate", "The observed Talos version and schematic match the desired image.",
		metav1.ConditionTrue, "Ready", "The Host is running the desired Talos version and schematic.",
		0)
	return result, true, err
}

func wasImageUpgradePending(condition *metav1.Condition) bool {
	return condition != nil && condition.Status == metav1.ConditionFalse && (condition.Reason == "VersionMismatch" || condition.Reason == "SchematicMismatch")
}

func (r *TartMachineReconciler) finalizeObservedImageUpgrade(ctx context.Context, machine *infrav1alpha1.TartMachine, authenticated *talos.Client, version, schematicID string, pending bool) (ctrl.Result, bool, error) {
	if !pending {
		return ctrl.Result{}, false, nil
	}
	clusterMachine, err := controller.FindCAPIMachineForInfrastructure(ctx, r.Client, machine)
	if err != nil {
		closeTalosClient(ctx, authenticated)
		result, reportErr := r.reportTalosStatusWithVersion(ctx, machine, version, schematicID,
			metav1.ConditionTrue, "TalosReachable", "The authenticated Talos API is reachable.",
			metav1.ConditionTrue, "Provisioned", "Talos installation has completed and the node is running.",
			metav1.ConditionTrue, "UpToDate", "The observed Talos version and schematic match the desired image.",
			metav1.ConditionFalse, "NodeRecoveryUnavailable", "The owning CAPI Machine could not be observed while finalizing the Talos image update.",
			talosRequeue)
		return result, true, reportErr
	}
	if ready, message := runtimeextension.FinalizeImageUpgrade(ctx, r.Client, clusterMachine, machine.Spec.ProviderID.String()); !ready {
		closeTalosClient(ctx, authenticated)
		result, reportErr := r.reportTalosStatusWithVersion(ctx, machine, version, schematicID,
			metav1.ConditionTrue, "TalosReachable", "The authenticated Talos API is reachable.",
			metav1.ConditionTrue, "Provisioned", "Talos installation has completed and the node is running.",
			metav1.ConditionTrue, "UpToDate", "The observed Talos version and schematic match the desired image.",
			metav1.ConditionFalse, "NodeRecoveryPending", message,
			talosRequeue)
		return result, true, reportErr
	}
	return ctrl.Result{}, false, nil
}

func closeTalosClient(ctx context.Context, talosClient *talos.Client) {
	if err := talosClient.Close(); err != nil {
		ctrl.LoggerFrom(ctx).Error(err, "close authenticated Talos client")
	}
}

// applyTalosUpgradeは、観測したTalos version/schematicのmismatchを実際のOS image upgrade(Talosの
// Upgrade RPC)でin-placeに解消しようと試みる。安全条件(control planeのetcd quorum、workload Podの
// drain policy)の評価はcontroller/runtimeextensionが実装済みのPerformImageUpgradeへ委譲する
// (これはversionが既にdesiredへ到達済みの場合のmachine configuration差分適用とは別の経路であり、
// そちらが使うApplyConfigurationUpdate/MachineConfigurationUpdateはここでは使わない)。
func (r *TartMachineReconciler) applyTalosUpgrade(ctx context.Context, machine *infrav1alpha1.TartMachine, observedVersion string, authenticated *talos.Client) runtimeextension.ConfigurationUpdateOutcome {
	if err := talos.ValidateUpgrade(observedVersion, machine.Spec.Image.Version); err != nil {
		return runtimeextension.ConfigurationUpdateOutcome{FailureMessage: "The requested Talos version transition is not supported; the in-place update is stopped."}
	}
	image, err := talos.InstallerImage(machine.Spec.Image.Version, machine.Spec.Image.SchematicID)
	if err != nil {
		return runtimeextension.ConfigurationUpdateOutcome{FailureMessage: "The desired Talos installer image is invalid; the in-place update is stopped."}
	}
	clusterMachine, err := controller.FindCAPIMachineForInfrastructure(ctx, r.Client, machine)
	if err != nil {
		return runtimeextension.ConfigurationUpdateOutcome{RetryMessage: "The owning CAPI Machine could not be observed while the Talos in-place update is being prepared."}
	}
	// PerformImageUpgradeが呼ぶUpgrade() RPCはinstaller imageのpullとdisk書き込みが完了するまで
	// streamを読み切る長時間実行の呼び出しであり(imageのpullも含めtalosImageUpgradeTimeout=8分)、それに加えて
	// etcd quorum gate/drain policyの評価時間も見込む必要がある。この呼び出し元にも明示的な
	// boundを与えないと、外部Talos APIが応答しない場合にreconcile workerが無期限に停止しうる。
	upgradeContext, cancel := context.WithTimeout(ctx, 9*time.Minute)
	defer cancel()
	return runtimeextension.PerformImageUpgrade(upgradeContext, r.Client, clusterMachine, machine.Spec.ProviderID.String(), image, authenticated)
}

func (r *TartMachineReconciler) reconcileMaintenanceTalos(ctx context.Context, machine *infrav1alpha1.TartMachine, selected *infrav1alpha1.TartHost, endpoint string, configuration []byte) (ctrl.Result, error) {
	if machineusecase.IsProvisioned(machine) {
		return r.reportTalosStatus(ctx, machine,
			metav1.ConditionFalse, "TalosUnreachable", "The authenticated Talos API is not reachable.",
			metav1.ConditionTrue, "Provisioned", "Talos installation was previously observed.",
			"TalosUnreachable", "The desired Talos version cannot be verified.",
			"TalosUnreachable", "The provisioned Host is temporarily unreachable.",
			talosRequeue)
	}

	maintenance, maintenanceErr := talos.DialMaintenance(ctx, endpoint)
	if maintenanceErr != nil {
		return r.reportTalosStatus(ctx, machine,
			metav1.ConditionFalse, "MaintenanceUnavailable", "The Talos maintenance API is unavailable.",
			metav1.ConditionFalse, "MaintenanceUnavailable", "Talos installation is waiting for a reachable maintenance API.",
			"MaintenanceUnavailable", "Talos version cannot be verified before installation.",
			"MaintenanceUnavailable", "The Talos maintenance API is not reachable yet.",
			talosRequeue)
	}

	identity, identityErr := maintenance.Inventory(ctx)
	if identityErr != nil {
		if closeErr := maintenance.Close(); closeErr != nil {
			ctrl.LoggerFrom(ctx).Error(closeErr, "close maintenance Talos client")
		}
		return r.reportTalosStatus(ctx, machine,
			metav1.ConditionFalse, "MaintenanceUnavailable", "The Talos maintenance identity could not be observed.",
			metav1.ConditionFalse, "MaintenanceUnavailable", "Talos installation is waiting for verified Host identity.",
			"MaintenanceUnavailable", "Talos version cannot be verified before installation.",
			"MaintenanceUnavailable", "The Talos maintenance inventory is not available.",
			talosRequeue)
	}
	if !identity.HasMAC(selected.Spec.MACAddress) {
		if closeErr := maintenance.Close(); closeErr != nil {
			ctrl.LoggerFrom(ctx).Error(closeErr, "close maintenance Talos client")
		}
		return r.reportTalosStatus(ctx, machine,
			metav1.ConditionFalse, infrav1alpha1.ReasonIdentityConflict, "The Talos endpoint MAC address does not match the claimed Host.",
			metav1.ConditionFalse, infrav1alpha1.ReasonIdentityConflict, "Talos configuration apply is stopped until Host identity matches.",
			infrav1alpha1.ReasonIdentityConflict, "Talos version cannot be trusted for a different Host.",
			infrav1alpha1.ReasonIdentityConflict, "The Talos endpoint belongs to a different Host identity.",
			0)
	}

	effectiveConfiguration, err := talos.SetInstallerImage(configuration, machine.Spec.Image.Version, machine.Spec.Image.SchematicID)
	if err != nil {
		return r.reportMaintenanceConfigurationError(ctx, machine, maintenance, "ConfigurationInvalid", "The Talos machine configuration could not be prepared for the desired installer image.", "The desired Talos installer image could not be applied to the machine configuration.")
	}
	effectiveConfiguration, err = talos.SetProviderID(effectiveConfiguration, machine.Spec.ProviderID.String())
	if err != nil {
		reason := "ConfigurationInvalid"
		message := "The Talos machine configuration could not be prepared with the allocated ProviderID."
		if errors.Is(err, talos.ErrProviderIDConflict) {
			reason = "ConfigurationConflict"
			message = "The Talos machine configuration contains a ProviderID that conflicts with the allocated Host."
		}
		return r.reportMaintenanceConfigurationError(ctx, machine, maintenance, reason, message, message)
	}
	if err := configbuilder.ValidateMachineConfiguration(effectiveConfiguration); err != nil {
		return r.reportMaintenanceConfigurationError(ctx, machine, maintenance, "ConfigurationInvalid", "The complete Talos machine configuration failed client-side validation.", "The complete Talos machine configuration is invalid.")
	}
	// maintenance mode(未installのnode)ではSTAGED modeはpersisted configを書くだけでSetConfigを
	// 呼ばないため、boot sequenceがconfig完了を検知できず永久にmaintenance modeへ留まる。
	// NO_REBOOT(AUTO) modeはpersisted configに加えてSetConfigも呼ぶため、Talos自身のmaintenance
	// mode boot sequenceがconfigの完了を検知し、自動でinstallとrebootへ進む。
	if err := maintenance.ApplyConfigurationNoReboot(ctx, effectiveConfiguration); err != nil {
		return r.reportMaintenanceConfigurationError(ctx, machine, maintenance, "ConfigurationApplyFailed", "The complete Talos machine configuration could not be applied.", "The Talos maintenance API rejected the machine configuration.")
	}
	if closeErr := maintenance.Close(); closeErr != nil {
		ctrl.LoggerFrom(ctx).Error(closeErr, "close maintenance Talos client")
	}
	return r.reportTalosStatus(ctx, machine,
		metav1.ConditionTrue, "MaintenanceReachable", "The Talos maintenance API accepted the machine configuration.",
		metav1.ConditionFalse, "Provisioning", "Talos is installing the machine configuration and will reboot.",
		"Provisioning", "The authenticated Talos version will be checked after reboot.",
		"Provisioning", "Talos installation is in progress.",
		talosRequeue)
}

func (r *TartMachineReconciler) reportMaintenanceConfigurationError(ctx context.Context, machine *infrav1alpha1.TartMachine, maintenance *talos.Client, reason, message, readyMessage string) (ctrl.Result, error) {
	if closeErr := maintenance.Close(); closeErr != nil {
		ctrl.LoggerFrom(ctx).Error(closeErr, "close maintenance Talos client")
	}
	return r.reportTalosStatus(ctx, machine,
		metav1.ConditionFalse, reason, message,
		metav1.ConditionFalse, reason, "Talos installation has not been confirmed.",
		reason, "Talos version cannot be verified before installation.",
		reason, readyMessage,
		talosRequeue)
}

func (r *TartMachineReconciler) reportTalosStatus(ctx context.Context, machine *infrav1alpha1.TartMachine,
	reachableStatus metav1.ConditionStatus, reachableReason, reachableMessage string,
	provisionedStatus metav1.ConditionStatus, provisionedReason, provisionedMessage string,
	upToDateReason, upToDateMessage, readyReason, readyMessage string,
	requeueAfter time.Duration,
) (ctrl.Result, error) {
	return r.reportTalosStatusWithVersion(ctx, machine, "", "", reachableStatus, reachableReason, reachableMessage, provisionedStatus, provisionedReason, provisionedMessage, metav1.ConditionFalse, upToDateReason, upToDateMessage, metav1.ConditionFalse, readyReason, readyMessage, requeueAfter)
}

func (r *TartMachineReconciler) reportTalosStatusWithVersion(ctx context.Context, machine *infrav1alpha1.TartMachine, talosVersion string,
	talosSchematicID string,
	reachableStatus metav1.ConditionStatus, reachableReason, reachableMessage string,
	provisionedStatus metav1.ConditionStatus, provisionedReason, provisionedMessage string,
	upToDateStatus metav1.ConditionStatus, upToDateReason, upToDateMessage string,
	readyStatus metav1.ConditionStatus, readyReason, readyMessage string,
	requeueAfter time.Duration,
) (ctrl.Result, error) {
	original := machine.DeepCopy()
	if talosVersion != "" {
		machine.Status.TalosVersion = talosVersion
	}
	if talosSchematicID != "" {
		machine.Status.TalosSchematicID = talosSchematicID
	}
	if provisionedStatus == metav1.ConditionTrue {
		machine.Status.Initialization.Provisioned = new(true)
	}
	controller.SetCondition(&machine.Status.Conditions, infrav1alpha1.TartMachineTalosReachableCondition, reachableStatus, reachableReason, reachableMessage, machine.Generation)
	controller.SetCondition(&machine.Status.Conditions, infrav1alpha1.TartMachineProvisionedCondition, provisionedStatus, provisionedReason, provisionedMessage, machine.Generation)
	controller.SetCondition(&machine.Status.Conditions, infrav1alpha1.TartMachineTalosUpToDateCondition, upToDateStatus, upToDateReason, upToDateMessage, machine.Generation)
	controller.SetCondition(&machine.Status.Conditions, infrav1alpha1.TartMachineReadyCondition, readyStatus, readyReason, readyMessage, machine.Generation)
	machine.Status.ObservedGeneration = machine.Generation
	if err := r.Status().Patch(ctx, machine, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

func (r *TartMachineReconciler) observedOrSelectedHost(ctx context.Context, machine *infrav1alpha1.TartMachine, hosts []infrav1alpha1.TartHost) (*infrav1alpha1.TartHost, error) {
	capiMachine, err := controller.FindCAPIMachineForInfrastructure(ctx, r.Client, machine)
	if err != nil {
		return nil, err
	}
	failureDomain := capiMachine.Spec.FailureDomain
	// spec.hostRefはclaim後immutableである。statusと不一致の場合はsafe-stopする。
	if machine.Status.HostRef != nil && machine.Spec.HostRef != nil && machine.Spec.HostRef.Name != "" && machine.Spec.HostRef.Name != machine.Status.HostRef.Name {
		return nil, errHostSelectionMismatch
	}
	if machine.Status.HostRef != nil {
		observed := &infrav1alpha1.TartHost{}
		if err := r.Get(ctx, client.ObjectKey{Name: machine.Status.HostRef.Name}, observed); err != nil {
			return nil, err
		}
		if observed.Spec.ConsumerRef == nil || observed.Spec.ConsumerRef.UID != machine.UID {
			return nil, nil
		}
		if !hostusecase.MatchesForFailureDomain(observed.Labels, observed.Spec, machine.Spec.HostSelector, failureDomain) {
			return nil, errHostSelectionMismatch
		}
		return observed, nil
	}
	for index := range hosts {
		if hosts[index].Spec.ConsumerRef != nil && hosts[index].Spec.ConsumerRef.UID == machine.UID {
			if !hostusecase.MatchesForFailureDomain(hosts[index].Labels, hosts[index].Spec, machine.Spec.HostSelector, failureDomain) {
				return nil, errHostSelectionMismatch
			}
			return hosts[index].DeepCopy(), nil
		}
	}
	if machine.Spec.HostRef != nil {
		selected := &infrav1alpha1.TartHost{}
		if err := r.Get(ctx, client.ObjectKey{Name: machine.Spec.HostRef.Name}, selected); err != nil {
			return nil, err
		}
		if selected.Spec.ConsumerRef != nil && selected.Spec.ConsumerRef.UID == machine.UID {
			return selected, nil
		}
		// 明示的なspec.hostRefは、reuse approvalとreuse modeが揃ったReusable Hostを再利用する唯一の経路である。自動選択経路(SelectFreshForFailureDomain)はAvailable Hostしか選ばない。
		eligibility := hostusecase.Classify(selected.Spec)
		if (eligibility != hostdomain.Available && eligibility != hostdomain.Reusable) || !hostusecase.MatchesForFailureDomain(selected.Labels, selected.Spec, machine.Spec.HostSelector, failureDomain) {
			return nil, hostusecase.ErrNoEligibleHost
		}
		return selected, nil
	}
	selected, err := hostusecase.SelectFreshForFailureDomainWithRendezvous(hosts, machine.Spec.HostSelector, failureDomain, machine.UID)
	return selected, err
}
