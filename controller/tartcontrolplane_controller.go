package controller

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/validation"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	bootstrapv1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/bootstrap/v1alpha1"
	controlplanev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/controlplane/v1alpha1"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/controlplane"
	"github.com/walnuts1018/cluster-api-provider-tart/talos"
)

const controlPlaneOrdinalLabel = "tart.cluster.x-k8s.io/control-plane-index"

// TartControlPlaneReconciler reconciles a TartControlPlane object.
type TartControlPlaneReconciler struct {
	client.Client
}

type controlPlaneFailure struct {
	reason  string
	message string
}

type controlPlaneBootstrapState struct {
	initialized  bool
	etcdReady    bool
	reason       string
	message      string
	requeueAfter time.Duration
}

func (f *controlPlaneFailure) Error() string {
	return f.reason + ": " + f.message
}

// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=tartcontrolplanes,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=tartcontrolplanes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachinetemplates,verbs=get;list;watch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachines,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=tartbootstrapconfigtemplates,verbs=get;list;watch
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=tartbootstrapconfigs,verbs=get;list;watch;create
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *TartControlPlaneReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var cp controlplanev1alpha1.TartControlPlane
	if err := r.Get(ctx, req.NamespacedName, &cp); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if isPaused(&cp) {
		return r.reconcilePaused(ctx, &cp)
	}
	if !cp.DeletionTimestamp.IsZero() {
		return r.reconcileDeleting(ctx, &cp)
	}

	clusterName, err := controlPlaneClusterName(&cp)
	if err != nil {
		return r.reportFailure(ctx, &cp, err)
	}
	if cp.Spec.Version == "" {
		return r.reportFailure(ctx, &cp, &controlPlaneFailure{
			reason:  "InvalidSpec",
			message: "The Kubernetes version is required before control-plane Machines can be created.",
		})
	}
	desiredReplicas, err := desiredControlPlaneReplicas(&cp)
	if err != nil {
		return r.reportFailure(ctx, &cp, err)
	}

	var cluster clusterv1.Cluster
	if err := r.Get(ctx, client.ObjectKey{Namespace: cp.Namespace, Name: clusterName}, &cluster); err != nil {
		if apierrors.IsNotFound(err) {
			return r.reportFailure(ctx, &cp, &controlPlaneFailure{
				reason:  reasonClusterUnavailable,
				message: "The referenced CAPI Cluster is not available yet.",
			})
		}
		return ctrl.Result{}, err
	}

	tartCluster, err := r.getTartCluster(ctx, &cluster)
	if err != nil {
		return r.reportFailure(ctx, &cp, err)
	}
	if err := r.validateActiveBundle(ctx, tartCluster); err != nil {
		return r.reportFailure(ctx, &cp, err)
	}

	var machineTemplate infrav1alpha1.TartMachineTemplate
	if err := r.getTartMachineTemplate(ctx, cp.Namespace, &cp.Spec.MachineTemplate.Spec.InfrastructureRef, &machineTemplate); err != nil {
		return r.reportFailure(ctx, &cp, err)
	}
	var bootstrapTemplate bootstrapv1alpha1.TartBootstrapConfigTemplate
	if err := r.getBootstrapTemplate(ctx, cp.Namespace, &cp.Spec.BootstrapConfigTemplate, &bootstrapTemplate); err != nil {
		return r.reportFailure(ctx, &cp, err)
	}
	if bootstrapTemplate.Spec.Template.Spec.ConfigSecretRef == nil || bootstrapTemplate.Spec.Template.Spec.ConfigSecretRef.Name == "" {
		return r.reportFailure(ctx, &cp, &controlPlaneFailure{
			reason:  reasonBootstrapTemplateInvalid,
			message: "The BootstrapConfigTemplate must reference an immutable configuration Secret.",
		})
	}

	machines, err := r.ensureMachines(ctx, &cp, clusterName, desiredReplicas, &machineTemplate, &bootstrapTemplate)
	if err != nil {
		if failure, ok := errors.AsType[*controlPlaneFailure](err); ok {
			return r.reportFailure(ctx, &cp, failure)
		}
		return ctrl.Result{}, err
	}
	bootstrapState, err := r.reconcileControlPlaneBootstrap(ctx, &cp, machines)
	if err != nil {
		return ctrl.Result{}, err
	}

	original := cp.DeepCopy()
	setControlPlaneStatus(&cp, clusterName, desiredReplicas, machines, bootstrapState)
	if err := r.Status().Patch(ctx, &cp, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: bootstrapState.requeueAfter}, nil
}

