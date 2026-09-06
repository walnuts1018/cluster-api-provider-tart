//go:build e2e

package lab

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// diskRole is one of the three fixed disk roles a lab VM always has.
type diskRole string

const (
	diskRoleSystem diskRole = "system"
	diskRoleSSD    diskRole = "ssd"
	diskRoleHDD    diskRole = "hdd"
)

// diskSerial builds a stable-looking serial number for a disk role scoped to a VM name.
// Talosのhardware discoveryはこのserialをDiskInventory.Serialとして観測し、
// UniqueDiskSelectorがsystem/ssd/hddを区別するために利用する。
func diskSerial(role diskRole, vmName string) string {
	return fmt.Sprintf("%s-%s", role, vmName)
}

// diskWWN builds a deterministic pseudo-WWN (NAA locally-administered format) for the disk,
// derived from the VM name and role so that repeated EnsureVM calls stay idempotent.
func diskWWN(role diskRole, vmName string) string {
	sum := sha256.Sum256([]byte(vmName + "/" + string(role)))
	seed := hex.EncodeToString(sum[:])
	// NAA type 5 (locally administered) prefix。残り15 hex桁をseedから採る。
	return "5" + seed[:15]
}

// createQcow2は指定サイズ(GiB)のsparse qcow2 disk imageをqemu-imgで作成する。
// 既にファイルが存在する場合は何もしない(idempotent)。
func createQcow2(ctx context.Context, path string, sizeGiB uint64) error {
	if sizeGiB == 0 {
		return fmt.Errorf("disk size for %q must be greater than zero", path)
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create disk image directory: %w", err)
	}
	cmd := exec.CommandContext(ctx, "qemu-img", "create", "-f", "qcow2", path, fmt.Sprintf("%dG", sizeGiB))
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("qemu-img create %q: %w: %s", path, err, string(output))
	}
	return nil
}
