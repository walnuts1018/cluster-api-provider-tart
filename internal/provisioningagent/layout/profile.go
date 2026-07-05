package layout

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

const (
	ProfileID            = "amd64-uefi-ab/v1"
	MinimumDiskSizeBytes = int64(64 << 30)
	alignmentBytes       = int64(1 << 20)
	gptGuardBytes        = int64(1 << 20)
)

const (
	efiSystemPartitionType = "c12a7328-f81f-11d2-ba4b-00a0c93ec93b"
	linuxRootAMD64Type     = "4f68bce3-e8cd-4db1-96e7-fbcaf984b709"
	linuxRootVerityType    = "2c7357ed-ebd2-46d9-aec1-23d437ec2bf5"
	linuxFilesystemType    = "0fc63daf-8483-4772-8e79-3d69d8477de4"
)

var ErrInvalidLayout = errors.New("invalid amd64-uefi-ab/v1 disk layout")

type PartitionDefinition struct {
	Role             agentprotocol.DiskRole
	Label            string
	TypeGUID         string
	MinimumSizeBytes int64
	Grow             bool
}

type PlannedPartition struct {
	PartitionDefinition
	Number      int
	StartSector int64
	SizeSectors int64
}

type PlannedLayout struct {
	ProfileID  string
	SectorSize int64
	Partitions []PlannedPartition
	TotalBytes int64
}

type ObservedPartition struct {
	DevicePath  string
	Label       string
	TypeGUID    string
	PARTUUID    string
	StartSector int64
	SizeSectors int64
}

type ObservedLayout struct {
	TableType  string
	SectorSize int64
	Partitions []ObservedPartition
}

type RoleDevice struct {
	Role       agentprotocol.DiskRole
	DevicePath string
	PARTUUID   string
	StartByte  int64
	SizeBytes  int64
}

func Definitions() []PartitionDefinition {
	return slices.Clone(profileDefinitions)
}

var profileDefinitions = []PartitionDefinition{
	{Role: agentprotocol.DiskRoleBoot, Label: "tart-boot", TypeGUID: efiSystemPartitionType, MinimumSizeBytes: 512 << 20},
	{Role: agentprotocol.DiskRoleOSA, Label: "tart-os-a", TypeGUID: linuxRootAMD64Type, MinimumSizeBytes: 8 << 30},
	{Role: agentprotocol.DiskRoleVerityA, Label: "tart-verity-a", TypeGUID: linuxRootVerityType, MinimumSizeBytes: 1 << 30},
	{Role: agentprotocol.DiskRoleOSB, Label: "tart-os-b", TypeGUID: linuxRootAMD64Type, MinimumSizeBytes: 8 << 30},
	{Role: agentprotocol.DiskRoleVerityB, Label: "tart-verity-b", TypeGUID: linuxRootVerityType, MinimumSizeBytes: 1 << 30},
	{Role: agentprotocol.DiskRoleState, Label: "tart-state", TypeGUID: linuxFilesystemType, MinimumSizeBytes: 8 << 30},
	{Role: agentprotocol.DiskRoleData, Label: "tart-data", TypeGUID: linuxFilesystemType, MinimumSizeBytes: 16 << 30, Grow: true},
}

func Plan(diskSizeBytes, sectorSize int64) (PlannedLayout, error) {
	if diskSizeBytes < MinimumDiskSizeBytes {
		return PlannedLayout{}, fmt.Errorf("%w: disk has %d bytes, profile requires at least %d", ErrInvalidLayout, diskSizeBytes, MinimumDiskSizeBytes)
	}
	if sectorSize != 512 && sectorSize != 4096 {
		return PlannedLayout{}, fmt.Errorf("%w: unsupported logical sector size %d", ErrInvalidLayout, sectorSize)
	}

	alignmentSectors := alignmentBytes / sectorSize
	usableEndSector := alignDown((diskSizeBytes-gptGuardBytes)/sectorSize, alignmentSectors)
	nextSector := alignmentSectors
	partitions := make([]PlannedPartition, 0, len(profileDefinitions))
	for index, definition := range profileDefinitions {
		sizeSectors := definition.MinimumSizeBytes / sectorSize
		if definition.Grow {
			sizeSectors = usableEndSector - nextSector
		}
		if sizeSectors < definition.MinimumSizeBytes/sectorSize {
			return PlannedLayout{}, fmt.Errorf("%w: %s has only %d sectors", ErrInvalidLayout, definition.Role, sizeSectors)
		}
		partitions = append(partitions, PlannedPartition{
			PartitionDefinition: definition,
			Number:              index + 1,
			StartSector:         nextSector,
			SizeSectors:         sizeSectors,
		})
		nextSector += sizeSectors
	}

	return PlannedLayout{
		ProfileID:  ProfileID,
		SectorSize: sectorSize,
		Partitions: partitions,
		TotalBytes: diskSizeBytes,
	}, nil
}

