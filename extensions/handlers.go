package extensions

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	bootstrapv1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/bootstrap/v1alpha1"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/bootstrap"
	"github.com/walnuts1018/cluster-api-provider-tart/controlplane"
	"github.com/walnuts1018/cluster-api-provider-tart/talos"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	runtimehooksv1 "sigs.k8s.io/cluster-api/api/runtime/hooks/v1alpha1"
)

const (
	updateRetryAfterSeconds int32 = 30
	talosUpdateTimeout            = 20 * time.Second
	updateCapiMachineKind         = "Machine"
	tartMachineKind               = "TartMachine"
	imageField                    = "image"
	unsafeUpdateMessage           = "The requested in-place update contains an unsupported or unsafe difference; no patch was returned."
	updateClientUnavailable       = "The Runtime Extension Kubernetes client is unavailable; the update cannot be executed safely."
	updateVersionRejected         = "The requested Talos version transition is not supported; the in-place update is stopped."
)

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachines,verbs=get
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts,verbs=get
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=tartbootstrapconfigs,verbs=get
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get

type jsonPatchOperation struct {
	Operation string `json:"op"`
	Path      string `json:"path"`
	Value     any    `json:"value"`
}

// canUpdateMachineはTartMachineのinstaller image変更だけをin-place updateとして認める。その他の差分は完全に評価できないためpatchなしでvetoする。
func canUpdateMachine(_ context.Context, req *runtimehooksv1.CanUpdateMachineRequest, resp *runtimehooksv1.CanUpdateMachineResponse) {
	resp.MachinePatch = runtimehooksv1.Patch{}
	resp.InfrastructureMachinePatch = runtimehooksv1.Patch{}
	resp.BootstrapConfigPatch = runtimehooksv1.Patch{}
	if req == nil {
		resp.Status = runtimehooksv1.ResponseStatusFailure
		resp.Message = unsafeUpdateMessage
		return
	}
	machinePatch, infrastructurePatch, bootstrapPatch, err := planMachineUpdate(req)
	if err != nil {
		resp.Status = runtimehooksv1.ResponseStatusFailure
		resp.Message = unsafeUpdateMessage
		return
	}
	resp.Status = runtimehooksv1.ResponseStatusSuccess
	resp.Message = "The requested Talos image update is covered by a complete in-place patch."
	resp.MachinePatch = machinePatch
	resp.InfrastructureMachinePatch = infrastructurePatch
	resp.BootstrapConfigPatch = bootstrapPatch
}

// canUpdateMachineSetはMachineSet templateのTartMachine image変更だけをin-place updateとして認める。
func canUpdateMachineSet(_ context.Context, req *runtimehooksv1.CanUpdateMachineSetRequest, resp *runtimehooksv1.CanUpdateMachineSetResponse) {
	resp.MachineSetPatch = runtimehooksv1.Patch{}
	resp.InfrastructureMachineTemplatePatch = runtimehooksv1.Patch{}
	resp.BootstrapConfigTemplatePatch = runtimehooksv1.Patch{}
	if req == nil {
		resp.Status = runtimehooksv1.ResponseStatusFailure
		resp.Message = unsafeUpdateMessage
		return
	}
	machinePatch, infrastructurePatch, bootstrapPatch, err := planMachineSetUpdate(req)
	if err != nil {
		resp.Status = runtimehooksv1.ResponseStatusFailure
		resp.Message = unsafeUpdateMessage
		return
	}
	resp.Status = runtimehooksv1.ResponseStatusSuccess
	resp.Message = "The requested Talos image update is covered by a complete in-place template patch."
	resp.MachineSetPatch = machinePatch
	resp.InfrastructureMachineTemplatePatch = infrastructurePatch
	resp.BootstrapConfigTemplatePatch = bootstrapPatch
}

func newUpdateMachineHandler(kubeClient client.Reader) func(context.Context, *runtimehooksv1.UpdateMachineRequest, *runtimehooksv1.UpdateMachineResponse) {
	return func(ctx context.Context, req *runtimehooksv1.UpdateMachineRequest, resp *runtimehooksv1.UpdateMachineResponse) {
		updateMachineWithClient(ctx, req, resp, kubeClient)
	}
}

