//go:build e2e && linux

// libvirt.org/go/libvirtはcgoでlibvirt-devへリンクするため、このファイルはlinux runner専用とする。
// darwinの開発環境ではビルドできない(GitHub Actions runner上のKVM/libvirt labでのみ使う想定のため
// 問題にならない)。darwin等での`go vet -tags e2e`検証はlab_stub.goが肩代わりする。
package lab

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
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

type networkConfig struct {
	Name        string
	Bridge      string
	BridgeSTP   string
	BridgeDelay string
	Forward     string
	IPAddress   string
	DHCPStart   string
	DHCPEnd     string
	Hosts       []networkHostConfig
}

type networkHostConfig struct {
	MAC string
	IP  string
}

type networkXML struct {
	Name    string            `xml:"name"`
	Bridge  networkBridgeXML  `xml:"bridge"`
	Forward networkForwardXML `xml:"forward"`
	IPs     []networkIPXML    `xml:"ip"`
}

type networkBridgeXML struct {
	Name  string `xml:"name,attr"`
	STP   string `xml:"stp,attr"`
	Delay string `xml:"delay,attr"`
}

type networkForwardXML struct {
	Mode string `xml:"mode,attr"`
}

type networkIPXML struct {
	Address string         `xml:"address,attr"`
	DHCP    networkDHCPXML `xml:"dhcp"`
}

type networkDHCPXML struct {
	Ranges []networkRangeXML `xml:"range"`
	Hosts  []networkHostXML  `xml:"host"`
}

type networkRangeXML struct {
	Start string `xml:"start,attr"`
	End   string `xml:"end,attr"`
}

type networkHostXML struct {
	MAC string `xml:"mac,attr"`
	IP  string `xml:"ip,attr"`
}

// libvirtが補うuuidやdefault値に左右されないよう、再利用に必要なnetwork設定だけを比較する。
func networkMatches(network *libvirt.Network, desired networkConfig, active bool) (bool, error) {
	inactiveXML, err := network.GetXMLDesc(libvirt.NETWORK_XML_INACTIVE)
	if err != nil {
		return false, err
	}
	matches, err := networkXMLMatches(inactiveXML, desired)
	if err != nil || !matches || !active {
		return matches, err
	}

	liveXML, err := network.GetXMLDesc(0)
	if err != nil {
		return false, err
	}
	return networkXMLMatches(liveXML, desired)
}

func networkXMLMatches(xmlText string, desired networkConfig) (bool, error) {
	var actual networkXML
	if err := xml.Unmarshal([]byte(xmlText), &actual); err != nil {
		return false, err
	}
	if strings.TrimSpace(actual.Name) != desired.Name || actual.Bridge.Name != desired.Bridge || actual.Bridge.STP != desired.BridgeSTP || actual.Bridge.Delay != desired.BridgeDelay || actual.Forward.Mode != desired.Forward || len(actual.IPs) != 1 {
		return false, nil
	}

	ip := actual.IPs[0]
	if normalizedIP(ip.Address) != normalizedIP(desired.IPAddress) || len(ip.DHCP.Ranges) != 1 {
		return false, nil
	}
	rangeConfig := ip.DHCP.Ranges[0]
	if normalizedIP(rangeConfig.Start) != normalizedIP(desired.DHCPStart) || normalizedIP(rangeConfig.End) != normalizedIP(desired.DHCPEnd) {
		return false, nil
	}

	expectedHosts := make(map[string]string, len(desired.Hosts))
	for _, host := range desired.Hosts {
		expectedHosts[normalizedMAC(host.MAC)] = normalizedIP(host.IP)
	}
	actualHosts := make(map[string]string, len(ip.DHCP.Hosts))
	for _, host := range ip.DHCP.Hosts {
		key := normalizedMAC(host.MAC)
		if _, exists := actualHosts[key]; exists {
			return false, nil
		}
		actualHosts[key] = normalizedIP(host.IP)
	}
	if len(actualHosts) != len(expectedHosts) {
		return false, nil
	}
	for mac, expectedIP := range expectedHosts {
		if actualHosts[mac] != expectedIP {
			return false, nil
		}
	}
	return true, nil
}

