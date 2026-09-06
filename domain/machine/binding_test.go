package machine

import (
	"errors"
	"testing"
	"time"
)

func TestFindClaimedHostName(t *testing.T) {
	tests := []struct {
		name       string
		candidates []HostClaimCandidate
		machineUID ConsumerUID
		want       string
		wantErr    error
	}{
		{
			name:       "一致なし",
			candidates: []HostClaimCandidate{{Name: "host-a", ConsumerUID: "other"}},
			machineUID: "machine-1",
			want:       "",
		},
		{
			name:       "一意に一致",
			candidates: []HostClaimCandidate{{Name: "host-a", ConsumerUID: "other"}, {Name: "host-b", ConsumerUID: "machine-1"}},
			machineUID: "machine-1",
			want:       "host-b",
		},
		{
			name:       "同一Hostが重複しても一意扱い",
			candidates: []HostClaimCandidate{{Name: "host-b", ConsumerUID: "machine-1"}, {Name: "host-b", ConsumerUID: "machine-1"}},
			machineUID: "machine-1",
			want:       "host-b",
		},
		{
			name:       "複数Hostが同一Machineをclaimしているのはambiguous",
			candidates: []HostClaimCandidate{{Name: "host-a", ConsumerUID: "machine-1"}, {Name: "host-b", ConsumerUID: "machine-1"}},
			machineUID: "machine-1",
			wantErr:    ErrAmbiguousClaim,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FindClaimedHostName(tt.candidates, tt.machineUID)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected error %v, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestIsShutdownSettled(t *testing.T) {
	now := time.Now()
	delay := 30 * time.Second

	if IsShutdownSettled("SomethingElse", now.Add(-time.Minute), now, delay) {
		t.Fatal("reasonが異なる場合はsettled扱いにならない")
	}
	if IsShutdownSettled(ShutdownRequestedReason, time.Time{}, now, delay) {
		t.Fatal("transitionedAtが未設定の場合はsettled扱いにならない")
	}
	if IsShutdownSettled(ShutdownRequestedReason, now.Add(-time.Second), now, delay) {
		t.Fatal("delay未経過はsettled扱いにならない")
	}
	if !IsShutdownSettled(ShutdownRequestedReason, now.Add(-time.Minute), now, delay) {
		t.Fatal("delay経過後はsettled扱いになるべき")
	}
}
