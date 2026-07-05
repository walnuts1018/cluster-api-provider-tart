// Package initialprovisioning はv1beta1 TartMachineの初期Provisioningを
// オーケストレーションするApplication Use Caseを提供する。
package initialprovisioning

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	jsoncanonicalizer "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
	"github.com/opencontainers/go-digest"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	agentsessiondomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/agentsession"
	allocationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/allocation"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/domain/capability"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
)

var (
	// ErrNoAvailableHost は条件に合うHostが存在しない場合に返す。
	ErrNoAvailableHost = errors.New("no available TartHost matches requirements")
	// ErrBootstrapNotReady はBootstrap Secretがまだ用意されていない場合に返す。
	ErrBootstrapNotReady = errors.New("bootstrap secret is not yet available")
)

// defaultOperationDeadline はProvision Operationのデフォルトのタイムアウト期間。
// 大容量ディスクへの書き込みに十分な時間を確保する。
const defaultOperationDeadline = 2 * time.Hour

// HostReserveService はTartHostを予約するサービスインターフェース。
type HostReserveService interface {
	Reserve(
		ctx context.Context,
		machine *infrastructurev1beta1.TartMachine,
		requirements allocationdomain.Requirements,
	) (*infrastructurev1beta1.TartHost, error)
}

// HostPhaseService はTartHostのPhaseを更新するサービスインターフェース。
type HostPhaseService interface {
	ReserveForMachine(ctx context.Context, host *infrastructurev1beta1.TartHost, machine *infrastructurev1beta1.TartMachine) error
	MarkHostProvisioned(ctx context.Context, host *infrastructurev1beta1.TartHost) error
}

// OperationService はTartHostOperationを作成・管理するサービスインターフェース。
type OperationService interface {
	Start(ctx context.Context, desired *infrastructurev1beta1.TartHostOperation) (*infrastructurev1beta1.TartHostOperation, error)
	CompleteProvision(ctx context.Context, operation *infrastructurev1beta1.TartHostOperation) error
}

// CompleteProvisioning はOperationとHostを最終状態へ順に収束させる。
// Operationを先に完了させ、再試行時はSucceededを冪等に受け入れる。
func (o *Orchestrator) CompleteProvisioning(
	ctx context.Context,
	host *infrastructurev1beta1.TartHost,
	operation *infrastructurev1beta1.TartHostOperation,
) error {
	if err := o.operations.CompleteProvision(ctx, operation); err != nil {
		return fmt.Errorf("complete Provision operation: %w", err)
	}
	if err := o.hostPhase.MarkHostProvisioned(ctx, host); err != nil {
		return fmt.Errorf("mark TartHost provisioned: %w", err)
	}
	return nil
}

// SessionTokenIssuer はOperation用のSession Tokenを発行するインターフェース。
type SessionTokenIssuer interface {
	Issue(ctx context.Context, key client.ObjectKey, hostUID, operationUID string, now time.Time) (agentsessiondomain.Token, time.Time, error)
}

// Orchestrator はv1beta1 TartMachineの初期Provisioningを組み立てる。
type Orchestrator struct {
	hostReserve HostReserveService
	hostPhase   HostPhaseService
	operations  OperationService
}

// NewOrchestrator はOrchestratorを生成する。
func NewOrchestrator(
	hostReserve HostReserveService,
	hostPhase HostPhaseService,
	operations OperationService,
) *Orchestrator {
	return &Orchestrator{
		hostReserve: hostReserve,
		hostPhase:   hostPhase,
		operations:  operations,
	}
}

