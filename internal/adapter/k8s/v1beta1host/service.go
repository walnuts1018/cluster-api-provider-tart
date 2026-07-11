// Copyright 2026.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v1beta1host

import (
	"context"
	"fmt"
	"slices"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	capabilitydomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/capability"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/host"
)

// Service はv1beta1 TartHostのPhaseを更新するアダプターサービスである。
// v1beta1 TartHostはSpec.ConsumerRefで排他割当を管理するためStatusにMachineRefを持たない。
type Service struct {
	client client.Client
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts/status,verbs=get;update;patch

func NewService(k8sClient client.Client) *Service {
	return &Service{client: k8sClient}
}

// UpdateCapabilities はdriver discovery結果をTartHost Statusへ保存する。
func (s *Service) UpdateCapabilities(
	ctx context.Context,
	host *infrastructurev1beta1.TartHost,
	capabilities capabilitydomain.Set,
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &infrastructurev1beta1.TartHost{}
		if err := s.client.Get(ctx, client.ObjectKeyFromObject(host), current); err != nil {
			return fmt.Errorf("get TartHost for capability update: %w", err)
		}

		values := capabilities.Values()
		apiValues := make([]infrastructurev1beta1.Capability, 0, len(values))
		for _, value := range values {
			apiValues = append(apiValues, infrastructurev1beta1.Capability(value))
		}
		if slices.Equal(current.Status.Capabilities, apiValues) {
			return nil
		}

		original := current.DeepCopy()
		current.Status.Capabilities = apiValues
		current.Status.ObservedGeneration = current.Generation
		return s.client.Status().Patch(ctx, current, client.MergeFrom(original))
	})
}

// UpdatePowerState はdriverのPowerState観測結果をTartHost Statusへ保存する。
func (s *Service) UpdatePowerState(
	ctx context.Context,
	host *infrastructurev1beta1.TartHost,
	state infrastructurev1beta1.PowerState,
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &infrastructurev1beta1.TartHost{}
		if err := s.client.Get(ctx, client.ObjectKeyFromObject(host), current); err != nil {
			return fmt.Errorf("get TartHost for power state update: %w", err)
		}
		if current.Status.PowerState == state {
			return nil
		}

		original := current.DeepCopy()
		current.Status.PowerState = state
		current.Status.ObservedGeneration = current.Generation
		return s.client.Status().Patch(ctx, current, client.MergeFrom(original))
	})
}

// UpdateBootState はdriverのBootOverride/VirtualMedia観測結果をTartHost Statusへ保存する。
func (s *Service) UpdateBootState(
	ctx context.Context,
	host *infrastructurev1beta1.TartHost,
	state infrastructurev1beta1.BootStateStatus,
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &infrastructurev1beta1.TartHost{}
		if err := s.client.Get(ctx, client.ObjectKeyFromObject(host), current); err != nil {
			return fmt.Errorf("get TartHost for boot state update: %w", err)
		}
		if current.Status.BootState != nil && *current.Status.BootState == state {
			return nil
		}

		original := current.DeepCopy()
		current.Status.BootState = &state
		current.Status.ObservedGeneration = current.Generation
		return s.client.Status().Patch(ctx, current, client.MergeFrom(original))
	})
}

// MarkHostProvisioning はHostをProvisioningフェーズに遷移させる。
func (s *Service) MarkHostProvisioning(ctx context.Context, host *infrastructurev1beta1.TartHost) error {
	return s.updatePhase(ctx, host, infrastructurev1beta1.TartHostPhaseProvisioning, "Provisioning",
		"Host is provisioning a TartMachine after Wake-on-LAN")
}

// MarkHostUpdating はHostをUpdatingフェーズに遷移させる。
func (s *Service) MarkHostUpdating(ctx context.Context, host *infrastructurev1beta1.TartHost) error {
	return s.updatePhase(ctx, host, infrastructurev1beta1.TartHostPhaseUpdating, "Updating",
		"Host is applying an in-place OS update")
}

// MarkHostProvisioned はHostをProvisionedフェーズに遷移させる。
func (s *Service) MarkHostProvisioned(ctx context.Context, host *infrastructurev1beta1.TartHost) error {
	return s.updatePhase(ctx, host, infrastructurev1beta1.TartHostPhaseProvisioned, "Provisioned",
		"Host has been provisioned successfully")
}

// MarkHostRecoveryRequired はHostをRecoveryRequiredフェーズに遷移させる。
func (s *Service) MarkHostRecoveryRequired(ctx context.Context, host *infrastructurev1beta1.TartHost) error {
	return s.updatePhase(ctx, host, infrastructurev1beta1.TartHostPhaseRecoveryRequired, "RecoveryRequired",
		"Host requires operator recovery after an in-place OS update failure")
}

