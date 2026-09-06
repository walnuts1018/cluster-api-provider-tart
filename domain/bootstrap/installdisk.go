package bootstrap

// InstallDiskはTalosのsystem disk選択に必要な非機密hardware observationである。
// DevicePathは旧Talos設定形式で使用し、現行設定形式では生成したstable selectorを使用する。
// このstructはbyte列やhardware観測値のみで構成され、siderolabs machinery型を持たない。
type InstallDisk struct {
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