// ReserveAndStartOperation はHostを選択・予約してProvision Operationを作成する。
//
// 冪等性: 同じmachine/hostのOperationが既に存在する場合は既存のOperationを返す。
// 呼び出し元はHostRef/OperationRefをStatus Patchで永続化する責務を持つ。
func (o *Orchestrator) ReserveAndStartOperation(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	planDigest string,
) (*infrastructurev1beta1.TartHost, *infrastructurev1beta1.TartHostOperation, error) {
	requirements, err := requirementsForMachine(machine)
	if err != nil {
		return nil, nil, fmt.Errorf("build allocation requirements: %w", err)
	}

	host, err := o.hostReserve.Reserve(ctx, machine, requirements)
	if err != nil {
		if errors.Is(err, allocationdomain.ErrNoMatchingHost) {
			return nil, nil, ErrNoAvailableHost
		}
		return nil, nil, fmt.Errorf("reserve TartHost: %w", err)
	}
	if host == nil {
		return nil, nil, ErrNoAvailableHost
	}

	// Host StatusをReservedに更新する
	if err := o.hostPhase.ReserveForMachine(ctx, host, machine); err != nil {
		return nil, nil, fmt.Errorf("mark TartHost reserved: %w", err)
	}

	// Operation IDはHostUID/MachineUIDから決定論的に生成する
	// 再起動後も同じMachine/Hostに対して同一のUIDが生成されることを保証する
	operationUID, err := deterministicOperationUID(host, machine)
	if err != nil {
		return nil, nil, err
	}

	deadline := metav1.NewTime(time.Now().Add(defaultOperationDeadline))
	desiredMachine := machine.DeepCopy()
	expectedProviderID := fmt.Sprintf("tart://%s", host.Name)
	if desiredMachine.Spec.ProviderID != "" && desiredMachine.Spec.ProviderID != expectedProviderID {
		return nil, nil, fmt.Errorf(
			"TartMachine providerID %q does not match reserved TartHost %q",
			desiredMachine.Spec.ProviderID,
			host.Name,
		)
	}
	desiredMachine.Spec.ProviderID = expectedProviderID
	objectsDigest, err := desiredObjectsDigest(desiredMachine)
	if err != nil {
		return nil, nil, fmt.Errorf("build desired objects digest: %w", err)
	}
	desired := &infrastructurev1beta1.TartHostOperation{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: machine.Namespace,
		},
		Spec: infrastructurev1beta1.TartHostOperationSpec{
			OperationID: operationUID,
			Type:        infrastructurev1beta1.OperationTypeProvision,
			HostRef: infrastructurev1beta1.ResourceReference{
				Namespace: host.Namespace,
				Name:      host.Name,
				UID:       host.UID,
			},
			MachineRef: &infrastructurev1beta1.ResourceReference{
				Namespace: machine.Namespace,
				Name:      machine.Name,
				UID:       machine.UID,
			},
			PlanDigest:           planDigest,
			DesiredObjectsDigest: objectsDigest,
			Deadline:             deadline,
		},
	}

	operation, err := o.operations.Start(ctx, desired)
	if err != nil {
		return nil, nil, fmt.Errorf("start TartHostOperation: %w", err)
	}

	return host, operation, nil
}

// requirementsForMachine はTartMachineからAllocation Requirementsを構築する。
func requirementsForMachine(machine *infrastructurev1beta1.TartMachine) (allocationdomain.Requirements, error) {
	architecture, firmware, err := parsePlatformProfile(machine.Spec.PlatformProfile)
	if err != nil {
		return allocationdomain.Requirements{}, err
	}

	matchLabels := machine.Spec.HostSelector.MatchLabels
	const minRootDiskBytes int64 = 64 * 1024 * 1024 * 1024
	return allocationdomain.NewRequirements(
		architecture,
		firmware,
		machine.Spec.PlatformProfile,
		minRootDiskBytes,
		[]capability.Capability{capability.PowerOn},
		matchLabels,
	)
}

// parsePlatformProfile はPlatformProfile IDからarchitectureとfirmwareを解析する。
// 例: "amd64-uefi-ab/v1" → ("amd64", "UEFI")
func parsePlatformProfile(profile string) (architecture, firmware string, err error) {
	switch profile {
	case "amd64-uefi-ab/v1":
		return "amd64", "UEFI", nil
	default:
		return "", "", fmt.Errorf("unsupported platform profile %q", profile)
	}
}

// deterministicOperationUID はHostUID/MachineUIDから決定論的なOperation UIDを生成する。
// 再起動後も同じMachine/Hostの組み合わせに対して同一のUIDが生成されることを保証する。
func deterministicOperationUID(host *infrastructurev1beta1.TartHost, machine *infrastructurev1beta1.TartMachine) (string, error) {
	key := string(host.UID) + "/" + string(machine.UID)
	id, err := operationdomain.DeterministicID(key)
	if err != nil {
		return "", fmt.Errorf("generate deterministic operation ID: %w", err)
	}
	return id.String(), nil
}

// desiredObjectsDigest は初期Provisioning入力をRFC 8785 Canonical JSONで固定する。
// CAPI MachineとBootstrap SecretはPlan生成時に別途追加する。
func desiredObjectsDigest(machine *infrastructurev1beta1.TartMachine) (string, error) {
	input := struct {
		MachineUID string                                `json:"machineUID"`
		Spec       infrastructurev1beta1.TartMachineSpec `json:"spec"`
	}{
		MachineUID: string(machine.UID),
		Spec:       machine.Spec,
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("marshal TartMachine desired state: %w", err)
	}
	canonical, err := jsoncanonicalizer.Transform(encoded)
	if err != nil {
		return "", fmt.Errorf("canonicalize TartMachine desired state: %w", err)
	}
	return digest.FromBytes(canonical).String(), nil
}