func normalizedIP(value string) string {
	value = strings.TrimSpace(value)
	if addr, err := netip.ParseAddr(value); err == nil {
		return addr.String()
	}
	return value
}

func normalizedMAC(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func freeLibvirtNetwork(network *libvirt.Network) {
	if err := network.Free(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "warning: failed to free libvirt network handle: %v\n", err)
	}
}

func (l *libvirtLab) EnsureNetwork(ctx context.Context) error {
	desired, err := l.desiredNetworkConfig()
	if err != nil {
		return err
	}

	existing, err := l.conn.LookupNetworkByName(l.cfg.NetworkName)
	switch {
	case err == nil:
		active, activeErr := existing.IsActive()
		if activeErr != nil {
			freeLibvirtNetwork(existing)
			return fmt.Errorf("check network %q active state: %w", l.cfg.NetworkName, activeErr)
		}
		persistent, persistentErr := existing.IsPersistent()
		if persistentErr != nil {
			freeLibvirtNetwork(existing)
			return fmt.Errorf("check network %q persistence: %w", l.cfg.NetworkName, persistentErr)
		}
		if !persistent {
			freeLibvirtNetwork(existing)
			return fmt.Errorf("network %q is transient and cannot be safely reconfigured", l.cfg.NetworkName)
		}

		matches, matchErr := networkMatches(existing, desired, active)
		if matchErr != nil {
			freeLibvirtNetwork(existing)
			return fmt.Errorf("validate existing network %q: %w", l.cfg.NetworkName, matchErr)
		}
		if !matches {
			if active {
				freeLibvirtNetwork(existing)
				return fmt.Errorf("existing network %q is active but does not match the requested configuration", l.cfg.NetworkName)
			}
			if undefineErr := existing.Undefine(); undefineErr != nil {
				freeLibvirtNetwork(existing)
				return fmt.Errorf("undefine stale network %q: %w", l.cfg.NetworkName, undefineErr)
			}
			freeLibvirtNetwork(existing)
		} else {
			if setErr := existing.SetAutostart(true); setErr != nil {
				freeLibvirtNetwork(existing)
				return fmt.Errorf("set network autostart: %w", setErr)
			}
			if !active {
				if createErr := existing.Create(); createErr != nil {
					freeLibvirtNetwork(existing)
					return fmt.Errorf("start libvirt network %q: %w", l.cfg.NetworkName, createErr)
				}
			}
			freeLibvirtNetwork(existing)
			return l.ensureNetworkHostConfig(ctx)
		}
	case !errors.Is(err, libvirt.ERR_NO_NETWORK):
		return fmt.Errorf("lookup libvirt network %q: %w", l.cfg.NetworkName, err)
	}

	network, err := l.conn.NetworkDefineXML(desired.XML())
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
	return l.ensureNetworkHostConfig(ctx)
}

func (l *libvirtLab) ensureNetworkHostConfig(ctx context.Context) error {
	if l.cfg.NetbootAdvertiseIP != "" {
		prefix, prefixErr := netip.ParsePrefix(l.cfg.NetworkCIDR)
		if prefixErr != nil {
			return fmt.Errorf("parse network CIDR %q: %w", l.cfg.NetworkCIDR, prefixErr)
		}
		addOutput, addErr := exec.CommandContext(ctx, "ip", "addr", "add",
			fmt.Sprintf("%s/%d", l.cfg.NetbootAdvertiseIP, prefix.Bits()),
			"dev", l.cfg.NetworkBridge).CombinedOutput()
		if addErr != nil && !strings.Contains(string(addOutput), "File exists") {
			return fmt.Errorf("add netboot advertise address %q to %q: %w: %s", l.cfg.NetbootAdvertiseIP, l.cfg.NetworkBridge, addErr, string(addOutput))
		}
	}

	// libvirtのNAT forward modeは、bridge上のguestが自発的に開始した接続(とその戻りの
	// established/related応答)のみを許可し、他のlocal interface(kind clusterのdocker
	// bridge等)からguestへの新規接続は既定でblockする。infrastructure-managerはkind cluster
	// (別のdocker bridge network)からこのbridge上のVMへ新規にTalos APIへ接続する必要がある
	// ため、明示的にFORWARDを許可する(このhostは使い捨てのCI runnerであり、他のtenantとの
	// 分離を考慮する必要はない)。
	for _, rule := range [][]string{
		{"FORWARD", "-o", l.cfg.NetworkBridge, "-j", "ACCEPT"},
		{"FORWARD", "-i", l.cfg.NetworkBridge, "-j", "ACCEPT"},
	} {
		checkArgs := append([]string{"-C"}, rule...)
		if _, err := exec.CommandContext(ctx, "iptables", checkArgs...).CombinedOutput(); err == nil {
			continue
		}
		insertArgs := append([]string{"-I"}, rule...)
		if output, err := exec.CommandContext(ctx, "iptables", insertArgs...).CombinedOutput(); err != nil {
			return fmt.Errorf("insert iptables FORWARD rule %v: %w: %s", insertArgs, err, string(output))
		}
	}
	return nil
}

