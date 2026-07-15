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

package drivertarget

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	driverdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/driver"
)

const (
	credentialsUsernameKey = "username"
	credentialsPasswordKey = "password"
	caBundleKey            = "ca.crt"
)

// Service はTartHostからdriverdomain.HostTargetを組み立てる。
type Service struct {
	client client.Client
}

func NewService(k8sClient client.Client) *Service {
	return &Service{client: k8sClient}
}

func (service *Service) Build(
	ctx context.Context,
	host *infrastructurev1beta1.TartHost,
) (driverdomain.HostTarget, error) {
	bootMAC, err := driverdomain.ParseMACAddress(host.Spec.Identifiers.BootMACAddress)
	if err != nil {
		return driverdomain.HostTarget{}, fmt.Errorf("parse TartHost boot MAC address: %w", err)
	}
	target := driverdomain.NewHostTarget(bootMAC)
	if host.Spec.Management.Redfish == nil {
		return target, nil
	}
	access, err := service.redfishAccess(ctx, host)
	if err != nil {
		return driverdomain.HostTarget{}, err
	}
	return target.WithRedfishAccess(access), nil
}

func (service *Service) redfishAccess(
	ctx context.Context,
	host *infrastructurev1beta1.TartHost,
) (driverdomain.RedfishAccess, error) {
	management := host.Spec.Management
	if management.CredentialsSecretRef == nil {
		return driverdomain.RedfishAccess{}, fmt.Errorf("redfish credentialsSecretRef is required")
	}
	credentials := &corev1.Secret{}
	if err := service.client.Get(ctx, client.ObjectKey{
		Namespace: host.Namespace,
		Name:      management.CredentialsSecretRef.Name,
	}, credentials); err != nil {
		return driverdomain.RedfishAccess{}, fmt.Errorf("get Redfish credentials Secret: %w", err)
	}
	username, ok := credentials.Data[credentialsUsernameKey]
	if !ok || len(username) == 0 {
		return driverdomain.RedfishAccess{}, fmt.Errorf("redfish credentials Secret must contain %q", credentialsUsernameKey)
	}
	password, ok := credentials.Data[credentialsPasswordKey]
	if !ok || len(password) == 0 {
		return driverdomain.RedfishAccess{}, fmt.Errorf("redfish credentials Secret must contain %q", credentialsPasswordKey)
	}

	var caBundle []byte
	if redfish := management.Redfish; redfish != nil && redfish.CABundleSecretRef != nil {
		caSecret := &corev1.Secret{}
		if err := service.client.Get(ctx, client.ObjectKey{
			Namespace: host.Namespace,
			Name:      redfish.CABundleSecretRef.Name,
		}, caSecret); err != nil {
			return driverdomain.RedfishAccess{}, fmt.Errorf("get Redfish CA bundle Secret: %w", err)
		}
		caBundle = caSecret.Data[caBundleKey]
		if len(caBundle) == 0 {
			return driverdomain.RedfishAccess{}, fmt.Errorf("redfish CA bundle Secret must contain %q", caBundleKey)
		}
	}

	return driverdomain.NewRedfishAccess(
		management.Redfish.Endpoint,
		string(username),
		string(password),
		caBundle,
		management.Redfish.SPKIPins,
	)
}
