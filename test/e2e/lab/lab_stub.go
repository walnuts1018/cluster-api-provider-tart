//go:build e2e && !linux

// libvirt.org/go/libvirtはlinux上のlibvirt-devへのcgoリンクを要求するため、linux以外では
// ビルドできない。このstubはdarwin開発環境などでも`go vet -tags e2e ./test/e2e/...`が
// 通るようにするための代替実装であり、実際のE2E実行(GitHub Actions linux runner)では
// libvirtlab.goのNewLibvirtLabが使われる。
package lab

import (
	"context"
	"errors"
)

// ErrLabUnsupportedOnPlatformは、libvirt bindingがビルドできないplatformでLabを構築しようとした
// ことを示す。
var ErrLabUnsupportedOnPlatform = errors.New("bare-metal lab requires libvirt on a linux runner")

type unsupportedLab struct{}

// NewLibvirtLabはこのplatformでは常にErrLabUnsupportedOnPlatformを返す。
func NewLibvirtLab(_ string, _ Config) (Lab, error) {
	return unsupportedLab{}, ErrLabUnsupportedOnPlatform
}

func (unsupportedLab) EnsureNetwork(context.Context) error { return ErrLabUnsupportedOnPlatform }
func (unsupportedLab) EnsureVM(context.Context, VMSpec) (DiskPaths, error) {
	return DiskPaths{}, ErrLabUnsupportedOnPlatform
}
func (unsupportedLab) PowerOff(context.Context, string) error { return ErrLabUnsupportedOnPlatform }
func (unsupportedLab) IsRunning(context.Context, string) (bool, error) {
	return false, ErrLabUnsupportedOnPlatform
}
func (unsupportedLab) DestroyAll(context.Context) error { return ErrLabUnsupportedOnPlatform }
func (unsupportedLab) Close() error                     { return nil }