func (l *libvirtLab) desiredNetworkConfig() (networkConfig, error) {
	gateway, dhcpStart, dhcpEnd, err := networkAddresses(l.cfg.NetworkCIDR)
	if err != nil {
		return networkConfig{}, err
	}

	config := networkConfig{
		Name:        l.cfg.NetworkName,
		Bridge:      l.cfg.NetworkBridge,
		BridgeSTP:   "on",
		BridgeDelay: "0",
		Forward:     "nat",
		IPAddress:   gateway,
		DHCPStart:   dhcpStart,
		DHCPEnd:     dhcpEnd,
	}
	for _, vm := range l.cfg.VMs {
		if vm.StaticIP == "" {
			continue
		}
		for _, host := range config.Hosts {
			if normalizedMAC(host.MAC) == normalizedMAC(vm.MACAddress) {
				return networkConfig{}, fmt.Errorf("duplicate DHCP reservation MAC address %q", vm.MACAddress)
			}
		}
		config.Hosts = append(config.Hosts, networkHostConfig{MAC: vm.MACAddress, IP: vm.StaticIP})
	}
	return config, nil
}

func (c networkConfig) XML() string {
	var staticHosts strings.Builder
	for _, host := range c.Hosts {
		fmt.Fprintf(&staticHosts, "      <host mac='%s' ip='%s'/>\n", host.MAC, host.IP)
	}
	return fmt.Sprintf(`<network>
  <name>%s</name>
  <bridge name='%s' stp='%s' delay='%s'/>
  <forward mode='%s'/>
  <ip address='%s'>
    <dhcp>
      <range start='%s' end='%s'/>
%s    </dhcp>
  </ip>
</network>`, c.Name, c.Bridge, c.BridgeSTP, c.BridgeDelay, c.Forward, c.IPAddress, c.DHCPStart, c.DHCPEnd, staticHosts.String())
}

type domainXML struct {
	Type       string           `xml:"type,attr"`
	Name       string           `xml:"name"`
	UUID       string           `xml:"uuid"`
	Memory     domainMemoryXML  `xml:"memory"`
	CurrentMem domainMemoryXML  `xml:"currentMemory"`
	VCPU       domainVCPUXML    `xml:"vcpu"`
	Devices    domainDevicesXML `xml:"devices"`
}

type domainMemoryXML struct {
	Unit  string `xml:"unit,attr"`
	Value string `xml:",chardata"`
}

type domainVCPUXML struct {
	Placement string `xml:"placement,attr"`
	Value     string `xml:",chardata"`
}

type domainDevicesXML struct {
	Disks      []domainDiskXML      `xml:"disk"`
	Interfaces []domainInterfaceXML `xml:"interface"`
}

type domainDiskXML struct {
	Type   string              `xml:"type,attr"`
	Device string              `xml:"device,attr"`
	Driver domainDiskDriverXML `xml:"driver"`
	Source domainDiskSourceXML `xml:"source"`
	Target domainDiskTargetXML `xml:"target"`
	Serial string              `xml:"serial"`
	WWN    string              `xml:"wwn"`
}

type domainDiskDriverXML struct {
	Name    string `xml:"name,attr"`
	Type    string `xml:"type,attr"`
	Discard string `xml:"discard,attr"`
}

