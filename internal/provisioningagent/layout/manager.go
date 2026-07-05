package layout

import (
	"context"
	"fmt"

	"github.com/walnuts1018/cluster-api-provider-tart/internal/provisioningagent/disk"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

type DiskIO interface {
	LogicalSectorSize(context.Context, string) (int64, error)
	CreatePartitionTable(context.Context, string, PlannedLayout) error
	ReadPartitionTable(context.Context, string) (ObservedLayout, error)
}

type Manager struct {
	diskIO DiskIO
}

func NewManager(diskIO DiskIO) *Manager {
	return &Manager{diskIO: diskIO}
}

// PrepareはProvision時だけGPTを作成し、Update時は既存GPTの検証とRole解決だけを行う。
func (manager *Manager) Prepare(
	ctx context.Context,
	operation agentprotocol.OperationType,
	device disk.Device,
) (map[agentprotocol.DiskRole]RoleDevice, error) {
	switch operation {
	case agentprotocol.OperationTypeProvision:
		sectorSize, err := manager.diskIO.LogicalSectorSize(ctx, device.Path)
		if err != nil {
			return nil, fmt.Errorf("read logical sector size: %w", err)
		}
		planned, err := Plan(device.SizeBytes, sectorSize)
		if err != nil {
			return nil, err
		}
		if err := manager.diskIO.CreatePartitionTable(ctx, device.Path, planned); err != nil {
			return nil, fmt.Errorf("create partition table: %w", err)
		}
	case agentprotocol.OperationTypeUpdate:
		// 更新ではpartition tableへ副作用を起こさず、既存Roleの一致だけを確認する。
	default:
		return nil, fmt.Errorf("unsupported operation type: %q", operation)
	}

	observed, err := manager.diskIO.ReadPartitionTable(ctx, device.Path)
	if err != nil {
		return nil, fmt.Errorf("read partition table: %w", err)
	}
	resolved, err := Resolve(observed)
	if err != nil {
		return nil, err
	}
	return resolved, nil
}