func (r *TartControlPlaneReconciler) reconcilePaused(ctx context.Context, cp *controlplanev1alpha1.TartControlPlane) (ctrl.Result, error) {
	original := cp.DeepCopy()
	setPausedCondition(&cp.Status.Conditions, true, cp.Generation)
	if err := r.Status().Patch(ctx, cp, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *TartControlPlaneReconciler) reconcileDeleting(ctx context.Context, cp *controlplanev1alpha1.TartControlPlane) (ctrl.Result, error) {
	original := cp.DeepCopy()
	setCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneDeletingCondition, metav1.ConditionTrue, "Deleting", "The control plane is being deleted; no new Machine or Host allocation is started.", cp.Generation)
	setCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneReadyCondition, metav1.ConditionFalse, "Deleting", "The control plane is being deleted.", cp.Generation)
	setPausedCondition(&cp.Status.Conditions, false, cp.Generation)
	cp.Status.ObservedGeneration = cp.Generation
	if err := r.Status().Patch(ctx, cp, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *TartControlPlaneReconciler) reportFailure(ctx context.Context, cp *controlplanev1alpha1.TartControlPlane, report error) (ctrl.Result, error) {
	failure := &controlPlaneFailure{reason: "ReconcileFailed", message: "The control plane cannot proceed until its dependencies are available."}
	var typedFailure *controlPlaneFailure
	if failureValue, ok := errors.AsType[*controlPlaneFailure](report); ok {
		typedFailure = failureValue
		failure = typedFailure
	}
	original := cp.DeepCopy()
	setCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneReadyCondition, metav1.ConditionFalse, failure.reason, failure.message, cp.Generation)
	setPausedCondition(&cp.Status.Conditions, false, cp.Generation)
	cp.Status.ObservedGeneration = cp.Generation
	if err := r.Status().Patch(ctx, cp, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func controlPlaneClusterName(cp *controlplanev1alpha1.TartControlPlane) (string, error) {
	if name := cp.Labels[clusterv1.ClusterNameLabel]; name != "" {
		return name, nil
	}
	for _, owner := range cp.OwnerReferences {
		if owner.APIVersion == clusterv1.GroupVersion.String() && owner.Kind == "Cluster" && owner.Name != "" {
			return owner.Name, nil
		}
	}
	return "", &controlPlaneFailure{
		reason:  reasonClusterUnavailable,
		message: "The TartControlPlane has no reference to a CAPI Cluster.",
	}
}

func desiredControlPlaneReplicas(cp *controlplanev1alpha1.TartControlPlane) (int32, error) {
	if cp.Spec.Replicas == nil {
		return 1, nil
	}
	if *cp.Spec.Replicas < 0 {
		return 0, &controlPlaneFailure{
			reason:  "InvalidSpec",
			message: "The control-plane replica count cannot be negative.",
		}
	}
	return *cp.Spec.Replicas, nil
}

func (r *TartControlPlaneReconciler) getTartCluster(ctx context.Context, cluster *clusterv1.Cluster) (*infrav1alpha1.TartCluster, error) {
	ref := cluster.Spec.InfrastructureRef
	if ref.APIGroup != infrav1alpha1.GroupVersion.Group || ref.Kind != "TartCluster" || ref.Name == "" {
		return nil, &controlPlaneFailure{
			reason:  reasonClusterUnavailable,
			message: "The CAPI Cluster does not reference a TartCluster infrastructure resource.",
		}
	}
	var tartCluster infrav1alpha1.TartCluster
	if err := r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: ref.Name}, &tartCluster); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, &controlPlaneFailure{
				reason:  reasonClusterUnavailable,
				message: "The referenced TartCluster is not available yet.",
			}
		}
		return nil, err
	}
	if tartCluster.Spec.ID == "" || tartCluster.Status.ActiveSecretGeneration < 1 {
		return nil, &controlPlaneFailure{
			reason:  reasonSecretBundleUnavailable,
			message: "The TartCluster identity and active secret bundle are not ready yet.",
		}
	}
	return &tartCluster, nil
}

