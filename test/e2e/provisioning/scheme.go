package provisioning

import (
	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

func newScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()

	_ = clientgoscheme.AddToScheme(scheme)
	_ = infrastructurev1alpha1.AddToScheme(scheme)
	_ = clusterv1.AddToScheme(scheme)

	return scheme
}
