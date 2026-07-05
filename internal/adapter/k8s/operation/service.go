package operation

import (
	"context"
	"errors"
	"fmt"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
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

// sameOperationSpec は同じOperation IDの再試行で、最初に保存されたdeadlineを正本とする。
// deadline以外の入力差分は異なるPlanや対象を同じIDで実行する危険があるため拒否する。
func sameOperationSpec(
	existing infrastructurev1beta1.TartHostOperationSpec,
	desired infrastructurev1beta1.TartHostOperationSpec,
) bool {
	desired.Deadline = existing.Deadline
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