type machineUpdatePreparation struct {
	desiredInfrastructure *infrav1alpha1.TartMachine
	providerMachine       *infrav1alpha1.TartMachine
	endpoint              string
	configuration         []byte
	image                 string
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

func prepareMachineUpdate(ctx context.Context, req *runtimehooksv1.UpdateMachineRequest, kubeClient client.Reader) (*machineUpdatePreparation, error) {
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

	configuration, configurationErr := bootstrapConfiguration(ctx, kubeClient, &req.Desired.Machine)
	if configurationErr != nil {
		if errors.Is(configurationErr, errUpdateBootstrapUnavailable) {
			return nil, &updateRetryError{message: "The immutable Bootstrap Secret is not available while the in-place update is being prepared."}
		}
		return nil, errors.New("the immutable Bootstrap Secret does not satisfy the update contract")
	}
	return &machineUpdatePreparation{
		desiredInfrastructure: desiredInfrastructure,
		providerMachine:       providerMachine,
		endpoint:              endpoint,
		configuration:         configuration,
		image:                 image,
	}, nil
}

func updateMachineAtTalos(ctx context.Context, req *runtimehooksv1.UpdateMachineRequest, resp *runtimehooksv1.UpdateMachineResponse, kubeClient client.Reader, preparation *machineUpdatePreparation) {
	connectionContext, cancel := context.WithTimeout(ctx, talosUpdateTimeout)
	authenticated, err := talos.DialAuthenticatedFromConfiguration(connectionContext, preparation.endpoint, preparation.configuration)
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
	if preparation.providerMachine.Status.TalosVersion == preparation.desiredInfrastructure.Spec.Image.Version && preparation.providerMachine.Status.TalosSchematicID == preparation.desiredInfrastructure.Spec.Image.SchematicID && (version.Tag != preparation.desiredInfrastructure.Spec.Image.Version || observedSchematicID != preparation.desiredInfrastructure.Spec.Image.SchematicID) && machineWasPreviouslyUpToDate(preparation.providerMachine) {
		if !closeAuthenticatedForUpdate(resp, authenticated) {
			return
		}
		resp.Status = runtimehooksv1.ResponseStatusFailure
		resp.Message = "The Talos node rolled back after reaching the desired image; automatic recovery is stopped until the image transition is reviewed."
		return
	}
	if version.Tag == preparation.desiredInfrastructure.Spec.Image.Version && observedSchematicID == preparation.desiredInfrastructure.Spec.Image.SchematicID {
		if !closeAuthenticatedForUpdate(resp, authenticated) {
			return
		}
		resp.Status = runtimehooksv1.ResponseStatusSuccess
		resp.Message = "The Talos node is running the desired image."
		resp.RetryAfterSeconds = 0
		return
	}
	if err := talos.ValidateUpgrade(version.Tag, preparation.desiredInfrastructure.Spec.Image.Version); err != nil {
		if !closeAuthenticatedForUpdate(resp, authenticated) {
			return
		}
		resp.Status = runtimehooksv1.ResponseStatusFailure
		resp.Message = updateVersionRejected
		return
	}
	if isControlPlaneMachine(&req.Desired.Machine) {
		gateContext, gateCancel := context.WithTimeout(ctx, talosUpdateTimeout)
		gateErr := controlPlaneUpgradeSafe(gateContext, kubeClient, &req.Desired.Machine, authenticated)
		gateCancel()
		if gateErr != nil {
			if !closeAuthenticatedForUpdate(resp, authenticated) {
				return
			}
			setUpdateRetry(resp, "The control-plane etcd quorum could not be proven safe for a Talos restart; waiting before the upgrade.")
			return
		}
	}
	// node-disruptiveなTalos restartの前に、workload Podへの影響(availability、PDB)を考慮した
	// cordon/drainを試みる。allowDowntime policyで緩和されない限り、drain失敗はUpgradeへ進めず安全に中断する。
	if proceed, retryMessage := enforceDrainPolicy(ctx, kubeClient, &req.Desired.Machine, string(preparation.providerMachine.Spec.ProviderID)); !proceed {
		if !closeAuthenticatedForUpdate(resp, authenticated) {
			return
		}
		setUpdateRetry(resp, retryMessage)
		return
	}
	upgradeContext, upgradeCancel := context.WithTimeout(ctx, talosUpdateTimeout)
	upgradeErr := authenticated.Upgrade(upgradeContext, preparation.image)
	upgradeCancel()
	if !closeAuthenticatedForUpdate(resp, authenticated) {
		return
	}
	if upgradeErr != nil {
		resp.Status = runtimehooksv1.ResponseStatusFailure
		resp.Message = "The Talos API rejected the requested image upgrade; the Machine remains stopped for safety."
		return
	}
	setUpdateRetry(resp, "The Talos image upgrade was requested; waiting for the node to reboot and report the desired version and schematic.")
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
	if !controlplane.CanTemporarilyDisruptMember(controlplane.RemovalObservation{
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

func bootstrapConfiguration(ctx context.Context, kubeClient client.Reader, machine *clusterv1.Machine) ([]byte, error) {
	ref := machine.Spec.Bootstrap.ConfigRef
	if ref.APIGroup != bootstrapv1alpha1.GroupVersion.Group || ref.Kind != "TartBootstrapConfig" || ref.Name == "" {
		return nil, errUpdateBootstrapUnavailable
	}
	config := &bootstrapv1alpha1.TartBootstrapConfig{}
	if err := kubeClient.Get(ctx, client.ObjectKey{Namespace: machine.Namespace, Name: ref.Name}, config); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, errUpdateBootstrapUnavailable
		}
		return nil, err
	}
	if strings.TrimSpace(config.Status.DataSecretName) == "" {
		return nil, errUpdateBootstrapUnavailable
	}
	secret := &corev1.Secret{}
	if err := kubeClient.Get(ctx, client.ObjectKey{Namespace: machine.Namespace, Name: config.Status.DataSecretName}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, errUpdateBootstrapUnavailable
		}
		return nil, err
	}
	if !bootstrap.IsContractSecret(secret, config.Labels[bootstrap.ClusterNameLabel], config.UID) {
		return nil, errors.New("bootstrap Secret contract is invalid")
	}
	return bytes.Clone(secret.Data[bootstrap.BootstrapSecretKey]), nil
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

func planMachineUpdate(req *runtimehooksv1.CanUpdateMachineRequest) (runtimehooksv1.Patch, runtimehooksv1.Patch, runtimehooksv1.Patch, error) {
	desiredInfrastructure, err := decodeTartMachine(req.Desired.InfrastructureMachine)
	if err != nil {
		return runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, err
	}
	if _, err := talos.InstallerImage(desiredInfrastructure.Spec.Image.Version, desiredInfrastructure.Spec.Image.SchematicID); err != nil {
		return runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, err
	}
	currentMachine, err := marshalMap(req.Current.Machine.Spec)
	if err != nil {
		return runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, err
	}
	desiredMachine, err := marshalMap(req.Desired.Machine.Spec)
	if err != nil {
		return runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, err
	}
	// CAPI MachineのKubernetes version変更はTalosのcluster-wide upgrade sequenceが必要であり、
	// このExtension単体では実行できないためpatchを返さずに停止する。cluster、bootstrap、
	// infrastructure、ProviderIDなどの差分も同じくpatch経由で変更できないようにする。
	machinePatch, err := planSpecPatch(currentMachine, desiredMachine, nil, "/spec")
	if err != nil {
		return runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, err
	}
	infrastructurePatch, err := planRawObjectPatch(req.Current.InfrastructureMachine, req.Desired.InfrastructureMachine, infrav1alpha1.GroupVersion.String(), tartMachineKind, []string{imageField, "version"}, []string{imageField, "schematicID"})
	if err != nil {
		return runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, err
	}
	bootstrapPatch, err := planRawObjectPatch(req.Current.BootstrapConfig, req.Desired.BootstrapConfig, bootstrapv1alpha1.GroupVersion.String(), "TartBootstrapConfig")
	return machinePatch, infrastructurePatch, bootstrapPatch, err
}

func planMachineSetUpdate(req *runtimehooksv1.CanUpdateMachineSetRequest) (runtimehooksv1.Patch, runtimehooksv1.Patch, runtimehooksv1.Patch, error) {
	if err := validateTemplateImage(req.Desired.InfrastructureMachineTemplate); err != nil {
		return runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, err
	}
	currentMachine, err := marshalMap(req.Current.MachineSet.Spec.Template.Spec)
	if err != nil {
		return runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, err
	}
	desiredMachine, err := marshalMap(req.Desired.MachineSet.Spec.Template.Spec)
	if err != nil {
		return runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, err
	}
	// MachineSetのtemplateでも、Kubernetes version変更はcluster-wide upgrade sequenceへ委譲する。
	// このExtensionはTalos OS image変更だけを扱うため、CAPI MachineSet側のspec差分は許可しない。
	machinePatch, err := planSpecPatch(currentMachine, desiredMachine, nil, "/spec/template/spec")
	if err != nil {
		return runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, err
	}
	infrastructurePatch, err := planRawTemplatePatch(req.Current.InfrastructureMachineTemplate, req.Desired.InfrastructureMachineTemplate, infrav1alpha1.GroupVersion.String(), "TartMachineTemplate", []string{imageField, "version"}, []string{imageField, "schematicID"})
	if err != nil {
		return runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, err
	}
	bootstrapPatch, err := planRawTemplatePatch(req.Current.BootstrapConfigTemplate, req.Desired.BootstrapConfigTemplate, bootstrapv1alpha1.GroupVersion.String(), "TartBootstrapConfigTemplate")
	return machinePatch, infrastructurePatch, bootstrapPatch, err
}

func planRawObjectPatch(currentRaw, desiredRaw runtime.RawExtension, expectedAPIVersion, expectedKind string, allowedPaths ...[]string) (runtimehooksv1.Patch, error) {
	current, currentPresent, err := decodeRawObject(currentRaw)
	if err != nil {
		return runtimehooksv1.Patch{}, err
	}
	desired, desiredPresent, err := decodeRawObject(desiredRaw)
	if err != nil {
		return runtimehooksv1.Patch{}, err
	}
	if !currentPresent || !desiredPresent {
		if currentPresent == desiredPresent {
			return runtimehooksv1.Patch{}, nil
		}
		return runtimehooksv1.Patch{}, errors.New("optional update object presence changed")
	}
	if err := validateRawObjectIdentity(current, expectedAPIVersion, expectedKind); err != nil {
		return runtimehooksv1.Patch{}, err
	}
	if err := validateRawObjectIdentity(desired, expectedAPIVersion, expectedKind); err != nil {
		return runtimehooksv1.Patch{}, err
	}
	currentSpec, err := requiredMap(current, "spec")
	if err != nil {
		return runtimehooksv1.Patch{}, err
	}
	desiredSpec, err := requiredMap(desired, "spec")
	if err != nil {
		return runtimehooksv1.Patch{}, err
	}
	return planSpecPatch(currentSpec, desiredSpec, allowedPaths, "/spec")
}

func planRawTemplatePatch(currentRaw, desiredRaw runtime.RawExtension, expectedAPIVersion, expectedKind string, allowedPaths ...[]string) (runtimehooksv1.Patch, error) {
	current, currentPresent, err := decodeRawObject(currentRaw)
	if err != nil {
		return runtimehooksv1.Patch{}, err
	}
	desired, desiredPresent, err := decodeRawObject(desiredRaw)
	if err != nil {
		return runtimehooksv1.Patch{}, err
	}
	if !currentPresent || !desiredPresent {
		if currentPresent == desiredPresent {
			return runtimehooksv1.Patch{}, nil
		}
		return runtimehooksv1.Patch{}, errors.New("optional template presence changed")
	}
	if err := validateRawObjectIdentity(current, expectedAPIVersion, expectedKind); err != nil {
		return runtimehooksv1.Patch{}, err
	}
	if err := validateRawObjectIdentity(desired, expectedAPIVersion, expectedKind); err != nil {
		return runtimehooksv1.Patch{}, err
	}
	currentSpec, err := requiredMap(current, "spec", "template", "spec")
	if err != nil {
		return runtimehooksv1.Patch{}, err
	}
	desiredSpec, err := requiredMap(desired, "spec", "template", "spec")
	if err != nil {
		return runtimehooksv1.Patch{}, err
	}
	return planSpecPatch(currentSpec, desiredSpec, allowedPaths, "/spec/template/spec")
}

func validateRawObjectIdentity(object map[string]any, expectedAPIVersion, expectedKind string) error {
	apiVersion, ok := object["apiVersion"].(string)
	if !ok || apiVersion != expectedAPIVersion {
		return errors.New("update object has an unexpected apiVersion")
	}
	kind, ok := object["kind"].(string)
	if !ok || kind != expectedKind {
		return errors.New("update object has an unexpected kind")
	}
	return nil
}

func planSpecPatch(current, desired map[string]any, allowedPaths [][]string, patchPath string) (runtimehooksv1.Patch, error) {
	if reflect.DeepEqual(current, desired) {
		return runtimehooksv1.Patch{}, nil
	}
	normalized := cloneMap(current)
	for _, path := range allowedPaths {
		if err := copyOrDeletePath(normalized, desired, path); err != nil {
			return runtimehooksv1.Patch{}, err
		}
	}
	if !reflect.DeepEqual(normalized, desired) {
		return runtimehooksv1.Patch{}, errors.New("update contains an unsupported spec difference")
	}
	patch, err := json.Marshal([]jsonPatchOperation{{Operation: "replace", Path: patchPath, Value: desired}})
	if err != nil {
		return runtimehooksv1.Patch{}, fmt.Errorf("encode update patch: %w", err)
	}
	return runtimehooksv1.Patch{PatchType: runtimehooksv1.JSONPatchType, Patch: patch}, nil
}

func copyOrDeletePath(current, desired map[string]any, path []string) error {
	value, exists, err := readPath(desired, path)
	if err != nil {
		return err
	}
	if exists {
		return writePath(current, path, value)
	}
	return deletePath(current, path)
}

func readPath(root map[string]any, path []string) (any, bool, error) {
	var current any = root
	for _, part := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false, errors.New("update path parent is not an object")
		}
		value, exists := object[part]
		if !exists {
			return nil, false, nil
		}
		current = value
	}
	return current, true, nil
}