type domainDiskSourceXML struct {
	File string `xml:"file,attr"`
}

type domainDiskTargetXML struct {
	Dev string `xml:"dev,attr"`
	Bus string `xml:"bus,attr"`
}

type domainInterfaceXML struct {
	Type   string                   `xml:"type,attr"`
	Source domainInterfaceSourceXML `xml:"source"`
	MAC    domainInterfaceMACXML    `xml:"mac"`
	Model  domainInterfaceModelXML  `xml:"model"`
}

type domainInterfaceSourceXML struct {
	Network string `xml:"network,attr"`
}

type domainInterfaceMACXML struct {
	Address string `xml:"address,attr"`
}

type domainInterfaceModelXML struct {
	Type string `xml:"type,attr"`
}

func (l *libvirtLab) diskPaths(spec VMSpec) (DiskPaths, error) {
	if spec.Name == "" || spec.Name == "." || spec.Name == ".." || filepath.Base(spec.Name) != spec.Name {
		return DiskPaths{}, fmt.Errorf("VM name %q cannot be used as a disk directory", spec.Name)
	}
	diskDir := filepath.Join(l.cfg.WorkDir, spec.Name)
	workDirAbs, err := filepath.Abs(l.cfg.WorkDir)
	if err != nil {
		return DiskPaths{}, fmt.Errorf("resolve lab work directory: %w", err)
	}
	diskDirAbs, err := filepath.Abs(diskDir)
	if err != nil {
		return DiskPaths{}, fmt.Errorf("resolve VM disk directory: %w", err)
	}
	rel, err := filepath.Rel(workDirAbs, diskDirAbs)
	if err != nil {
		return DiskPaths{}, fmt.Errorf("check VM disk directory: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return DiskPaths{}, fmt.Errorf("VM disk directory %q is outside lab work directory %q", diskDir, l.cfg.WorkDir)
	}
	return DiskPaths{
		System: filepath.Join(diskDir, "system.qcow2"),
		SSD:    filepath.Join(diskDir, "ssd.qcow2"),
		HDD:    filepath.Join(diskDir, "hdd.qcow2"),
	}, nil
}

func validateDiskFiles(paths DiskPaths) error {
	for _, path := range []string{paths.System, paths.SSD, paths.HDD} {
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("stat existing disk %q: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("existing disk %q is not a regular file", path)
		}
	}
	return nil
}

func removeDiskFiles(paths DiskPaths) error {
	for _, path := range []string{paths.System, paths.SSD, paths.HDD} {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("stat disk for removal %q: %w", path, err)
		}
		if info.IsDir() {
			return fmt.Errorf("refusing to remove directory as disk %q", path)
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove disk %q: %w", path, err)
		}
	}
	return nil
}

