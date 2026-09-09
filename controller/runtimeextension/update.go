package runtimeextension

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"reflect"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/walnuts1018/cluster-api-provider-tart/adapter/talos"
	bootstrapv1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/bootstrap/v1alpha1"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	domaincontrolplane "github.com/walnuts1018/cluster-api-provider-tart/domain/controlplane"
	"github.com/walnuts1018/cluster-api-provider-tart/usecase/bootstrap"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	runtimehooksv1 "sigs.k8s.io/cluster-api/api/runtime/hooks/v1alpha1"
)

type MachineUpdatePreparation struct {
	DesiredInfrastructure *infrav1alpha1.TartMachine
	ProviderMachine       *infrav1alpha1.TartMachine
	Endpoint              string
	Configuration         []byte
	Image                 string
	Strategy              bootstrapv1alpha1.ConfigurationApplyStrategy
}

type updateRetryError struct {
	message string
}

func (e *updateRetryError) Error() string {
	return e.message
}

func updateMachineWithClient(ctx context.Context, req *runtimehooksv1.UpdateMachineRequest, resp *runtimehooksv1.UpdateMachineResponse, kubeClient client.Reader) {
	resp.RetryAfterSeconds = 0
	if req == nil {
		resp.Status = runtimehooksv1.ResponseStatusFailure
		resp.Message = unsafeUpdateMessage
		return
	}
	if kubeClient == nil {
		resp.Status = runtimehooksv1.ResponseStatusFailure
		resp.Message = updateClientUnavailable
		return
	}
	preparation, err := prepareMachineUpdate(ctx, req, kubeClient)
	if err != nil {
		if retry, ok := errors.AsType[*updateRetryError](err); ok {
			setUpdateRetry(resp, retry.message)
			return
		}
		resp.Status = runtimehooksv1.ResponseStatusFailure
		resp.Message = unsafeUpdateMessage
		return
	}
	updateMachineAtTalos(ctx, req, resp, kubeClient, preparation)
}

