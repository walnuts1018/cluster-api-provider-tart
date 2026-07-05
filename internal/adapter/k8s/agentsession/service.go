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

package agentsession

import (
	"context"
	"errors"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	agentsessiondomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/agentsession"
)

var (
	ErrOperationNotFound = errors.New("operation or plan not found")
	ErrUnauthorized      = errors.New("session authentication failed")
)

type Service struct {
	client client.Client
	ttl    time.Duration
}

func NewService(k8sClient client.Client, ttl time.Duration) *Service {
	return &Service{client: k8sClient, ttl: ttl}
}

func (service *Service) Issue(
	ctx context.Context,
	key client.ObjectKey,
	hostUID, operationUID string,
	now time.Time,
) (agentsessiondomain.Token, time.Time, error) {
	token, session, err := agentsessiondomain.Issue(hostUID, operationUID, now, service.ttl)
	if err != nil {
		return agentsessiondomain.Token{}, time.Time{}, err
	}

	err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		operation := &infrastructurev1beta1.TartHostOperation{}
		if getErr := service.client.Get(ctx, key, operation); getErr != nil {
			if apierrors.IsNotFound(getErr) {
				return ErrOperationNotFound
			}
			return getErr
		}
		if !operationMatches(operation, hostUID, operationUID) {
			return ErrOperationNotFound
		}

		operation.Status.SessionTokenHash = session.Digest.String()
		operation.Status.SessionTokenExpiresAt = &metav1.Time{Time: session.ExpiresAt}
		operation.Status.SessionAuthenticationFailures = 0
		operation.Status.SessionTokenConsumed = false
		return service.client.Status().Update(ctx, operation)
	})
	if err != nil {
		return agentsessiondomain.Token{}, time.Time{}, fmt.Errorf("persist agent session: %w", err)
	}
	return token, session.ExpiresAt, nil
}

func (service *Service) Authenticate(
	ctx context.Context,
	key client.ObjectKey,
	providedToken, hostUID, operationUID string,
	now time.Time,
) error {
	return service.authenticate(ctx, key, providedToken, hostUID, operationUID, now, false)
}

// ClaimBootstrapは認証と消費を同じresourceVersion付きStatus更新にまとめ、
// 並列リクエストでBootstrap Bundleを複数回返さない。
func (service *Service) ClaimBootstrap(
	ctx context.Context,
	key client.ObjectKey,
	providedToken, hostUID, operationUID string,
	now time.Time,
) error {
	return service.authenticate(ctx, key, providedToken, hostUID, operationUID, now, true)
}

func (service *Service) authenticate(
	ctx context.Context,
	key client.ObjectKey,
	providedToken, hostUID, operationUID string,
	now time.Time,
	consume bool,
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		operation := &infrastructurev1beta1.TartHostOperation{}
		if err := service.client.Get(ctx, key, operation); err != nil {
			if apierrors.IsNotFound(err) {
				return ErrUnauthorized
			}
			return err
		}
		if !operationMatches(operation, hostUID, operationUID) {
			return ErrUnauthorized
		}
		if consume && operation.Status.BootstrapDelivered {
			return ErrUnauthorized
		}
		session, err := sessionFromOperation(operation)
		if err != nil {
			return ErrUnauthorized
		}
		updated, result := agentsessiondomain.Authenticate(session, providedToken, hostUID, operationUID, now)
		if result == agentsessiondomain.AuthenticationAccepted && consume {
			updated = agentsessiondomain.Consume(updated)
		}

		statusChanged := updated.AuthenticationFailures != session.AuthenticationFailures ||
			updated.Consumed != session.Consumed
		if statusChanged {
			operation.Status.SessionAuthenticationFailures = int32(updated.AuthenticationFailures)
			operation.Status.SessionTokenConsumed = updated.Consumed
			if result == agentsessiondomain.AuthenticationAccepted && consume {
				// Bootstrap配信済み状態はOperation単位で保持し、再登録後の新Sessionでも再配信しない。
				operation.Status.BootstrapDelivered = true
			}
			if err := service.client.Status().Update(ctx, operation); err != nil {
				return err
			}
		}
		if result != agentsessiondomain.AuthenticationAccepted {
			return ErrUnauthorized
		}
		return nil
	})
}

func sessionFromOperation(operation *infrastructurev1beta1.TartHostOperation) (agentsessiondomain.Session, error) {
	if operation.Status.SessionTokenExpiresAt == nil {
		return agentsessiondomain.Session{}, ErrUnauthorized
	}
	digest, err := agentsessiondomain.ParseDigest(operation.Status.SessionTokenHash)
	if err != nil {
		return agentsessiondomain.Session{}, err
	}
	return agentsessiondomain.Session{
		Digest:                 digest,
		HostUID:                string(operation.Spec.HostRef.UID),
		OperationUID:           operation.Spec.OperationID,
		ExpiresAt:              operation.Status.SessionTokenExpiresAt.Time,
		AuthenticationFailures: int(operation.Status.SessionAuthenticationFailures),
		Consumed:               operation.Status.SessionTokenConsumed,
	}, nil
}

func operationMatches(
	operation *infrastructurev1beta1.TartHostOperation,
	hostUID, operationUID string,
) bool {
	return string(operation.Spec.HostRef.UID) == hostUID &&
		operation.Spec.OperationID == operationUID
}