// libvirtが正規化したXMLから、domainのidentity、resource、disk、network設定を検証する。
func domainXMLMatches(xmlText string, spec VMSpec, paths DiskPaths, networkName string) (bool, error) {
	var actual domainXML
	if err := xml.Unmarshal([]byte(xmlText), &actual); err != nil {
		return false, err
	}
	if actual.Type != "kvm" || strings.TrimSpace(actual.Name) != spec.Name || !strings.EqualFold(strings.TrimSpace(actual.UUID), deterministicUUID(spec.Name)) {
		return false, nil
	}
	actualMemory, actualMemoryOK := memoryMiB(actual.Memory)
	actualCurrentMemory, actualCurrentMemoryOK := memoryMiB(actual.CurrentMem)
	if !actualMemoryOK || !actualCurrentMemoryOK || actualMemory != spec.MemoryMiB || actualCurrentMemory != spec.MemoryMiB {
		return false, nil
	}
	actualVCPUs, actualVCPUsOK := parseXMLUint(actual.VCPU.Value)
	if actual.VCPU.Placement != "static" || !actualVCPUsOK || actualVCPUs != uint64(spec.VCPUs) {
		return false, nil
	}
	expectedDisks := map[string]struct {
		path   string
		serial string
		wwn    string
	}{
		"sda": {path: paths.System, serial: diskSerial(diskRoleSystem, spec.Name), wwn: diskWWN(diskRoleSystem, spec.Name)},
		"sdb": {path: paths.SSD, serial: diskSerial(diskRoleSSD, spec.Name), wwn: diskWWN(diskRoleSSD, spec.Name)},
		"sdc": {path: paths.HDD, serial: diskSerial(diskRoleHDD, spec.Name), wwn: diskWWN(diskRoleHDD, spec.Name)},
	}
	if len(actual.Devices.Disks) != len(expectedDisks) {
		return false, nil
	}
	for _, disk := range actual.Devices.Disks {
		expected, ok := expectedDisks[disk.Target.Dev]
		if !ok || disk.Type != "file" || disk.Device != "disk" || disk.Driver.Name != "qemu" || disk.Driver.Type != "qcow2" || disk.Driver.Discard != "unmap" || disk.Source.File != expected.path || strings.TrimSpace(disk.Serial) != expected.serial || !strings.EqualFold(strings.TrimSpace(disk.WWN), expected.wwn) || disk.Target.Bus != "scsi" {
			return false, nil
		}
		delete(expectedDisks, disk.Target.Dev)
	}
	if len(expectedDisks) != 0 {
		return false, nil
	}

	if len(actual.Devices.Interfaces) != 1 {
		return false, nil
	}
	iface := actual.Devices.Interfaces[0]
	if iface.Type != "network" || iface.Source.Network != networkName || !strings.EqualFold(iface.MAC.Address, spec.MACAddress) || iface.Model.Type != "virtio" {
		return false, nil
	}
	return true, nil
}

func memoryMiB(memory domainMemoryXML) (uint64, bool) {
	value, err := strconv.ParseUint(strings.TrimSpace(memory.Value), 10, 64)
	if err != nil {
		return 0, false
	}
	switch strings.ToLower(strings.TrimSpace(memory.Unit)) {
	case "mib":
		return value, true
	case "kib":
		if value%1024 != 0 {
			return 0, false
		}
		return value / 1024, true
	case "gib":
		if value > ^uint64(0)/1024 {
			return 0, false
		}
		return value * 1024, true
	default:
		return 0, false
	}
}

func parseXMLUint(value string) (uint64, bool) {
	parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

func freeLibvirtDomain(domain *libvirt.Domain) {
	if err := domain.Free(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "warning: failed to free libvirt domain handle: %v\n", err)
	}
}

func (l *libvirtLab) EnsureVM(ctx context.Context, spec VMSpec) (DiskPaths, error) {
	paths, err := l.diskPaths(spec)
	if err != nil {
		return DiskPaths{}, err
	}
	existing, err := l.conn.LookupDomainByName(spec.Name)
	if err == nil {
		persistent, persistentErr := existing.IsPersistent()
		if persistentErr != nil {
			freeLibvirtDomain(existing)
			return DiskPaths{}, fmt.Errorf("check domain %q persistence: %w", spec.Name, persistentErr)
		}
		if !persistent {
			freeLibvirtDomain(existing)
			return DiskPaths{}, fmt.Errorf("domain %q is transient and cannot be safely reused", spec.Name)
		}
		existingXML, xmlErr := existing.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
		if xmlErr != nil {
			freeLibvirtDomain(existing)
			return DiskPaths{}, fmt.Errorf("get existing domain %q XML: %w", spec.Name, xmlErr)
		}
		matches, matchErr := domainXMLMatches(existingXML, spec, paths, l.cfg.NetworkName)
		if matchErr != nil {
			freeLibvirtDomain(existing)
			return DiskPaths{}, fmt.Errorf("validate existing domain %q XML: %w", spec.Name, matchErr)
		}
		active, activeErr := existing.IsActive()
		if activeErr != nil {
			freeLibvirtDomain(existing)
			return DiskPaths{}, fmt.Errorf("check domain %q active state: %w", spec.Name, activeErr)
		}
		if active {
			freeLibvirtDomain(existing)
			if matches {
				return DiskPaths{}, fmt.Errorf("domain %q is already active", spec.Name)
			}
			return DiskPaths{}, fmt.Errorf("existing domain %q is active but does not match the requested configuration", spec.Name)
		}
		if matches {
			if diskErr := validateDiskFiles(paths); diskErr != nil {
				freeLibvirtDomain(existing)
				return DiskPaths{}, diskErr
			}
			freeLibvirtDomain(existing)
			return paths, nil
		}
		if undefineErr := existing.Undefine(); undefineErr != nil {
			freeLibvirtDomain(existing)
			return DiskPaths{}, fmt.Errorf("undefine stale domain %q: %w", spec.Name, undefineErr)
		}
		freeLibvirtDomain(existing)
	} else if !errors.Is(err, libvirt.ERR_NO_DOMAIN) {
		return DiskPaths{}, fmt.Errorf("lookup libvirt domain %q: %w", spec.Name, err)
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

	domainXML, err := l.domainXML(spec, paths)
	if err != nil {
		return DiskPaths{}, err
	}
	domain, err := l.conn.DomainDefineXML(domainXML)
	if err != nil {
		return DiskPaths{}, fmt.Errorf("define domain %q: %w", spec.Name, err)
	}
	defer func() {
		freeLibvirtDomain(domain)
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
		SerialLogPath:    filepath.Join(filepath.Dir(paths.System), "serial-console.log"),
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
		if errors.Is(err, libvirt.ERR_NO_DOMAIN) {
			// 既に存在しない場合は停止済みとして扱う。
			return nil
		}
		return fmt.Errorf("lookup domain %q: %w", name, err)
	}
	defer freeLibvirtDomain(domain)
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
		if errors.Is(err, libvirt.ERR_NO_DOMAIN) {
			return false, nil
		}
		return false, fmt.Errorf("lookup domain %q: %w", name, err)
	}
	defer freeLibvirtDomain(domain)
	return domain.IsActive()
}