func (r *TartControlPlaneReconciler) validateActiveBundle(ctx context.Context, cluster *infrav1alpha1.TartCluster) error {
	generation := cluster.Status.ActiveSecretGeneration
	name, err := controlplane.BundleName(cluster.Name, cluster.Spec.ID, generation)
	if err != nil {
		return &controlPlaneFailure{
			reason:  reasonSecretBundleUnavailable,
			message: "The active cluster secret bundle identity is invalid.",
		}
	}
	var secret corev1.Secret
	if err := r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: name}, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			return &controlPlaneFailure{
				reason:  reasonSecretBundleUnavailable,
				message: "The active cluster secret bundle is not available yet.",
			}
		}
		return err
	}
	if err := controlplane.ValidateBundleSecretContract(&secret, cluster.Namespace, cluster.Name, cluster.Spec.ID, generation, controlplane.BundleStateActive, cluster.UID); err != nil {
		return &controlPlaneFailure{
			reason:  reasonSecretBundleUnavailable,
			message: "The active cluster secret bundle does not satisfy its identity contract.",
		}
	}
	if err := controlplane.ValidateBundleData(secret.Data, cluster.Spec.ID); err != nil {
		return &controlPlaneFailure{
			reason:  reasonSecretBundleUnavailable,
			message: "The active cluster secret bundle data is invalid.",
		}
	}
	return nil
}

func (r *TartControlPlaneReconciler) getTartMachineTemplate(ctx context.Context, namespace string, ref *clusterv1.ContractVersionedObjectReference, template *infrav1alpha1.TartMachineTemplate) error {
	if ref.APIGroup != infrav1alpha1.GroupVersion.Group || ref.Kind != "TartMachineTemplate" || ref.Name == "" {
		return &controlPlaneFailure{
			reason:  "MachineTemplateInvalid",
			message: "The control-plane infrastructureRef must reference a TartMachineTemplate.",
		}
	}
	if err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: ref.Name}, template); err != nil {
		if apierrors.IsNotFound(err) {
			return &controlPlaneFailure{
				reason:  "MachineTemplateUnavailable",
				message: "The referenced TartMachineTemplate is not available yet.",
			}
		}
		return err
	}
	return nil
}

func (r *TartControlPlaneReconciler) getBootstrapTemplate(ctx context.Context, namespace string, ref *corev1.ObjectReference, template *bootstrapv1alpha1.TartBootstrapConfigTemplate) error {
	if ref.APIVersion != bootstrapv1alpha1.GroupVersion.String() || ref.Kind != "TartBootstrapConfigTemplate" || ref.Name == "" {
		return &controlPlaneFailure{
			reason:  reasonBootstrapTemplateInvalid,
			message: "The bootstrapConfigTemplate must reference a TartBootstrapConfigTemplate.",
		}
	}
	if err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: ref.Name}, template); err != nil {
		if apierrors.IsNotFound(err) {
			return &controlPlaneFailure{
				reason:  "BootstrapTemplateUnavailable",
				message: "The referenced TartBootstrapConfigTemplate is not available yet.",
			}
		}
		return err
	}
	return nil
}

func (r *TartControlPlaneReconciler) ensureMachines(ctx context.Context, cp *controlplanev1alpha1.TartControlPlane, clusterName string, desired int32, machineTemplate *infrav1alpha1.TartMachineTemplate, bootstrapTemplate *bootstrapv1alpha1.TartBootstrapConfigTemplate) ([]clusterv1.Machine, error) {
	var list clusterv1.MachineList
	if err := r.List(ctx, &list, client.InNamespace(cp.Namespace), client.MatchingLabels{
		clusterv1.ClusterNameLabel:             clusterName,
		clusterv1.MachineControlPlaneNameLabel: cp.Name,
	}); err != nil {
		return nil, err
	}
	byName := make(map[string]*clusterv1.Machine, len(list.Items))
	for i := range list.Items {
		byName[list.Items[i].Name] = &list.Items[i]
	}

	for ordinal := range int(desired) {
		ordinal32 := int32(ordinal)
		machineName, err := controlPlaneChildName(cp.Name, ordinal32, "")
		if err != nil {
			return nil, &controlPlaneFailure{reason: reasonMachineNameInvalid, message: "A deterministic control-plane Machine name is invalid."}
		}
		machine, ok := byName[machineName]
		if !ok {
			machine, err = r.createMachine(ctx, cp, clusterName, ordinal32, machineName, bootstrapTemplate)
			if err != nil {
				return nil, err
			}
			byName[machine.Name] = machine
		}
		bootstrapName, nameErr := bootstrapConfigName(cp.Name, ordinal32)
		if nameErr != nil {
			return nil, &controlPlaneFailure{reason: reasonMachineNameInvalid, message: bootstrapConfigNameInvalidMessage}
		}
		if err := validateMachineReference(machine, cp, clusterName, machineName, bootstrapName, cp.Spec.Version); err != nil {
			return nil, err
		}
		if err := r.ensureProviderResources(ctx, cp, clusterName, ordinal32, machine, machineTemplate, bootstrapTemplate); err != nil {
			return nil, err
		}
	}

	var refreshed clusterv1.MachineList
	if err := r.List(ctx, &refreshed, client.InNamespace(cp.Namespace), client.MatchingLabels{
		clusterv1.ClusterNameLabel:             clusterName,
		clusterv1.MachineControlPlaneNameLabel: cp.Name,
	}); err != nil {
		return nil, err
	}
	return refreshed.Items, nil
}