func prepareMachineUpdate(ctx context.Context, req *runtimehooksv1.UpdateMachineRequest, kubeClient client.Reader) (*MachineUpdatePreparation, error) {
	desiredInfrastructure, err := decodeTartMachine(req.Desired.InfrastructureMachine)
	if err != nil || desiredInfrastructure.Spec.Image.Version == "" || desiredInfrastructure.Spec.Image.SchematicID == "" {
		return nil, errors.New("desired TartMachine image is invalid")
	}
	image, err := talos.InstallerImage(desiredInfrastructure.Spec.Image.Version, desiredInfrastructure.Spec.Image.SchematicID)
	if err != nil {
		return nil, err
	}
	ref := req.Desired.Machine.Spec.InfrastructureRef
	if ref.APIGroup != infrav1alpha1.GroupVersion.Group || ref.Kind != tartMachineKind || ref.Name == "" {
		return nil, errors.New("the CAPI Machine does not reference an updatable TartMachine")
	}
	providerMachine := &infrav1alpha1.TartMachine{}
	if err := kubeClient.Get(ctx, client.ObjectKey{Namespace: req.Desired.Machine.Namespace, Name: ref.Name}, providerMachine); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, &updateRetryError{message: "The TartMachine is not available while the in-place update is being prepared."}
		}
		return nil, &updateRetryError{message: "The TartMachine could not be observed while the in-place update is being prepared."}
	}
	if err := validateUpdateProviderOwner(providerMachine, &req.Desired.Machine); err != nil {
		return nil, errors.New("the TartMachine owner does not match the CAPI Machine identity")
	}
	if !reflect.DeepEqual(providerMachine.Spec.HostRef, desiredInfrastructure.Spec.HostRef) || !reflect.DeepEqual(providerMachine.Spec.HostSelector, desiredInfrastructure.Spec.HostSelector) || providerMachine.Spec.ProviderID != desiredInfrastructure.Spec.ProviderID {
		return nil, &updateRetryError{message: "The live TartMachine identity differs from the update request; waiting for the desired object to settle."}
	}
	if providerMachine.Status.HostRef == nil || providerMachine.Status.HostRef.Name == "" {
		return nil, &updateRetryError{message: "The TartMachine has no observed Host binding yet."}
	}
	providerHost := &infrav1alpha1.TartHost{}
	if err := kubeClient.Get(ctx, client.ObjectKey{Name: providerMachine.Status.HostRef.Name}, providerHost); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, &updateRetryError{message: "The allocated TartHost is not available while the in-place update is being prepared."}
		}
		return nil, &updateRetryError{message: "The allocated TartHost could not be observed while the in-place update is being prepared."}
	}
	if providerHost.Spec.ConsumerRef == nil || providerHost.Spec.ConsumerRef.UID != providerMachine.UID {
		return nil, errors.New("the allocated TartHost binding does not match the TartMachine identity")
	}
	endpoint := hostEndpoint(providerHost)
	if endpoint == "" {
		return nil, &updateRetryError{message: "The allocated TartHost has no reachable Talos endpoint yet."}
	}

	bootstrapConfig, configuration, configurationErr := bootstrapConfigurationWithPolicy(ctx, kubeClient, &req.Desired.Machine)
	if configurationErr != nil {
		if errors.Is(configurationErr, errUpdateBootstrapUnavailable) {
			return nil, &updateRetryError{message: "The immutable Bootstrap Secret is not available while the in-place update is being prepared."}
		}
		if errors.Is(configurationErr, errUpdateBootstrapContractInvalid) {
			return nil, errors.New("the immutable Bootstrap Secret does not satisfy the update contract")
		}
		// TartBootstrapConfigまたはBootstrap SecretのGetがNotFound以外の理由で失敗した場合は
		// APIサーバーの一時的な障害の可能性があるため、恒久的なcontract違反として扱わずretryする。
		// TartMachine/TartHostの同様なGet失敗(上記)と同じ方針。
		return nil, &updateRetryError{message: "The immutable Bootstrap Secret could not be observed while the in-place update is being prepared."}
	}
	if providerMachine.Spec.ProviderID.IsZero() {
		return nil, &updateRetryError{message: "The TartMachine ProviderID is not available while the in-place update is being prepared."}
	}
	configuration, err = talos.SetProviderID(configuration, providerMachine.Spec.ProviderID.String())
	if err != nil {
		return nil, errors.New("the immutable Bootstrap Secret does not contain the allocated ProviderID")
	}
	return &MachineUpdatePreparation{
		DesiredInfrastructure: desiredInfrastructure,
		ProviderMachine:       providerMachine,
		Endpoint:              endpoint,
		Configuration:         configuration,
		Image:                 image,
		Strategy:              bootstrapUpdateStrategy(bootstrapConfig),
	}, nil
}