// MarkHostAvailable はHostをAvailableに戻す。ConsumerRefも除去する。
func (s *Service) MarkHostAvailable(ctx context.Context, host *infrastructurev1beta1.TartHost) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &infrastructurev1beta1.TartHost{}
		if err := s.client.Get(ctx, client.ObjectKeyFromObject(host), current); err != nil {
			return fmt.Errorf("get TartHost for phase update: %w", err)
		}
		original := current.DeepCopy()
		// ConsumerRefを除去してAvailableに戻す
		current.Spec.ConsumerRef = nil
		if err := s.client.Update(ctx, current); err != nil {
			return fmt.Errorf("clear TartHost ConsumerRef: %w", err)
		}

		current.Status.Phase = infrastructurev1beta1.TartHostPhaseAvailable
		current.Status.LastStablePhase = infrastructurev1beta1.TartHostPhaseAvailable
		current.Status.ObservedGeneration = current.Generation
		apimeta.SetStatusCondition(&current.Status.Conditions, metav1.Condition{
			Type:               "Available",
			Status:             metav1.ConditionTrue,
			Reason:             "Available",
			Message:            "Host is available for allocation",
			ObservedGeneration: current.Generation,
		})
		return s.client.Status().Patch(ctx, current, client.MergeFrom(original))
	})
}

// ReserveForMachine はHostを指定MachineのConsumerRefとして予約し、ReservedフェーズにStatusを更新する。
func (s *Service) ReserveForMachine(
	ctx context.Context,
	host *infrastructurev1beta1.TartHost,
	machine *infrastructurev1beta1.TartMachine,
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &infrastructurev1beta1.TartHost{}
		if err := s.client.Get(ctx, client.ObjectKeyFromObject(host), current); err != nil {
			return fmt.Errorf("get TartHost for reservation: %w", err)
		}
		// ConsumerRefはallocation.Service.Reserveで設定済みなので、StatusのみReservedに更新する
		original := current.DeepCopy()
		current.Status.Phase = infrastructurev1beta1.TartHostPhaseReserved
		current.Status.LastStablePhase = infrastructurev1beta1.TartHostPhaseAvailable
		current.Status.ObservedGeneration = current.Generation
		apimeta.SetStatusCondition(&current.Status.Conditions, metav1.Condition{
			Type:               "Available",
			Status:             metav1.ConditionFalse,
			Reason:             "Reserved",
			Message:            fmt.Sprintf("Reserved by TartMachine %s/%s", machine.Namespace, machine.Name),
			ObservedGeneration: current.Generation,
		})
		return s.client.Status().Patch(ctx, current, client.MergeFrom(original))
	})
}

// ReleaseHost はHostのConsumerRefを除去してAvailableに戻す。
// machine.Status.HostRefが指すHostのUID不一致時は何もしない。
func (s *Service) ReleaseHost(ctx context.Context, machine *infrastructurev1beta1.TartMachine) error {
	if machine.Status.HostRef == nil {
		return nil
	}

	host := &infrastructurev1beta1.TartHost{}
	if err := s.client.Get(ctx, client.ObjectKey{
		Namespace: machine.Status.HostRef.Namespace,
		Name:      machine.Status.HostRef.Name,
	}, host); err != nil {
		return client.IgnoreNotFound(err)
	}
	// UID不一致のHostは操作しない（別のMachineに再割当済みの可能性）
	if host.UID != machine.Status.HostRef.UID {
		return nil
	}
	if host.Spec.ConsumerRef == nil || host.Spec.ConsumerRef.UID != machine.UID {
		return nil
	}

	return s.MarkHostAvailable(ctx, host)
}

// MarkHostCleaningForDeletion はHostをCleaningフェーズに遷移させる（削除ポリシー処理前）。
func (s *Service) MarkHostCleaningForDeletion(
	ctx context.Context,
	host *infrastructurev1beta1.TartHost,
	deletionPolicy infrastructurev1beta1.DeletionPolicy,
) error {
	reason := fmt.Sprintf("Cleaning%s", deletionPolicy)
	message := fmt.Sprintf("Cleaning host with policy %s", deletionPolicy)
	return s.updatePhase(ctx, host, infrastructurev1beta1.TartHostPhaseCleaning, reason, message)
}