func (r *TartControlPlaneReconciler) reconcileControlPlaneBootstrap(ctx context.Context, cp *controlplanev1alpha1.TartControlPlane, machines []clusterv1.Machine) (controlPlaneBootstrapState, error) {
	state := controlPlaneBootstrapState{
		reason:       "MachinesUnavailable",
		message:      "The first control-plane Machine is not running the desired Talos version yet.",
		requeueAfter: 30 * time.Second,
	}
	if cp.Status.Initialization.ControlPlaneInitialized != nil && *cp.Status.Initialization.ControlPlaneInitialized {
		return controlPlaneBootstrapState{
			initialized: true,
			etcdReady:   true,
			reason:      "EtcdClusterAvailable",
			message:     "The control-plane etcd bootstrap has been observed.",
		}, nil
	}
	firstName, err := controlPlaneChildName(cp.Name, 0, "")
	if err != nil {
		return controlPlaneBootstrapState{}, &controlPlaneFailure{reason: reasonMachineNameInvalid, message: "A deterministic control-plane Machine name is invalid."}
	}
	var firstMachine *clusterv1.Machine
	for index := range machines {
		if machines[index].Name == firstName {
			firstMachine = &machines[index]
			break
		}
	}
	if firstMachine == nil {
		return state, nil
	}

	var providerMachine infrav1alpha1.TartMachine
	if err := r.Get(ctx, client.ObjectKey{Namespace: cp.Namespace, Name: firstMachine.Name}, &providerMachine); err != nil {
		if apierrors.IsNotFound(err) {
			return state, nil
		}
		return controlPlaneBootstrapState{}, err
	}
	ready := meta.FindStatusCondition(providerMachine.Status.Conditions, infrav1alpha1.TartMachineReadyCondition)
	if ready == nil || ready.Status != metav1.ConditionTrue {
		return state, nil
	}
	if providerMachine.Status.HostRef == nil {
		state.reason = "HostUnavailable"
		state.message = "The first control-plane Machine has no observed TartHost binding yet."
		return state, nil
	}

	var providerHost infrav1alpha1.TartHost
	if err := r.Get(ctx, client.ObjectKey{Name: providerMachine.Status.HostRef.Name}, &providerHost); err != nil {
		if apierrors.IsNotFound(err) {
			state.reason = "HostUnavailable"
			state.message = "The first control-plane Machine Host is not available yet."
			return state, nil
		}
		return controlPlaneBootstrapState{}, err
	}
	endpoint := hostTalosEndpoint(&providerHost)
	if endpoint == "" {
		state.reason = "EndpointUnavailable"
		state.message = "The first control-plane Machine has no reachable Talos endpoint yet."
		return state, nil
	}
	configuration, err := (&TartMachineReconciler{Client: r.Client}).bootstrapConfiguration(ctx, &providerMachine)
	if err != nil {
		if errors.Is(err, errBootstrapDataUnavailable) {
			state.reason = "BootstrapDataUnavailable"
			state.message = "The immutable Bootstrap Secret is not available for the first control-plane Machine yet."
			return state, nil
		}
		return controlPlaneBootstrapState{}, err
	}

	authenticated, err := talos.DialAuthenticatedFromConfiguration(ctx, endpoint, configuration)
	if err != nil {
		state.reason = "TalosUnavailable"
		state.message = "The authenticated Talos API is not reachable on the first control-plane Machine."
		return state, nil //nolint:nilerr // an unavailable node is a normal reconcile observation.
	}
	etcdStatus, etcdErr := authenticated.EtcdStatus(ctx)
	if etcdErr == nil && etcdStatus.MemberID != 0 && etcdStatus.Leader != 0 && len(etcdStatus.Errors) == 0 {
		if closeErr := authenticated.Close(); closeErr != nil {
			ctrl.LoggerFrom(ctx).Error(closeErr, "close authenticated Talos client")
		}
		return controlPlaneBootstrapState{
			initialized: true,
			etcdReady:   true,
			reason:      "EtcdClusterAvailable",
			message:     "The first control-plane Machine reports a healthy etcd member and leader.",
		}, nil
	}

	bootstrapErr := authenticated.Bootstrap(ctx)
	if closeErr := authenticated.Close(); closeErr != nil {
		ctrl.LoggerFrom(ctx).Error(closeErr, "close authenticated Talos client")
	}
	if bootstrapErr != nil && status.Code(bootstrapErr) != codes.AlreadyExists {
		return controlPlaneBootstrapState{
			reason:       "EtcdBootstrapFailed",
			message:      "The first control-plane Machine could not start the Talos etcd bootstrap.",
			requeueAfter: 30 * time.Second,
		}, nil
	}
	return controlPlaneBootstrapState{
		reason:       "Bootstrapping",
		message:      "Talos etcd bootstrap was requested; waiting for a healthy member and leader.",
		requeueAfter: 15 * time.Second,
	}, nil
}

