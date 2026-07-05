package agentapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/opencontainers/go-digest"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

const (
	OperationIDField       = "spec.operationID"
	PlanSecretSuffix       = "-agent-plan"
	PlanSecretPlanKey      = "plan.json"
	PlanSecretSignatureKey = "signature.json"
)

var (
	ErrNotFound  = errors.New("agent API resource not found")
	ErrAmbiguous = errors.New("multiple operations have the same operation ID")
)

type Provider struct {
	client client.Client
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthostoperations,verbs=get;list;watch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachines,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func NewProvider(k8sClient client.Client) *Provider {
	return &Provider{client: k8sClient}
}

func OperationIDIndex(object client.Object) []string {
	operation, ok := object.(*infrastructurev1beta1.TartHostOperation)
	if !ok || operation.Spec.OperationID == "" {
		return nil
	}
	return []string{operation.Spec.OperationID}
}

func (provider *Provider) Resolve(
	ctx context.Context,
	operationUID string,
) (client.ObjectKey, *infrastructurev1beta1.TartHostOperation, error) {
	operations := &infrastructurev1beta1.TartHostOperationList{}
	if err := provider.client.List(ctx, operations, client.MatchingFields{OperationIDField: operationUID}); err != nil {
		return client.ObjectKey{}, nil, fmt.Errorf("list TartHostOperations: %w", err)
	}
	switch len(operations.Items) {
	case 0:
		return client.ObjectKey{}, nil, ErrNotFound
	case 1:
		operation := operations.Items[0].DeepCopy()
		return client.ObjectKeyFromObject(operation), operation, nil
	default:
		return client.ObjectKey{}, nil, ErrAmbiguous
	}
}

func (provider *Provider) GetPlan(
	ctx context.Context,
	key client.ObjectKey,
) (agentprotocol.SignedPlan, error) {
	secret := &corev1.Secret{}
	secretKey := client.ObjectKey{
		Namespace: key.Namespace,
		Name:      key.Name + PlanSecretSuffix,
	}
	if err := provider.client.Get(ctx, secretKey, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return agentprotocol.SignedPlan{}, ErrNotFound
		}
		return agentprotocol.SignedPlan{}, fmt.Errorf("get Agent Plan Secret: %w", err)
	}
	validated, err := agentprotocol.ParsePlan(secret.Data[PlanSecretPlanKey])
	if err != nil {
		return agentprotocol.SignedPlan{}, fmt.Errorf("parse Agent Plan: %w", err)
	}
	var signature agentprotocol.Signature
	if err := decodeStrict(secret.Data[PlanSecretSignatureKey], &signature); err != nil {
		return agentprotocol.SignedPlan{}, fmt.Errorf("parse Agent Plan signature: %w", err)
	}
	return agentprotocol.SignedPlan{
		Plan:      validated.Value(),
		Signature: signature,
	}, nil
}

func (provider *Provider) GetBootstrapBundle(
	ctx context.Context,
	key client.ObjectKey,
) (agentprotocol.BootstrapBundle, error) {
	operation := &infrastructurev1beta1.TartHostOperation{}
	if err := provider.client.Get(ctx, key, operation); err != nil {
		if apierrors.IsNotFound(err) {
			return agentprotocol.BootstrapBundle{}, ErrNotFound
		}
		return agentprotocol.BootstrapBundle{}, fmt.Errorf("get TartHostOperation: %w", err)
	}
	if operation.Spec.MachineRef == nil {
		return agentprotocol.BootstrapBundle{}, ErrNotFound
	}

	machine := &infrastructurev1beta1.TartMachine{}
	machineKey := client.ObjectKey{
		Namespace: operation.Spec.MachineRef.Namespace,
		Name:      operation.Spec.MachineRef.Name,
	}
	if err := provider.client.Get(ctx, machineKey, machine); err != nil {
		if apierrors.IsNotFound(err) {
			return agentprotocol.BootstrapBundle{}, ErrNotFound
		}
		return agentprotocol.BootstrapBundle{}, fmt.Errorf("get TartMachine: %w", err)
	}
	owner, ok := capiMachineOwner(machine.OwnerReferences)
	if !ok {
		return agentprotocol.BootstrapBundle{}, ErrNotFound
	}

	bootstrapSecretName, err := provider.bootstrapSecretName(ctx, machine.Namespace, owner.Name)
	if err != nil {
		return agentprotocol.BootstrapBundle{}, err
	}
	secret := &corev1.Secret{}
	if err := provider.client.Get(ctx, client.ObjectKey{
		Namespace: machine.Namespace,
		Name:      bootstrapSecretName,
	}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return agentprotocol.BootstrapBundle{}, ErrNotFound
		}
		return agentprotocol.BootstrapBundle{}, fmt.Errorf("get Bootstrap Secret: %w", err)
	}
	payload := secret.Data["value"]
	format := string(secret.Data["format"])
	if format == "" {
		format = agentprotocol.BootstrapFormatCloud
	}
	bundle := agentprotocol.BootstrapBundle{
		APIVersion:    agentprotocol.APIVersion,
		Format:        format,
		Payload:       append([]byte(nil), payload...),
		PayloadDigest: digest.FromBytes(payload).String(),
		MachineUID:    string(owner.UID),
		OperationUID:  operation.Spec.OperationID,
	}
	if err := agentprotocol.ValidateBootstrapBundle(bundle); err != nil {
		return agentprotocol.BootstrapBundle{}, fmt.Errorf("validate Bootstrap Bundle: %w", err)
	}
	return bundle, nil
}

func (provider *Provider) bootstrapSecretName(
	ctx context.Context,
	namespace, machineName string,
) (string, error) {
	machine := &unstructured.Unstructured{}
	machine.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "cluster.x-k8s.io",
		Version: "v1beta2",
		Kind:    "Machine",
	})
	if err := provider.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: machineName}, machine); err != nil {
		if apierrors.IsNotFound(err) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("get CAPI Machine: %w", err)
	}
	name, found, err := unstructured.NestedString(machine.Object, "spec", "bootstrap", "dataSecretName")
	if err != nil {
		return "", fmt.Errorf("read CAPI Machine bootstrap Secret reference: %w", err)
	}
	if !found || name == "" {
		return "", ErrNotFound
	}
	return name, nil
}

func capiMachineOwner(references []metav1.OwnerReference) (metav1.OwnerReference, bool) {
	for _, reference := range references {
		if reference.APIVersion == "cluster.x-k8s.io/v1beta2" && reference.Kind == "Machine" {
			return reference, true
		}
	}
	return metav1.OwnerReference{}, false
}

func decodeStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing json.RawMessage
	err := decoder.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return err
	}
	return errors.New("document must contain exactly one JSON value")
}
