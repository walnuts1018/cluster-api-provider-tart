package agentsession

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	agentsessiondomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/agentsession"
)

func TestServicePersistsAndRestoresSession(t *testing.T) {
	ctx := context.Background()
	key := client.ObjectKey{Namespace: "default", Name: "operation"}
	k8sClient := newFakeClient(t, testOperation(key))
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)

	token, expiresAt, err := NewService(k8sClient, agentsessiondomain.DefaultTTL).
		Issue(ctx, key, "host-uid", "operation-uid", now)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if expiresAt != now.Add(agentsessiondomain.DefaultTTL) {
		t.Fatalf("expiresAt = %v", expiresAt)
	}

	// 新しいService instanceでもKubernetes Statusから認証状態を復元できる。
	restarted := NewService(k8sClient, agentsessiondomain.DefaultTTL)
	if err := restarted.Authenticate(ctx, key, token.BearerValue(), "host-uid", "operation-uid", now); err != nil {
		t.Fatalf("Authenticate() after restart error = %v", err)
	}
}

func TestServiceLocksAfterFiveFailures(t *testing.T) {
	ctx := context.Background()
	key := client.ObjectKey{Namespace: "default", Name: "operation"}
	k8sClient := newFakeClient(t, testOperation(key))
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	service := NewService(k8sClient, agentsessiondomain.DefaultTTL)
	token, _, err := service.Issue(ctx, key, "host-uid", "operation-uid", now)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	for range agentsessiondomain.MaximumAuthenticationFailures {
		if err := service.Authenticate(ctx, key, "invalid", "host-uid", "operation-uid", now); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("Authenticate(invalid) error = %v", err)
		}
	}
	if err := service.Authenticate(ctx, key, token.BearerValue(), "host-uid", "operation-uid", now); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Authenticate(correct after lock) error = %v", err)
	}
}

func TestServiceAllowsOneOfOneHundredConcurrentBootstrapClaims(t *testing.T) {
	ctx := context.Background()
	key := client.ObjectKey{Namespace: "default", Name: "operation"}
	k8sClient := newFakeClient(t, testOperation(key))
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	service := NewService(k8sClient, agentsessiondomain.DefaultTTL)
	token, _, err := service.Issue(ctx, key, "host-uid", "operation-uid", now)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	var successes atomic.Int32
	var wait sync.WaitGroup
	wait.Add(100)
	for range 100 {
		go func() {
			defer wait.Done()
			if claimErr := service.ClaimBootstrap(
				ctx,
				key,
				token.BearerValue(),
				"host-uid",
				"operation-uid",
				now,
			); claimErr == nil {
				successes.Add(1)
			}
		}()
	}
	wait.Wait()
	if got := successes.Load(); got != 1 {
		t.Fatalf("successful claims = %d, want 1", got)
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

func testOperation(key client.ObjectKey) *infrastructurev1beta1.TartHostOperation {
	return &infrastructurev1beta1.TartHostOperation{
		ObjectMeta: metav1.ObjectMeta{Namespace: key.Namespace, Name: key.Name},
		Spec: infrastructurev1beta1.TartHostOperationSpec{
			OperationID: "operation-uid",
			HostRef: infrastructurev1beta1.ResourceReference{
				Namespace: "default",
				Name:      "host",
				UID:       types.UID("host-uid"),
			},
		},
	}
}