func (r *TartControlPlaneReconciler) createMachine(ctx context.Context, cp *controlplanev1alpha1.TartControlPlane, clusterName string, ordinal int32, name string, bootstrapTemplate *bootstrapv1alpha1.TartBootstrapConfigTemplate) (*clusterv1.Machine, error) {
	bootstrapName, err := bootstrapConfigName(cp.Name, ordinal)
	if err != nil {
		return nil, &controlPlaneFailure{reason: reasonMachineNameInvalid, message: bootstrapConfigNameInvalidMessage}
	}
	objectLabels, annotations := controlPlaneMetadata(cp.Spec.MachineTemplate.ObjectMeta, clusterName, cp.Name, ordinal)
	machine := &clusterv1.Machine{
		APIVersion:      clusterv1.GroupVersion.String(),
		Kind:            "Machine",
		Name:            name,
		Namespace:       cp.Namespace,
		Labels:          objectLabels,
		Annotations:     annotations,
		OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(cp, controlplanev1alpha1.GroupVersion.String(), tartControlPlaneKind)},
		Spec: clusterv1.MachineSpec{
			ClusterName: clusterName,
			Version:     cp.Spec.Version,
			Bootstrap: clusterv1.Bootstrap{ConfigRef: clusterv1.ContractVersionedObjectReference{
				APIGroup: bootstrapv1alpha1.GroupVersion.Group,
				Kind:     tartBootstrapConfigKind,
				Name:     bootstrapName,
			}},
			InfrastructureRef: clusterv1.ContractVersionedObjectReference{
				APIGroup: infrav1alpha1.GroupVersion.Group,
				Kind:     tartMachineKind,
				Name:     name,
			},
		},
	}
	if bootstrapTemplate.Spec.Template.Spec.ConfigSecretRef == nil {
		return nil, &controlPlaneFailure{reason: reasonBootstrapTemplateInvalid, message: "The BootstrapConfigTemplate has no configuration Secret reference."}
	}
	if err := r.Create(ctx, machine); err != nil && !apierrors.IsAlreadyExists(err) {
		return nil, err
	}
	var current clusterv1.Machine
	if err := r.Get(ctx, client.ObjectKey{Namespace: cp.Namespace, Name: name}, &current); err != nil {
		return nil, err
	}
	return &current, nil
}

