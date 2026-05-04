package machine

import (
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
)

func TestBeginProvisioningStatus(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)
	machine := &infrastructurev1alpha1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "machine-a", Namespace: "default", Generation: 7},
	}
	host := &infrastructurev1alpha1.TartHost{
		ObjectMeta: metav1.ObjectMeta{Name: "host-a", Namespace: "default", UID: types.UID("host-a-uid")},
	}

	got, err := BeginProvisioningStatus(machine, host, "token-a", now, 10*time.Minute)
	if err != nil {
		t.Fatalf("BeginProvisioningStatus returned error: %v", err)
	}
	if got.Ready {
		t.Fatal("ready = true, want false")
	}
	if got.HostRef == nil || got.HostRef.Name != host.Name || got.HostRef.UID != host.UID {
		t.Fatalf("hostRef = %#v, want reference to host", got.HostRef)
	}
	if got.BootstrapToken != "token-a" {
		t.Fatalf("bootstrapToken = %q, want token-a", got.BootstrapToken)
	}
	if got.ProvisioningStartTime == nil || !got.ProvisioningStartTime.Time.Equal(now) {
		t.Fatalf("provisioningStartTime = %#v, want %s", got.ProvisioningStartTime, now)
	}
	if got.TokenExpiresAt == nil || !got.TokenExpiresAt.Time.Equal(now.Add(10*time.Minute)) {
		t.Fatalf("tokenExpiresAt = %#v, want %s", got.TokenExpiresAt, now.Add(10*time.Minute))
	}
	condition := findCondition(got.Conditions, "HostReserved")
	if condition == nil || condition.Status != metav1.ConditionTrue || condition.Reason != "ProvisioningStarted" {
		t.Fatalf("host reserved condition = %#v, want true ProvisioningStarted", condition)
	}
}

func TestRetryExpiredTokenStatus(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)
	machine := &infrastructurev1alpha1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "machine-a", Namespace: "default", Generation: 8},
		Status: infrastructurev1alpha1.TartMachineStatus{
			HostRef:        &corev1.ObjectReference{Name: "host-a", Namespace: "default"},
			BootstrapToken: "old-token",
			TokenExpiresAt: &metav1.Time{Time: now.Add(-time.Second)},
		},
	}

	got, err := RetryExpiredTokenStatus(machine, "new-token", now, 10*time.Minute)
	if err != nil {
		t.Fatalf("RetryExpiredTokenStatus returned error: %v", err)
	}
	if got.Ready {
		t.Fatal("ready = true, want false")
	}
	if got.BootstrapToken != "new-token" {
		t.Fatalf("bootstrapToken = %q, want new-token", got.BootstrapToken)
	}
	if got.TokenExpiresAt == nil || !got.TokenExpiresAt.Time.Equal(now.Add(10*time.Minute)) {
		t.Fatalf("tokenExpiresAt = %#v, want %s", got.TokenExpiresAt, now.Add(10*time.Minute))
	}
	condition := findCondition(got.Conditions, "Provisioning")
	if condition == nil || condition.Status != metav1.ConditionFalse || condition.Reason != "TokenExpired" {
		t.Fatalf("provisioning condition = %#v, want false TokenExpired", condition)
	}
}

func TestRetryExpiredTokenStatusRejectsReadyMachineWithToken(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)
	machine := &infrastructurev1alpha1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "machine-a", Namespace: "default", Generation: 8},
		Status: infrastructurev1alpha1.TartMachineStatus{
			Ready:          true,
			HostRef:        &corev1.ObjectReference{Name: "host-a", Namespace: "default"},
			BootstrapToken: "old-token",
			TokenExpiresAt: &metav1.Time{Time: now.Add(-time.Second)},
		},
	}

	if _, err := RetryExpiredTokenStatus(machine, "new-token", now, 10*time.Minute); !errors.Is(err, ErrIllegalMachineState) {
		t.Fatalf("RetryExpiredTokenStatus error = %v, want %v", err, ErrIllegalMachineState)
	}
}