func writePath(root map[string]any, path []string, value any) error {
	current := root
	for _, part := range path[:len(path)-1] {
		value, exists := current[part]
		if !exists {
			nested := make(map[string]any)
			current[part] = nested
			current = nested
			continue
		}
		nested, ok := value.(map[string]any)
		if !ok {
			return errors.New("update path parent is not an object")
		}
		current = nested
	}
	current[path[len(path)-1]] = cloneJSONValue(value)
	return nil
}

func deletePath(root map[string]any, path []string) error {
	current := root
	for _, part := range path[:len(path)-1] {
		value, exists := current[part]
		if !exists {
			return nil
		}
		nested, ok := value.(map[string]any)
		if !ok {
			return errors.New("update path parent is not an object")
		}
		current = nested
	}
	delete(current, path[len(path)-1])
	return nil
}

func decodeRawObject(raw runtime.RawExtension) (map[string]any, bool, error) {
	data, err := rawBytes(raw)
	if err != nil {
		return nil, false, err
	}
	if len(data) == 0 {
		return nil, false, nil
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		return nil, false, fmt.Errorf("decode update object: %w", err)
	}
	if object == nil {
		return nil, false, errors.New("update object is not a JSON object")
	}
	return object, true, nil
}

func rawBytes(raw runtime.RawExtension) ([]byte, error) {
	if len(raw.Raw) > 0 {
		return raw.Raw, nil
	}
	if raw.Object == nil {
		return nil, nil
	}
	data, err := json.Marshal(raw.Object)
	if err != nil {
		return nil, fmt.Errorf("encode update object: %w", err)
	}
	return data, nil
}