// MarkHostRetained はHostをRetainedフェーズに遷移させる（RetainData削除後）。
func (s *Service) MarkHostRetained(ctx context.Context, host *infrastructurev1beta1.TartHost) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &infrastructurev1beta1.TartHost{}
		if err := s.client.Get(ctx, client.ObjectKeyFromObject(host), current); err != nil {
			return fmt.Errorf("get TartHost for Retained phase: %w", err)
		}
		original := current.DeepCopy()
		// ConsumerRefを除去してRetainedに移行する
		current.Spec.ConsumerRef = nil
		if err := s.client.Update(ctx, current); err != nil {
			return fmt.Errorf("clear TartHost ConsumerRef for Retained: %w", err)
		}

		current.Status.Phase = infrastructurev1beta1.TartHostPhaseRetained
		current.Status.LastStablePhase = infrastructurev1beta1.TartHostPhaseRetained
		current.Status.ObservedGeneration = current.Generation
		apimeta.SetStatusCondition(&current.Status.Conditions, metav1.Condition{
			Type:               "Available",
			Status:             metav1.ConditionFalse,
			Reason:             "Retained",
			Message:            "Host is retained with Data partition intact",
			ObservedGeneration: current.Generation,
		})
		return s.client.Status().Patch(ctx, current, client.MergeFrom(original))
	})
}

// MarkHostDetached はHostをDetachedフェーズに遷移させる（RetainState削除後）。
func (s *Service) MarkHostDetached(ctx context.Context, host *infrastructurev1beta1.TartHost) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &infrastructurev1beta1.TartHost{}
		if err := s.client.Get(ctx, client.ObjectKeyFromObject(host), current); err != nil {
			return fmt.Errorf("get TartHost for Detached phase: %w", err)
		}
		original := current.DeepCopy()
		// ConsumerRefを除去してDetachedに移行する
		current.Spec.ConsumerRef = nil
		if err := s.client.Update(ctx, current); err != nil {
			return fmt.Errorf("clear TartHost ConsumerRef for Detached: %w", err)
		}

		current.Status.Phase = infrastructurev1beta1.TartHostPhaseDetached
		current.Status.LastStablePhase = infrastructurev1beta1.TartHostPhaseDetached
		current.Status.ObservedGeneration = current.Generation
		apimeta.SetStatusCondition(&current.Status.Conditions, metav1.Condition{
			Type:               "Available",
			Status:             metav1.ConditionFalse,
			Reason:             "Detached",
			Message:            "Host is detached with State and Data partitions intact",
			ObservedGeneration: current.Generation,
		})
		return s.client.Status().Patch(ctx, current, client.MergeFrom(original))
	})
}

func (s *Service) updatePhase(
	ctx context.Context,
	host *infrastructurev1beta1.TartHost,
	target infrastructurev1beta1.TartHostPhase,
	reason, message string,
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &infrastructurev1beta1.TartHost{}
		if err := s.client.Get(ctx, client.ObjectKeyFromObject(host), current); err != nil {
			return fmt.Errorf("get TartHost for phase update: %w", err)
		}
		if current.Status.Phase == target {
			return nil
		}

		// ホストフェーズの状態機械による遷移検証
		currentPhase, err := hostdomain.ParsePhase(string(current.Status.Phase))
		if err != nil {
			// フェーズが未設定の場合は直接書き込む
			currentPhase = hostdomain.PhaseAvailable
		}
		lastStable := currentPhase
		if !currentPhase.Stable() {
			if current.Status.LastStablePhase != "" {
				lastStable, err = hostdomain.ParsePhase(string(current.Status.LastStablePhase))
				if err != nil {
					return fmt.Errorf("parse TartHost last stable phase: %w", err)
				}
			} else {
				lastStable = hostdomain.PhaseAvailable
			}
		}
		state, err := hostdomain.NewState(currentPhase, lastStable)
		if err != nil {
			return fmt.Errorf("build TartHost state: %w", err)
		}
		targetDomain, err := hostdomain.ParsePhase(string(target))
		if err != nil {
			return fmt.Errorf("parse target TartHost phase: %w", err)
		}
		nextState, err := state.Transition(targetDomain)
		if err != nil {
			return fmt.Errorf("invalid TartHost phase transition: %w", err)
		}

		original := current.DeepCopy()
		current.Status.Phase = infrastructurev1beta1.TartHostPhase(nextState.Phase())
		current.Status.LastStablePhase = infrastructurev1beta1.TartHostPhase(nextState.LastStablePhase())
		current.Status.ObservedGeneration = current.Generation
		apimeta.SetStatusCondition(&current.Status.Conditions, metav1.Condition{
			Type:               "Available",
			Status:             metav1.ConditionFalse,
			Reason:             reason,
			Message:            message,
			ObservedGeneration: current.Generation,
		})
		return s.client.Status().Patch(ctx, current, client.MergeFrom(original))
	})
}