func updateMachineAtTalos(ctx context.Context, req *runtimehooksv1.UpdateMachineRequest, resp *runtimehooksv1.UpdateMachineResponse, kubeClient client.Reader, preparation *MachineUpdatePreparation) {
	connectionContext, cancel := context.WithTimeout(ctx, talosUpdateTimeout)
	authenticated, err := talos.DialAuthenticatedFromConfiguration(connectionContext, preparation.Endpoint, preparation.Configuration)
	cancel()
	if err != nil {
		setUpdateRetry(resp, "The authenticated Talos API is not reachable while the in-place update is being prepared.")
		return
	}
	versionContext, versionCancel := context.WithTimeout(ctx, talosUpdateTimeout)
	version, versionErr := authenticated.Version(versionContext)
	versionCancel()
	if versionErr != nil {
		if !closeAuthenticatedForUpdate(resp, authenticated) {
			return
		}
		setUpdateRetry(resp, "The Talos version could not be observed while the in-place update is being prepared.")
		return
	}
	schematicContext, schematicCancel := context.WithTimeout(ctx, talosUpdateTimeout)
	observedSchematicID, schematicErr := authenticated.SchematicID(schematicContext)
	schematicCancel()
	if schematicErr != nil {
		if !closeAuthenticatedForUpdate(resp, authenticated) {
			return
		}
		setUpdateRetry(resp, "The Talos schematic identity could not be observed while the in-place update is being prepared.")
		return
	}
	if preparation.ProviderMachine.Status.TalosVersion == preparation.DesiredInfrastructure.Spec.Image.Version && preparation.ProviderMachine.Status.TalosSchematicID == preparation.DesiredInfrastructure.Spec.Image.SchematicID && (version.Tag != preparation.DesiredInfrastructure.Spec.Image.Version || observedSchematicID != preparation.DesiredInfrastructure.Spec.Image.SchematicID) && machineWasPreviouslyUpToDate(preparation.ProviderMachine) {
		if !closeAuthenticatedForUpdate(resp, authenticated) {
			return
		}
		resp.Status = runtimehooksv1.ResponseStatusFailure
		resp.Message = "The Talos node rolled back after reaching the desired image; automatic recovery is stopped until the image transition is reviewed."
		return
	}
	if version.Tag == preparation.DesiredInfrastructure.Spec.Image.Version && observedSchematicID == preparation.DesiredInfrastructure.Spec.Image.SchematicID {
		// imageがdesiredへ到達している場合だけ、machine configuration差分をpolicyへ従ってin-placeで適用する。
		outcome := ApplyConfigurationUpdate(ctx, MachineConfigurationUpdate(kubeClient, &req.Desired.Machine, preparation, authenticated))
		if !closeAuthenticatedForUpdate(resp, authenticated) {
			return
		}
		switch {
		case outcome.FailureMessage != "":
			resp.Status = runtimehooksv1.ResponseStatusFailure
			resp.Message = outcome.FailureMessage
		case outcome.RetryMessage != "":
			setUpdateRetry(resp, outcome.RetryMessage)
		default:
			// Kubernetes version upgradeはTartControlPlaneがcluster単位で実行済みである。ここでは自Nodeの
			// observed Kubernetes versionがdesired versionへ収束したことだけを確認し、個別のupgradeは行わない。
			if converged, retryMessage := nodeKubernetesVersionConverged(ctx, kubeClient, &req.Desired.Machine, string(preparation.ProviderMachine.Spec.ProviderID), req.Desired.Machine.Spec.Version); !converged {
				setUpdateRetry(resp, retryMessage)
				return
			}
			resp.Status = runtimehooksv1.ResponseStatusSuccess
			resp.Message = "The Talos node is running the desired image, machine configuration, and Kubernetes version."
			resp.RetryAfterSeconds = 0
		}
		return
	}
	if err := talos.ValidateUpgrade(version.Tag, preparation.DesiredInfrastructure.Spec.Image.Version); err != nil {
		if !closeAuthenticatedForUpdate(resp, authenticated) {
			return
		}
		resp.Status = runtimehooksv1.ResponseStatusFailure
		resp.Message = updateVersionRejected
		return
	}
	outcome := PerformImageUpgrade(ctx, kubeClient, &req.Desired.Machine, string(preparation.ProviderMachine.Spec.ProviderID), preparation.Image, authenticated)
	if !closeAuthenticatedForUpdate(resp, authenticated) {
		return
	}
	if outcome.FailureMessage != "" {
		resp.Status = runtimehooksv1.ResponseStatusFailure
		resp.Message = outcome.FailureMessage
		return
	}
	setUpdateRetry(resp, outcome.RetryMessage)
}

func closeAuthenticatedForUpdate(resp *runtimehooksv1.UpdateMachineResponse, authenticated *talos.Client) bool {
	if err := authenticated.Close(); err != nil {
		returnUpdateCloseError(resp, err)
		return false
	}
	return true
}

func machineWasPreviouslyUpToDate(machine *infrav1alpha1.TartMachine) bool {
	if machine == nil {
		return false
	}
	condition := meta.FindStatusCondition(machine.Status.Conditions, infrav1alpha1.TartMachineTalosUpToDateCondition)
	return condition != nil && condition.Status == metav1.ConditionTrue
}

func isControlPlaneMachine(machine *clusterv1.Machine) bool {
	if machine == nil {
		return false
	}
	_, exists := machine.Labels[clusterv1.MachineControlPlaneLabel]
	return exists
}

