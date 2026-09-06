//go:build e2e && linux

// libvirt.org/go/libvirtはcgoでlibvirt-devへリンクするため、このファイルはlinux runner専用とする。
// darwinの開発環境ではビルドできない(GitHub Actions runner上のKVM/libvirt labでのみ使う想定のため
// 問題にならない)。darwin等での`go vet -tags e2e`検証はlab_stub.goが肩代わりする。
package lab

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"

	libvirt "libvirt.org/go/libvirt"
)

// domainTemplateTextはdomain.tmpl.xmlの内容である。go:embedはこのファイルと同じディレクトリの
// テンプレートを対象にするため、パッケージ初期化時に自身のソースファイルパスから解決して読み込む。
var domainTemplateText string

func init() {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "domain.tmpl.xml"))
	if err == nil {
		domainTemplateText = string(data)
	}
}

// libvirtLabはLabインターフェースのlinux実装である。qemu:///systemへ接続し、
// isolated networkとdomain(VM)をtext/templateで生成したXMLからDefineする。
type libvirtLab struct {
	conn *libvirt.Connect
	cfg  Config
}

// NewLibvirtLabはqemu:///system(またはcfgで指定したURI)へ接続し、Labを構築する。
func NewLibvirtLab(uri string, cfg Config) (Lab, error) {
	if uri == "" {
		uri = "qemu:///system"
	}
	conn, err := libvirt.NewConnect(uri)
	if err != nil {
		return nil, fmt.Errorf("connect to libvirt %q: %w", uri, err)
	}
	return &libvirtLab{conn: conn, cfg: cfg}, nil
}

func (l *libvirtLab) EnsureNetwork(_ context.Context) error {
	if existing, err := l.conn.LookupNetworkByName(l.cfg.NetworkName); err == nil {
		return existing.Free()
	}

	gateway, dhcpStart, dhcpEnd, err := networkAddresses(l.cfg.NetworkCIDR)
	if err != nil {
		return err
	}

	networkXML := fmt.Sprintf(`<network>
  <name>%s</name>
  <bridge name='%s' stp='on' delay='0'/>
  <forward mode='nat'/>
  <ip address='%s'>
    <dhcp>
      <range start='%s' end='%s'/>
    </dhcp>
  </ip>
</network>`, l.cfg.NetworkName, l.cfg.NetworkBridge, gateway, dhcpStart, dhcpEnd)

	network, err := l.conn.NetworkDefineXML(networkXML)
	if err != nil {
		return fmt.Errorf("define libvirt network %q: %w", l.cfg.NetworkName, err)
	}
	defer func() {
		if closeErr := network.Free(); closeErr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "warning: failed to free libvirt network handle: %v\n", closeErr)
		}
	}()
	if err := network.SetAutostart(true); err != nil {
		return fmt.Errorf("set network autostart: %w", err)
	}
	if err := network.Create(); err != nil {
		return fmt.Errorf("start libvirt network %q: %w", l.cfg.NetworkName, err)
	}
	return nil
}

func (l *libvirtLab) EnsureVM(ctx context.Context, spec VMSpec) (DiskPaths, error) {
	diskDir := filepath.Join(l.cfg.WorkDir, spec.Name)
	paths := DiskPaths{
		System: filepath.Join(diskDir, "system.qcow2"),
		SSD:    filepath.Join(diskDir, "ssd.qcow2"),
		HDD:    filepath.Join(diskDir, "hdd.qcow2"),
	}
	if err := createQcow2(ctx, paths.System, spec.SystemDiskGiB); err != nil {
		return DiskPaths{}, err
	}
	if err := createQcow2(ctx, paths.SSD, spec.SSDDiskGiB); err != nil {
		return DiskPaths{}, err
	}
	if err := createQcow2(ctx, paths.HDD, spec.HDDDiskGiB); err != nil {
		return DiskPaths{}, err
	}

	if existing, err := l.conn.LookupDomainByName(spec.Name); err == nil {
		return paths, existing.Free()
	}

	domainXML, err := l.domainXML(spec, paths)
	if err != nil {
		return DiskPaths{}, err
	}
	domain, err := l.conn.DomainDefineXML(domainXML)
	if err != nil {
		return DiskPaths{}, fmt.Errorf("define domain %q: %w", spec.Name, err)
	}
	defer func() {
		if closeErr := domain.Free(); closeErr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "warning: failed to free libvirt domain handle: %v\n", closeErr)
		}
	}()
	return paths, nil
}

