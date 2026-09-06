package bootstrap

// DiskIdentityは、stableなTalos CEL disk selectorを構築するために必要な非機密hardware observationである。
// install target(UnattendedInstallConfig)だけでなく、VolumeConfig、UserVolumeConfig、RawVolumeConfig、
// LVMVolumeGroupConfigなど、physical diskをbindする全てのTalos storage documentで共通に使う。
// DevicePathは旧Talos設定形式で使用し、現行設定形式では生成したstable selectorを使用する。
// このstructはbyte列やhardware観測値のみで構成され、siderolabs machinery型を持たない。
type DiskIdentity struct {
	DevicePath string
	SizeBytes  uint64
	Model      string
	Serial     string
	WWID       string
	BusPath    string
	Transport  string
	Rotational bool
	ReadOnly   bool
}