func controlPlaneUpgradeSafe(ctx context.Context, kubeClient client.Reader, target *clusterv1.Machine, targetClient *talos.Client) error {
	if kubeClient == nil || target == nil || targetClient == nil || target.Namespace == "" || target.Name == "" || target.UID == "" || target.Spec.ClusterName == "" {
		return errors.New("control-plane upgrade quorum context is incomplete")
	}
	controlPlaneMachines, err := controlPlaneUpgradeSurvivors(ctx, kubeClient, target)
	if err != nil {
		return err
	}
	members, err := targetClient.EtcdMembers(ctx)
	if err != nil {
		return err
	}
	memberIDs, err := validateControlPlaneEtcdMembers(members)
	if err != nil {
		return err
	}
	if len(members) != len(controlPlaneMachines)+1 {
		return errors.New("control-plane Machine and etcd member counts do not match")
	}
	targetStatus, err := targetClient.EtcdStatus(ctx)
	if err != nil {
		return err
	}
	if _, exists := memberIDs[targetStatus.MemberID]; !exists {
		return errors.New("target control-plane Machine is not an etcd member")
	}
	if !controlPlaneEtcdHealthy(targetStatus) {
		return errors.New("target control-plane etcd health is not ready")
	}

	healthyMembers := 1
	observedMemberIDs := map[uint64]struct{}{targetStatus.MemberID: {}}
	for _, machine := range controlPlaneMachines {
		status, err := observeControlPlaneEtcdStatus(ctx, kubeClient, machine)
		if err != nil {
			return err
		}
		if !controlPlaneEtcdHealthy(status) {
			return errors.New("a surviving control-plane Machine is not a healthy etcd member")
		}
		if _, exists := memberIDs[status.MemberID]; !exists {
			return errors.New("a surviving control-plane Machine is not a healthy etcd member")
		}
		if _, exists := observedMemberIDs[status.MemberID]; exists {
			return errors.New("control-plane Machines report duplicate etcd member identities")
		}
		observedMemberIDs[status.MemberID] = struct{}{}
		healthyMembers++
	}
	if !domaincontrolplane.CanTemporarilyDisruptMember(domaincontrolplane.RemovalObservation{
		MemberCount:          len(members),
		HealthyMemberCount:   healthyMembers,
		TargetHealthy:        true,
		TargetHealthObserved: true,
	}) {
		return errors.New("control-plane etcd quorum would be lost during the restart")
	}
	return nil
}

func controlPlaneUpgradeSurvivors(ctx context.Context, kubeClient client.Reader, target *clusterv1.Machine) ([]*clusterv1.Machine, error) {
	var machines clusterv1.MachineList
	if err := kubeClient.List(ctx, &machines, client.InNamespace(target.Namespace)); err != nil {
		return nil, fmt.Errorf("list control-plane Machines: %w", err)
	}
	controlPlaneMachines := make([]*clusterv1.Machine, 0, len(machines.Items))
	targetFound := false
	for index := range machines.Items {
		machine := &machines.Items[index]
		if machine.Spec.ClusterName != target.Spec.ClusterName || !isControlPlaneMachine(machine) {
			continue
		}
		if !machine.DeletionTimestamp.IsZero() {
			return nil, errors.New("a control-plane Machine is deleting")
		}
		if machine.UID == target.UID {
			if targetFound {
				return nil, errors.New("the control-plane Machine inventory contains duplicate target identities")
			}
			targetFound = true
			continue
		}
		if machine.Annotations[clusterv1.UpdateInProgressAnnotation] != "" {
			return nil, errors.New("another control-plane Machine is already updating")
		}
		ready := meta.FindStatusCondition(machine.Status.Conditions, clusterv1.MachineReadyCondition)
		if ready == nil || ready.Status != metav1.ConditionTrue {
			return nil, errors.New("a surviving control-plane Machine is not Ready")
		}
		controlPlaneMachines = append(controlPlaneMachines, machine)
	}
	if !targetFound {
		return nil, errors.New("the target control-plane Machine is not in the current cluster inventory")
	}
	return controlPlaneMachines, nil
}