func (l *libvirtLab) DestroyAll(ctx context.Context) error {
	var cleanupErrors []error
	domainCleanupFailed := false
	for _, vm := range l.cfg.VMs {
		if err := l.PowerOff(ctx, vm.Name); err != nil {
			cleanupErrors = append(cleanupErrors, err)
			domainCleanupFailed = true
			continue
		}
		domain, err := l.conn.LookupDomainByName(vm.Name)
		if err == nil {
			if undefErr := domain.Undefine(); undefErr != nil {
				freeLibvirtDomain(domain)
				cleanupErrors = append(cleanupErrors, fmt.Errorf("undefine domain %q: %w", vm.Name, undefErr))
				domainCleanupFailed = true
				continue
			}
			freeLibvirtDomain(domain)
		} else if !errors.Is(err, libvirt.ERR_NO_DOMAIN) {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("lookup domain %q during cleanup: %w", vm.Name, err))
			domainCleanupFailed = true
			continue
		}

		paths, pathsErr := l.diskPaths(vm)
		if pathsErr != nil {
			cleanupErrors = append(cleanupErrors, pathsErr)
			continue
		}
		if err := removeDiskFiles(paths); err != nil {
			cleanupErrors = append(cleanupErrors, err)
		}
	}
	if !domainCleanupFailed {
		if network, err := l.conn.LookupNetworkByName(l.cfg.NetworkName); err == nil {
			active, activeErr := network.IsActive()
			if activeErr != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("check network %q active state during cleanup: %w", l.cfg.NetworkName, activeErr))
			} else {
				if active {
					if destroyErr := network.Destroy(); destroyErr != nil {
						cleanupErrors = append(cleanupErrors, fmt.Errorf("destroy network %q: %w", l.cfg.NetworkName, destroyErr))
					} else {
						active = false
					}
				}
				if !active {
					if undefErr := network.Undefine(); undefErr != nil {
						cleanupErrors = append(cleanupErrors, fmt.Errorf("undefine network %q: %w", l.cfg.NetworkName, undefErr))
					}
				}
			}
			freeLibvirtNetwork(network)
		} else if !errors.Is(err, libvirt.ERR_NO_NETWORK) {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("lookup network %q during cleanup: %w", l.cfg.NetworkName, err))
		}
	}
	return errors.Join(cleanupErrors...)
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
	if !prefix.Addr().Is4() {
		return "", "", "", fmt.Errorf("network CIDR %q must be IPv4", cidr)
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
