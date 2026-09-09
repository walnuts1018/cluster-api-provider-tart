package host

import (
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
)

func validConsumer() corev1.ObjectReference {
	return corev1.ObjectReference{
		APIVersion: "infrastructure.cluster.x-k8s.io/v1alpha1",
		Kind:       "TartMachine",
		Namespace:  "default",
		Name:       "machine-a",
		UID:        types.UID("uid-a"),
	}
}

func TestDecideClaim(t *testing.T) {
	t.Parallel()

	consumer := validConsumer()
	other := validConsumer()
	other.UID = types.UID("uid-b")

	tests := []struct {
		name       string
		spec       infrav1alpha1.TartHostSpec
		consumer   corev1.ObjectReference
		wantResult ClaimDecision
		wantErr    error
	}{
		{
			name:       "invalid consumer reference is rejected",
			spec:       infrav1alpha1.TartHostSpec{},
			consumer:   corev1.ObjectReference{},
			wantResult: ClaimNoop,
			wantErr:    ErrInvalidClaim,
		},
		{
			name:       "unclaimed host applies the claim",
			spec:       infrav1alpha1.TartHostSpec{},
			consumer:   consumer,
			wantResult: ClaimApply,
			wantErr:    nil,
		},
		{
			name:       "already claimed by the same consumer is a noop",
			spec:       infrav1alpha1.TartHostSpec{ConsumerRef: &consumer},
			consumer:   consumer,
			wantResult: ClaimNoop,
			wantErr:    nil,
		},
		{
			name:       "claimed by a different consumer conflicts",
			spec:       infrav1alpha1.TartHostSpec{ConsumerRef: &consumer},
			consumer:   other,
			wantResult: ClaimNoop,
			wantErr:    ErrClaimConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := DecideClaim(tt.spec, tt.consumer)
			if got != tt.wantResult {
				t.Errorf("DecideClaim() = %v, want %v", got, tt.wantResult)
			}
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("DecideClaim() error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("DecideClaim() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestDecideRetention(t *testing.T) {
	t.Parallel()

	consumer := validConsumer()
	previous := infrav1alpha1.PreviousConsumerRef{UID: consumer.UID}
	mismatchedPrevious := infrav1alpha1.PreviousConsumerRef{UID: types.UID("other-uid")}

	tests := []struct {
		name       string
		spec       infrav1alpha1.TartHostSpec
		consumer   corev1.ObjectReference
		previous   infrav1alpha1.PreviousConsumerRef
		wantResult ClaimDecision
		wantErr    error
	}{
		{
			name:       "invalid consumer reference is rejected",
			spec:       infrav1alpha1.TartHostSpec{ConsumerRef: &consumer},
			consumer:   corev1.ObjectReference{},
			previous:   previous,
			wantResult: ClaimNoop,
			wantErr:    ErrInvalidRetention,
		},
		{
			name:       "previous UID must match the consumer UID",
			spec:       infrav1alpha1.TartHostSpec{ConsumerRef: &consumer},
			consumer:   consumer,
			previous:   mismatchedPrevious,
			wantResult: ClaimNoop,
			wantErr:    ErrInvalidRetention,
		},
		{
			name:       "currently claimed by the consumer applies the retention",
			spec:       infrav1alpha1.TartHostSpec{ConsumerRef: &consumer},
			consumer:   consumer,
			previous:   previous,
			wantResult: ClaimApply,
			wantErr:    nil,
		},
		{
			name:       "claimed by another consumer conflicts",
			spec:       infrav1alpha1.TartHostSpec{ConsumerRef: func() *corev1.ObjectReference { c := validConsumer(); c.UID = "another"; return &c }()},
			consumer:   consumer,
			previous:   previous,
			wantResult: ClaimNoop,
			wantErr:    ErrClaimConflict,
		},
		{
			name: "already retained with matching previous is a noop",
			spec: infrav1alpha1.TartHostSpec{
				ConsumerRef:         nil,
				PreviousConsumerRef: &infrav1alpha1.PreviousConsumerRef{UID: consumer.UID},
			},
			consumer:   consumer,
			previous:   previous,
			wantResult: ClaimNoop,
			wantErr:    nil,
		},
		{
			name: "no longer claimed by the deleting machine conflicts",
			spec: infrav1alpha1.TartHostSpec{
				ConsumerRef:         nil,
				PreviousConsumerRef: nil,
			},
			consumer:   consumer,
			previous:   previous,
			wantResult: ClaimNoop,
			wantErr:    ErrClaimConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := DecideRetention(tt.spec, tt.consumer, tt.previous)
			if got != tt.wantResult {
				t.Errorf("DecideRetention() = %v, want %v", got, tt.wantResult)
			}
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("DecideRetention() error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("DecideRetention() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateClaimCandidate(t *testing.T) {
	t.Parallel()

	hostID := mustHostID(t, "018f3c5e-5f8a-7c1b-9a2d-123456789abc")

	tests := []struct {
		name    string
		host    *infrav1alpha1.TartHost
		req     ClaimRequest
		wantErr error
	}{
		{
			name:    "nil host is rejected",
			host:    nil,
			req:     ClaimRequest{Mode: ClaimFreshAutomatic},
			wantErr: ErrInvalidClaim,
		},
		{
			name: "expected host id mismatch is rejected",
			host: &infrav1alpha1.TartHost{Spec: infrav1alpha1.TartHostSpec{HostID: hostID}},
			req: ClaimRequest{
				Mode:           ClaimFreshAutomatic,
				ExpectedHostID: mustHostID(t, "018f3c5e-5f8a-7c1b-9a2d-123456789abd"),
			},
			wantErr: ErrHostIdentityChanged,
		},
		{
			name: "fresh automatic requires availability",
			host: &infrav1alpha1.TartHost{Spec: infrav1alpha1.TartHostSpec{
				HostID:      hostID,
				ConsumerRef: &corev1.ObjectReference{Name: "already-claimed"},
			}},
			req:     ClaimRequest{Mode: ClaimFreshAutomatic},
			wantErr: ErrHostNoLongerEligible,
		},
		{
			name: "fresh automatic rejects retained hosts even without reuse approval fields",
			host: &infrav1alpha1.TartHost{Spec: infrav1alpha1.TartHostSpec{
				HostID:              hostID,
				PreviousConsumerRef: &infrav1alpha1.PreviousConsumerRef{UID: "old"},
			}},
			req:     ClaimRequest{Mode: ClaimFreshAutomatic},
			wantErr: ErrHostNoLongerEligible,
		},
		{
			name: "fresh automatic succeeds for an available host",
			host: &infrav1alpha1.TartHost{Spec: infrav1alpha1.TartHostSpec{HostID: hostID}},
			req:  ClaimRequest{Mode: ClaimFreshAutomatic},
		},
		{
			name:    "explicit reusable requires a reusable classification",
			host:    &infrav1alpha1.TartHost{Spec: infrav1alpha1.TartHostSpec{HostID: hostID}},
			req:     ClaimRequest{Mode: ClaimExplicitReusable},
			wantErr: ErrReuseApprovalRequired,
		},
		{
			name: "explicit reusable succeeds when reuse approval matches",
			host: &infrav1alpha1.TartHost{Spec: infrav1alpha1.TartHostSpec{
				HostID:              hostID,
				PreviousConsumerRef: &infrav1alpha1.PreviousConsumerRef{UID: "old"},
				ReusePolicy:         infrav1alpha1.ReusePolicyAllowReuse,
				ReuseMode:           infrav1alpha1.ReuseModeAdopt,
				ReuseApproval:       &infrav1alpha1.ReuseApproval{PreviousConsumerUID: "old"},
			}},
			req: ClaimRequest{Mode: ClaimExplicitReusable},
		},
		{
			name:    "unknown mode is rejected",
			host:    &infrav1alpha1.TartHost{Spec: infrav1alpha1.TartHostSpec{HostID: hostID}},
			req:     ClaimRequest{Mode: ClaimMode(99)},
			wantErr: ErrInvalidClaim,
		},
		{
			name: "selector mismatch is rejected after eligibility passes",
			host: &infrav1alpha1.TartHost{Spec: infrav1alpha1.TartHostSpec{HostID: hostID, Architecture: "amd64"}},
			req: ClaimRequest{
				Mode:     ClaimFreshAutomatic,
				Selector: &infrav1alpha1.HostSelector{Architecture: "arm64"},
			},
			wantErr: ErrHostSelectionMismatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateClaimCandidate(tt.host, tt.req)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("ValidateClaimCandidate() error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("ValidateClaimCandidate() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidConsumerReference(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ref  corev1.ObjectReference
		want bool
	}{
		{name: "fully populated reference is valid", ref: validConsumer(), want: true},
		{name: "missing UID is invalid", ref: func() corev1.ObjectReference { r := validConsumer(); r.UID = ""; return r }(), want: false},
		{name: "missing namespace is invalid", ref: func() corev1.ObjectReference { r := validConsumer(); r.Namespace = ""; return r }(), want: false},
		{name: "zero value is invalid", ref: corev1.ObjectReference{}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := ValidConsumerReference(tt.ref); got != tt.want {
				t.Errorf("ValidConsumerReference() = %v, want %v", got, tt.want)
			}
		})
	}
}