func validateControlPlaneEtcdMembers(members []talos.EtcdMember) (map[uint64]struct{}, error) {
	memberIDs := make(map[uint64]struct{}, len(members))
	for _, member := range members {
		if member.ID == 0 || member.Learner {
			return nil, errors.New("control-plane etcd membership is incomplete or contains a learner")
		}
		if _, exists := memberIDs[member.ID]; exists {
			return nil, errors.New("control-plane etcd membership contains duplicate IDs")
		}
		memberIDs[member.ID] = struct{}{}
	}
	if len(memberIDs) == 0 {
		return nil, errors.New("control-plane etcd membership is empty")
	}
	return memberIDs, nil
}

func controlPlaneEtcdHealthy(status talos.EtcdStatus) bool {
	return status.MemberID != 0 && status.Leader != 0 && len(status.Errors) == 0
}

func observeControlPlaneEtcdStatus(ctx context.Context, kubeClient client.Reader, machine *clusterv1.Machine) (talos.EtcdStatus, error) {
	ref := machine.Spec.InfrastructureRef
	if ref.APIGroup != infrav1alpha1.GroupVersion.Group || ref.Kind != tartMachineKind || ref.Name == "" {
		return talos.EtcdStatus{}, errors.New("control-plane infrastructure reference is invalid")
	}
	providerMachine := &infrav1alpha1.TartMachine{}
	if err := kubeClient.Get(ctx, client.ObjectKey{Namespace: machine.Namespace, Name: ref.Name}, providerMachine); err != nil {
		return talos.EtcdStatus{}, fmt.Errorf("get surviving TartMachine: %w", err)
	}
	if err := validateUpdateProviderOwner(providerMachine, machine); err != nil {
		return talos.EtcdStatus{}, err
	}
	if providerMachine.Status.HostRef == nil || providerMachine.Status.HostRef.Name == "" {
		return talos.EtcdStatus{}, errors.New("surviving TartMachine Host binding is unavailable")
	}
	providerHost := &infrav1alpha1.TartHost{}
	if err := kubeClient.Get(ctx, client.ObjectKey{Name: providerMachine.Status.HostRef.Name}, providerHost); err != nil {
		return talos.EtcdStatus{}, fmt.Errorf("get surviving TartHost: %w", err)
	}
	if providerHost.Spec.ConsumerRef == nil || providerHost.Spec.ConsumerRef.UID != providerMachine.UID {
		return talos.EtcdStatus{}, errors.New("surviving TartHost binding does not match TartMachine")
	}
	endpoint := hostEndpoint(providerHost)
	if endpoint == "" {
		return talos.EtcdStatus{}, errors.New("surviving TartHost endpoint is unavailable")
	}
	configuration, err := bootstrapConfiguration(ctx, kubeClient, machine)
	if err != nil {
		return talos.EtcdStatus{}, err
	}
	connectionContext, cancel := context.WithTimeout(ctx, talosUpdateTimeout)
	authenticated, err := talos.DialAuthenticatedFromConfiguration(connectionContext, endpoint, configuration)
	cancel()
	if err != nil {
		return talos.EtcdStatus{}, err
	}
	status, statusErr := authenticated.EtcdStatus(ctx)
	closeErr := authenticated.Close()
	if statusErr != nil {
		return talos.EtcdStatus{}, statusErr
	}
	if closeErr != nil {
		return talos.EtcdStatus{}, closeErr
	}
	return status, nil
}

func validateUpdateProviderOwner(providerMachine *infrav1alpha1.TartMachine, machine *clusterv1.Machine) error {
	if providerMachine == nil || machine == nil || machine.Namespace == "" || machine.Name == "" || machine.UID == "" {
		return errors.New("update provider owner identity is incomplete")
	}
	if providerMachine.Namespace != machine.Namespace || len(providerMachine.OwnerReferences) != 1 {
		return errors.New("update provider owner identity is invalid")
	}
	owner := providerMachine.OwnerReferences[0]
	if owner.APIVersion != clusterv1.GroupVersion.String() || owner.Kind != updateCapiMachineKind || owner.Name != machine.Name || owner.UID != machine.UID || owner.Controller == nil || !*owner.Controller {
		return errors.New("update provider owner identity does not match CAPI Machine")
	}
	return nil
}

