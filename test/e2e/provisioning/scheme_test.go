package provisioning

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
)

func TestNewSchemeRegistersBootstrapDependencies(t *testing.T) {
	t.Parallel()

	scheme := newScheme()

	if _, err := scheme.New(appsv1.SchemeGroupVersion.WithKind("Deployment")); err != nil {
		t.Fatalf("expected apps/v1 Deployment to be registered: %v", err)
	}

	if _, err := scheme.New(infrastructurev1alpha1.GroupVersion.WithKind("TartHost")); err != nil {
		t.Fatalf("expected infrastructure API to be registered: %v", err)
	}
}
