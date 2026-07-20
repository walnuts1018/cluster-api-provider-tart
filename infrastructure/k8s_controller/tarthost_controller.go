package controller

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	capabilitydomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/capability"
	driverdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/driver"
	k8sdrivercapability "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/repository/k8s/drivercapability"
	k8sdrivertarget "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/repository/k8s/drivertarget"
	"github.com/walnuts1018/cluster-api-provider-tart/infrastructure/repository/k8s/v1beta1host"
	applicationdriver "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/service/driver"
	"github.com/walnuts1018/cluster-api-provider-tart/utils/telemetry"
)

type TartHostReconciler struct {
	client.Client
	DriverService *applicationdriver.Service
	HostService   *v1beta1host.Service
	TargetBuilder *k8sdrivertarget.Service
	Capability    *k8sdrivercapability.Service
	Recorder      events.EventRecorder
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *TartHostReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx, span := telemetry.Tracer.Start(ctx, "TartHost.Reconcile")
	span.SetAttributes(
		attribute.String("kubernetes.resource.name", req.Name),
		attribute.String("kubernetes.resource.namespace", req.Namespace),
	)
	defer span.End()

	var host infrastructurev1beta1.TartHost
	if err := r.Get(ctx, req.NamespacedName, &host); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if host.Status.Phase != "" && host.Status.Phase != infrastructurev1beta1.TartHostPhaseAvailable {
		// Only initialize when Phase is empty, or keep updating if Available but missing inventory.
		if host.Status.Phase != "" {
			return ctrl.Result{}, nil
		}
	}

	// Initialize Phase and Inventory if needed
	var needsUpdate bool
	current := host.DeepCopy()

	if current.Status.Phase == "" {
		// Capability Discovery
		target, err := r.TargetBuilder.Build(ctx, current)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("build targets for capability discovery: %w", err)
		}

		powerCaps, err := r.DriverService.DiscoverCapabilities(ctx, driverdomain.Name(current.Spec.Management.PowerDriver), target, applicationdriver.Invocation{})
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("discover capabilities for power driver %s: %w", current.Spec.Management.PowerDriver, err)
		}

		bootCaps, err := r.DriverService.DiscoverCapabilities(ctx, driverdomain.Name(current.Spec.Management.BootDriver), target, applicationdriver.Invocation{})
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("discover capabilities for boot driver %s: %w", current.Spec.Management.BootDriver, err)
		}

		allCaps := append(powerCaps.Values(), bootCaps.Values()...)
		mergedSet, _ := capabilitydomain.NewSet(allCaps...)
		values := mergedSet.Values()
		apiValues := make([]infrastructurev1beta1.Capability, 0, len(values))
		for _, value := range values {
			apiValues = append(apiValues, infrastructurev1beta1.Capability(value))
		}
		current.Status.Capabilities = apiValues
		needsUpdate = true
	}

	if current.Status.Inventory.RootDisk.SizeBytes == 0 && current.Spec.RootDeviceHints.MinSizeBytes > 0 {
		current.Status.Inventory.RootDisk = infrastructurev1beta1.ObservedDisk{
			DeviceName:   current.Spec.RootDeviceHints.DeviceName,
			SerialNumber: current.Spec.RootDeviceHints.SerialNumber,
			SizeBytes:    current.Spec.RootDeviceHints.MinSizeBytes,
		}
		needsUpdate = true
	}

	if current.Status.Phase == "" {
		needsUpdate = true
	}

	if needsUpdate {
		if err := r.Client.Status().Update(ctx, current); err != nil {
			return ctrl.Result{}, fmt.Errorf("update TartHost status: %w", err)
		}
		// Now mark it available properly using HostService to handle phase transitions and conditions
		if current.Status.Phase == "" {
			if err := r.HostService.MarkHostAvailable(ctx, current); err != nil {
				return ctrl.Result{}, fmt.Errorf("mark host available: %w", err)
			}
		}
	}

	return ctrl.Result{}, nil
}

func (r *TartHostReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Recorder == nil {
		r.Recorder = mgr.GetEventRecorder("tarthost")
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrastructurev1beta1.TartHost{}).
		Named("tarthost").
		Complete(r)
}