func setUpdateRetry(resp *runtimehooksv1.UpdateMachineResponse, message string) {
	resp.Status = runtimehooksv1.ResponseStatusSuccess
	resp.Message = message
	resp.RetryAfterSeconds = updateRetryAfterSeconds
}

func returnUpdateCloseError(resp *runtimehooksv1.UpdateMachineResponse, _ error) {
	resp.Status = runtimehooksv1.ResponseStatusFailure
	resp.Message = "The Talos API connection could not be closed safely; the in-place update is stopped."
}

var errUpdateBootstrapUnavailable = errors.New("bootstrap data is unavailable for update")

// errUpdateBootstrapContractInvalidは、取得自体は成功したがBootstrap Secretの内容がCAPI contractを
// 満たさない恒久的な不整合を表す。APIサーバーの一時的な障害による取得失敗とは区別し、前者だけを
// リトライ対象として扱う。
var errUpdateBootstrapContractInvalid = errors.New("bootstrap Secret contract is invalid")

func bootstrapConfiguration(ctx context.Context, kubeClient client.Reader, machine *clusterv1.Machine) ([]byte, error) {
	_, configuration, err := bootstrapConfigurationWithPolicy(ctx, kubeClient, machine)
	return configuration, err
}

// bootstrapConfigurationWithPolicyはimmutable Bootstrap Secretのdesired configurationと、その適用方針を持つTartBootstrapConfigを返す。
func bootstrapConfigurationWithPolicy(ctx context.Context, kubeClient client.Reader, machine *clusterv1.Machine) (*bootstrapv1alpha1.TartBootstrapConfig, []byte, error) {
	ref := machine.Spec.Bootstrap.ConfigRef
	if ref.APIGroup != bootstrapv1alpha1.GroupVersion.Group || ref.Kind != tartBootstrapConfigKind || ref.Name == "" {
		return nil, nil, errUpdateBootstrapUnavailable
	}
	config := &bootstrapv1alpha1.TartBootstrapConfig{}
	if err := kubeClient.Get(ctx, client.ObjectKey{Namespace: machine.Namespace, Name: ref.Name}, config); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil, errUpdateBootstrapUnavailable
		}
		return nil, nil, err
	}
	if strings.TrimSpace(config.Status.DataSecretName) == "" {
		return nil, nil, errUpdateBootstrapUnavailable
	}
	secret := &corev1.Secret{}
	if err := kubeClient.Get(ctx, client.ObjectKey{Namespace: machine.Namespace, Name: config.Status.DataSecretName}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil, errUpdateBootstrapUnavailable
		}
		return nil, nil, err
	}
	if !bootstrap.IsContractSecret(secret, config.Labels[bootstrap.ClusterNameLabel], config.UID) {
		return nil, nil, errUpdateBootstrapContractInvalid
	}
	return config, bytes.Clone(secret.Data[bootstrap.BootstrapSecretKey]), nil
}

func decodeTartMachine(raw runtime.RawExtension) (*infrav1alpha1.TartMachine, error) {
	data, err := rawBytes(raw)
	if err != nil {
		return nil, err
	}
	var machine infrav1alpha1.TartMachine
	if err := json.Unmarshal(data, &machine); err != nil {
		return nil, fmt.Errorf("decode TartMachine: %w", err)
	}
	if machine.APIVersion != infrav1alpha1.GroupVersion.String() || machine.Kind != tartMachineKind {
		return nil, errors.New("update object is not a TartMachine")
	}
	return &machine, nil
}

func hostEndpoint(host *infrav1alpha1.TartHost) string {
	if endpoint := host.Spec.TalosAPIAddress.String(); endpoint != "" {
		return endpoint
	}
	for _, addressType := range []clusterv1.MachineAddressType{clusterv1.MachineInternalIP, clusterv1.MachineExternalIP, clusterv1.MachineHostName} {
		for _, address := range host.Status.Addresses {
			if address.Type == addressType && strings.TrimSpace(address.Address) != "" {
				return strings.TrimSpace(address.Address)
			}
		}
	}
	return ""
}