func (r *TartControlPlaneReconciler) ensureProviderResources(ctx context.Context, cp *controlplanev1alpha1.TartControlPlane, clusterName string, ordinal int32, machine *clusterv1.Machine, machineTemplate *infrav1alpha1.TartMachineTemplate, bootstrapTemplate *bootstrapv1alpha1.TartBootstrapConfigTemplate) error {
	if machine.UID == "" {
		return &controlPlaneFailure{reason: "MachineIdentityUnavailable", message: "The CAPI Machine has no UID yet; provider resources are not created."}
	}
	machineName := machine.Name
	providerLabels, _ := controlPlaneMetadata(machineTemplate.Spec.Template.ObjectMeta, clusterName, cp.Name, ordinal)
	expectedMachine := &infrav1alpha1.TartMachine{
		APIVersion:      infrav1alpha1.GroupVersion.String(),
		Kind:            tartMachineKind,
		Name:            machineName,
		Namespace:       cp.Namespace,
		Labels:          providerLabels,
		OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(machine, clusterv1.GroupVersion.String(), "Machine")},
		Spec: infrav1alpha1.TartMachineSpec{
			HostSelector: machineTemplate.Spec.Template.Spec.HostSelector.DeepCopy(),
			TalosImage:   machineTemplate.Spec.Template.Spec.TalosImage,
		},
	}
	var tartMachine infrav1alpha1.TartMachine
	if err := r.Get(ctx, client.ObjectKey{Namespace: cp.Namespace, Name: machineName}, &tartMachine); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		if err := r.Create(ctx, expectedMachine); err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
		if err := r.Get(ctx, client.ObjectKey{Namespace: cp.Namespace, Name: machineName}, &tartMachine); err != nil {
			return err
		}
	}
	if err := validateProviderOwner(&tartMachine, machine, infrav1alpha1.GroupVersion.String(), tartMachineKind); err != nil {
		return err
	}
	if !reflect.DeepEqual(tartMachine.Spec.HostSelector, expectedMachine.Spec.HostSelector) || tartMachine.Spec.TalosImage != expectedMachine.Spec.TalosImage {
		return &controlPlaneFailure{reason: "MachineSpecMismatch", message: "The existing TartMachine does not match its immutable control-plane template."}
	}

	bootstrapName, err := bootstrapConfigName(cp.Name, ordinal)
	if err != nil {
		return &controlPlaneFailure{reason: reasonMachineNameInvalid, message: bootstrapConfigNameInvalidMessage}
	}
	bootstrapLabels, _ := controlPlaneMetadata(bootstrapTemplate.Spec.Template.ObjectMeta, clusterName, cp.Name, ordinal)
	expectedBootstrap := &bootstrapv1alpha1.TartBootstrapConfig{
		APIVersion:      bootstrapv1alpha1.GroupVersion.String(),
		Kind:            tartBootstrapConfigKind,
		Name:            bootstrapName,
		Namespace:       cp.Namespace,
		Labels:          bootstrapLabels,
		OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(machine, clusterv1.GroupVersion.String(), "Machine")},
		Spec: bootstrapv1alpha1.TartBootstrapConfigSpec{
			ConfigSecretRef: bootstrapTemplate.Spec.Template.Spec.ConfigSecretRef.DeepCopy(),
		},
	}
	var bootstrapConfig bootstrapv1alpha1.TartBootstrapConfig
	if err := r.Get(ctx, client.ObjectKey{Namespace: cp.Namespace, Name: bootstrapName}, &bootstrapConfig); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		if err := r.Create(ctx, expectedBootstrap); err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
		if err := r.Get(ctx, client.ObjectKey{Namespace: cp.Namespace, Name: bootstrapName}, &bootstrapConfig); err != nil {
			return err
		}
	}
	if err := validateProviderOwner(&bootstrapConfig, machine, clusterv1.GroupVersion.String(), tartBootstrapConfigKind); err != nil {
		return err
	}
	if bootstrapConfig.Spec.ConfigSecretRef == nil || expectedBootstrap.Spec.ConfigSecretRef == nil || bootstrapConfig.Spec.ConfigSecretRef.Name != expectedBootstrap.Spec.ConfigSecretRef.Name {
		return &controlPlaneFailure{reason: "BootstrapConfigMismatch", message: "The existing TartBootstrapConfig does not match its immutable template."}
	}
	return nil
}

func validateMachineReference(machine *clusterv1.Machine, cp *controlplanev1alpha1.TartControlPlane, clusterName, machineName, bootstrapName, version string) error {
	if !hasControllerOwner(machine, cp, controlplanev1alpha1.GroupVersion.String(), tartControlPlaneKind) || machine.Spec.ClusterName != clusterName || machine.Spec.Version != version || machine.Spec.InfrastructureRef.APIGroup != infrav1alpha1.GroupVersion.Group || machine.Spec.InfrastructureRef.Kind != tartMachineKind || machine.Spec.InfrastructureRef.Name != machineName || machine.Spec.Bootstrap.ConfigRef.APIGroup != bootstrapv1alpha1.GroupVersion.Group || machine.Spec.Bootstrap.ConfigRef.Kind != tartBootstrapConfigKind || machine.Spec.Bootstrap.ConfigRef.Name != bootstrapName {
		return &controlPlaneFailure{reason: "MachineSpecMismatch", message: "The existing control-plane Machine does not match the TartControlPlane references."}
	}
	return nil
}

func validateProviderOwner(object metav1.Object, machine *clusterv1.Machine, apiVersion, kind string) error {
	if len(object.GetOwnerReferences()) != 1 || !hasControllerOwner(object, machine, apiVersion, kind) {
		return &controlPlaneFailure{reason: "MachineOwnershipMismatch", message: "A provider resource is not owned by its corresponding CAPI Machine."}
	}
	return nil
}

