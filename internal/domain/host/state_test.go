package host

import (
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
)

func TestReserveStatus(t *testing.T) {
	t.Parallel()

	machine := &infrastructurev1alpha1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "machine-a",
			Namespace: "default",
			UID:       types.UID("machine-a-uid"),
		},
	}

	tests := []struct {
		name    string
		host    *infrastructurev1alpha1.TartHost
		wantErr error
	}{
		{
			name: "available host can be reserved",
			host: &infrastructurev1alpha1.TartHost{
				ObjectMeta: metav1.ObjectMeta{Name: "host-a", Namespace: "default", Generation: 3},
				Status: infrastructurev1alpha1.TartHostStatus{
					State: infrastructurev1alpha1.TartHostStateAvailable,
				},
			},
		},
		{
			name: "available state with machine reference is illegal",
			host: &infrastructurev1alpha1.TartHost{
				Status: infrastructurev1alpha1.TartHostStatus{
					State:      infrastructurev1alpha1.TartHostStateAvailable,
					MachineRef: &corev1.ObjectReference{Name: "other-machine", Namespace: "default"},
				},
			},
			wantErr: ErrIllegalHostState,
		},
		{
			name: "reserved host cannot be reserved again",
			host: &infrastructurev1alpha1.TartHost{
				Status: infrastructurev1alpha1.TartHostStatus{
					State:      infrastructurev1alpha1.TartHostStateReserved,
					MachineRef: &corev1.ObjectReference{Name: "other-machine", Namespace: "default"},
				},
			},
			wantErr: ErrHostNotAvailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ReserveStatus(tt.host, machine)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("ReserveStatus error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ReserveStatus returned error: %v", err)
			}
			if got.State != infrastructurev1alpha1.TartHostStateReserved {
				t.Fatalf("state = %s, want %s", got.State, infrastructurev1alpha1.TartHostStateReserved)
			}
			if got.MachineRef == nil || got.MachineRef.Name != machine.Name || got.MachineRef.UID != machine.UID {
				t.Fatalf("machine ref = %#v, want reference to machine", got.MachineRef)
			}
			if got.ObservedGeneration != tt.host.Generation {
				t.Fatalf("observedGeneration = %d, want %d", got.ObservedGeneration, tt.host.Generation)
			}
			condition := findCondition(got.Conditions, "Available")
			if condition == nil || condition.Status != metav1.ConditionFalse || condition.Reason != "Reserved" {
				t.Fatalf("available condition = %#v, want false Reserved", condition)
			}
		})
	}
}

func TestHostStatusTransitionsRequireAssignedHost(t *testing.T) {
	t.Parallel()

	host := &infrastructurev1alpha1.TartHost{
		ObjectMeta: metav1.ObjectMeta{Name: "host-a", Namespace: "default", Generation: 4},
		Status: infrastructurev1alpha1.TartHostStatus{
			State: infrastructurev1alpha1.TartHostStateReserved,
		},
	}

	if _, err := ProvisioningStatus(host); !errors.Is(err, ErrIllegalHostState) {
		t.Fatalf("ProvisioningStatus error = %v, want %v", err, ErrIllegalHostState)
	}

	host.Status.MachineRef = &corev1.ObjectReference{Name: "machine-a", Namespace: "default"}
	got, err := ProvisioningStatus(host)
	if err != nil {
		t.Fatalf("ProvisioningStatus returned error: %v", err)
	}
	if got.State != infrastructurev1alpha1.TartHostStateProvisioning {
		t.Fatalf("state = %s, want %s", got.State, infrastructurev1alpha1.TartHostStateProvisioning)
	}
}

func TestAvailableStatusClearsMachineSpecificFields(t *testing.T) {
	t.Parallel()

	host := &infrastructurev1alpha1.TartHost{
		ObjectMeta: metav1.ObjectMeta{Name: "host-a", Namespace: "default", Generation: 5},
		Status: infrastructurev1alpha1.TartHostStatus{
			State:      infrastructurev1alpha1.TartHostStateProvisioned,
			MachineRef: &corev1.ObjectReference{Name: "machine-a", Namespace: "default"},
		},
	}

	got := AvailableStatus(host, "Released", "Released from TartMachine default/machine-a")
	if got.State != infrastructurev1alpha1.TartHostStateAvailable {
		t.Fatalf("state = %s, want %s", got.State, infrastructurev1alpha1.TartHostStateAvailable)
	}
	if got.MachineRef != nil {
		t.Fatalf("machineRef = %#v, want nil", got.MachineRef)
	}
	condition := findCondition(got.Conditions, "Available")
	if condition == nil || condition.Status != metav1.ConditionTrue || condition.Reason != "Released" {
		t.Fatalf("available condition = %#v, want true Released", condition)
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