func validateTemplateImage(raw runtime.RawExtension) error {
	object, present, err := decodeRawObject(raw)
	if err != nil {
		return err
	}
	if !present {
		return errors.New("update object is missing its infrastructure template")
	}
	spec, err := requiredMap(object, "spec", "template", "spec")
	if err != nil {
		return err
	}
	value, exists, err := readPath(spec, []string{imageField})
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("update object is missing its Talos image")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode Talos image: %w", err)
	}
	var image infrav1alpha1.TalosImageSpec
	if err := json.Unmarshal(data, &image); err != nil {
		return fmt.Errorf("decode Talos image: %w", err)
	}
	_, err = talos.InstallerImage(image.Version, image.SchematicID)
	return err
}

func marshalMap(value any) (map[string]any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, errors.New("update spec is not a JSON object")
	}
	return result, nil
}

func requiredMap(root map[string]any, path ...string) (map[string]any, error) {
	value, exists, err := readPath(root, path)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.New("update object is missing its spec")
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("update object spec is not an object")
	}
	return object, nil
}

func cloneMap(value map[string]any) map[string]any {
	return cloneJSONValue(value).(map[string]any)
}

func cloneJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, nested := range typed {
			result[key] = cloneJSONValue(nested)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, nested := range typed {
			result[index] = cloneJSONValue(nested)
		}
		return result
	default:
		return value
	}
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