func hasControllerOwner(object metav1.Object, owner metav1.Object, apiVersion, kind string) bool {
	for _, reference := range object.GetOwnerReferences() {
		if reference.APIVersion == apiVersion && reference.Kind == kind && reference.Name == owner.GetName() && reference.UID == owner.GetUID() && reference.Controller != nil && *reference.Controller {
			return true
		}
	}
	return false
}

func controllerOwnerReference(owner metav1.Object, apiVersion, kind string) metav1.OwnerReference {
	controller := true
	blockOwnerDeletion := true
	return metav1.OwnerReference{
		APIVersion:         apiVersion,
		Kind:               kind,
		Name:               owner.GetName(),
		UID:                owner.GetUID(),
		Controller:         &controller,
		BlockOwnerDeletion: &blockOwnerDeletion,
	}
}

func controlPlaneMetadata(template clusterv1.ObjectMeta, clusterName, controlPlaneName string, ordinal int32) (map[string]string, map[string]string) {
	objectLabels := maps.Clone(template.Labels)
	if objectLabels == nil {
		objectLabels = make(map[string]string)
	}
	objectLabels[clusterv1.ClusterNameLabel] = clusterName
	objectLabels[clusterv1.MachineControlPlaneLabel] = ""
	objectLabels[clusterv1.MachineControlPlaneNameLabel] = controlPlaneName
	objectLabels[controlPlaneOrdinalLabel] = strconv.FormatInt(int64(ordinal), 10)

	annotations := maps.Clone(template.Annotations)
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[clusterv1.MachineSkipRemediationAnnotation] = "true"
	return objectLabels, annotations
}

func controlPlaneChildName(controlPlaneName string, ordinal int32, suffix string) (string, error) {
	name := controlPlaneName + "-" + strconv.FormatInt(int64(ordinal), 10)
	if suffix != "" {
		name += "-" + suffix
	}
	if len(validation.IsDNS1123Subdomain(name)) != 0 {
		return "", fmt.Errorf("invalid control-plane child name")
	}
	return name, nil
}

func bootstrapConfigName(controlPlaneName string, ordinal int32) (string, error) {
	return controlPlaneChildName(controlPlaneName, ordinal, "bootstrap")
}

func setControlPlaneStatus(cp *controlplanev1alpha1.TartControlPlane, clusterName string, desired int32, machines []clusterv1.Machine, bootstrapState controlPlaneBootstrapState) {
	actual := int32(len(machines))
	ready := countMachineCondition(machines, clusterv1.MachineReadyCondition)
	available := countMachineCondition(machines, clusterv1.MachineAvailableCondition)
	upToDate := countMachineCondition(machines, clusterv1.MachineUpToDateCondition)
	cp.Status.Selector = labels.SelectorFromSet(labels.Set{
		clusterv1.ClusterNameLabel:             clusterName,
		clusterv1.MachineControlPlaneNameLabel: cp.Name,
	}).String()
	cp.Status.Replicas = new(actual)
	cp.Status.ReadyReplicas = new(ready)
	cp.Status.AvailableReplicas = new(available)
	cp.Status.UpToDateReplicas = new(upToDate)
	cp.Status.Versions = machineVersions(machines)
	if bootstrapState.initialized {
		cp.Status.Initialization.ControlPlaneInitialized = new(true)
	} else if cp.Status.Initialization.ControlPlaneInitialized == nil {
		cp.Status.Initialization.ControlPlaneInitialized = new(false)
	}

	setCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneReadyCondition, metav1.ConditionFalse, "WorkloadAPIUnavailable", "Talos control-plane bootstrap is observed, but workload Kubernetes API readiness is not observed yet.", cp.Generation)
	setCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneAvailableCondition, metav1.ConditionFalse, "WorkloadAPIUnavailable", "Workload Kubernetes API availability is not observed yet.", cp.Generation)
	setCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneUpToDateCondition, metav1.ConditionFalse, "ConfigurationUnavailable", "Effective control-plane configuration and workload API state are not observed yet.", cp.Generation)
	setCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneRollingOutCondition, metav1.ConditionFalse, "NotRollingOut", "The control plane is not performing a rollout.", cp.Generation)
	setCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneScalingUpCondition, conditionStatus(actual < desired), scalingReason(actual < desired, "ScalingUp"), scalingMessage(actual < desired, "Control-plane Machines are being created.", "The desired control-plane replica count is satisfied."), cp.Generation)
	setCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneScalingDownCondition, conditionStatus(actual > desired), scalingReason(actual > desired, "ScalingDown"), scalingMessage(actual > desired, "Control-plane scale-down requires the not-yet-implemented etcd safety path.", "The desired control-plane replica count is not above the observed count."), cp.Generation)
	setCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneMachinesReadyCondition, conditionStatus(desired > 0 && ready == desired), machineReadinessReason(desired > 0 && ready == desired), machineReadinessMessage(desired > 0 && ready == desired), cp.Generation)
	setCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneMachinesUpToDateCondition, conditionStatus(desired > 0 && upToDate == desired), machineUpToDateReason(desired > 0 && upToDate == desired), machineUpToDateMessage(desired > 0 && upToDate == desired), cp.Generation)
	setCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneEtcdClusterAvailableCond, conditionStatus(bootstrapState.etcdReady), bootstrapState.reason, bootstrapState.message, cp.Generation)
	setCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneDeletingCondition, metav1.ConditionFalse, "NotDeleting", "The control plane is not being deleted.", cp.Generation)
	setPausedCondition(&cp.Status.Conditions, false, cp.Generation)
	cp.Status.ObservedGeneration = cp.Generation
}