func (l *libvirtLab) domainXML(spec VMSpec, paths DiskPaths) (string, error) {
	if domainTemplateText == "" {
		return "", fmt.Errorf("domain.tmpl.xml could not be loaded")
	}
	tmpl, err := template.New("domain").Parse(domainTemplateText)
	if err != nil {
		return "", fmt.Errorf("parse domain template: %w", err)
	}

	data := struct {
		Name             string
		UUID             string
		VCPUs            uint
		MemoryMiB        uint64
		MACAddress       string
		NetworkName      string
		SystemDiskPath   string
		SystemDiskSerial string
		SystemDiskWWN    string
		SSDDiskPath      string
		SSDDiskSerial    string
		SSDDiskWWN       string
		HDDDiskPath      string
		HDDDiskSerial    string
		HDDDiskWWN       string
		SerialLogPath    string
	}{
		Name:             spec.Name,
		UUID:             deterministicUUID(spec.Name),
		VCPUs:            spec.VCPUs,
		MemoryMiB:        spec.MemoryMiB,
		MACAddress:       spec.MACAddress,
		NetworkName:      l.cfg.NetworkName,
		SystemDiskPath:   paths.System,
		SystemDiskSerial: diskSerial(diskRoleSystem, spec.Name),
		SystemDiskWWN:    diskWWN(diskRoleSystem, spec.Name),
		SSDDiskPath:      paths.SSD,
		SSDDiskSerial:    diskSerial(diskRoleSSD, spec.Name),
		SSDDiskWWN:       diskWWN(diskRoleSSD, spec.Name),
		HDDDiskPath:      paths.HDD,
		HDDDiskSerial:    diskSerial(diskRoleHDD, spec.Name),
		HDDDiskWWN:       diskWWN(diskRoleHDD, spec.Name),
		SerialLogPath:    filepath.Join(l.cfg.WorkDir, spec.Name, "serial-console.log"),
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute domain template: %w", err)
	}
	return buf.String(), nil
}

func (l *libvirtLab) PowerOff(_ context.Context, name string) error {
	domain, err := l.conn.LookupDomainByName(name)
	if err != nil {
		// 既に存在しない場合は停止済みとして扱う。
		return nil
	}
	defer func() {
		if closeErr := domain.Free(); closeErr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "warning: failed to free libvirt domain handle: %v\n", closeErr)
		}
	}()
	active, err := domain.IsActive()
	if err != nil {
		return fmt.Errorf("check domain %q active state: %w", name, err)
	}
	if !active {
		return nil
	}
	if err := domain.Destroy(); err != nil {
		return fmt.Errorf("destroy domain %q: %w", name, err)
	}
	return nil
}

func (l *libvirtLab) IsRunning(_ context.Context, name string) (bool, error) {
	domain, err := l.conn.LookupDomainByName(name)
	if err != nil {
		return false, nil
	}
	defer func() {
		if closeErr := domain.Free(); closeErr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "warning: failed to free libvirt domain handle: %v\n", closeErr)
		}
	}()
	return domain.IsActive()
}

func (l *libvirtLab) DestroyAll(ctx context.Context) error {
	for _, vm := range l.cfg.VMs {
		if err := l.PowerOff(ctx, vm.Name); err != nil {
			return err
		}
		domain, err := l.conn.LookupDomainByName(vm.Name)
		if err == nil {
			if undefErr := domain.Undefine(); undefErr != nil {
				return fmt.Errorf("undefine domain %q: %w", vm.Name, undefErr)
			}
			if closeErr := domain.Free(); closeErr != nil {
				_, _ = fmt.Fprintf(os.Stderr, "warning: failed to free libvirt domain handle: %v\n", closeErr)
			}
		}
	}
	if network, err := l.conn.LookupNetworkByName(l.cfg.NetworkName); err == nil {
		if destroyErr := network.Destroy(); destroyErr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "warning: failed to destroy libvirt network: %v\n", destroyErr)
		}
		if undefErr := network.Undefine(); undefErr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "warning: failed to undefine libvirt network: %v\n", undefErr)
		}
		if closeErr := network.Free(); closeErr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "warning: failed to free libvirt network handle: %v\n", closeErr)
		}
	}
	return nil
}

func (l *libvirtLab) Close() error {
	if _, err := l.conn.Close(); err != nil {
		return fmt.Errorf("close libvirt connection: %w", err)
	}
	return nil
}

// deterministicUUIDはVM名から決定論的なUUIDv4相当の文字列を導出する。再実行してもdomain定義が
// 安定するようにする(乱数UUIDだと再実行のたびにdomainが再定義され、identityの比較が難しくなる)。
func deterministicUUID(name string) string {
	sum := sha256.Sum256([]byte("tart-e2e-domain/" + name))
	hexStr := hex.EncodeToString(sum[:16])
	return fmt.Sprintf("%s-%s-%s-%s-%s", hexStr[0:8], hexStr[8:12], hexStr[12:16], hexStr[16:20], hexStr[20:32])
}

// networkAddressesは、CIDR(例: RFC 5737 TEST-NET-1の192.0.2.0/24)からlibvirt networkの
// gateway address(先頭+1)とDHCP range(先頭+10〜先頭+199相当)を導出する。
func networkAddresses(cidr string) (gateway, dhcpStart, dhcpEnd string, err error) {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return "", "", "", fmt.Errorf("parse network CIDR %q: %w", cidr, err)
	}
	base := prefix.Masked().Addr()
	gatewayAddr := addOffset(base, 1)
	startAddr := addOffset(base, 10)
	endAddr := addOffset(base, 199)
	return gatewayAddr.String(), startAddr.String(), endAddr.String(), nil
}

func addOffset(addr netip.Addr, offset int) netip.Addr {
	bytesAddr := addr.As4()
	value := uint32(bytesAddr[0])<<24 | uint32(bytesAddr[1])<<16 | uint32(bytesAddr[2])<<8 | uint32(bytesAddr[3])
	value += uint32(offset) //nolint:gosec // offsetは本ファイル内の固定小定数のみ
	next := [4]byte{byte(value >> 24), byte(value >> 16), byte(value >> 8), byte(value)}
	return netip.AddrFrom4(next)
}