func Resolve(observed ObservedLayout) (map[agentprotocol.DiskRole]RoleDevice, error) {
	if !strings.EqualFold(observed.TableType, "gpt") {
		return nil, fmt.Errorf("%w: partition table is %q, want GPT", ErrInvalidLayout, observed.TableType)
	}
	if observed.SectorSize <= 0 {
		return nil, fmt.Errorf("%w: sector size must be greater than zero", ErrInvalidLayout)
	}
	if len(observed.Partitions) != len(profileDefinitions) {
		return nil, fmt.Errorf("%w: found %d partitions, want %d", ErrInvalidLayout, len(observed.Partitions), len(profileDefinitions))
	}

	byLabel := make(map[string]ObservedPartition, len(observed.Partitions))
	devicePaths := make(map[string]struct{}, len(observed.Partitions))
	partUUIDs := make(map[string]struct{}, len(observed.Partitions))
	for _, partition := range observed.Partitions {
		if _, exists := byLabel[partition.Label]; exists {
			return nil, fmt.Errorf("%w: duplicate partition label %q", ErrInvalidLayout, partition.Label)
		}
		if partition.DevicePath == "" || partition.PARTUUID == "" {
			return nil, fmt.Errorf("%w: partition %q is missing device path or PARTUUID", ErrInvalidLayout, partition.Label)
		}
		if _, exists := devicePaths[partition.DevicePath]; exists {
			return nil, fmt.Errorf("%w: duplicate device path %q", ErrInvalidLayout, partition.DevicePath)
		}
		partUUID := strings.ToLower(partition.PARTUUID)
		if _, exists := partUUIDs[partUUID]; exists {
			return nil, fmt.Errorf("%w: duplicate PARTUUID %q", ErrInvalidLayout, partition.PARTUUID)
		}
		if partition.StartSector <= 0 || partition.SizeSectors <= 0 ||
			partition.StartSector > math.MaxInt64/observed.SectorSize ||
			partition.SizeSectors > math.MaxInt64/observed.SectorSize {
			return nil, fmt.Errorf("%w: partition %q has invalid geometry", ErrInvalidLayout, partition.Label)
		}
		if partition.StartSector*observed.SectorSize%alignmentBytes != 0 {
			return nil, fmt.Errorf("%w: partition %q is not 1 MiB aligned", ErrInvalidLayout, partition.Label)
		}
		byLabel[partition.Label] = partition
		devicePaths[partition.DevicePath] = struct{}{}
		partUUIDs[partUUID] = struct{}{}
	}

	resolved := make(map[agentprotocol.DiskRole]RoleDevice, len(profileDefinitions))
	var previousEnd int64
	for _, definition := range profileDefinitions {
		partition, ok := byLabel[definition.Label]
		if !ok {
			return nil, fmt.Errorf("%w: partition label %q is missing", ErrInvalidLayout, definition.Label)
		}
		if !strings.EqualFold(partition.TypeGUID, definition.TypeGUID) {
			return nil, fmt.Errorf("%w: %s has type GUID %q, want %q", ErrInvalidLayout, definition.Role, partition.TypeGUID, definition.TypeGUID)
		}
		if partition.StartSector < previousEnd {
			return nil, fmt.Errorf("%w: %s overlaps or is out of physical order", ErrInvalidLayout, definition.Role)
		}
		sizeBytes := partition.SizeSectors * observed.SectorSize
		if sizeBytes < definition.MinimumSizeBytes {
			return nil, fmt.Errorf("%w: %s has %d bytes, want at least %d", ErrInvalidLayout, definition.Role, sizeBytes, definition.MinimumSizeBytes)
		}
		if !definition.Grow && sizeBytes != definition.MinimumSizeBytes {
			return nil, fmt.Errorf("%w: %s has %d bytes, want %d", ErrInvalidLayout, definition.Role, sizeBytes, definition.MinimumSizeBytes)
		}
		resolved[definition.Role] = RoleDevice{
			Role:       definition.Role,
			DevicePath: partition.DevicePath,
			PARTUUID:   partition.PARTUUID,
			StartByte:  partition.StartSector * observed.SectorSize,
			SizeBytes:  sizeBytes,
		}
		previousEnd = partition.StartSector + partition.SizeSectors
	}
	return resolved, nil
}

func alignDown(value, alignment int64) int64 {
	return value / alignment * alignment
}