func countMachineCondition(machines []clusterv1.Machine, conditionType string) int32 {
	var count int32
	for i := range machines {
		condition := meta.FindStatusCondition(machines[i].Status.Conditions, conditionType)
		if condition != nil && condition.Status == metav1.ConditionTrue {
			count++
		}
	}
	return count
}

func machineVersions(machines []clusterv1.Machine) []clusterv1.StatusVersion {
	counts := make(map[string]int32)
	for i := range machines {
		if machines[i].Status.NodeInfo != nil && machines[i].Status.NodeInfo.KubeletVersion != "" {
			counts[machines[i].Status.NodeInfo.KubeletVersion]++
		}
	}
	versions := make([]string, 0, len(counts))
	for version := range counts {
		versions = append(versions, version)
	}
	slices.Sort(versions)
	result := make([]clusterv1.StatusVersion, 0, len(versions))
	for _, version := range versions {
		result = append(result, clusterv1.StatusVersion{Version: version, Replicas: counts[version]})
	}
	return result
}

func conditionStatus(value bool) metav1.ConditionStatus {
	if value {
		return metav1.ConditionTrue
	}
	return metav1.ConditionFalse
}

func scalingReason(value bool, activeReason string) string {
	if value {
		return activeReason
	}
	return "NotScaling"
}

func scalingMessage(value bool, activeMessage, inactiveMessage string) string {
	if value {
		return activeMessage
	}
	return inactiveMessage
}

func machineReadinessReason(ready bool) string {
	if ready {
		return "MachinesReady"
	}
	return "MachinesNotReady"
}

func machineReadinessMessage(ready bool) string {
	if ready {
		return "All desired control-plane Machines report Ready."
	}
	return "Not all desired control-plane Machines report Ready."
}

func machineUpToDateReason(upToDate bool) string {
	if upToDate {
		return "MachinesUpToDate"
	}
	return "MachinesNotUpToDate"
}

func machineUpToDateMessage(upToDate bool) string {
	if upToDate {
		return "All desired control-plane Machines report UpToDate."
	}
	return "Not all desired control-plane Machines report UpToDate."
}

func (r *TartControlPlaneReconciler) enqueueAllControlPlanes(ctx context.Context, _ client.Object) []reconcile.Request {
	var list controlplanev1alpha1.TartControlPlaneList
	if err := r.List(ctx, &list); err != nil {
		return nil
	}
	requests := make([]reconcile.Request, 0, len(list.Items))
	for i := range list.Items {
		requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&list.Items[i])})
	}
	return requests
}

func (r *TartControlPlaneReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&controlplanev1alpha1.TartControlPlane{}).
		Owns(&clusterv1.Machine{}).
		Watches(&infrav1alpha1.TartMachine{}, handler.EnqueueRequestsFromMapFunc(r.enqueueAllControlPlanes)).
		Watches(&bootstrapv1alpha1.TartBootstrapConfig{}, handler.EnqueueRequestsFromMapFunc(r.enqueueAllControlPlanes)).
		Watches(&infrav1alpha1.TartCluster{}, handler.EnqueueRequestsFromMapFunc(r.enqueueAllControlPlanes)).
		Watches(&infrav1alpha1.TartMachineTemplate{}, handler.EnqueueRequestsFromMapFunc(r.enqueueAllControlPlanes)).
		Watches(&bootstrapv1alpha1.TartBootstrapConfigTemplate{}, handler.EnqueueRequestsFromMapFunc(r.enqueueAllControlPlanes)).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(r.enqueueAllControlPlanes)).
		Named("tartcontrolplane").
		Complete(r)
}
