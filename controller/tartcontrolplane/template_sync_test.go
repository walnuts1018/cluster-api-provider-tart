package tartcontrolplane

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	bootstrapv1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/bootstrap/v1alpha1"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
)

func TestSyncMachineTemplatePropagatesOnlyPreviouslyOwnedImage(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := infrav1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	oldImage := infrav1alpha1.TalosImageSpec{Version: "v1.14.0", SchematicID: "old"}
	newImage := infrav1alpha1.TalosImageSpec{Version: "v1.14.1", SchematicID: "new"}
	current := &infrav1alpha1.TartMachine{
		Name: "machine", Namespace: "cluster", Annotations: map[string]string{lastAppliedMachineTemplateDigestAnnotation: machineImageDigest(oldImage)},
		Spec: infrav1alpha1.TartMachineSpec{Image: oldImage},
	}
	expected := &infrav1alpha1.TartMachine{Spec: infrav1alpha1.TartMachineSpec{Image: newImage}}
	reconciler := &TartControlPlaneReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(current).Build()}
	if err := reconciler.syncMachineTemplate(t.Context(), current, expected); err != nil {
		t.Fatalf("syncMachineTemplate() error = %v", err)
	}
	var observed infrav1alpha1.TartMachine
	if err := reconciler.Get(t.Context(), client.ObjectKey{Namespace: current.Namespace, Name: current.Name}, &observed); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if observed.Spec.Image != newImage || observed.Annotations[lastAppliedMachineTemplateDigestAnnotation] != machineImageDigest(newImage) {
		t.Fatalf("synced machine = %#v, want image and digest from template", observed)
	}
}

func TestSyncMachineTemplatePreservesIndividualImageOverride(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := infrav1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	oldImage := infrav1alpha1.TalosImageSpec{Version: "v1.14.0", SchematicID: "old"}
	individualImage := infrav1alpha1.TalosImageSpec{Version: "v1.14.0", SchematicID: "individual"}
	templateImage := infrav1alpha1.TalosImageSpec{Version: "v1.14.1", SchematicID: "new"}
	current := &infrav1alpha1.TartMachine{
		Name: "machine", Namespace: "cluster", Annotations: map[string]string{lastAppliedMachineTemplateDigestAnnotation: machineImageDigest(oldImage)},
		Spec: infrav1alpha1.TartMachineSpec{Image: individualImage},
	}
	expected := &infrav1alpha1.TartMachine{Spec: infrav1alpha1.TartMachineSpec{Image: templateImage}}
	reconciler := &TartControlPlaneReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(current).Build()}
	if err := reconciler.syncMachineTemplate(t.Context(), current, expected); err != nil {
		t.Fatalf("syncMachineTemplate() error = %v", err)
	}
	if current.Spec.Image != individualImage {
		t.Fatalf("syncMachineTemplate() changed individual image to %#v", current.Spec.Image)
	}
}

func TestSyncBootstrapTemplatePropagatesPolicyAndPatchReference(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := bootstrapv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	old := bootstrapv1alpha1.TartBootstrapConfigSpec{ConfigPatchesSecretRef: &corev1.LocalObjectReference{Name: "patch-old"}}
	desired := bootstrapv1alpha1.TartBootstrapConfigSpec{ConfigPatchesSecretRef: &corev1.LocalObjectReference{Name: "patch-new"}, UpdatePolicy: bootstrapv1alpha1.TartBootstrapConfigUpdatePolicy{Configuration: bootstrapv1alpha1.ConfigurationApplyStrategyApplyOnly}}
	current := &bootstrapv1alpha1.TartBootstrapConfig{
		Name: "bootstrap", Namespace: "cluster", Annotations: map[string]string{lastAppliedBootstrapTemplateDigestAnnotation: bootstrapConfigTemplateDigest(&old)},
		Spec: old,
	}
	expected := &bootstrapv1alpha1.TartBootstrapConfig{Spec: desired}
	reconciler := &TartControlPlaneReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(current).Build()}
	if err := reconciler.syncBootstrapTemplate(t.Context(), current, expected); err != nil {
		t.Fatalf("syncBootstrapTemplate() error = %v", err)
	}
	if current.Spec.ConfigPatchesSecretRef.Name != "patch-new" || current.Spec.UpdatePolicy.Configuration != bootstrapv1alpha1.ConfigurationApplyStrategyApplyOnly {
		t.Fatalf("syncBootstrapTemplate() spec = %#v, want desired template values", current.Spec)
	}
}
