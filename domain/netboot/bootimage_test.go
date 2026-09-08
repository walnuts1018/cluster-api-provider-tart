package netboot

import "testing"

func TestDiscoveryImageIsZero(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		image DiscoveryImage
		want  bool
	}{
		"未設定":         {want: true},
		"versionのみ":   {image: DiscoveryImage{Version: "v1.14.0"}, want: true},
		"schematicのみ": {image: DiscoveryImage{SchematicID: "schematic-a"}, want: true},
		"空白を含む未設定":    {image: DiscoveryImage{Version: "  ", SchematicID: "schematic-a"}, want: true},
		"設定済み":        {image: DiscoveryImage{Version: "v1.14.0", SchematicID: "schematic-a"}},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := tt.image.IsZero(); got != tt.want {
				t.Errorf("DiscoveryImage.IsZero() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestDecideAgentBootFile(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		arch              Arch
		archOptionPresent bool
		isIPXE            bool
		baseURL           string
		macAddress        string
		wantBootFile      string
		wantSupported     bool
	}{
		"architecture optionなし": {
			arch: ArchEFIx8664, baseURL: "http://test.walnuts.dev:8080", macAddress: "00%3A00%3A5e%3A00%3A53%3A02",
		},
		"EFI ARM64": {arch: ArchEFIARM64, archOptionPresent: true},
		"EFI BC":    {arch: ArchEFIBC, archOptionPresent: true},
		"amd64の初回request": {
			arch: ArchEFIx8664, archOptionPresent: true,
			wantBootFile: IPXEBootFileNameAMD64, wantSupported: true,
		},
		"legacy BIOSの初回request": {
			arch: ArchIntelx86PC, archOptionPresent: true,
			wantBootFile: IPXEBootFileNameLegacyBIOS, wantSupported: true,
		},
		"iPXEのchain": {
			arch: ArchEFIx8664, archOptionPresent: true, isIPXE: true,
			baseURL: "http://test.walnuts.dev:8080", macAddress: "00%3A00%3A5e%3A00%3A53%3A02",
			wantBootFile: "http://test.walnuts.dev:8080/ipxe?mac=00%3A00%3A5e%3A00%3A53%3A02", wantSupported: true,
		},
		"legacy BIOSのiPXE chain": {
			arch: ArchIntelx86PC, archOptionPresent: true, isIPXE: true,
			baseURL: "http://test.walnuts.dev:8080", macAddress: "00%3A00%3A5e%3A00%3A53%3A02",
			wantBootFile: "http://test.walnuts.dev:8080/ipxe?mac=00%3A00%3A5e%3A00%3A53%3A02", wantSupported: true,
		},
		"iPXEの空入力": {
			arch: ArchEFIx8664, archOptionPresent: true, isIPXE: true,
			wantBootFile: "/ipxe?mac=", wantSupported: true,
		},
		"EFI ARM64のiPXE chainは未対応": {arch: ArchEFIARM64, archOptionPresent: true, isIPXE: true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			gotBootFile, gotSupported := DecideAgentBootFile(tt.arch, tt.archOptionPresent, tt.isIPXE, tt.baseURL, tt.macAddress)
			if gotBootFile != tt.wantBootFile || gotSupported != tt.wantSupported {
				t.Errorf("DecideAgentBootFile() = (%q, %t), want (%q, %t)", gotBootFile, gotSupported, tt.wantBootFile, tt.wantSupported)
			}
		})
	}
}

func TestPXEArchFromQuery(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"":        "amd64",
		"amd64":   "amd64",
		"ARM64":   "arm64",
		"riscv64": "amd64",
	}
	for input, want := range tests {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			if got := PXEArchFromQuery(input); got != want {
				t.Errorf("PXEArchFromQuery(%q) = %q, want %q", input, got, want)
			}
		})
	}
}
