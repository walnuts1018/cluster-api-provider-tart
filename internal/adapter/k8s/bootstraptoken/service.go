package bootstraptoken

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
	applicationbootstraptoken "github.com/walnuts1018/cluster-api-provider-tart/internal/application/bootstraptoken"
	onetimetoken "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/onetime_token"
)

const (
	secretNameSuffix = "-bootstrap-token"
	tokenDataKey     = "token"
)

var _ applicationbootstraptoken.Service = (*Service)(nil)

type Service struct {
	client client.Client
}

func NewService(k8sClient client.Client) *Service {
	return &Service{client: k8sClient}
}

func SecretName(machine *infrastructurev1alpha1.TartMachine) string {
	return machine.Name + secretNameSuffix
}

func (s *Service) Ensure(ctx context.Context, machine *infrastructurev1alpha1.TartMachine, token onetimetoken.OneTimeToken) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      SecretName(machine),
			Namespace: machine.Namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, s.client, secret, func() error {
		secret.Type = corev1.SecretTypeOpaque
		secret.Data = map[string][]byte{
			tokenDataKey: []byte(token.String()),
		}
		return controllerutil.SetControllerReference(machine, secret, s.client.Scheme())
	})
	if err != nil {
		return fmt.Errorf("failed to persist bootstrap token secret: %w", err)
	}
	return nil
}

func (s *Service) Get(ctx context.Context, machine *infrastructurev1alpha1.TartMachine) (onetimetoken.OneTimeToken, bool, error) {
	var secret corev1.Secret
	if err := s.client.Get(ctx, client.ObjectKey{Namespace: machine.Namespace, Name: SecretName(machine)}, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("failed to get bootstrap token secret: %w", err)
	}

	token, ok := secret.Data[tokenDataKey]
	if !ok || len(token) == 0 {
		return "", false, nil
	}
	return onetimetoken.OneTimeToken(token), true, nil
}

func (s *Service) Delete(ctx context.Context, machine *infrastructurev1alpha1.TartMachine) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      SecretName(machine),
			Namespace: machine.Namespace,
		},
	}
	if err := s.client.Delete(ctx, secret); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete bootstrap token secret: %w", err)
	}
	return nil
}