func TestReadyStatusConsumesProvisioningFields(t *testing.T) {
	t.Parallel()

	machine := &infrastructurev1alpha1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "machine-a", Namespace: "default", Generation: 9},
		Status: infrastructurev1alpha1.TartMachineStatus{
			HostRef:               &corev1.ObjectReference{Name: "host-a", Namespace: "default"},
			ProvisioningStartTime: &metav1.Time{Time: time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)},
		},
	}

	got, err := ReadyStatus(machine)
	if err != nil {
		t.Fatalf("ReadyStatus returned error: %v", err)
	}
	if !got.Ready {
		t.Fatal("ready = false, want true")
	}
	if got.ProvisioningStartTime != nil {
		t.Fatalf("provisioningStartTime = %#v, want nil", got.ProvisioningStartTime)
	}
	if got.HostRef == nil || got.HostRef.Name != "host-a" {
		t.Fatalf("hostRef = %#v, want preserved host ref", got.HostRef)
	}
}

func TestReadyStatusRejectsMachineWithToken(t *testing.T) {
	t.Parallel()

	machine := &infrastructurev1alpha1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "machine-a", Namespace: "default", Generation: 9},
		Status: infrastructurev1alpha1.TartMachineStatus{
			HostRef:        &corev1.ObjectReference{Name: "host-a", Namespace: "default"},
			BootstrapToken: "token-a",
			TokenExpiresAt: &metav1.Time{Time: time.Date(2026, 5, 4, 10, 10, 0, 0, time.UTC)},
		},
	}

	if _, err := ReadyStatus(machine); !errors.Is(err, ErrIllegalMachineState) {
		t.Fatalf("ReadyStatus error = %v, want %v", err, ErrIllegalMachineState)
	}
}

func TestBootstrapTokenConsumedStatus(t *testing.T) {
	t.Parallel()

	startedAt := metav1.Time{Time: time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)}
	expiresAt := metav1.Time{Time: time.Date(2026, 5, 4, 10, 10, 0, 0, time.UTC)}
	machine := &infrastructurev1alpha1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "machine-a", Namespace: "default", Generation: 10},
		Status: infrastructurev1alpha1.TartMachineStatus{
			Ready:                 false,
			HostRef:               &corev1.ObjectReference{Name: "host-a", Namespace: "default"},
			BootstrapToken:        "token-a",
			ProvisioningStartTime: &startedAt,
			TokenExpiresAt:        &expiresAt,
		},
	}

	got, err := BootstrapTokenConsumedStatus(machine)
	if err != nil {
		t.Fatalf("BootstrapTokenConsumedStatus returned error: %v", err)
	}
	if got.Ready {
		t.Fatal("ready = true, want false")
	}
	if got.BootstrapToken != "" {
		t.Fatalf("bootstrapToken = %q, want empty", got.BootstrapToken)
	}
	if got.TokenExpiresAt != nil {
		t.Fatalf("tokenExpiresAt = %#v, want nil", got.TokenExpiresAt)
	}
	if got.ProvisioningStartTime == nil || !got.ProvisioningStartTime.Time.Equal(startedAt.Time) {
		t.Fatalf("provisioningStartTime = %#v, want preserved start time", got.ProvisioningStartTime)
	}
	if got.HostRef == nil || got.HostRef.Name != "host-a" {
		t.Fatalf("hostRef = %#v, want preserved host ref", got.HostRef)
	}
}

func TestTokenExpired(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		expires *metav1.Time
		want    bool
	}{
		{name: "nil deadline is not expired"},
		{name: "past deadline is expired", expires: &metav1.Time{Time: now.Add(-time.Second)}, want: true},
		{name: "future deadline is not expired", expires: &metav1.Time{Time: now.Add(time.Second)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			machine := &infrastructurev1alpha1.TartMachine{
				Status: infrastructurev1alpha1.TartMachineStatus{TokenExpiresAt: tt.expires},
			}
			if got := TokenExpired(machine, now); got != tt.want {
				t.Fatalf("TokenExpired = %t, want %t", got, tt.want)
			}
		})
	}
}

func findCondition(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}
