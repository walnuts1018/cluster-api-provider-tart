package operation

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
)

func TestServiceStartAllowsOneConcurrentOperationPerHost(t *testing.T) {
	ctx := context.Background()
	k8sClient := newFakeClient(t)
	service := NewService(k8sClient)

	const goroutines = 100
	var successes atomic.Int32
	var activeConflicts atomic.Int32
	var unexpectedErrors atomic.Int32
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for index := range goroutines {
		go func() {
			defer wg.Done()
			operation := desiredOperation(fmt.Sprintf("00000000-0000-4000-8000-%012d", index))
			_, err := service.Start(ctx, operation)
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, ErrActiveOperation):
				activeConflicts.Add(1)
			default:
				unexpectedErrors.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := successes.Load(); got != 1 {
		t.Fatalf("successful starts = %d, want 1", got)
	}
	if got := activeConflicts.Load(); got != goroutines-1 {
		t.Fatalf("active conflicts = %d, want %d", got, goroutines-1)
	}
	if got := unexpectedErrors.Load(); got != 0 {
		t.Fatalf("unexpected errors = %d, want 0", got)
	}

	var operations infrastructurev1beta1.TartHostOperationList
	if err := k8sClient.List(ctx, &operations, client.InNamespace("default")); err != nil {
		t.Fatalf("list TartHostOperations: %v", err)
	}
	if len(operations.Items) != 1 {
		t.Fatalf("operation count = %d, want 1", len(operations.Items))
	}
}

func TestServiceStartIsIdempotentForSameOperation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	k8sClient := newFakeClient(t)
	service := NewService(k8sClient)
	desired := desiredOperation("0197d640-8d00-7a65-b67f-3f7c42a6935f")

	first, err := service.Start(ctx, desired)
	if err != nil {
		t.Fatalf("first Start() error = %v", err)
	}
	second, err := service.Start(ctx, desired)
	if err != nil {
		t.Fatalf("second Start() error = %v", err)
	}
	if first.Name != second.Name || first.Spec.OperationID != second.Spec.OperationID {
		t.Fatalf("idempotent result differs: first=%#v second=%#v", first, second)
	}
}

func TestServiceStartReplacesTerminalOperation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	old := desiredOperation("0197d640-8d00-7a65-b67f-3f7c42a6935f")
	name, err := operationdomain.ResourceName(string(old.Spec.HostRef.UID))
	if err != nil {
		t.Fatalf("ResourceName() error = %v", err)
	}
	old.Name = name
	old.UID = types.UID("old-operation-uid")
	old.ResourceVersion = "1"
	old.Status.Phase = infrastructurev1beta1.TartHostOperationPhaseSucceeded
	k8sClient := newFakeClient(t, old)

	desired := desiredOperation("0197d640-8d00-7a65-b67f-3f7c42a69360")
	started, err := NewService(k8sClient).Start(ctx, desired)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if started.Spec.OperationID != desired.Spec.OperationID {
		t.Fatalf("operationID = %q, want %q", started.Spec.OperationID, desired.Spec.OperationID)
	}

	current := &infrastructurev1beta1.TartHostOperation{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: "default", Name: name}, current); err != nil {
		t.Fatalf("get replacement TartHostOperation: %v", err)
	}
	if current.Spec.OperationID != desired.Spec.OperationID {
		t.Fatalf("persisted operationID = %q, want %q", current.Spec.OperationID, desired.Spec.OperationID)
	}
}

func newFakeClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := infrastructurev1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&infrastructurev1beta1.TartHostOperation{}).
		WithObjects(objects...).
		Build()
}

func desiredOperation(operationID string) *infrastructurev1beta1.TartHostOperation {
	return &infrastructurev1beta1.TartHostOperation{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
		Spec: infrastructurev1beta1.TartHostOperationSpec{
			OperationID: operationID,
			HostRef: infrastructurev1beta1.ResourceReference{
				Namespace: "default",
				Name:      "host-a",
				UID:       types.UID("host-a-uid"),
			},
		},
	}
}
