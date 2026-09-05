package nodelifecycleengine

import "testing"

func TestPreflightはminorVersionSkipを拒否する(t *testing.T) {
	tests := []struct {
		name    string
		current string
		target  string
		wantErr bool
	}{
		{name: "patch更新", current: "v1.34.1", target: "v1.34.2"},
		{name: "minorを1つ上げる", current: "v1.34.4", target: "v1.35.0"},
		{name: "minorを2つ上げる", current: "v1.34.9", target: "v1.36.0", wantErr: true},
		{name: "downgrade", current: "v1.35.0", target: "v1.34.0", wantErr: true},
		{name: "major更新", current: "v1.35.0", target: "v2.0.0", wantErr: true},
		{name: "不正なversion", current: "v1.35.0", target: "latest", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Preflight(PreflightInput{
				LifecycleRuntime: LifecycleRuntimeKubeadm,
				CurrentVersion:   tt.current,
				TargetVersion:    tt.target,
				UpdateClass:      UpdateClassKubernetesBinary,
				NodeRole:         NodeRoleWorker,
			})
			if (err != nil) != tt.wantErr {
				t.Fatalf("Preflight() error = %v, wantErr %t", err, tt.wantErr)
			}
		})
	}
}

func TestPreflightはWorkerがControlPlaneより先に進むことを拒否する(t *testing.T) {
	err := Preflight(PreflightInput{
		LifecycleRuntime:                LifecycleRuntimeKubeadm,
		CurrentVersion:                  "v1.34.0",
		TargetVersion:                   "v1.35.0",
		ControlPlaneAcceptedVersion:     "v1.34.0",
		RequireControlPlaneTargetAccept: true,
		UpdateClass:                     UpdateClassKubernetesBinary,
		NodeRole:                        NodeRoleWorker,
	})
	if err == nil {
		t.Fatal("Preflight() error = nil, want worker ordering error")
	}
}

func TestPreflightはStateMigrationでSnapshotRefを必須にする(t *testing.T) {
	err := Preflight(PreflightInput{
		LifecycleRuntime: LifecycleRuntimeKubeadm,
		CurrentVersion:   "v1.34.0",
		TargetVersion:    "v1.35.0",
		UpdateClass:      UpdateClassStateMigration,
		NodeRole:         NodeRoleControlPlane,
	})
	if err == nil {
		t.Fatal("Preflight() error = nil, want snapshot requirement error")
	}

	err = Preflight(PreflightInput{
		LifecycleRuntime: LifecycleRuntimeKubeadm,
		CurrentVersion:   "v1.34.0",
		TargetVersion:    "v1.35.0",
		UpdateClass:      UpdateClassStateMigration,
		NodeRole:         NodeRoleControlPlane,
		SnapshotRef:      "etcd-snapshot-1",
	})
	if err != nil {
		t.Fatalf("Preflight() error = %v, want nil", err)
	}
}

func TestPreflightはK0sのKubernetesBinary更新を許可する(t *testing.T) {
	err := Preflight(PreflightInput{
		LifecycleRuntime: LifecycleRuntimeK0s,
		CurrentVersion:   "v1.35.0",
		TargetVersion:    "v1.36.0",
		UpdateClass:      UpdateClassKubernetesBinary,
		NodeRole:         NodeRoleControlPlane,
	})
	if err != nil {
		t.Fatalf("Preflight() error = %v, want nil", err)
	}
}

func TestPreflightはK3sのNodeLifecycle更新を拒否する(t *testing.T) {
	err := Preflight(PreflightInput{
		LifecycleRuntime: LifecycleRuntimeUnsupported,
		CurrentVersion:   "v1.35.0",
		TargetVersion:    "v1.36.0",
		UpdateClass:      UpdateClassKubernetesBinary,
		NodeRole:         NodeRoleWorker,
	})
	if err == nil {
		t.Fatal("Preflight() error = nil, want k3s lifecycle unsupported")
	}
}
