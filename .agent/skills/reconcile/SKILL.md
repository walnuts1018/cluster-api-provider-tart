---
name: reconcile
description: KubernetesリソースをReconcileするときの実装ルール
when_to_use: KubernetesリソースのReconcileを実装する時
---

KubernetesリソースのReconcileを実装する際には、必ず`Patch()`を用いて、Server-Side Applyを使用してください。`Update()`や`Create()`は、リソースの状態を完全に上書きしてしまうため、他のコントローラーやユーザーが行った変更を上書きしてしまう可能性があります。`Patch()`を使用することで、必要な変更のみを適用し、他の変更を保持することができます。

## Custom Resourceの定義

corev1など、既存のKubernetesで定義されているフィールドをCustom Resourceで再利用する場合は、applyconfigurationの型を利用するようにしてください。ただし、そのまま利用するとDeepCopyやRefが実装されないので、以下のように再定義した型を利用してください。

```go
// api/v1beta1/apply_configuration.go
import (
	"encoding/json"

	corev1apply "k8s.io/client-go/applyconfigurations/core/v1"
)

type EnvVarApplyConfigurationList []*corev1apply.EnvVarApplyConfiguration

func (c *EnvVarApplyConfigurationList) DeepCopy() *EnvVarApplyConfigurationList {
	out := new(EnvVarApplyConfigurationList)
	bytes, err := json.Marshal(c)
	if err != nil {
		panic("Failed to marshal")
	}
	if err := json.Unmarshal(bytes, out); err != nil {
		panic("Failed to unmarshal")
	}
	return out
}

func (l EnvVarApplyConfigurationList) Ref() []*corev1apply.EnvVarApplyConfiguration {
	if l == nil {
		return nil
	}
	s := make([]*corev1apply.EnvVarApplyConfiguration, len(l))
	copy(s, l)
	return s
}
```

```go
// api/v1beta1/hoge_types.go
type HogeSpec struct {
  ExtraEnv EnvVarApplyConfigurationList `json:"extraEnv,omitempty"`
}
```

## Reconcileの実装部分

ApplyConfigurationを利用して型安全に書くようにしてください。以下は、ServiceをApplyConfigurationで作成し、Patchする例です。

```go
	service := corev1apply.Service(cfTunnel.Name, cfTunnel.Namespace).
		WithLabels(labels).
		WithOwnerReferences(owner).
		WithSpec(corev1apply.ServiceSpec().
			WithPorts(corev1apply.ServicePort().
				WithName("metrics").
				WithProtocol(corev1.ProtocolTCP).
				WithPort(MetricsPort).
				WithTargetPort(intstr.FromString("metrics")),
			).
			WithSelector(labels).
			WithType(corev1.ServiceTypeClusterIP),
		)

	obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(service)
	if err != nil {
		return fmt.Errorf("failed to convert service to unstructured: %w", err)
	}

	patch := &unstructured.Unstructured{
		Object: obj,
	}

	var current corev1.Service
	err = r.Get(ctx, client.ObjectKey{Namespace: cfTunnel.Namespace, Name: cfTunnel.Name}, &current)
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to get service: %w", err)
	}

	currentApplyConfig, err := corev1apply.ExtractService(&current, managerName)
	if err != nil {
		return fmt.Errorf("failed to extract apply configuration from service: %w", err)
	}

	if equality.Semantic.DeepEqual(service, currentApplyConfig) {
		return nil
	}

	if err = r.Patch(ctx, patch, client.Apply, &client.PatchOptions{FieldManager: managerName, Force: ptr.To(true)}); err != nil {
		return fmt.Errorf("failed to apply service: %w", err)
	}
```
