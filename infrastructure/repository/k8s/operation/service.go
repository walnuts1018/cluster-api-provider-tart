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

package operation

import (
	"context"
	"errors"
	"fmt"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/operation"
)

var (
	ErrActiveOperation     = errors.New("active TartHostOperation already exists")
	ErrOperationIDConflict = errors.New("operation ID conflicts with existing spec")
)

type Service struct {
	client client.Client
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthostoperations,verbs=get;list;watch;create;delete

func NewService(k8sClient client.Client) *Service {
	return &Service{client: k8sClient}
}

func (s *Service) Start(
	ctx context.Context,
	desired *infrastructurev1beta1.TartHostOperation,
) (*infrastructurev1beta1.TartHostOperation, error) {
	candidate := desired.DeepCopy()
	name, err := operationdomain.ResourceName(string(candidate.Spec.HostRef.UID))
	if err != nil {
		return nil, err
	}
	candidate.Name = name

	key := client.ObjectKey{Namespace: candidate.Namespace, Name: candidate.Name}
	existing := &infrastructurev1beta1.TartHostOperation{}
	if err := s.client.Get(ctx, key, existing); err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("get active TartHostOperation: %w", err)
		}
		if err := s.client.Create(ctx, candidate); err != nil {
			if apierrors.IsAlreadyExists(err) {
				return s.resolveExisting(ctx, key, candidate)
			}
			return nil, fmt.Errorf("create TartHostOperation: %w", err)
		}
		return candidate, nil
	}

	if existing.Spec.OperationID == candidate.Spec.OperationID {
		if !sameOperationSpec(existing.Spec, candidate.Spec) {
			return nil, ErrOperationIDConflict
		}
		return existing, nil
	}
	terminal, err := terminal(existing.Status.Phase)
	if err != nil {
		return nil, err
	}
	if !terminal {
		return nil, ErrActiveOperation
	}

	uid := existing.UID
	resourceVersion := existing.ResourceVersion
	if err := s.client.Delete(ctx, existing, &client.DeleteOptions{
		Preconditions: &metav1.Preconditions{
			UID:             &uid,
			ResourceVersion: &resourceVersion,
		},
	}); err != nil && !apierrors.IsNotFound(err) {
		if apierrors.IsConflict(err) {
			return s.resolveExisting(ctx, key, candidate)
		}
		return nil, fmt.Errorf("delete terminal TartHostOperation: %w", err)
	}

	if err := s.client.Create(ctx, candidate); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return s.resolveExisting(ctx, key, candidate)
		}
		return nil, fmt.Errorf("replace terminal TartHostOperation: %w", err)
	}
	return candidate, nil
}

func (s *Service) resolveExisting(
	ctx context.Context,
	key client.ObjectKey,
	desired *infrastructurev1beta1.TartHostOperation,
) (*infrastructurev1beta1.TartHostOperation, error) {
	current := &infrastructurev1beta1.TartHostOperation{}
	if err := s.client.Get(ctx, key, current); err != nil {
		return nil, fmt.Errorf("get competing TartHostOperation: %w", err)
	}
	if current.Spec.OperationID == desired.Spec.OperationID {
		if !sameOperationSpec(current.Spec, desired.Spec) {
			return nil, ErrOperationIDConflict
		}
		return current, nil
	}
	return nil, ErrActiveOperation
}

// CompleteProvision はHealth Gateを通過したProvision Operationを一度だけ完了させる。
func (s *Service) CompleteProvision(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &infrastructurev1beta1.TartHostOperation{}
		if err := s.client.Get(ctx, client.ObjectKeyFromObject(operation), current); err != nil {
			return fmt.Errorf("get TartHostOperation for completion: %w", err)
		}
		if current.UID != operation.UID || current.Spec.OperationID != operation.Spec.OperationID {
			return ErrOperationIDConflict
		}
		if current.Status.Phase == infrastructurev1beta1.TartHostOperationPhaseSucceeded {
			return nil
		}
		currentPhase, err := operationdomain.ParsePhase(string(current.Status.Phase))
		if err != nil {
			return err
		}
		target, err := operationdomain.Transition(currentPhase, operationdomain.PhaseSucceeded)
		if err != nil {
			return err
		}
		original := current.DeepCopy()
		current.Status.Phase = infrastructurev1beta1.TartHostOperationPhase(target)
		current.Status.ObservedGeneration = current.Generation
		return s.client.Status().Patch(ctx, current, client.MergeFrom(original))
	})
}

// sameOperationSpec は同じOperation IDの再試行で、最初に保存されたdeadlineとPlan digestを正本とする。
// Planは保存済みdeadlineから再生成してdigestを照合するため、候補Planの時刻由来の差分だけは無視する。
// それ以外の入力差分は異なる対象を同じIDで実行する危険があるため拒否する。
func sameOperationSpec(
	existing infrastructurev1beta1.TartHostOperationSpec,
	desired infrastructurev1beta1.TartHostOperationSpec,
) bool {
	desired.Deadline = existing.Deadline
	desired.PlanDigest = existing.PlanDigest
	return apiequality.Semantic.DeepEqual(existing, desired)
}

func terminal(phase infrastructurev1beta1.TartHostOperationPhase) (bool, error) {
	if phase == "" {
		return false, nil
	}
	parsed, err := operationdomain.ParsePhase(string(phase))
	if err != nil {
		return false, err
	}
	return parsed.Terminal(), nil
}
