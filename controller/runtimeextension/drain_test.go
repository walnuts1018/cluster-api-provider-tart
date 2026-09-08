package runtimeextension

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestKubernetesVersionConverged(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		observed string
		desired  string
		want     bool
	}{
		"same version":          {observed: "v1.34.0", desired: "1.34.0", want: true},
		"whitespace and prefix": {observed: " v1.34.0 ", desired: " v1.34.0 ", want: true},
		"different version":     {observed: "v1.33.0", desired: "v1.34.0"},
		"empty desired":         {observed: "v1.34.0"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := kubernetesVersionConverged(tt.observed, tt.desired); got != tt.want {
				t.Fatalf("kubernetesVersionConverged(%q, %q) = %t, want %t", tt.observed, tt.desired, got, tt.want)
			}
		})
	}
}

func TestNodeKubernetesVersionConvergedWaitsForKubeletVersion(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/nodes" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		if err := json.MarshalWrite(writer, &corev1.NodeList{Items: []corev1.Node{{Spec: corev1.NodeSpec{ProviderID: "tart://host/test"}}}}); err != nil {
			t.Errorf("MarshalWrite() error = %v", err)
		}
	}))
	defer server.Close()

	kubeconfig, err := clientcmd.Write(clientcmdapi.Config{
		Clusters:       map[string]*clientcmdapi.Cluster{"cluster": {Server: server.URL}},
		AuthInfos:      map[string]*clientcmdapi.AuthInfo{"user": {}},
		Contexts:       map[string]*clientcmdapi.Context{"context": {Cluster: "cluster", AuthInfo: "user"}},
		CurrentContext: "context",
	})
	if err != nil {
		t.Fatalf("clientcmd.Write() error = %v", err)
	}

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(core) error = %v", err)
	}
	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&corev1.Secret{
		Namespace: "ns",
		Name:      "cluster-a-kubeconfig",
		Data:      map[string][]byte{"value": kubeconfig},
	}).Build()
	machine := &clusterv1.Machine{Namespace: "ns", Spec: clusterv1.MachineSpec{ClusterName: "cluster-a"}}

	converged, message := nodeKubernetesVersionConverged(t.Context(), reader, machine, "tart://host/test", "v1.34.0")
	if converged {
		t.Fatal("nodeKubernetesVersionConverged() = true for Node without a kubelet version")
	}
	if message == "" {
		t.Fatal("nodeKubernetesVersionConverged() returned an empty retry message")
	}
}
